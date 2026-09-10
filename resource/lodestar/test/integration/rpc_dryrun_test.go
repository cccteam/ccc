// Demonstrates: rpc.dry-run.
package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/session/sessioninfo"
)

// TestDryRun drives HoldMission and InspectShip under X-Dry-Run: the whole frame and
// the body run, then the transaction rolls back. A caller the real call would admit
// gets 200 with no body and nothing changes; a caller the real call would refuse gets
// the same refusal, here the Flight Lead's notes refusal from inside the transaction.
func TestDryRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		user       accesstypes.User
		method     string
		body       string
		wantStatus int
		// column and key name the row the dry run must leave as seeded.
		table, column string
		key           string
		wantRefusal   string
	}{
		{name: "the marshal's hold would commit", user: "marshal", method: "hold-mission", body: `{"missionId":%q,"reason":"debris"}`, wantStatus: http.StatusOK, table: "Missions", column: "StatusId", key: missionConvoyID},
		{name: "the flight lead's hold would be refused on the notes", user: "lead", method: "hold-mission", body: `{"missionId":%q,"reason":"debris"}`, wantStatus: http.StatusForbidden, table: "Missions", column: "StatusId", key: missionConvoyID, wantRefusal: "does not have (Update) on [Missions]"},
		{name: "an inspection would commit and answers with no body", user: "engineer", method: "inspect-ship", body: `{"refitId":%q}`, wantStatus: http.StatusOK, table: "Refits", column: "StatusId", key: refitLanternID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, db, h := demoWorld(t)
			before := readColumn[string](ctx, t, db, tt.table, spanner.Key{tt.key}, tt.column)

			status, body := doDryRunAs(t, h, tt.user, sectorPath(anvil, tt.method), fmt.Sprintf(tt.body, tt.key))
			assertStatus(t, status, tt.wantStatus, body)
			if tt.wantRefusal != "" && !strings.Contains(string(body), tt.wantRefusal) {
				t.Errorf("refusal = %s, want it to contain %q", body, tt.wantRefusal)
			}
			if status == http.StatusOK && strings.TrimSpace(string(body)) != "null" && strings.TrimSpace(string(body)) != "" {
				t.Errorf("a dry run that would succeed answers with no body, got %s", body)
			}
			if after := readColumn[string](ctx, t, db, tt.table, spanner.Key{tt.key}, tt.column); after != before {
				t.Errorf("%s.%s = %q after the dry run, want %q: nothing commits", tt.table, tt.column, after, before)
			}
		})
	}
}

// doDryRunAs posts the body under the dry-run header as the user.
func doDryRunAs(t *testing.T, h http.Handler, user accesstypes.User, target, body string) (statusCode int, respBody []byte) {
	t.Helper()

	sessionID, err := ccc.NewUUID()
	if err != nil {
		t.Fatalf("ccc.NewUUID: %v", err)
	}
	ctx := context.WithValue(t.Context(), sessioninfo.CtxSessionInfo, &sessioninfo.SessionData{
		SessionInfo: &sessioninfo.SessionInfo{ID: sessionID, Username: string(user)},
	})
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(resource.DryRunHeader, "true")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	return rr.Code, rr.Body.Bytes()
}
