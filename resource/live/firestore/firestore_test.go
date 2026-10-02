package firestore_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/live"
	livefirestore "github.com/cccteam/ccc/resource/live/firestore"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// resourceFor names a resource after the test, so parallel tests on the shared
// emulator never see each other's subscriptions.
func resourceFor(t *testing.T) accesstypes.Resource {
	t.Helper()

	return accesstypes.Resource(strings.NewReplacer("/", "_", " ", "_", ",", "", ":", "").Replace(t.Name()))
}

// principalFor names a principal after the test, for the same isolation.
func principalFor(t *testing.T, who string) string {
	t.Helper()

	return who + "-" + string(resourceFor(t))
}

// without drops the expiry for comparison: the record returns what was written.
var ignoreExpiry = cmpopts.IgnoreFields(live.Subscription{}, "Expiry")

func TestService_lookups(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		// subs are written before the lookup; principals and resources are made unique
		// per test.
		subs   []live.Subscription
		lookup func(t *testing.T, svc *livefirestore.Service, res accesstypes.Resource) ([]live.Subscription, error)
		want   []live.Subscription
	}{
		{
			name: "the subscribers of a row: its readers, not the list's, not an expired one",
			subs: []live.Subscription{
				{Principal: "p1", Tab: "t1", Key: "k1"},
				{Principal: "p2", Tab: "t2", Domain: "anvil"},
				{Principal: "p3", Tab: "t3", Key: "k2"},
				{Principal: "p4", Tab: "t4", Key: "k1", Expiry: now.Add(-time.Second)},
				{Principal: "p5", Tab: "t5", Key: "k1", Domain: "anvil"},
			},
			lookup: func(t *testing.T, svc *livefirestore.Service, res accesstypes.Resource) ([]live.Subscription, error) {
				t.Helper()

				return svc.SubscribersOfRow(t.Context(), res, "k1")
			},
			want: []live.Subscription{
				{Principal: "p1", Tab: "t1", Key: "k1"},
				// A row subscription carries no domain in the record.
				{Principal: "p5", Tab: "t5", Key: "k1"},
			},
		},
		{
			name: "the subscribers of a tenant's list: not another tenant's, not a row's",
			subs: []live.Subscription{
				{Principal: "p1", Tab: "t1", Domain: "anvil"},
				{Principal: "p2", Tab: "t2", Domain: "bastion"},
				{Principal: "p3", Tab: "t3", Key: "k1"},
				{Principal: "p4", Tab: "t4", Domain: "anvil", Expiry: now},
			},
			lookup: func(t *testing.T, svc *livefirestore.Service, res accesstypes.Resource) ([]live.Subscription, error) {
				t.Helper()

				return svc.SubscribersOfList(t.Context(), res, "anvil")
			},
			want: []live.Subscription{{Principal: "p1", Tab: "t1", Domain: "anvil"}},
		},
		{
			name: "the subscribers of a global list: the rows in the empty domain are left out",
			subs: []live.Subscription{
				{Principal: "p1", Tab: "t1"},
				{Principal: "p2", Tab: "t2", Key: "k1"},
			},
			lookup: func(t *testing.T, svc *livefirestore.Service, res accesstypes.Resource) ([]live.Subscription, error) {
				t.Helper()

				return svc.SubscribersOfList(t.Context(), res, "")
			},
			want: []live.Subscription{{Principal: "p1", Tab: "t1"}},
		},
		{
			name: "every live subscriber of a resource, rows and lists in every domain",
			subs: []live.Subscription{
				{Principal: "p1", Tab: "t1", Key: "k1"},
				{Principal: "p2", Tab: "t2", Domain: "anvil"},
				{Principal: "p3", Tab: "t3", Domain: "bastion"},
				{Principal: "p4", Tab: "t4"},
				{Principal: "p5", Tab: "t5", Key: "k2", Expiry: now.Add(-time.Minute)},
			},
			lookup: func(t *testing.T, svc *livefirestore.Service, res accesstypes.Resource) ([]live.Subscription, error) {
				t.Helper()

				return svc.SubscribersOfResource(t.Context(), res)
			},
			want: []live.Subscription{
				{Principal: "p1", Tab: "t1", Key: "k1"},
				{Principal: "p2", Tab: "t2", Domain: "anvil"},
				{Principal: "p3", Tab: "t3", Domain: "bastion"},
				{Principal: "p4", Tab: "t4"},
			},
		},
		{
			name: "a subscription written twice is one document",
			subs: []live.Subscription{
				{Principal: "p1", Tab: "t1", Key: "k1"},
				{Principal: "p1", Tab: "t1", Key: "k1"},
			},
			lookup: func(t *testing.T, svc *livefirestore.Service, res accesstypes.Resource) ([]live.Subscription, error) {
				t.Helper()

				return svc.SubscribersOfRow(t.Context(), res, "k1")
			},
			want: []live.Subscription{{Principal: "p1", Tab: "t1", Key: "k1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := newService(t, fixedClock(now))
			res := resourceFor(t)
			subs := make([]live.Subscription, 0, len(tt.subs))
			for _, sub := range tt.subs {
				sub.Resource = res
				sub.Principal = principalFor(t, sub.Principal)
				subs = append(subs, sub)
			}
			register(t, svc, now, subs...)

			got, err := tt.lookup(t, svc, res)
			if err != nil {
				t.Fatalf("lookup error = %v", err)
			}
			want := make([]live.Subscription, 0, len(tt.want))
			for _, sub := range tt.want {
				sub.Resource = res
				sub.Principal = principalFor(t, sub.Principal)
				want = append(want, sub)
			}
			sortByPrincipal := cmpopts.SortSlices(func(a, b live.Subscription) bool {
				return a.Principal < b.Principal
			})
			if diff := cmp.Diff(want, got, ignoreExpiry, sortByPrincipal); diff != "" {
				t.Errorf("lookup mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestService_renewExtendsExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	svc := newService(t, fixedClock(now))
	res := resourceFor(t)
	sub := live.Subscription{Principal: principalFor(t, "p1"), Tab: "t1", Resource: res, Key: "k1", Expiry: now.Add(time.Minute)}
	register(t, svc, now, sub)

	sub.Expiry = now.Add(live.SubscriptionTTL)
	if err := svc.Renew(t.Context(), []live.Subscription{sub}); err != nil {
		t.Fatalf("Renew() error = %v", err)
	}

	got, err := svc.SubscribersOfRow(t.Context(), res, "k1")
	if err != nil {
		t.Fatalf("SubscribersOfRow() error = %v", err)
	}
	if len(got) != 1 || !got[0].Expiry.Equal(sub.Expiry) {
		t.Errorf("SubscribersOfRow() = %+v, want one subscription expiring at %v", got, sub.Expiry)
	}
}

func TestService_unsubscribe(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		leave func(t *testing.T, svc *livefirestore.Service, p1, p2 string) error
		want  []string // "principal|tab"
	}{
		{
			name: "a tab leaving drops its subscriptions alone",
			leave: func(t *testing.T, svc *livefirestore.Service, p1, _ string) error {
				t.Helper()

				return svc.Unsubscribe(t.Context(), p1, "t1")
			},
			want: []string{"p1|t2", "p2|t3"},
		},
		{
			name: "a logout drops every subscription of the principal",
			leave: func(t *testing.T, svc *livefirestore.Service, p1, _ string) error {
				t.Helper()

				return svc.UnsubscribeAll(t.Context(), p1)
			},
			want: []string{"p2|t3"},
		},
		{
			name: "a tab with nothing there is nothing to delete",
			leave: func(t *testing.T, svc *livefirestore.Service, p1, _ string) error {
				t.Helper()

				return svc.Unsubscribe(t.Context(), p1, "t9")
			},
			want: []string{"p1|t1", "p1|t2", "p2|t3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := newService(t, fixedClock(now))
			res := resourceFor(t)
			p1, p2 := principalFor(t, "p1"), principalFor(t, "p2")
			register(t, svc, now,
				live.Subscription{Principal: p1, Tab: "t1", Resource: res, Domain: "anvil"},
				live.Subscription{Principal: p1, Tab: "t2", Resource: res, Key: "k1"},
				live.Subscription{Principal: p2, Tab: "t3", Resource: res, Domain: "anvil"},
			)

			if err := tt.leave(t, svc, p1, p2); err != nil {
				t.Fatalf("leave error = %v", err)
			}

			subs, err := svc.SubscribersOfResource(t.Context(), res)
			if err != nil {
				t.Fatalf("SubscribersOfResource() error = %v", err)
			}
			left := make([]string, 0, len(subs))
			for _, sub := range subs {
				left = append(left, strings.TrimSuffix(sub.Principal, "-"+string(res))+"|"+sub.Tab)
			}
			sorted := cmpopts.SortSlices(func(a, b string) bool {
				return a < b
			})
			if diff := cmp.Diff(tt.want, left, sorted); diff != "" {
				t.Errorf("subscriptions left mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// manyRows returns n touched rows.
func manyRows(n int) []resource.RowChange {
	rows := make([]resource.RowChange, 0, n)
	for i := range n {
		rows = append(rows, resource.RowChange{Key: "k" + string(rune('a'+i%26)) + string(rune('0'+i/26))})
	}

	return rows
}

func TestService_Publish(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	second := now.Unix()

	tests := []struct {
		name    string
		subs    []live.Subscription
		domain  accesstypes.Domain
		touched func(res accesstypes.Resource) map[accesstypes.Resource][]resource.RowChange
		// want is each principal's change set, documents by id and content.
		want map[string][]livefirestore.Change
	}{
		{
			name:   "nobody subscribed: no document is written",
			subs:   []live.Subscription{{Principal: "p1", Tab: "t1", Domain: "bastion"}},
			domain: "anvil",
			touched: func(res accesstypes.Resource) map[accesstypes.Resource][]resource.RowChange {
				return map[accesstypes.Resource][]resource.RowChange{res: {{Key: "k1"}}}
			},
			want: map[string][]livefirestore.Change{"p1": {}},
		},
		{
			name: "one document per subscriber: a row document with its key and a list document with none",
			subs: []live.Subscription{
				{Principal: "p1", Tab: "t1", Key: "k1"},
				{Principal: "p2", Tab: "t2", Domain: "anvil"},
			},
			domain: "anvil",
			touched: func(res accesstypes.Resource) map[accesstypes.Resource][]resource.RowChange {
				return map[accesstypes.Resource][]resource.RowChange{res: {{Key: "k1", Deleted: true}}}
			},
			want: map[string][]livefirestore.Change{
				"p1": {{ChangeDocument: live.ChangeDocument{Kind: live.RowChange, Key: "k1", Deleted: true}}},
				"p2": {{ChangeDocument: live.ChangeDocument{Kind: live.ListChange, Domain: "anvil"}}},
			},
		},
		{
			name: "over the threshold every subscriber gets one resource document",
			subs: []live.Subscription{
				{Principal: "p1", Tab: "t1", Key: "ka0"},
				{Principal: "p2", Tab: "t2", Domain: "anvil"},
				{Principal: "p3", Tab: "t3", Domain: "bastion"},
			},
			domain: "anvil",
			touched: func(res accesstypes.Resource) map[accesstypes.Resource][]resource.RowChange {
				return map[accesstypes.Resource][]resource.RowChange{res: manyRows(live.BulkThreshold + 1)}
			},
			want: map[string][]livefirestore.Change{
				"p1": {{ChangeDocument: live.ChangeDocument{Kind: live.ResourceChange}}},
				"p2": {{ChangeDocument: live.ChangeDocument{Kind: live.ResourceChange}}},
				"p3": {},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := newService(t, fixedClock(now))
			res := resourceFor(t)
			subs := make([]live.Subscription, 0, len(tt.subs))
			for _, sub := range tt.subs {
				sub.Resource = res
				sub.Principal = principalFor(t, sub.Principal)
				subs = append(subs, sub)
			}
			register(t, svc, now, subs...)

			if err := svc.Publish(t.Context(), tt.domain, tt.touched(res)); err != nil {
				t.Fatalf("Publish() error = %v", err)
			}

			for who, want := range tt.want {
				got, err := svc.Changes(t.Context(), principalFor(t, who), time.Time{})
				if err != nil {
					t.Fatalf("Changes(%s) error = %v", who, err)
				}
				for i := range want {
					want[i].Resource = res
					want[i].ID = want[i].ChangeDocument.ID(second)
					want[i].Expires = now.Add(live.ChangeTTL)
				}
				if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(livefirestore.Change{}, "At")); diff != "" {
					t.Errorf("Changes(%s) mismatch (-want +got):\n%s", who, diff)
				}
				for _, change := range got {
					if change.At.IsZero() {
						t.Errorf("Changes(%s): document %s has no server timestamp", who, change.ID)
					}
				}
			}
		})
	}
}

func TestService_Publish_coalesces(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	clock := now
	svc := newService(t, func() time.Time {
		return clock
	})
	res := resourceFor(t)
	p1 := principalFor(t, "p1")
	register(t, svc, now, live.Subscription{Principal: p1, Tab: "t1", Resource: res, Key: "k1"})

	// Two publishes within one second are one document, the second's deleted flag kept.
	if err := svc.Publish(t.Context(), "anvil", map[accesstypes.Resource][]resource.RowChange{res: {{Key: "k1"}}}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	clock = now.Add(700 * time.Millisecond)
	if err := svc.Publish(t.Context(), "anvil", map[accesstypes.Resource][]resource.RowChange{res: {{Key: "k1", Deleted: true}}}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	// The next second is a new document.
	clock = now.Add(time.Second)
	if err := svc.Publish(t.Context(), "anvil", map[accesstypes.Resource][]resource.RowChange{res: {{Key: "k1"}}}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	got, err := svc.Changes(t.Context(), p1, time.Time{})
	if err != nil {
		t.Fatalf("Changes() error = %v", err)
	}
	want := []livefirestore.Change{
		{ID: live.ChangeDocument{Kind: live.RowChange, Resource: res, Key: "k1"}.ID(now.Unix()), ChangeDocument: live.ChangeDocument{Kind: live.RowChange, Resource: res, Key: "k1", Deleted: true}},
		{ID: live.ChangeDocument{Kind: live.RowChange, Resource: res, Key: "k1"}.ID(now.Unix() + 1), ChangeDocument: live.ChangeDocument{Kind: live.RowChange, Resource: res, Key: "k1"}},
	}
	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(livefirestore.Change{}, "At", "Expires")); diff != "" {
		t.Errorf("Changes() mismatch (-want +got):\n%s", diff)
	}
	if len(got) == 2 && !got[1].At.After(got[0].At) {
		t.Errorf("documents are not ordered by their server timestamp: %v then %v", got[0].At, got[1].At)
	}
}

func TestService_Token_emulator(t *testing.T) {
	t.Parallel()

	svc := newService(t, time.Now)
	got, err := svc.Token(t.Context(), "dispatcher")
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	want := &live.TokenPayload{UID: "dispatcher", Project: testProject, Database: testDatabase, Emulator: firestoreEmulator(t)}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Token() mismatch (-want +got):\n%s", diff)
	}
	if err := svc.Revoke(t.Context(), "dispatcher"); err != nil {
		t.Errorf("Revoke() error = %v, want nothing against the emulator", err)
	}
}

func TestNew_requiresProject(t *testing.T) {
	t.Parallel()

	if _, err := livefirestore.New(t.Context(), livefirestore.Config{EmulatorHost: "127.0.0.1:1"}); err == nil {
		t.Fatal("New() error = nil, want a refusal without a project")
	}
}

// TestService_topic pins the application topic against the emulator: the document's
// state at the start is not a signal, every broadcast after the watch began is one, and
// a stopped watch hears nothing more.
func TestService_topic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// before broadcasts before the watch begins.
		before int
		// after broadcasts after the watch begins, stopAfter of them after the stop.
		after     int
		stopAfter int
		want      int
	}{
		{name: "a broadcast after the watch began signals once", after: 1, want: 1},
		{name: "a broadcast before the watch is the state at the start, not a signal", before: 1, after: 1, want: 1},
		{name: "every broadcast signals", after: 3, want: 3},
		{name: "a stopped watch hears nothing more", after: 1, stopAfter: 2, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := newService(t, time.Now)
			topic := string(resourceFor(t))
			for range tt.before {
				if err := svc.Broadcast(t.Context(), topic); err != nil {
					t.Fatalf("Broadcast() error = %v", err)
				}
			}

			signals := make(chan struct{}, 16)
			stop, err := svc.Watch(t.Context(), topic, func() { signals <- struct{}{} })
			if err != nil {
				t.Fatalf("Watch() error = %v", err)
			}
			defer stop()

			for range tt.after {
				if err := svc.Broadcast(t.Context(), topic); err != nil {
					t.Fatalf("Broadcast() error = %v", err)
				}
			}
			got := 0
			deadline := time.After(10 * time.Second)
			for got < tt.want {
				select {
				case <-signals:
					got++
				case <-deadline:
					t.Fatalf("heard %d signals within 10s, want %d", got, tt.want)
				}
			}

			stop()
			for range tt.stopAfter {
				if err := svc.Broadcast(t.Context(), topic); err != nil {
					t.Fatalf("Broadcast() error = %v", err)
				}
			}
			select {
			case <-signals:
				t.Fatalf("heard a signal beyond the %d wanted", tt.want)
			case <-time.After(500 * time.Millisecond):
			}
		})
	}
}
