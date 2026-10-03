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

// Kind is a signal's kind: which of the application's shared states changed. It is
// resource.SignalKind, declared in the resource package so the resource package's
// consumers (FeatureSet) and the live service name one type; the kinds are re-exported
// here under the same names.
type Kind = resource.SignalKind

// The kinds.
const (
	// KindFeatures is a feature flag flip.
	KindFeatures = resource.KindFeatures
	// KindTenants is a change of the tenant roster.
	KindTenants = resource.KindTenants
	// KindPolicy is a change of the permission policy.
	KindPolicy = resource.KindPolicy
)

// Signaler is the application's own channel between its instances: a signal of a
// kind, delivered to every instance subscribed to the kind, carrying nothing but the
// fact that something of the kind changed. The Firestore implementation keeps one
// signals document per application, application/signals, with a field per kind
// holding the time of the last signal and the instance that sent it; a signal of a
// kind writes that kind's field alone, so two kinds never clobber each other, and a
// subscriber served one snapshot after two quick signals of different kinds sees both.
type Signaler interface {
	// Signal signals the kind: every subscription to it, on every instance, runs its
	// onSignal once. While a write of the kind is in flight, a later Signal of the kind
	// is absorbed into one following write the implementation makes on its own and
	// answers nil at once: one write after the last call covers every call before it.
	// The error of the caller's own write is returned for the caller to log; the
	// caller's contract stays log and never fail the request, since every subscriber
	// also rereads at its own backstop.
	Signal(ctx context.Context, kind Kind) error
}

// Subscriber delivers the signals to this instance's consumers: one subscriber per
// instance holds the one listener on the signals document and runs, for every kind
// whose time advanced since it last looked, the onSignal of each subscription to the
// kind. Subscribe waits on nothing: a subscription made before the listener's first
// snapshot may be woken once for a kind the document already holds and misses no
// signal after it was made; one made after the listener began receives the signals
// after it was made alone; a listener the backend ends on its
// own is logged and reopened with backoff by the subscriber, and on the reopen every
// kind whose time advanced while it was down is signaled once.
type Subscriber interface {
	// Subscribe runs onSignal on every signal of the kind from now on, on the
	// subscriber's goroutine, so it must return quickly (a non-blocking send is the
	// usual shape). Several subscriptions to one kind each run. stop ends this
	// subscription alone.
	Subscribe(kind Kind, onSignal func()) (stop func(), err error)
}

// Service is the live service an application wires: the record, the change publisher,
// the identity, and the signals' signaler and subscriber together. The generated
// handlers draw on it through the application's LiveService accessor, and every
// application wires one: the Firestore service, or the Fake in a test harness.
type Service interface {
	SubscriptionRecord
	ChangePublisher
	Identity
	Signaler
	Subscriber
}
