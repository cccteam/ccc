// Package firestore is the Firestore implementation of the live service: the
// subscription record in one server-owned collection, the change sets under each
// user's document, and the browser's identity as a Firebase custom token. Against the
// Firestore emulator (EmulatorHost set) the service connects as the emulator's owner,
// mints no token and revokes nothing; the browser then connects with the emulator's
// mock user token.
//
// The layout, the indexes it needs and the time-to-live policies it relies on are in
// the README beside this file, with firestore.rules and firestore.indexes.json.
package firestore

import (
	"context"
	"time"

	cloudfirestore "cloud.google.com/go/firestore"
	firebase "firebase.google.com/go/v4"
	fbauth "firebase.google.com/go/v4/auth"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/live"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// The layout.
const (
	// DefaultDatabase is the database id a Config without one names.
	DefaultDatabase = "(default)"
	// SubscriptionsCollection is the server-owned record: one document per subscription,
	// id live.Subscription.ID, no client access.
	SubscriptionsCollection = "subscriptions"
	// UsersCollection and ChangesCollection are the change sets: users/{uid}/changes/{id},
	// which a user reads for their own uid and nobody writes but the server.
	UsersCollection   = "users"
	ChangesCollection = "changes"
)

// Config says which database the service uses and how the browser reaches it.
type Config struct {
	// ProjectID is the Google Cloud project the database belongs to. Against the
	// emulator any id serves, and the browser connects with the same one.
	ProjectID string
	// DatabaseID is the Firestore database, by id (APP_FIRESTORE_DATABASE as bedrock
	// hands it to an application); empty is the default database.
	DatabaseID string
	// APIKey is the Firebase web API key the browser initializes the SDK with in
	// production; optional, and unused against the emulator.
	APIKey string
	// EmulatorHost is the Firestore emulator's host:port (FIRESTORE_EMULATOR_HOST).
	// Set, the service talks to the emulator with its owner credential, answers the
	// token route with the host and no token, and revokes nothing.
	EmulatorHost string
}

// database returns the database id, the default when none is configured.
func (c Config) database() string {
	if c.DatabaseID == "" {
		return DefaultDatabase
	}

	return c.DatabaseID
}

// Service is the live service over one Firestore database.
type Service struct {
	client *cloudfirestore.Client
	// auth mints and revokes the browser's identity; nil against the emulator.
	auth *fbauth.Client
	cfg  Config
	now  func() time.Time
}

var _ live.Service = (*Service)(nil)

// Option configures a Service at construction.
type Option func(*Service)

// WithClock sets the clock the service stamps subscriptions, change document ids and
// expiries with; the wall clock by default. Tests pin it.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		s.now = now
	}
}

// New opens the service on the configured database. In production the Firestore
// client and the Firebase Admin SDK authenticate with the application's default
// credentials; the custom tokens are signed through the IAM credentials API with the
// service account the metadata server names, so no key file is configured anywhere.
func New(ctx context.Context, cfg Config, opts ...Option) (*Service, error) {
	if cfg.ProjectID == "" {
		return nil, errors.New("live/firestore: a project id is required")
	}
	s := &Service{cfg: cfg, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}

	if cfg.EmulatorHost != "" {
		conn, err := grpc.NewClient(cfg.EmulatorHost,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithPerRPCCredentials(emulatorCredentials{}),
		)
		if err != nil {
			return nil, errors.Wrap(err, "grpc.NewClient()")
		}
		client, err := cloudfirestore.NewClientWithDatabase(ctx, cfg.ProjectID, cfg.database(), option.WithGRPCConn(conn))
		if err != nil {
			return nil, errors.Wrap(err, "firestore.NewClientWithDatabase()")
		}
		s.client = client

		return s, nil
	}

	client, err := cloudfirestore.NewClientWithDatabase(ctx, cfg.ProjectID, cfg.database())
	if err != nil {
		return nil, errors.Wrap(err, "firestore.NewClientWithDatabase()")
	}
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: cfg.ProjectID})
	if err != nil {
		return nil, errors.Wrap(err, "firebase.NewApp()")
	}
	auth, err := app.Auth(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "firebase.App.Auth()")
	}
	s.client = client
	s.auth = auth

	return s, nil
}

// Close releases the Firestore client.
func (s *Service) Close() error {
	if err := s.client.Close(); err != nil {
		return errors.Wrap(err, "firestore.Client.Close()")
	}

	return nil
}

// emulatorCredentials is the emulator's owner credential, which every request to it
// carries as the Firestore client itself does when FIRESTORE_EMULATOR_HOST is set.
type emulatorCredentials struct{}

func (emulatorCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer owner"}, nil
}

func (emulatorCredentials) RequireTransportSecurity() bool {
	return false
}

// subscriptionDoc is a subscription as the record stores it.
type subscriptionDoc struct {
	Principal string    `firestore:"principal"`
	Tab       string    `firestore:"tab"`
	Resource  string    `firestore:"resource"`
	Key       string    `firestore:"key"`
	Domain    string    `firestore:"domain"`
	Expiry    time.Time `firestore:"expiry"`
}

// subscription returns the stored document as a Subscription.
func (d *subscriptionDoc) subscription() live.Subscription {
	return live.Subscription{
		Principal: d.Principal,
		Tab:       d.Tab,
		Resource:  accesstypes.Resource(d.Resource),
		Key:       d.Key,
		Domain:    accesstypes.Domain(d.Domain),
		Expiry:    d.Expiry,
	}
}

// subscriptions returns the record's collection.
func (s *Service) subscriptions() *cloudfirestore.CollectionRef {
	return s.client.Collection(SubscriptionsCollection)
}

// changes returns the principal's change set.
func (s *Service) changes(principal string) *cloudfirestore.CollectionRef {
	return s.client.Collection(UsersCollection).Doc(principal).Collection(ChangesCollection)
}

// Register writes the subscriptions in one batch, each under its id with its expiry;
// the same subscription handed twice is written once, with the later expiry.
func (s *Service) Register(ctx context.Context, subs []live.Subscription) error {
	if len(subs) == 0 {
		return nil
	}
	byID := make(map[string]live.Subscription, len(subs))
	order := make([]string, 0, len(subs))
	for i := range subs {
		sub := subs[i].Normalized()
		id := sub.ID()
		if _, seen := byID[id]; !seen {
			order = append(order, id)
		}
		byID[id] = sub
	}
	writer := s.client.BulkWriter(ctx)
	jobs := make([]*cloudfirestore.BulkWriterJob, 0, len(order))
	for _, id := range order {
		sub := byID[id]
		doc := subscriptionDoc{
			Principal: sub.Principal,
			Tab:       sub.Tab,
			Resource:  string(sub.Resource),
			Key:       sub.Key,
			Domain:    string(sub.Domain),
			Expiry:    sub.Expiry,
		}
		job, err := writer.Set(s.subscriptions().Doc(id), doc)
		if err != nil {
			return errors.Wrap(err, "firestore.BulkWriter.Set()")
		}
		jobs = append(jobs, job)
	}

	return await(writer, jobs)
}

// Renew writes the renewed subscriptions with their fresh expiry, as Register does.
func (s *Service) Renew(ctx context.Context, subs []live.Subscription) error {
	return s.Register(ctx, subs)
}

// Unsubscribe deletes the tab's subscriptions.
func (s *Service) Unsubscribe(ctx context.Context, principal, tab string) error {
	query := s.subscriptions().Where("principal", "==", principal).Where("tab", "==", tab)

	return s.deleteMatching(ctx, &query)
}

// UnsubscribeAll deletes every subscription of the principal.
func (s *Service) UnsubscribeAll(ctx context.Context, principal string) error {
	query := s.subscriptions().Where("principal", "==", principal)

	return s.deleteMatching(ctx, &query)
}

// deleteMatching deletes every document the query names, in one batch.
func (s *Service) deleteMatching(ctx context.Context, query *cloudfirestore.Query) error {
	refs, err := query.Documents(ctx).GetAll()
	if err != nil {
		return errors.Wrap(err, "firestore.DocumentIterator.GetAll()")
	}
	if len(refs) == 0 {
		return nil
	}
	writer := s.client.BulkWriter(ctx)
	jobs := make([]*cloudfirestore.BulkWriterJob, 0, len(refs))
	for _, ref := range refs {
		job, err := writer.Delete(ref.Ref)
		if err != nil {
			return errors.Wrap(err, "firestore.BulkWriter.Delete()")
		}
		jobs = append(jobs, job)
	}

	return await(writer, jobs)
}

// SubscribersOfRow lists the live subscriptions to the row: the (resource, key, expiry)
// index.
func (s *Service) SubscribersOfRow(ctx context.Context, res accesstypes.Resource, key string) ([]live.Subscription, error) {
	query := s.subscriptions().
		Where("resource", "==", string(res)).
		Where("key", "==", key).
		Where("expiry", ">", s.now())

	return s.lookup(ctx, &query, nil)
}

// SubscribersOfList lists the live subscriptions to the resource's list in the domain:
// the (resource, domain, expiry) index, the rows it also matches in the empty domain
// left out.
func (s *Service) SubscribersOfList(ctx context.Context, res accesstypes.Resource, domain accesstypes.Domain) ([]live.Subscription, error) {
	query := s.subscriptions().
		Where("resource", "==", string(res)).
		Where("domain", "==", string(domain)).
		Where("expiry", ">", s.now())

	return s.lookup(ctx, &query, func(sub *live.Subscription) bool {
		return !sub.IsRow()
	})
}

// SubscribersOfResource lists every live subscription to the resource: the
// (resource, expiry) index.
func (s *Service) SubscribersOfResource(ctx context.Context, res accesstypes.Resource) ([]live.Subscription, error) {
	query := s.subscriptions().
		Where("resource", "==", string(res)).
		Where("expiry", ">", s.now())

	return s.lookup(ctx, &query, nil)
}

// lookup runs one record query and decodes its documents, keeping the ones the filter
// admits (every one when nil).
func (s *Service) lookup(ctx context.Context, query *cloudfirestore.Query, keep func(*live.Subscription) bool) ([]live.Subscription, error) {
	it := query.Documents(ctx)
	defer it.Stop()

	subs := make([]live.Subscription, 0)
	for {
		snapshot, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return subs, nil
		}
		if err != nil {
			return nil, errors.Wrap(err, "firestore.DocumentIterator.Next()")
		}
		var doc subscriptionDoc
		if err := snapshot.DataTo(&doc); err != nil {
			return nil, errors.Wrap(err, "firestore.DocumentSnapshot.DataTo()")
		}
		sub := doc.subscription()
		if keep == nil || keep(&sub) {
			subs = append(subs, sub)
		}
	}
}

// Publish fans the committed changes out through the record (live.Fanout) and writes
// one document per subscriber into their change set, set with merge under an id that
// carries the current second, so writes to one target within a second coalesce. Every
// document carries the server timestamp and an expiry the time-to-live policy acts on.
func (s *Service) Publish(ctx context.Context, domain accesstypes.Domain, touched map[accesstypes.Resource][]resource.RowChange) error {
	changes, err := live.Fanout(ctx, s, domain, touched)
	if err != nil {
		return errors.Wrap(err, "live.Fanout()")
	}
	if len(changes) == 0 {
		return nil
	}

	now := s.now()
	second := now.Unix()
	expires := now.Add(live.ChangeTTL)
	writer := s.client.BulkWriter(ctx)
	var jobs []*cloudfirestore.BulkWriterJob
	for principal, docs := range changes {
		for _, doc := range docs {
			job, err := writer.Set(s.changes(principal).Doc(doc.ID(second)), changeData(doc, expires), cloudfirestore.MergeAll)
			if err != nil {
				return errors.Wrap(err, "firestore.BulkWriter.Set()")
			}
			jobs = append(jobs, job)
		}
	}

	return await(writer, jobs)
}

// changeData renders a change document's fields: kind, resource and the two
// timestamps on every document, key and deleted on a row document, domain on a list
// document.
func changeData(doc live.ChangeDocument, expires time.Time) map[string]any {
	data := map[string]any{
		"kind":     string(doc.Kind),
		"resource": string(doc.Resource),
		"at":       cloudfirestore.ServerTimestamp,
		"expires":  expires,
	}
	switch doc.Kind {
	case live.RowChange:
		data["key"] = doc.Key
		data["deleted"] = doc.Deleted
	case live.ListChange:
		data["domain"] = string(doc.Domain)
	case live.ResourceChange:
	}

	return data
}

// Change is one document of a user's change set as the server reads it back.
type Change struct {
	ID string
	live.ChangeDocument
	At      time.Time
	Expires time.Time
}

// Changes reads the principal's change set the way the browser does: the documents
// whose timestamp is after the given instant, oldest first.
func (s *Service) Changes(ctx context.Context, principal string, after time.Time) ([]Change, error) {
	it := s.changes(principal).Where("at", ">", after).OrderBy("at", cloudfirestore.Asc).Documents(ctx)
	defer it.Stop()

	changes := make([]Change, 0)
	for {
		snapshot, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return changes, nil
		}
		if err != nil {
			return nil, errors.Wrap(err, "firestore.DocumentIterator.Next()")
		}
		var doc struct {
			Kind     string    `firestore:"kind"`
			Resource string    `firestore:"resource"`
			Key      string    `firestore:"key"`
			Domain   string    `firestore:"domain"`
			Deleted  bool      `firestore:"deleted"`
			At       time.Time `firestore:"at"`
			Expires  time.Time `firestore:"expires"`
		}
		if err := snapshot.DataTo(&doc); err != nil {
			return nil, errors.Wrap(err, "firestore.DocumentSnapshot.DataTo()")
		}
		changes = append(changes, Change{
			ID: snapshot.Ref.ID,
			ChangeDocument: live.ChangeDocument{
				Kind:     live.ChangeKind(doc.Kind),
				Resource: accesstypes.Resource(doc.Resource),
				Key:      doc.Key,
				Domain:   accesstypes.Domain(doc.Domain),
				Deleted:  doc.Deleted,
			},
			At:      doc.At,
			Expires: doc.Expires,
		})
	}
}

// Token answers how the browser connects as uid: against the emulator the host and no
// token; in production a Firebase custom token for uid, which the browser signs in with.
func (s *Service) Token(ctx context.Context, uid string) (*live.TokenPayload, error) {
	payload := &live.TokenPayload{
		UID:      uid,
		Project:  s.cfg.ProjectID,
		Database: s.cfg.database(),
		APIKey:   s.cfg.APIKey,
		Emulator: s.cfg.EmulatorHost,
	}
	if s.auth == nil {
		return payload, nil
	}
	token, err := s.auth.CustomToken(ctx, uid)
	if err != nil {
		return nil, errors.Wrap(err, "auth.Client.CustomToken()")
	}
	payload.Token = token

	return payload, nil
}

// Revoke ends the browser's identity: in production the refresh tokens of uid are
// revoked (a uid that never signed in has none, which is the same end); against the
// emulator nothing is held.
func (s *Service) Revoke(ctx context.Context, uid string) error {
	if s.auth == nil {
		return nil
	}
	if err := s.auth.RevokeRefreshTokens(ctx, uid); err != nil && !fbauth.IsUserNotFound(err) {
		return errors.Wrap(err, "auth.Client.RevokeRefreshTokens()")
	}

	return nil
}

// await ends the batch and reports the first write that failed.
func await(writer *cloudfirestore.BulkWriter, jobs []*cloudfirestore.BulkWriterJob) error {
	writer.End()
	for _, job := range jobs {
		if _, err := job.Results(); err != nil {
			return errors.Wrap(err, "firestore.BulkWriterJob.Results()")
		}
	}

	return nil
}
