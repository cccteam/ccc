package live

import (
	"errors"
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/google/go-cmp/cmp"
)

// fixedClock is a fake's clock pinned to one instant.
func fixedClock(at time.Time) func() time.Time {
	return func() time.Time {
		return at
	}
}

// subscribed returns a fake holding the subscriptions, each live until far after the
// clock unless its expiry is set.
func subscribed(t *testing.T, now time.Time, subs ...Subscription) *Fake {
	t.Helper()

	fake := NewFake()
	fake.Now = fixedClock(now)
	for i := range subs {
		if subs[i].Expiry.IsZero() {
			subs[i].Expiry = now.Add(SubscriptionTTL)
		}
	}
	if err := fake.Register(t.Context(), subs); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	return fake
}

// manyRows returns n touched rows of one resource.
func manyRows(n int) []resource.RowChange {
	rows := make([]resource.RowChange, 0, n)
	for i := range n {
		rows = append(rows, resource.RowChange{Key: "k" + string(rune('a'+i%26)) + string(rune('0'+i/26))})
	}

	return rows
}

func TestFanout(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		subs    []Subscription
		domain  accesstypes.Domain
		touched map[accesstypes.Resource][]resource.RowChange
		want    Changes
		// wantLookups is how many record queries the fan-out ran.
		wantLookups int
	}{
		{
			name:        "nobody subscribed: nothing is written",
			domain:      "anvil",
			touched:     map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1"}}},
			want:        Changes{},
			wantLookups: 2,
		},
		{
			name: "one document per subscriber: the row's reader a row document, the list's a list document with no key",
			subs: []Subscription{
				{Principal: "dispatcher", Tab: "t1", Resource: "Ships", Key: "s1"},
				{Principal: "mechanic", Tab: "t2", Resource: "Ships", Domain: "anvil"},
			},
			domain:  "anvil",
			touched: map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1"}}},
			want: Changes{
				"dispatcher": {{Kind: RowChange, Resource: "Ships", Key: "s1"}},
				"mechanic":   {{Kind: ListChange, Resource: "Ships", Domain: "anvil"}},
			},
			wantLookups: 2,
		},
		{
			name:        "a deleted row says so",
			subs:        []Subscription{{Principal: "dispatcher", Tab: "t1", Resource: "Ships", Key: "s1"}},
			domain:      "anvil",
			touched:     map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1", Deleted: true}}},
			want:        Changes{"dispatcher": {{Kind: RowChange, Resource: "Ships", Key: "s1", Deleted: true}}},
			wantLookups: 2,
		},
		{
			name:        "the list of another domain is not told",
			subs:        []Subscription{{Principal: "pilot", Tab: "t1", Resource: "Ships", Domain: "bastion"}},
			domain:      "anvil",
			touched:     map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1"}}},
			want:        Changes{},
			wantLookups: 2,
		},
		{
			name:        "a global resource's list is the empty domain",
			subs:        []Subscription{{Principal: "clerk", Tab: "t1", Resource: "Clients"}},
			domain:      "",
			touched:     map[accesstypes.Resource][]resource.RowChange{"Clients": {{Key: "c1"}}},
			want:        Changes{"clerk": {{Kind: ListChange, Resource: "Clients"}}},
			wantLookups: 2,
		},
		{
			name: "two tabs of one user, and the row beside the list, are each one document",
			subs: []Subscription{
				{Principal: "dispatcher", Tab: "t1", Resource: "Ships", Key: "s1"},
				{Principal: "dispatcher", Tab: "t2", Resource: "Ships", Key: "s1"},
				{Principal: "dispatcher", Tab: "t1", Resource: "Ships", Domain: "anvil"},
			},
			domain:  "anvil",
			touched: map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1"}}},
			want: Changes{"dispatcher": {
				{Kind: ListChange, Resource: "Ships", Domain: "anvil"},
				{Kind: RowChange, Resource: "Ships", Key: "s1"},
			}},
			wantLookups: 2,
		},
		{
			name: "an expired subscription is not notified",
			subs: []Subscription{
				{Principal: "dispatcher", Tab: "t1", Resource: "Ships", Key: "s1", Expiry: now.Add(-time.Second)},
				{Principal: "mechanic", Tab: "t2", Resource: "Ships", Domain: "anvil", Expiry: now},
			},
			domain:      "anvil",
			touched:     map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1"}}},
			want:        Changes{},
			wantLookups: 2,
		},
		{
			name: "over the threshold: one lookup, one resource document per subscribed user, no row lookups",
			subs: []Subscription{
				{Principal: "dispatcher", Tab: "t1", Resource: "Ships", Key: "ka0"},
				{Principal: "mechanic", Tab: "t2", Resource: "Ships", Domain: "anvil"},
				{Principal: "mechanic", Tab: "t3", Resource: "Ships", Key: "kb0"},
				{Principal: "pilot", Tab: "t4", Resource: "Ships", Domain: "bastion"},
				{Principal: "clerk", Tab: "t5", Resource: "Clients", Domain: "anvil"},
			},
			domain:  "anvil",
			touched: map[accesstypes.Resource][]resource.RowChange{"Ships": manyRows(BulkThreshold + 1)},
			want: Changes{
				"dispatcher": {{Kind: ResourceChange, Resource: "Ships"}},
				"mechanic":   {{Kind: ResourceChange, Resource: "Ships"}},
			},
			wantLookups: 1,
		},
		{
			name:        "exactly the threshold stays row by row",
			subs:        []Subscription{{Principal: "mechanic", Tab: "t2", Resource: "Ships", Domain: "anvil"}},
			domain:      "anvil",
			touched:     map[accesstypes.Resource][]resource.RowChange{"Ships": manyRows(BulkThreshold)},
			want:        Changes{"mechanic": {{Kind: ListChange, Resource: "Ships", Domain: "anvil"}}},
			wantLookups: 1 + BulkThreshold,
		},
		{
			name: "several resources fan out independently",
			subs: []Subscription{
				{Principal: "dispatcher", Tab: "t1", Resource: "Ships", Domain: "anvil"},
				{Principal: "dispatcher", Tab: "t1", Resource: "Refits", Key: "r1"},
			},
			domain: "anvil",
			touched: map[accesstypes.Resource][]resource.RowChange{
				"Ships":  {{Key: "s1"}},
				"Refits": {{Key: "r1"}, {Key: "r2"}},
			},
			want: Changes{"dispatcher": {
				{Kind: RowChange, Resource: "Refits", Key: "r1"},
				{Kind: ListChange, Resource: "Ships", Domain: "anvil"},
			}},
			wantLookups: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := subscribed(t, now, tt.subs...)
			got, err := Fanout(t.Context(), fake, tt.domain, tt.touched)
			if err != nil {
				t.Fatalf("Fanout() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Fanout() mismatch (-want +got):\n%s", diff)
			}
			if got := fake.Lookups(); got != tt.wantLookups {
				t.Errorf("record lookups = %d, want %d", got, tt.wantLookups)
			}
		})
	}
}

func TestFanout_recordFailure(t *testing.T) {
	t.Parallel()

	fake := NewFake()
	fake.Err = errors.New("record down")
	if _, err := Fanout(t.Context(), fake, "anvil", map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1"}}}); err == nil {
		t.Fatal("Fanout() error = nil, want the record's failure")
	}
}

func TestChangeDocument_ID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  ChangeDocument
		want string
	}{
		{name: "a row document", doc: ChangeDocument{Kind: RowChange, Resource: "Ships", Key: "s1", Deleted: true}, want: "row|Ships|s1|1759406400"},
		{name: "a compound key's slash is escaped", doc: ChangeDocument{Kind: RowChange, Resource: "RefitTasks", Key: "r1/3"}, want: "row|RefitTasks|r1%2F3|1759406400"},
		{name: "a list document", doc: ChangeDocument{Kind: ListChange, Resource: "Ships", Domain: "anvil"}, want: "list|Ships|anvil|1759406400"},
		{name: "a global list document", doc: ChangeDocument{Kind: ListChange, Resource: "Clients"}, want: "list|Clients||1759406400"},
		{name: "a resource document", doc: ChangeDocument{Kind: ResourceChange, Resource: "Ships"}, want: "resource|Ships|1759406400"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.doc.ID(1759406400); got != tt.want {
				t.Errorf("ID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFake_Publish_coalesces(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	fake := subscribed(t, now, Subscription{Principal: "dispatcher", Tab: "t1", Resource: "Ships", Key: "s1"})
	touched := map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1"}}}

	// Two publishes within one second are one document; the second's deleted flag wins.
	if err := fake.Publish(t.Context(), "anvil", touched); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	fake.Now = fixedClock(now.Add(500 * time.Millisecond))
	if err := fake.Publish(t.Context(), "anvil", map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1", Deleted: true}}}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	// A publish in the next second is a new document.
	fake.Now = fixedClock(now.Add(time.Second))
	if err := fake.Publish(t.Context(), "anvil", touched); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	want := []FakeChange{
		{ChangeDocument: ChangeDocument{Kind: RowChange, Resource: "Ships", Key: "s1", Deleted: true}, ID: "row|Ships|s1|1790942400", At: now.Add(500 * time.Millisecond)},
		{ChangeDocument: ChangeDocument{Kind: RowChange, Resource: "Ships", Key: "s1"}, ID: "row|Ships|s1|1790942401", At: now.Add(time.Second)},
	}
	if diff := cmp.Diff(want, fake.Changes("dispatcher")); diff != "" {
		t.Errorf("Changes() mismatch (-want +got):\n%s", diff)
	}
}

func TestSubscription_ID(t *testing.T) {
	t.Parallel()

	row := Subscription{Principal: "dispatcher", Tab: "t1", Resource: "Ships", Key: "s1"}
	rowInDomain := Subscription{Principal: "dispatcher", Tab: "t1", Resource: "Ships", Key: "s1", Domain: "anvil"}
	list := Subscription{Principal: "dispatcher", Tab: "t1", Resource: "Ships", Domain: "anvil"}
	otherTab := Subscription{Principal: "dispatcher", Tab: "t2", Resource: "Ships", Key: "s1"}

	tests := []struct {
		name string
		a, b Subscription
		same bool
	}{
		{name: "the same interest is the same document", a: row, b: row, same: true},
		{name: "a row subscription's domain is not part of its identity", a: row, b: rowInDomain, same: true},
		{name: "a row and the list are different documents", a: row, b: list},
		{name: "another tab is another document", a: row, b: otherTab},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.a.ID() == tt.b.ID(); got != tt.same {
				t.Errorf("ID() equal = %v, want %v (%s vs %s)", got, tt.same, tt.a.ID(), tt.b.ID())
			}
			if l := len(tt.a.ID()); l != 32 {
				t.Errorf("len(ID()) = %d, want 32", l)
			}
		})
	}
}

func TestValidTab(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tab  string
		want bool
	}{
		{name: "a base64url id", tab: "Ab0_-xyz", want: true},
		{name: "one character", tab: "a", want: true},
		{name: "64 characters", tab: string(make64()), want: true},
		{name: "empty", tab: "", want: false},
		{name: "65 characters", tab: string(make64()) + "a", want: false},
		{name: "a slash", tab: "a/b", want: false},
		{name: "a space", tab: "a b", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ValidTab(tt.tab); got != tt.want {
				t.Errorf("ValidTab(%q) = %v, want %v", tt.tab, got, tt.want)
			}
		})
	}
}

func make64() []byte {
	b := make([]byte, 64)
	for i := range b {
		b[i] = 'x'
	}

	return b
}
