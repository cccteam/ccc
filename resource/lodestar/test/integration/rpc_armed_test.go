package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
)

// TestArmedBodies drives the two methods whose bodies reach for the caller's
// permissions. HoldMission writes its reason as the caller: the same method records
// the note for the Sector Marshal (unconditional Update on notes), for the
// Dispatcher while the mission is live (a conditional Update, decided against the
// row in the transaction), and refuses the Flight Lead inside the transaction —
// Execute admitted the lead, the notes grant did not. CompleteMission reads the
// decision as data: it completes for everyone Execute admits and leaves a note only
// when the caller may update the notes.
func TestArmedBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		user       accesstypes.User
		method     string
		body       string
		wantStatus int
		// wantNote is a fragment the mission's notes must carry afterwards; empty
		// means the notes stay as seeded.
		wantNote string
		// wantRefusal is a fragment of the refusal message.
		wantRefusal string
	}{
		{name: "the marshal holds and the reason is recorded", user: "marshal", method: "hold-mission", body: `{"missionId":%q,"reason":"debris on the lane"}`, wantStatus: http.StatusOK, wantNote: "Hold: debris on the lane"},
		{name: "the dispatcher holds a live mission and the reason is recorded", user: "dispatcher", method: "hold-mission", body: `{"missionId":%q,"reason":"weather"}`, wantStatus: http.StatusOK, wantNote: "Hold: weather"},
		{name: "the flight lead may execute but not write the note", user: "lead", method: "hold-mission", body: `{"missionId":%q,"reason":"fuel"}`, wantStatus: http.StatusForbidden, wantRefusal: "user (lead) does not have (Update) on [Missions]"},
		{name: "the marshal completes and the decision grants the note", user: "marshal", method: "complete-mission", body: `{"missionId":%q}`, wantStatus: http.StatusOK, wantNote: "Completed:"},
		{name: "the flight lead completes without a note", user: "lead", method: "complete-mission", body: `{"missionId":%q}`, wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, db, h := demoWorld(t)
			before := readColumn[spanner.NullString](ctx, t, db, "Missions", spanner.Key{missionConvoyID}, "Notes")

			status, body := doRequestAs(t, h, tt.user, http.MethodPost, sectorPath(anvil, tt.method), fmt.Sprintf(tt.body, missionConvoyID))
			assertStatus(t, status, tt.wantStatus, body)
			if tt.wantRefusal != "" && !strings.Contains(string(body), tt.wantRefusal) {
				t.Errorf("refusal = %s, want it to name %q", body, tt.wantRefusal)
			}

			after := readColumn[spanner.NullString](ctx, t, db, "Missions", spanner.Key{missionConvoyID}, "Notes")
			switch {
			case tt.wantNote != "" && !strings.Contains(after.StringVal, tt.wantNote):
				t.Errorf("notes = %q, want a line containing %q", after.StringVal, tt.wantNote)
			case tt.wantNote == "" && after != before:
				t.Errorf("notes changed to %q, want them untouched (%q)", after.StringVal, before.StringVal)
			}
			if status != http.StatusOK {
				return
			}
			wantState := map[string]string{"hold-mission": "on_hold", "complete-mission": "completed"}[tt.method]
			if got := readColumn[string](ctx, t, db, "Missions", spanner.Key{missionConvoyID}, "StatusId"); got != wantState {
				t.Errorf("StatusId = %q, want %q", got, wantState)
			}
		})
	}
}
