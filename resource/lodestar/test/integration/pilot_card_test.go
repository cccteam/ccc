package integration

import (
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// TestPilotCardIsCallerScoped drives the caller-scoped read: PilotCards is a computed
// resource whose List yields the one row of the identity the permission check ran as
// (QuerySet.User), so two crew members asking the same route get two different single
// rows. The List grant still gates the route: a client outside the crew is refused
// before the function runs.
func TestPilotCardIsCallerScoped(t *testing.T) {
	t.Parallel()

	_, _, h := demoWorld(t)

	tests := []struct {
		name          string
		user          accesstypes.User
		wantStatus    int
		wantRows      int
		wantClearance float64
		wantSquadrons []any
	}{
		{name: "the pilot sees the pilot's card", user: "pilot", wantStatus: http.StatusOK, wantRows: 1, wantClearance: 3},
		{name: "the flight lead sees the lead's card with Hammer", user: "lead", wantStatus: http.StatusOK, wantRows: 1, wantClearance: -1, wantSquadrons: []any{"Hammer"}},
		{name: "a client outside the crew is refused by the grant", user: "client", wantStatus: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, "/api/pilot-cards", "")
			assertStatus(t, status, tt.wantStatus, body)
			if status != http.StatusOK {
				return
			}
			rows := decodeRows(t, body)
			if len(rows) != tt.wantRows {
				t.Fatalf("rows = %d, want %d: %s", len(rows), tt.wantRows, body)
			}
			row := rows[0]
			if got := row["userId"]; got != string(tt.user) {
				t.Errorf("userId = %v, want %v", got, tt.user)
			}
			if tt.wantClearance >= 0 && row["clearance"] != tt.wantClearance {
				t.Errorf("clearance = %v, want %v", row["clearance"], tt.wantClearance)
			}
			if tt.wantSquadrons != nil {
				got, _ := row["squadrons"].([]any)
				if len(got) != len(tt.wantSquadrons) {
					t.Fatalf("squadrons = %v, want %v", row["squadrons"], tt.wantSquadrons)
				}
				for i, want := range tt.wantSquadrons {
					if got[i] != want {
						t.Errorf("squadrons[%d] = %v, want %v", i, got[i], want)
					}
				}
			}
		})
	}
}
