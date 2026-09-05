package integration

// multifrom_test (design plan §9): Stand Down succeeds from open, claimed, and on_hold
// and is refused from underway with the transition named; Scrap likewise from its
// three sources and refused from docked; FailFlightTest's backward edge round-trips
// (workflow_test walks it).

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
)

func TestMultiFromTransitions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestApp(db, workflowGrants())

	tests := []struct {
		name       string
		route      string
		body       string
		table      string
		key        string
		wantStatus int
		wantState  string
	}{
		{
			name:       "stand down from open",
			route:      "stand-down-mission",
			body:       fmt.Sprintf(`{"missionId":%q}`, missionHaulerID),
			table:      "Missions",
			key:        missionHaulerID,
			wantStatus: http.StatusOK,
			wantState:  "stood_down",
		},
		{
			name:       "stand down from claimed",
			route:      "stand-down-mission",
			body:       fmt.Sprintf(`{"missionId":%q}`, missionCorvidID),
			table:      "Missions",
			key:        missionCorvidID,
			wantStatus: http.StatusOK,
			wantState:  "stood_down",
		},
		{
			name:       "stand down from on_hold",
			route:      "stand-down-mission",
			body:       fmt.Sprintf(`{"missionId":%q}`, missionCourierID),
			table:      "Missions",
			key:        missionCourierID,
			wantStatus: http.StatusOK,
			wantState:  "stood_down",
		},
		{
			name:       "stand down from underway is refused — hold it first",
			route:      "stand-down-mission",
			body:       fmt.Sprintf(`{"missionId":%q}`, missionConvoyID),
			table:      "Missions",
			key:        missionConvoyID,
			wantStatus: http.StatusForbidden,
			wantState:  "underway",
		},
		{
			name:       "scrap from inspected",
			route:      "scrap-ship",
			body:       fmt.Sprintf(`{"refitId":%q}`, refitMuleID),
			table:      "Refits",
			key:        refitMuleID,
			wantStatus: http.StatusOK,
			wantState:  "scrapped",
		},
		{
			name:       "scrap from in_refit",
			route:      "scrap-ship",
			body:       fmt.Sprintf(`{"refitId":%q}`, refitSamaritanID),
			table:      "Refits",
			key:        refitSamaritanID,
			wantStatus: http.StatusOK,
			wantState:  "scrapped",
		},
		{
			name:       "scrap from flight_test",
			route:      "scrap-ship",
			body:       fmt.Sprintf(`{"refitId":%q}`, refitBastionWatchID),
			table:      "Refits",
			key:        refitBastionWatchID,
			wantStatus: http.StatusOK,
			wantState:  "scrapped",
		},
		{
			name:       "scrap from docked is refused — inspect it first",
			route:      "scrap-ship",
			body:       fmt.Sprintf(`{"refitId":%q}`, refitLanternID),
			table:      "Refits",
			key:        refitLanternID,
			wantStatus: http.StatusForbidden,
			wantState:  "docked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sector := anvil
			if tt.key == refitBastionWatchID {
				sector = bastion
			}
			status, body := doRequest(t, h, http.MethodPost, sectorPath(sector, tt.route), tt.body)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", status, tt.wantStatus, body)
			}
			if tt.wantStatus == http.StatusForbidden && !strings.Contains(string(body), "may not run against") {
				t.Errorf("refusal must name the transition: %s", body)
			}
			if got := readColumn[string](ctx, t, db, tt.table, spanner.Key{tt.key}, "StatusId"); got != tt.wantState {
				t.Errorf("StatusId = %q, want %q", got, tt.wantState)
			}
		})
	}
}
