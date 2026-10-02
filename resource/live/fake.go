package live

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

// Fake is an in-memory Service for tests: the record is a map, a publish keeps the
// documents Fanout produced per principal with the fake's clock as their timestamp,
// the identity answers a recognizable payload and remembers what it revoked. Every
// method fails with Err when it is set, so a caller's logged-failure path can be
// driven. It is safe for concurrent use.
type Fake struct {
	// Now is the fake's clock, time.Now by default.
	Now func() time.Time
	// Err, when set, is every method's answer.
	Err error

	mu       sync.Mutex
	subs     map[string]Subscription
	changes  map[string][]FakeChange
	revoked  []string
	lookups  int
	publishs []FakePublish
}

// FakeChange is one change document the fake holds, with the fake's timestamp and the
// id the document was written under.
type FakeChange struct {
	ChangeDocument
	ID string
	At time.Time
}

// FakePublish is one Publish call the fake received.
type FakePublish struct {
	Domain  accesstypes.Domain
	Touched map[accesstypes.Resource][]resource.RowChange
}

// What the fake's Token answers: an emulator-shaped payload.
const (
	fakeProject  = "fake-project"
	fakeDatabase = "(default)"
	fakeEmulator = "fake:0"
)

// NewFake returns an empty fake on the wall clock.
func NewFake() *Fake {
	return &Fake{
		Now:     time.Now,
		subs:    make(map[string]Subscription),
		changes: make(map[string][]FakeChange),
	}
}

var _ Service = (*Fake)(nil)

// Register writes the subscriptions.
func (f *Fake) Register(_ context.Context, subs []Subscription) error {
	if f.Err != nil {
		return f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, sub := range subs {
		sub = sub.Normalized()
		f.subs[sub.ID()] = sub
	}

	return nil
}

// Renew writes the subscriptions, as Register does.
func (f *Fake) Renew(ctx context.Context, subs []Subscription) error {
	return f.Register(ctx, subs)
}

// Unsubscribe deletes the tab's subscriptions.
func (f *Fake) Unsubscribe(_ context.Context, principal, tab string) error {
	if f.Err != nil {
		return f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, sub := range f.subs {
		if sub.Principal == principal && sub.Tab == tab {
			delete(f.subs, id)
		}
	}

	return nil
}

// UnsubscribeAll deletes the principal's subscriptions.
func (f *Fake) UnsubscribeAll(_ context.Context, principal string) error {
	if f.Err != nil {
		return f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, sub := range f.subs {
		if sub.Principal == principal {
			delete(f.subs, id)
		}
	}

	return nil
}

// SubscribersOfRow lists the live subscriptions to the row.
func (f *Fake) SubscribersOfRow(_ context.Context, res accesstypes.Resource, key string) ([]Subscription, error) {
	return f.lookup(func(sub Subscription) bool {
		return sub.Resource == res && sub.Key == key
	})
}

// SubscribersOfList lists the live subscriptions to the resource's list in the domain.
func (f *Fake) SubscribersOfList(_ context.Context, res accesstypes.Resource, domain accesstypes.Domain) ([]Subscription, error) {
	return f.lookup(func(sub Subscription) bool {
		return sub.Resource == res && !sub.IsRow() && sub.Domain == domain
	})
}

// SubscribersOfResource lists every live subscription to the resource.
func (f *Fake) SubscribersOfResource(_ context.Context, res accesstypes.Resource) ([]Subscription, error) {
	return f.lookup(func(sub Subscription) bool {
		return sub.Resource == res
	})
}

// lookup answers one record query, counting it, over the live subscriptions in a stable
// order.
func (f *Fake) lookup(match func(Subscription) bool) ([]Subscription, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups++
	var found []Subscription
	for _, sub := range f.subs {
		if match(sub) {
			found = append(found, sub)
		}
	}
	sortSubscriptions(found)

	return Live(found, f.Now()), nil
}

// Publish fans the changes out and keeps the documents per principal, coalescing by id
// within the fake's current second as the Firestore implementation does.
func (f *Fake) Publish(ctx context.Context, domain accesstypes.Domain, touched map[accesstypes.Resource][]resource.RowChange) error {
	if f.Err != nil {
		return f.Err
	}
	f.mu.Lock()
	f.publishs = append(f.publishs, FakePublish{Domain: domain, Touched: touched})
	f.mu.Unlock()

	changes, err := Fanout(ctx, f, domain, touched)
	if err != nil {
		return err
	}

	now := f.Now()
	f.mu.Lock()
	defer f.mu.Unlock()
	for principal, docs := range changes {
		for _, doc := range docs {
			id := doc.ID(now.Unix())
			held := f.changes[principal]
			at := slices.IndexFunc(held, func(c FakeChange) bool {
				return c.ID == id
			})
			if at >= 0 {
				held[at].Deleted = doc.Deleted
				held[at].At = now

				continue
			}
			f.changes[principal] = append(held, FakeChange{ChangeDocument: doc, ID: id, At: now})
		}
	}

	return nil
}

// Token answers an emulator-shaped payload for uid.
func (f *Fake) Token(_ context.Context, uid string) (*TokenPayload, error) {
	if f.Err != nil {
		return nil, f.Err
	}

	return &TokenPayload{UID: uid, Project: fakeProject, Database: fakeDatabase, Emulator: fakeEmulator}, nil
}

// Revoke remembers uid.
func (f *Fake) Revoke(_ context.Context, uid string) error {
	if f.Err != nil {
		return f.Err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revoked = append(f.revoked, uid)

	return nil
}

// Subscriptions returns every subscription the fake holds, expired ones included, in a
// stable order.
func (f *Fake) Subscriptions() []Subscription {
	f.mu.Lock()
	defer f.mu.Unlock()
	subs := make([]Subscription, 0, len(f.subs))
	for _, sub := range f.subs {
		subs = append(subs, sub)
	}
	sortSubscriptions(subs)

	return subs
}

// Changes returns the principal's change set in writing order.
func (f *Fake) Changes(principal string) []FakeChange {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.changes[principal])
}

// Revoked returns the uids revoked so far.
func (f *Fake) Revoked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.revoked)
}

// Lookups returns how many record queries the fake answered.
func (f *Fake) Lookups() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.lookups
}

// Publishes returns the Publish calls received so far.
func (f *Fake) Publishes() []FakePublish {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.publishs)
}

// sortSubscriptions orders subscriptions by principal, tab, resource, key and domain.
func sortSubscriptions(subs []Subscription) {
	slices.SortFunc(subs, func(a, b Subscription) int {
		return strings.Compare(
			a.Principal+"|"+a.Tab+"|"+string(a.Resource)+"|"+a.Key+"|"+string(a.Domain),
			b.Principal+"|"+b.Tab+"|"+string(b.Resource)+"|"+b.Key+"|"+string(b.Domain),
		)
	})
}
