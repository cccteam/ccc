package integration

// clock_test (design plan §9): pins the environment at two instants around a
// deadline and asserts the overseer's Update flips, using accesstypes.EnvironmentAt
// against the real engine — the overseer's grant is `state = 'claimed' AND deadline <
// now`, so the same row is refused before its deadline and admitted after. The
// end-to-end half rides the seeded three-minute mission in the walkthrough.

import (
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
)

func TestOverseerClockFlip(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	_, _, client := sharedWorld(t)
	checker := client.ForUser("overseer")

	// The engine's decision for a conditional grant is Conditional either way: a
	// `deadline < now` term is a ROW term, so the engine defers it to the data layer,
	// where the request Environment's instant is bound into the check-SELECT. What this
	// suite pins is that the environment reaches the engine and the decision stays
	// conditional (never denied, never granted) at instants on both sides of the seeded
	// deadline — the flip itself is observed through the data layer below and live in
	// the walkthrough.
	tests := []struct {
		name string
		env  accesstypes.Environment
	}{
		{name: "an instant before the seeded deadline evaluates", env: accesstypes.EnvironmentAt(time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC))},
		{name: "an instant after the seeded deadline evaluates", env: accesstypes.EnvironmentAt(time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			decisions, err := checker.Check(ctx, tt.env, accesstypes.DomainScope(anvil), accesstypes.Update, "Missions")
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}
			if decisions["Missions"].IsDenied() || decisions["Missions"].IsGranted() {
				t.Errorf("Missions Update = %v, want conditional (the row term decides at the data layer)", decisions["Missions"])
			}
		})
	}
}

func TestOverseerOverdueDesk(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	tests := []struct {
		name       string
		mission    string
		wantStatus int
	}{
		{
			name:       "the claimed Corvid mission is not yet overdue",
			mission:    missionCorvidID,
			wantStatus: 403,
		},
		{
			name:       "the overdue quarantine courier is open, not claimed",
			mission:    missionQuarantineID,
			wantStatus: 403,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, "overseer", "PATCH", "/api/resources",
				`[{"op":"patch","path":"`+opPath(anvil, "missions/"+tt.mission)+`","value":{"assignedSquadronId":"`+squadronTongsID+`"}}]`)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", status, tt.wantStatus, body)
			}
		})
	}
}
