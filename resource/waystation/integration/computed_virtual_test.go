package integration

// This suite pins the read-only resource shapes over the demo world: computed
// resources (hand-written query logic behind generated routes, single and compound
// keys) and virtual resources (embedded subqueries, including a WITH clause with a
// named parameter, and the domain-scoped variant).

import (
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestComputedResources(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	fleet := accesstypes.Resource("FleetSummaries")
	board := accesstypes.Resource("StationStatusBoards")
	h := newTestApp(db, grants{
		accesstypes.List: {
			fleet,
			fieldResource(fleet, "name"),
			fieldResource(fleet, "openWorkOrders"),
			fieldResource(fleet, "pendingRequisitions"),
			board,
			fieldResource(board, "facilityName"),
			fieldResource(board, "latestReading"),
			fieldResource(board, "recordedAt"),
		},
		accesstypes.Read: {
			board,
			fieldResource(board, "facilityName"),
			fieldResource(board, "latestReading"),
		},
	})

	tests := []struct {
		name     string
		target   string
		wantRows int // list-row count, asserted when > 0
		check    func(t *testing.T, respBody []byte)
	}{
		{
			// FleetSummary aggregates the seeded world: ws-alpha has three open work
			// orders (in_progress, scheduled, draft; the completed one does not count)
			// and two requisitions awaiting approval.
			name:   "the fleet summary aggregates the seeded world",
			target: "/api/fleet-summaries",
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				alpha := rowsByID(t, decodeRows(t, respBody), "waystationId")[wsAlpha]
				if alpha == nil {
					t.Fatalf("no ws-alpha summary: %s", respBody)
				}
				if got := alpha["openWorkOrders"]; got != float64(3) {
					t.Errorf("ws-alpha openWorkOrders = %v, want 3", got)
				}
				if got := alpha["pendingRequisitions"]; got != float64(2) {
					t.Errorf("ws-alpha pendingRequisitions = %v, want 2", got)
				}
			},
		},
		{
			name:     "the status board folds readings to the latest per facility and metric",
			target:   "/api/waystations/ws-alpha/station-status-boards",
			wantRows: 3, // two alpha pairs + one beta
		},
		{
			name:   "the compound-key read route addresses one facility and metric pair",
			target: "/api/waystations/ws-alpha/station-status-boards/" + facilityReactorCtlID + "/coolant_temp",
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				if got := decodeRow(t, respBody)["latestReading"]; got != float64(91.2) {
					t.Errorf("latest coolant_temp = %v, want the newest reading 91.2", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequest(t, h, http.MethodGet, tt.target, "")
			assertStatus(t, status, http.StatusOK, body)
			if tt.wantRows > 0 {
				if rows := decodeRows(t, body); len(rows) != tt.wantRows {
					t.Errorf("rows = %d, want %d: %s", len(rows), tt.wantRows, body)
				}
			}
			if tt.check != nil {
				tt.check(t, body)
			}
		})
	}
}

func TestVirtualResources(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	spend := accesstypes.Resource("SpendByCategories")
	open := accesstypes.Resource("OpenWorkOrdersByTeams")
	h := newTestApp(db, grants{accesstypes.List: {
		spend,
		fieldResource(spend, "lineCount"),
		fieldResource(spend, "totalQuantity"),
		fieldResource(spend, "totalSpend"),
		open,
		fieldResource(open, "teamName"),
		fieldResource(open, "waystationId"),
		fieldResource(open, "openOrders"),
	}})

	tests := []struct {
		name     string
		target   string
		wantRows int // list-row count, asserted when > 0
		check    func(t *testing.T, respBody []byte)
	}{
		{
			// SpendByCategory runs the WITH-clause subquery with its named parameter:
			// only the approved requisition's line (one plasma torch) qualifies in the
			// seed.
			name:     "the WITH-clause subquery folds approved spend by category",
			target:   "/api/spend-by-categories",
			wantRows: 1,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				rows := decodeRows(t, respBody)
				if got := rows[0]["category"]; got != "tool" {
					t.Errorf("category = %v, want tool", got)
				}
				if got := rows[0]["totalSpend"]; got != "445.25" {
					t.Errorf("totalSpend = %v, want 445.25", got)
				}
			},
		},
		{
			// The domain-scoped virtual serves under the waystation segment and is
			// checked in that partition.
			name:     "the domain-scoped virtual serves under the waystation segment",
			target:   "/api/waystations/ws-alpha/open-work-orders-by-teams",
			wantRows: 3, // teams with open orders
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequest(t, h, http.MethodGet, tt.target, "")
			assertStatus(t, status, http.StatusOK, body)
			if tt.wantRows > 0 {
				if rows := decodeRows(t, body); len(rows) != tt.wantRows {
					t.Errorf("rows = %d, want %d: %s", len(rows), tt.wantRows, body)
				}
			}
			if tt.check != nil {
				tt.check(t, body)
			}
		})
	}
}
