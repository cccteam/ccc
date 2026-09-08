package integration

// This suite pins the query contract on a computed resource: the generated handler
// applies the filter, sort, and page over the rows the List function yields, with the
// same headers a table page carries, while the List function pushes the ship-name
// filter into its own query. The hazard board declares @order(WorstReading desc) and
// allow_filter on shipName and subsystem; the demo seed holds three boards in Anvil
// (Stubborn Mule hull 0.90, Kingfisher hull 0.61, Kingfisher reactor 0.20).

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

func TestComputedQuery(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	listGrants := grants{accesstypes.List: withFields("SectorHazardBoards", "shipName", "subsystem", "worstReading", "recordedAt", "recent")}
	testApp := newTestApp(db, listGrants)

	boards := func(rows []map[string]any) []string {
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			out = append(out, fmt.Sprintf("%v/%v", row["shipName"], row["subsystem"]))
		}

		return out
	}

	tests := []struct {
		name       string
		target     string
		wantStatus int
		wantBoards []string
		wantTotal  string
		wantLink   bool
	}{
		{
			name:       "the declared order lists the worst reading first",
			target:     sectorPath(anvil, "sector-hazard-boards"),
			wantStatus: http.StatusOK,
			wantBoards: []string{"Stubborn Mule/hull", "Kingfisher/hull", "Kingfisher/reactor"},
		},
		{
			name:       "the ship-name filter, pushed into the List function's query",
			target:     sectorPath(anvil, "sector-hazard-boards?filter=shipName:eq:Kingfisher"),
			wantStatus: http.StatusOK,
			wantBoards: []string{"Kingfisher/hull", "Kingfisher/reactor"},
		},
		{
			name:       "a ship with no readings answers an empty list",
			target:     sectorPath(anvil, "sector-hazard-boards?filter=shipName:eq:Lantern"),
			wantStatus: http.StatusOK,
			wantBoards: []string{},
		},
		{
			name:       "a condition the body did not take, applied by the handler",
			target:     sectorPath(anvil, "sector-hazard-boards?filter=subsystem:eq:hull"),
			wantStatus: http.StatusOK,
			wantBoards: []string{"Stubborn Mule/hull", "Kingfisher/hull"},
		},
		{
			name:       "both: the body's and the handler's conditions together",
			target:     sectorPath(anvil, "sector-hazard-boards?filter=shipName:eq:Kingfisher,subsystem:eq:reactor"),
			wantStatus: http.StatusOK,
			wantBoards: []string{"Kingfisher/reactor"},
		},
		{
			name:       "a requested sort replaces the declared order",
			target:     sectorPath(anvil, "sector-hazard-boards?sort=worstReading"),
			wantStatus: http.StatusOK,
			wantBoards: []string{"Kingfisher/reactor", "Kingfisher/hull", "Stubborn Mule/hull"},
		},
		{
			name:       "a page with the count of every filtered row",
			target:     sectorPath(anvil, "sector-hazard-boards?limit=2&count=true"),
			wantStatus: http.StatusOK,
			wantBoards: []string{"Stubborn Mule/hull", "Kingfisher/hull"},
			wantTotal:  "3",
			wantLink:   true,
		},
		{
			name:       "a filter on a field without allow_filter is refused",
			target:     sectorPath(anvil, "sector-hazard-boards?filter=worstReading:gt:0.5"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "a sort into the nested field is refused",
			target:     sectorPath(anvil, "sector-hazard-boards?sort=recent"),
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rr := doRequestRecorded(t, testApp, http.MethodGet, tt.target)
			assertStatus(t, rr.Code, tt.wantStatus, rr.Body.Bytes())
			if tt.wantStatus != http.StatusOK {
				return
			}
			rows := decodeRows(t, rr.Body.Bytes())
			if got := boards(rows); len(got) != len(tt.wantBoards) || !equalStrings(got, tt.wantBoards) {
				t.Errorf("boards = %v, want %v", got, tt.wantBoards)
			}
			if got := rr.Header().Get(resource.TotalCountHeader); got != tt.wantTotal {
				t.Errorf("Total-Count = %q, want %q", got, tt.wantTotal)
			}
			if hasLink := rr.Header().Get(resource.LinkHeader) != ""; hasLink != tt.wantLink {
				t.Errorf("Link present = %v, want %v: %q", hasLink, tt.wantLink, rr.Header().Get(resource.LinkHeader))
			}
		})
	}

	t.Run("a walk through the board's pages, forward and back", func(t *testing.T) {
		t.Parallel()

		first := doRequestRecorded(t, testApp, http.MethodGet, sectorPath(anvil, "sector-hazard-boards?limit=1"))
		assertStatus(t, first.Code, http.StatusOK, first.Body.Bytes())
		if got := boards(decodeRows(t, first.Body.Bytes())); !equalStrings(got, []string{"Stubborn Mule/hull"}) {
			t.Fatalf("page 1 = %v", got)
		}
		next := linkRelations(t, first.Header().Get(resource.LinkHeader))["next"]
		if next == "" {
			t.Fatal("page 1 carries no next relation")
		}
		if u, err := url.Parse(next); err != nil || u.Query().Get("cursor") == "" {
			t.Fatalf("next = %q, want a cursor", next)
		}

		second := doRequestRecorded(t, testApp, http.MethodGet, next)
		assertStatus(t, second.Code, http.StatusOK, second.Body.Bytes())
		if got := boards(decodeRows(t, second.Body.Bytes())); !equalStrings(got, []string{"Kingfisher/hull"}) {
			t.Fatalf("page 2 = %v", got)
		}
		rels := linkRelations(t, second.Header().Get(resource.LinkHeader))
		third := doRequestRecorded(t, testApp, http.MethodGet, rels["next"])
		assertStatus(t, third.Code, http.StatusOK, third.Body.Bytes())
		if got := boards(decodeRows(t, third.Body.Bytes())); !equalStrings(got, []string{"Kingfisher/reactor"}) {
			t.Fatalf("page 3 = %v", got)
		}
		if _, ok := linkRelations(t, third.Header().Get(resource.LinkHeader))["next"]; ok {
			t.Error("page 3 carries a next relation")
		}

		back := doRequestRecorded(t, testApp, http.MethodGet, rels["prev"])
		assertStatus(t, back.Code, http.StatusOK, back.Body.Bytes())
		if got := boards(decodeRows(t, back.Body.Bytes())); !equalStrings(got, []string{"Stubborn Mule/hull"}) {
			t.Errorf("walking back = %v, want Stubborn Mule/hull", got)
		}
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
