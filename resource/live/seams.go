package live

import (
	"context"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

// SubscriptionRecord is the server-owned record of who is subscribed to what: one
// flat collection no client reads, with a time-to-live on the expiry.
type SubscriptionRecord interface {
	// Register writes the subscriptions with their expiry in one batch; a
	// subscription written again is the same document with the new expiry.
	Register(ctx context.Context, subs []Subscription) error
	// Renew writes the subscriptions a renewal kept with their fresh expiry in one
	// batch. The permission re-check is the caller's (RenewHandler); the record
	// writes what it is handed.
	Renew(ctx context.Context, subs []Subscription) error
	// Unsubscribe deletes every subscription of the tab.
	Unsubscribe(ctx context.Context, principal, tab string) error
	// UnsubscribeAll deletes every subscription of the principal: the logout path.
	UnsubscribeAll(ctx context.Context, principal string) error
	// SubscribersOfRow lists the live (unexpired) subscriptions to the row.
	SubscribersOfRow(ctx context.Context, res accesstypes.Resource, key string) ([]Subscription, error)
	// SubscribersOfList lists the live subscriptions to the resource's list in the
	// domain (the empty domain for a global resource).
	SubscribersOfList(ctx context.Context, res accesstypes.Resource, domain accesstypes.Domain) ([]Subscription, error)
	// SubscribersOfResource lists every live subscription to the resource, rows and
	// lists in every domain: the bulk path's one lookup.
	SubscribersOfResource(ctx context.Context, res accesstypes.Resource) ([]Subscription, error)
}

// ChangePublisher writes a committed request's changes into its subscribers' change
// sets: for each touched row the subscribers of the row get a row document and the
// subscribers of the resource's list in the request's domain a list document, writes to
// one target within one second coalescing; above BulkThreshold touched rows of one
// resource, every subscriber of the resource gets one resource document instead. The
// publisher never checks permission: a subscription was written for a permitted request
// and is re-checked at renewal.
type ChangePublisher interface {
	Publish(ctx context.Context, domain accesstypes.Domain, touched map[accesstypes.Resource][]resource.RowChange) error
}

// Identity mints and revokes the browser's identity on the change set.
type Identity interface {
	// Token answers how the browser connects as uid: a custom token in production, the
	// emulator host against the emulator.
	Token(ctx context.Context, uid string) (*TokenPayload, error)
	// Revoke ends the browser's identity at logout: the refresh tokens of uid in
	// production, nothing against the emulator.
	Revoke(ctx context.Context, uid string) error
}

// ApplicationTopic is the application's own channel between its instances: a signal
// on a named topic, delivered to every instance watching it, carrying nothing but the
// fact that something changed. The feature flags use it (resource.FeaturesTopic): a flip
// broadcasts, and every instance's FeatureSet watching the topic rereads the table. The
// Firestore implementation keeps one document per topic, application/{topic}, and a
// watch is a snapshot listener on it.
type ApplicationTopic interface {
	// Broadcast signals the topic: every watch on it, on every instance, runs its
	// onSignal once.
	Broadcast(ctx context.Context, topic string) error
	// Watch runs onSignal on every broadcast of the topic after the watch began, on the
	// watcher's own goroutine; the state at the start is not a signal. stop ends the
	// watch, as does ctx ending.
	Watch(ctx context.Context, topic string, onSignal func()) (stop func(), err error)
}

// Service is the live service an application wires: the record, the publisher, the
// identity and the application topic together. The generated handlers draw on it
// through the application's LiveService accessor; nil means the application serves no
// live pages, and a request carrying the subscribe header is refused.
type Service interface {
	SubscriptionRecord
	ChangePublisher
	Identity
	ApplicationTopic
}
