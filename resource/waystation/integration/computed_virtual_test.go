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

	// FleetSummary aggregates the seeded world: ws-alpha has three open work orders
	// (in_progress, scheduled, draft; the completed one does not count) and two
	// requisitions awaiting approval.
	status, body := doRequest(t, h, http.MethodGet, "/api/fleet-summaries", "")
	assertStatus(t, status, http.StatusOK, body)
	summaries := rowsByID(t, decodeRows(t, body), "waystationId")
	alpha := summaries[wsAlpha]
	if alpha == nil {
		t.Fatalf("no ws-alpha summary: %s", body)
	}
	if got := alpha["openWorkOrders"]; got != float64(3) {
		t.Errorf("ws-alpha openWorkOrders = %v, want 3", got)
	}
	if got := alpha["pendingRequisitions"]; got != float64(2) {
		t.Errorf("ws-alpha pendingRequisitions = %v, want 2", got)
	}

	// StationStatusBoard folds sensor readings to the latest per facility+metric.
	status, body = doRequest(t, h, http.MethodGet, "/api/waystations/ws-alpha/station-status-boards", "")
	assertStatus(t, status, http.StatusOK, body)
	if rows := decodeRows(t, body); len(rows) != 3 {
		t.Errorf("status board rows = %d, want 3 (two alpha pairs + one beta)", len(rows))
	}

	// The compound-key read route addresses one facility+metric pair.
	status, body = doRequest(t, h, http.MethodGet, "/api/waystations/ws-alpha/station-status-boards/"+facilityReactorCtlID+"/coolant_temp", "")
	assertStatus(t, status, http.StatusOK, body)
	row := decodeRow(t, body)
	if got := row["latestReading"]; got != float64(91.2) {
		t.Errorf("latest coolant_temp = %v, want the newest reading 91.2", got)
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

	// SpendByCategory runs the WITH-clause subquery with its named parameter: only
	// the approved requisition's line (one plasma torch) qualifies in the seed.
	status, body := doRequest(t, h, http.MethodGet, "/api/spend-by-categories", "")
	assertStatus(t, status, http.StatusOK, body)
	rows := decodeRows(t, body)
	if len(rows) != 1 {
		t.Fatalf("spend rows = %d, want 1 (only the approved requisition qualifies): %s", len(rows), body)
	}
	if got := rows[0]["category"]; got != "tool" {
		t.Errorf("category = %v, want tool", got)
	}
	if got := rows[0]["totalSpend"]; got != "445.25" {
		t.Errorf("totalSpend = %v, want 445.25", got)
	}

	// The domain-scoped virtual serves under the waystation segment and is checked in
	// that partition.
	status, body = doRequest(t, h, http.MethodGet, "/api/waystations/ws-alpha/open-work-orders-by-teams", "")
	assertStatus(t, status, http.StatusOK, body)
	if rows := decodeRows(t, body); len(rows) != 3 {
		t.Errorf("open-by-team rows = %d, want 3 teams with open orders", len(rows))
	}
}
