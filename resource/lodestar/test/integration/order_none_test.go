package integration

// This suite pins the two halves of one rule: every paged list request carries an
// order, and only the whole list (limit=all) is served without one. The hull catalog
// (ShipClasses) is the one listed table without an @order and the briefing template
// catalog (BriefingTemplates) the computed resource without one; neither declares a
// maximum, so each is read whole by its picker. A bare GET or a limit on either without
// a sort is refused with a 400 naming the resource and the two ways out (order.required).
// limit=all answers every row unsorted, no Link header: the hulls in whatever order
// Spanner returns them, the sheets in the sequence the catalog keeps, the standard
// sheet first, which is neither name order nor key order (order.none). A requested sort
// pages either by cursor as any list.
//
// Demonstrates: order.none, order.required.

import (
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource"
)

func TestOrderRequired_pagedRequestsCarryAnOrder(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	// The marshal holds CrewCommon (List on ShipClasses) and BriefingClerk (List on
	// BriefingTemplates), so one persona reads both catalogs. Missions declares an
	// @order, so a sort-less page of it is the declared order, not a refusal.
	tests := []struct {
		name        string
		target      string
		wantStatus  int
		wantMessage string
	}{
		{
			name:        "a bare GET on the hull catalog is refused naming the resource and the two ways out",
			target:      "/api/ship-classes",
			wantStatus:  http.StatusBadRequest,
			wantMessage: "ShipClasses declares no order; add a sort, or ask limit=all",
		},
		{
			name:        "a limit on the hull catalog without a sort is refused the same way",
			target:      "/api/ship-classes?limit=2",
			wantStatus:  http.StatusBadRequest,
			wantMessage: "ShipClasses declares no order; add a sort, or ask limit=all",
		},
		{
			name:        "a filter alone is not an order",
			target:      "/api/ship-classes?filter=designation:eq:Corvid",
			wantStatus:  http.StatusBadRequest,
			wantMessage: "ShipClasses declares no order; add a sort, or ask limit=all",
		},
		{
			name:        "a bare GET on the briefing catalog, a computed resource, is refused too",
			target:      "/api/briefing-templates",
			wantStatus:  http.StatusBadRequest,
			wantMessage: "BriefingTemplates declares no order; add a sort, or ask limit=all",
		},
		{
			name:       "a requested sort pages the hull catalog",
			target:     "/api/ship-classes?sort=designation&limit=2",
			wantStatus: http.StatusOK,
		},
		{
			name:       "a declared order pages a sort-less request as before",
			target:     sectorPath(anvil, "missions?limit=3"),
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := doRequestRecordedAs(t, h, "marshal", http.MethodGet, tt.target, "")
			assertStatus(t, rec.Code, tt.wantStatus, rec.Body.Bytes())
			if tt.wantMessage != "" && !strings.Contains(rec.Body.String(), tt.wantMessage) {
				t.Errorf("body = %s, want it to contain %q", rec.Body.String(), tt.wantMessage)
			}
		})
	}
}

func TestOrderNone_wholeListsAreUnsorted(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	tests := []struct {
		name     string
		target   string
		wantRows int
		// wantIDs pins the rows and their order; nil accepts any order.
		wantIDs  []string
		wantRels []string
	}{
		{
			name:     "limit=all answers every hull, the catalog declaring no maximum, and writes no Link",
			target:   "/api/ship-classes?limit=all",
			wantRows: 4,
		},
		{
			name:     "a requested sort on the hull catalog pages by cursor",
			target:   "/api/ship-classes?sort=designation&limit=2",
			wantRows: 2,
			wantRels: []string{"next"},
		},
		{
			name:     "limit=all lists the briefing catalog in its own sequence, the standard sheet first, neither by name nor by key",
			target:   "/api/briefing-templates?limit=all",
			wantRows: 4,
			wantIDs:  []string{"standard", "hazard-first", "client-facing", "dispatch"},
		},
		{
			name:     "a requested sort orders the briefing catalog by name and pages it",
			target:   "/api/briefing-templates?sort=name&limit=2",
			wantRows: 2,
			wantIDs:  []string{"client-facing", "dispatch"},
			wantRels: []string{"next"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := doRequestRecordedAs(t, h, "marshal", http.MethodGet, tt.target, "")
			assertStatus(t, rec.Code, http.StatusOK, rec.Body.Bytes())
			rows := decodeRows(t, rec.Body.Bytes())
			if len(rows) != tt.wantRows {
				t.Fatalf("rows = %d, want %d", len(rows), tt.wantRows)
			}
			if tt.wantIDs != nil {
				got := make([]string, 0, len(rows))
				for _, row := range rows {
					got = append(got, cell[string](t, row, "id"))
				}
				if !slices.Equal(got, tt.wantIDs) {
					t.Errorf("rows in order = %v, want %v", got, tt.wantIDs)
				}
			}
			rels := slices.Sorted(maps.Keys(linkRelations(t, rec.Header().Get(resource.LinkHeader))))
			if !slices.Equal(rels, tt.wantRels) {
				t.Errorf("Link relations = %v, want %v", rels, tt.wantRels)
			}
		})
	}
}
