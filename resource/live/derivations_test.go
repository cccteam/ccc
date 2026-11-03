package live

import (
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/google/go-cmp/cmp"
)

// The derivations a generator would write for a fleet board carrying Ships' rows and
// reading the global ShipClasses, a mission board reading Clients and Squadrons with no
// root, and a global roster reading Pilots.
func fixtureDerivations() *Derivations {
	return &Derivations{Resources: []DerivedResource{
		{Resource: "FleetBoards", Root: "Ships", Tables: []SourceTable{{Name: "ShipClasses", Global: true}}},
		{Resource: "MissionBoards", Tables: []SourceTable{{Name: "Clients"}, {Name: "Squadrons"}}},
		{Resource: "Rosters", Global: true, Tables: []SourceTable{{Name: "Pilots"}}},
	}}
}

func TestDerivations_Expand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		derivations *Derivations
		touched     map[accesstypes.Resource][]resource.RowChange
		want        Writes
	}{
		{
			name:    "nil derivations: the rows as they are",
			touched: map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1"}}},
			want:    Writes{Rows: map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1"}}}},
		},
		{
			name:        "a root table's rows reach the derived resource under the same keys",
			derivations: fixtureDerivations(),
			touched:     map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1"}, {Key: "s2", Deleted: true}}},
			want: Writes{Rows: map[accesstypes.Resource][]resource.RowChange{
				"Ships":       {{Key: "s1"}, {Key: "s2", Deleted: true}},
				"FleetBoards": {{Key: "s1"}, {Key: "s2", Deleted: true}},
			}},
		},
		{
			name:        "another table it reads changes the derived resource wholesale, in the write's domain",
			derivations: fixtureDerivations(),
			touched:     map[accesstypes.Resource][]resource.RowChange{"Squadrons": {{Key: "q1"}}},
			want: Writes{
				Rows:  map[accesstypes.Resource][]resource.RowChange{"Squadrons": {{Key: "q1"}}},
				Whole: []WholeChange{{Resource: "MissionBoards"}},
			},
		},
		{
			name:        "a global table's change is told in every domain",
			derivations: fixtureDerivations(),
			touched:     map[accesstypes.Resource][]resource.RowChange{"ShipClasses": {{Key: "c1"}}},
			want: Writes{
				Rows:  map[accesstypes.Resource][]resource.RowChange{"ShipClasses": {{Key: "c1"}}},
				Whole: []WholeChange{{Resource: "FleetBoards", AllDomains: true}},
			},
		},
		{
			name:        "a global derived resource is told whatever domain the write carried",
			derivations: fixtureDerivations(),
			touched:     map[accesstypes.Resource][]resource.RowChange{"Pilots": {{Key: "p1"}}},
			want: Writes{
				Rows:  map[accesstypes.Resource][]resource.RowChange{"Pilots": {{Key: "p1"}}},
				Whole: []WholeChange{{Resource: "Rosters", AllDomains: true}},
			},
		},
		{
			name:        "reached by its root and by another table in one commit: the whole change alone",
			derivations: fixtureDerivations(),
			touched: map[accesstypes.Resource][]resource.RowChange{
				"Ships":       {{Key: "s1"}},
				"ShipClasses": {{Key: "c1"}},
			},
			want: Writes{
				Rows: map[accesstypes.Resource][]resource.RowChange{
					"Ships":       {{Key: "s1"}},
					"ShipClasses": {{Key: "c1"}},
				},
				Whole: []WholeChange{{Resource: "FleetBoards", AllDomains: true}},
			},
		},
		{
			name:        "a table nothing derives from expands to itself",
			derivations: fixtureDerivations(),
			touched:     map[accesstypes.Resource][]resource.RowChange{"Hangars": {{Key: "h1"}}},
			want:        Writes{Rows: map[accesstypes.Resource][]resource.RowChange{"Hangars": {{Key: "h1"}}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, tt.derivations.Expand(tt.touched)); diff != "" {
				t.Errorf("Expand() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFanout_derived(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name        string
		subs        []Subscription
		domain      accesstypes.Domain
		touched     map[accesstypes.Resource][]resource.RowChange
		want        Changes
		wantLookups int
	}{
		{
			name: "a root change reaches the derived row under the same key and the derived list in the domain, as the table's own do",
			subs: []Subscription{
				{Principal: "harbormaster", Tab: "t1", Resource: "FleetBoards", Key: "s1"},
				{Principal: "watch", Tab: "t2", Resource: "FleetBoards", Domain: "anvil"},
				{Principal: "pilot", Tab: "t3", Resource: "FleetBoards", Domain: "bastion"},
			},
			domain:  "anvil",
			touched: map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1"}}},
			want: Changes{
				"harbormaster": {{Kind: RowChange, Resource: "FleetBoards", Key: "s1"}},
				"watch":        {{Kind: ListChange, Resource: "FleetBoards", Domain: "anvil"}},
			},
			wantLookups: 4,
		},
		{
			name: "a change through another table reaches every subscriber of the derived resource in the domain as one resource document, with one lookup",
			subs: []Subscription{
				{Principal: "dispatcher", Tab: "t1", Resource: "MissionBoards", Key: "m1"},
				{Principal: "watch", Tab: "t2", Resource: "MissionBoards", Domain: "anvil"},
				{Principal: "pilot", Tab: "t3", Resource: "MissionBoards", Domain: "bastion"},
			},
			domain:  "anvil",
			touched: map[accesstypes.Resource][]resource.RowChange{"Squadrons": {{Key: "q1"}}},
			want: Changes{
				"dispatcher": {{Kind: ResourceChange, Resource: "MissionBoards"}},
				"watch":      {{Kind: ResourceChange, Resource: "MissionBoards"}},
			},
			wantLookups: 3,
		},
		{
			name: "a global table's change reaches the derived lists of every domain",
			subs: []Subscription{
				{Principal: "watch", Tab: "t2", Resource: "FleetBoards", Domain: "anvil"},
				{Principal: "pilot", Tab: "t3", Resource: "FleetBoards", Domain: "bastion"},
			},
			domain:  "",
			touched: map[accesstypes.Resource][]resource.RowChange{"ShipClasses": {{Key: "c1"}}},
			want: Changes{
				"watch": {{Kind: ResourceChange, Resource: "FleetBoards"}},
				"pilot": {{Kind: ResourceChange, Resource: "FleetBoards"}},
			},
			wantLookups: 3,
		},
		{
			name: "a user watching the table and the derived resource gets each distinct document once",
			subs: []Subscription{
				{Principal: "watch", Tab: "t1", Resource: "Ships", Domain: "anvil"},
				{Principal: "watch", Tab: "t1", Resource: "FleetBoards", Domain: "anvil"},
				{Principal: "watch", Tab: "t2", Resource: "FleetBoards", Domain: "anvil"},
			},
			domain:  "anvil",
			touched: map[accesstypes.Resource][]resource.RowChange{"Ships": {{Key: "s1"}}},
			want: Changes{"watch": {
				{Kind: ListChange, Resource: "FleetBoards", Domain: "anvil"},
				{Kind: ListChange, Resource: "Ships", Domain: "anvil"},
			}},
			wantLookups: 4,
		},
		{
			name:        "nobody watching the derived resource: nothing beyond the table's own",
			subs:        []Subscription{{Principal: "clerk", Tab: "t1", Resource: "Squadrons", Domain: "anvil"}},
			domain:      "anvil",
			touched:     map[accesstypes.Resource][]resource.RowChange{"Squadrons": {{Key: "q1"}}},
			want:        Changes{"clerk": {{Kind: ListChange, Resource: "Squadrons", Domain: "anvil"}}},
			wantLookups: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := subscribed(t, now, tt.subs...)
			got, err := Fanout(t.Context(), fake, tt.domain, fixtureDerivations().Expand(tt.touched))
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
