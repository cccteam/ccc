package integration

// execute_conditions_test (design plan §9): for each conditional Execute grant in §7,
// the row's Execute list carries the method exactly when the condition holds; firing
// it on a row where it does not yields the uniform refusal naming method and row (no
// from-set or condition detail on the wire); HailShip and ReleaseConsignment prove the
// plain @target(Root) form, including NotFound for a cross-sector key.

import (
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestExecuteConditionsOnRows(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	// Each case: the persona's per-row Execute envelope over one list, pinned for the
	// rows the persona can see, followed by one refused fire on a row whose condition
	// (or from-set) fails and one admitted fire where both hold.
	envelopes := []struct {
		name   string
		user   accesstypes.User
		target string
		want   map[string][]any
	}{
		{
			// Cadet's ClaimMission grant carries hazard IN (1, 2): Claim lights on the
			// open low-hazard rows only (the courier is on hold, the tow failed).
			name:   "cadet: Claim lights per row by hazard and from-set",
			user:   "cadet",
			target: sectorPath(anvil, "missions?capabilities=Execute"),
			want: map[string][]any{
				missionHaulerID:     {"ClaimMission"},
				missionQuarantineID: {"ClaimMission"},
				missionCourierID:    {},
				missionTowID:        {},
			},
		},
		{
			// Pilot's grant carries the clearance and certification rule; the hauler
			// (hazard 2, no cert) is claimable, the Corvid is already claimed.
			name:   "pilot: Claim follows clearance and certification",
			user:   "pilot",
			target: sectorPath(anvil, "missions?capabilities=Execute"),
			want: map[string][]any{
				missionHaulerID:     {"ClaimMission"},
				missionQuarantineID: {"ClaimMission"},
				missionCorvidID:     {},
			},
		},
		{
			// Flight Lead holds Launch/Hold/Resume unconditionally and Complete/Fail
			// under assignedSquadron IN subject.squadrons: on Hammer's underway convoy
			// every edge from underway lights; on Hammer's claimed Corvid only Launch.
			name:   "lead: Complete and Fail light on own squadron's underway mission",
			user:   "lead",
			target: sectorPath(anvil, "missions?capabilities=Execute"),
			want: map[string][]any{
				missionConvoyID: {"CompleteMission", "FailMission", "HoldMission"},
				missionCorvidID: {"LaunchMission"},
				missionPodID:    {},
			},
		},
		{
			// Booking Agent's StandDown grant carries bookedBy = subject: it lights on
			// Bex's own open/claimed/on-hold bookings and never on the Marshal's convoy.
			name:   "booking: Stand Down lights on own bookings from the three sources",
			user:   "booking",
			target: sectorPath(anvil, "missions?capabilities=Execute"),
			want: map[string][]any{
				missionHaulerID:     {"StandDownMission"},
				missionCorvidID:     {"StandDownMission"},
				missionCourierID:    {"StandDownMission"},
				missionQuarantineID: {"StandDownMission"},
				missionConvoyID:     {},
				missionBullionID:    {},
			},
		},
		{
			// Pilot's HailShip grant carries hangarZone != 'quarantine' on the plain
			// @target(Ship) form; the Lantern is in Quarantine Bay and out of view.
			name:   "pilot: Hail lights on ships outside quarantine",
			user:   "pilot",
			target: sectorPath(anvil, "ships?capabilities=Execute"),
			want: map[string][]any{
				shipKingfisherID:   {"HailShip"},
				shipStubbornMuleID: {"HailShip"},
			},
		},
		{
			// Supercargo's ReleaseConsignment grant carries releasedAt IS NULL.
			name:   "supercargo: Release lights while in bond only",
			user:   "supercargo",
			target: sectorPath(anvil, "consignments?capabilities=Execute"),
			want: map[string][]any{
				consignmentPodID:     {"ReleaseConsignment"},
				consignmentDronesID:  {"ReleaseConsignment"},
				consignmentBullionID: {},
			},
		},
		{
			// The Marshal's grants are unconditional: every legal edge lights, the
			// contrast row to the Pilot's.
			name:   "marshal: every legal edge lights",
			user:   "marshal",
			target: sectorPath(anvil, "missions?capabilities=Execute"),
			want: map[string][]any{
				missionHaulerID:  {"ClaimMission", "StandDownMission"},
				missionCorvidID:  {"LaunchMission", "StandDownMission"},
				missionConvoyID:  {"CompleteMission", "FailMission", "HoldMission"},
				missionCourierID: {"ResumeMission", "StandDownMission"},
				missionPodID:     {},
			},
		},
		{
			name:   "marshal: Scrap lights from its three bays",
			user:   "marshal",
			target: sectorPath(anvil, "refits?capabilities=Execute"),
			want: map[string][]any{
				refitLanternID:     {"InspectShip"},
				refitMuleID:        {"BeginRefit", "ScrapShip"},
				refitSamaritanID:   {"ScrapShip", "StartFlightTest"},
				refitRustyAnchorID: {},
			},
		},
	}
	for _, tt := range envelopes {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, tt.target, "")
			assertStatus(t, status, http.StatusOK, body)
			seen := make(map[string]bool, len(tt.want))
			for _, row := range decodeRows(t, body) {
				id, _ := row["id"].(string)
				want, pinned := tt.want[id]
				if !pinned {
					continue
				}
				seen[id] = true
				caps, _ := row["zzCapabilities"].(map[string]any)
				got, _ := caps["Execute"].([]any)
				if got == nil {
					got = []any{}
				}
				slices.SortFunc(got, func(a, b any) int { return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)) })
				if !reflect.DeepEqual(got, want) {
					t.Errorf("row %s Execute = %v, want %v", id, got, want)
				}
			}
			for id := range tt.want {
				if !seen[id] {
					t.Errorf("row %s missing from the response", id)
				}
			}
		})
	}
}

// TestExecuteConditionsFired fires the same grants against rows on both sides of
// each condition; the refusals never name the reason.
func TestExecuteConditionsFired(t *testing.T) {
	t.Parallel()

	_, _, h := demoWorld(t)

	fires := []struct {
		name       string
		user       accesstypes.User
		sector     string
		route      string
		body       string
		wantStatus int
	}{
		{
			name:       "cadet claims a hazard-2 mission",
			user:       "cadet",
			sector:     anvil,
			route:      "claim-mission",
			body:       fmt.Sprintf(`{"missionId":%q,"squadronId":%q}`, missionQuarantineID, squadronTongsID),
			wantStatus: http.StatusOK,
		},
		{
			name:       "pilot is refused the hazard-4 convoy by the grant, not the body",
			user:       "pilot",
			sector:     anvil,
			route:      "claim-mission",
			body:       fmt.Sprintf(`{"missionId":%q,"squadronId":%q}`, missionConvoyID, squadronTongsID),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "booking may not stand down the marshal's booking",
			user:       "booking",
			sector:     anvil,
			route:      "stand-down-mission",
			body:       fmt.Sprintf(`{"missionId":%q}`, missionConvoyID),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "lead may not complete another squadron's mission",
			user:       "lead",
			sector:     anvil,
			route:      "complete-mission",
			body:       fmt.Sprintf(`{"missionId":%q}`, missionCourierID),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "pilot hails a docked ship (plain located-row form)",
			user:       "pilot",
			sector:     anvil,
			route:      "hail-ship",
			body:       fmt.Sprintf(`{"shipId":%q}`, shipKingfisherID),
			wantStatus: http.StatusOK,
		},
		{
			name:       "pilot may not hail the quarantined Lantern",
			user:       "pilot",
			sector:     anvil,
			route:      "hail-ship",
			body:       fmt.Sprintf(`{"shipId":%q}`, shipLanternID),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "a Bastion ship through Anvil's route is NotFound, never Forbidden",
			user:       "marshal",
			sector:     anvil,
			route:      "hail-ship",
			body:       fmt.Sprintf(`{"shipId":%q}`, shipBastionWatchID),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "supercargo releases a consignment once",
			user:       "supercargo",
			sector:     anvil,
			route:      "release-consignment",
			body:       fmt.Sprintf(`{"consignmentId":%q}`, consignmentPodID),
			wantStatus: http.StatusOK,
		},
		{
			name:       "the second release is the frame's uniform Forbidden",
			user:       "supercargo",
			sector:     anvil,
			route:      "release-consignment",
			body:       fmt.Sprintf(`{"consignmentId":%q}`, consignmentBullionID),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "a Bastion consignment through Anvil's route is NotFound",
			user:       "supercargo",
			sector:     anvil,
			route:      "release-consignment",
			body:       fmt.Sprintf(`{"consignmentId":%q}`, consignmentRelayID),
			wantStatus: http.StatusNotFound,
		},
	}
	for _, tt := range fires {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodPost, sectorPath(tt.sector, tt.route), tt.body)
			if status != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", status, tt.wantStatus, body)
			}
			if tt.wantStatus == http.StatusForbidden {
				if !strings.Contains(string(body), "may not run against") {
					t.Errorf("refusal must name method and row: %s", body)
				}
				for _, leak := range []string{"hazard", "subject", "IN (", "from", "condition"} {
					if strings.Contains(string(body), leak) {
						t.Errorf("refusal leaks %q: %s", leak, body)
					}
				}
			}
		})
	}
}
