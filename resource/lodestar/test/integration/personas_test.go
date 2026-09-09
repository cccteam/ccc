// Demonstrates: cell-masking, pii, tenancy.concealed, login-manifest.
package integration

// The persona-view suite (design plan §4): each persona's first screen, derived from
// the §7 grants read against the seed — non-empty where it should be, empty where it
// should be, concealed where they hold nothing.

import (
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestDemoPersonaViews(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	tests := []struct {
		name       string
		user       accesstypes.User
		target     string
		wantStatus int
		wantRows   int
		check      func(t *testing.T, respBody []byte)
	}{
		{name: "the governor sees every Anvil mission", user: "governor", target: sectorPath(anvil, "missions?limit=200"), wantStatus: http.StatusOK, wantRows: 30},
		{name: "the governor's first page is the descriptor's default of twenty-five", user: "governor", target: sectorPath(anvil, "missions"), wantStatus: http.StatusOK, wantRows: 25},
		{name: "the governor sees Cinder", user: "governor", target: sectorPath(cinder, "missions"), wantStatus: http.StatusOK, wantRows: 1},
		{name: "the marshal sees every Anvil mission", user: "marshal", target: sectorPath(anvil, "missions?limit=200"), wantStatus: http.StatusOK, wantRows: 30},
		{name: "the marshal's second sector is concealed", user: "marshal", target: sectorPath(bastion, "missions"), wantStatus: http.StatusNotFound},
		{name: "the cadet's board is the two low-hazard classes", user: "cadet", target: sectorPath(anvil, "missions?limit=200"), wantStatus: http.StatusOK, wantRows: 14},
		{
			name: "the archivist's fee is redacted until a mission completed",
			user: "archivist", target: sectorPath(anvil, "missions?limit=200"), wantStatus: http.StatusOK, wantRows: 7,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				for _, row := range decodeRows(t, respBody) {
					_, hasFee := row["fee"]
					_, hasSettlement := row["settlement"]
					completed := row["statusId"] == "completed"
					if hasFee != completed || hasSettlement != completed {
						t.Errorf("mission %v: fee %v settlement %v visible, want both = %v", row["id"], hasFee, hasSettlement, completed)
					}
				}
			},
		},
		{
			name: "the archivist's distress calls carry no caller contact",
			user: "archivist", target: sectorPath(anvil, "distress-calls?limit=100"), wantStatus: http.StatusOK, wantRows: 12,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				for _, row := range decodeRows(t, respBody) {
					if _, ok := row["callerContact"]; ok {
						t.Errorf("callerContact visible to the archivist: %v", row)
					}
				}
			},
		},
		{name: "the engineer runs the hangar deck", user: "engineer", target: sectorPath(anvil, "refits?limit=100"), wantStatus: http.StatusOK, wantRows: 9},
		{name: "the quartermaster sees every expense", user: "quartermaster", target: sectorPath(anvil, "sortie-expenses?limit=100"), wantStatus: http.StatusOK, wantRows: 12},
		{name: "the supercargo's hold", user: "supercargo", target: sectorPath(anvil, "consignments?limit=100"), wantStatus: http.StatusOK, wantRows: 13},
		{name: "the supercargo's first page is the hold's default of ten", user: "supercargo", target: sectorPath(anvil, "consignments"), wantStatus: http.StatusOK, wantRows: 10},
		{name: "the hazard analyst's board, every row through limit=all", user: "hazards", target: sectorPath(anvil, "sector-hazard-boards?limit=all"), wantStatus: http.StatusOK, wantRows: 32},
		{name: "the cadet has no hangar deck", user: "cadet", target: sectorPath(anvil, "refits"), wantStatus: http.StatusForbidden},
		{name: "the client's grants answer by company wherever her session group binds them", user: "client", target: sectorPath(anvil, "missions?limit=200"), wantStatus: http.StatusOK, wantRows: 10},
		{name: "the bulletin officer issues a bulletin while the authorization holds", user: "marshal", target: "/api/issue-bulletin", wantStatus: http.StatusOK},
		{name: "a crew member without the authorization is refused", user: "cadet", target: "/api/issue-bulletin", wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			method, body := http.MethodGet, ""
			if tt.target == "/api/issue-bulletin" {
				method, body = http.MethodPost, `{"announcement":"All hands: drill at 0600"}`
			}
			status, respBody := doRequestAs(t, h, tt.user, method, tt.target, body)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", status, tt.wantStatus, respBody)
			}
			if tt.wantRows > 0 {
				if rows := decodeRows(t, respBody); len(rows) != tt.wantRows {
					t.Fatalf("rows = %d, want %d: %s", len(rows), tt.wantRows, respBody)
				}
			}
			if tt.check != nil {
				tt.check(t, respBody)
			}
		})
	}
}
