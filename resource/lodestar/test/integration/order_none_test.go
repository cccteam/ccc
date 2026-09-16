package integration

// This suite pins the rule that a list with no @order and no request sort is not
// sorted. The hull catalog (ShipClasses) is the one listed table without an @order: a
// first page smaller than the catalog answers Page-More and no Link, the default page
// that fits every hull answers neither, limit=all answers every hull since the catalog
// declares no maximum (the ship form's class picker reads it whole), and a requested sort
// restores the cursor. The briefing template catalog
// (BriefingTemplates) is the computed resource without one: its rows arrive in the
// sequence the catalog keeps, the standard sheet first, which is neither name order nor
// key order; a page keeps that sequence with Page-More and no cursor, and a requested
// sort still sorts and pages it.
//
// Demonstrates: order.none.

import (
	"maps"
	"net/http"
	"slices"
	"testing"

	"github.com/cccteam/ccc/resource"
)

func TestOrderNone_unsortedLists(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	// The marshal holds CrewCommon (List on ShipClasses) and BriefingClerk (List on
	// BriefingTemplates), so one persona reads both catalogs.
	tests := []struct {
		name     string
		target   string
		wantRows int
		// wantIDs pins the rows and their order; nil accepts any order.
		wantIDs  []string
		wantMore bool
		wantRels []string
	}{
		{
			name:     "the hull catalog's first page of two answers Page-More and no cursor",
			target:   "/api/ship-classes?limit=2",
			wantRows: 2,
			wantMore: true,
		},
		{
			name:     "the hull catalog's default page fits every hull: no Page-More, no cursor",
			target:   "/api/ship-classes",
			wantRows: 4,
		},
		{
			name:     "limit=all answers every hull, the catalog declaring no maximum, and writes nothing",
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
			name:     "the briefing catalog lists in its own sequence, the standard sheet first, neither by name nor by key",
			target:   "/api/briefing-templates",
			wantRows: 4,
			wantIDs:  []string{"standard", "hazard-first", "client-facing", "dispatch"},
		},
		{
			name:     "a page of the briefing catalog keeps that sequence: Page-More and no cursor",
			target:   "/api/briefing-templates?limit=2",
			wantRows: 2,
			wantIDs:  []string{"standard", "hazard-first"},
			wantMore: true,
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
			if got := rec.Header().Get(resource.PageMoreHeader) == "true"; got != tt.wantMore {
				t.Errorf("Page-More = %v, want %v", got, tt.wantMore)
			}
			rels := slices.Sorted(maps.Keys(linkRelations(t, rec.Header().Get(resource.LinkHeader))))
			if !slices.Equal(rels, tt.wantRels) {
				t.Errorf("Link relations = %v, want %v", rels, tt.wantRels)
			}
		})
	}
}
