package integration

// language_test is the grammar's drift gate (design plan §9): one case per construct
// in §7, each asserting BOTH directions — a row the condition admits and a row it
// refuses — through the real engine and the shipped role config over the shipped
// seed. Every expectation below is derived by reading the §7 condition against the
// seed rows in harness_test.go, never from an observed response.

import (
	"net/http"
	"slices"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestConditionLanguage(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	tests := []struct {
		name      string
		construct string
		user      accesstypes.User
		target    string
		wantIDs   []string // exactly these rows, sorted
	}{
		{
			name:      "cadet: numeric IN list admits hazard 1 and 2 only",
			construct: "hazard IN (1, 2)",
			user:      "cadet",
			target:    sectorPath(anvil, "missions"),
			wantIDs:   []string{missionHaulerID, missionCourierID, missionTowID, missionQuarantineID},
		},
		{
			name:      "veteran: prefix NOT over a parenthesised OR drops routine, low-fee work",
			construct: "NOT (hazard IN (1, 2) OR fee < 5000)",
			user:      "veteran",
			target:    sectorPath(anvil, "missions"),
			wantIDs:   []string{missionCorvidID, missionConvoyID, missionPodID, missionBullionID},
		},
		{
			name:      "dispatcher: NOT IN over the terminal states",
			construct: "state NOT IN ('completed', 'failed', 'stood_down')",
			user:      "dispatcher",
			target:    sectorPath(anvil, "missions"),
			wantIDs:   []string{missionHaulerID, missionCorvidID, missionConvoyID, missionCourierID, missionQuarantineID},
		},
		{
			name:      "overseer: now as a right-side operand — the overdue desk",
			construct: "deadline < now AND state NOT IN (...)",
			user:      "overseer",
			target:    sectorPath(anvil, "missions"),
			wantIDs:   []string{missionQuarantineID}, // the Corvid deadline is bootstrap+3m, still ahead at test time
		},
		{
			name:      "booking: > over a decimal OR subject scalar",
			construct: "fee > 10000 OR bookedBy = subject",
			user:      "booking",
			target:    sectorPath(anvil, "missions"),
			wantIDs:   []string{missionHaulerID, missionCorvidID, missionConvoyID, missionCourierID, missionPodID, missionBullionID, missionQuarantineID},
		},
		{
			name:      ">= over an int",
			construct: "hazard >= 4",
			user:      "wingco",
			target:    sectorPath(anvil, "missions"),
			wantIDs:   []string{missionConvoyID, missionPodID},
		},
		{
			name:      "wingco: subject set with a dotted value path",
			construct: "wing IN subject.wings",
			user:      "wingco",
			target:    sectorPath(anvil, "squadrons"),
			wantIDs:   []string{squadronHammerID, squadronTongsID}, // both Forge Wing squadrons; Wilde flies with Hammer
		},
		{
			name:      "pilot: subject value threshold, IS NULL inside OR, global subject set",
			construct: "hazard <= subject.clearance AND (requiredCert IS NULL OR requiredCert IN subject.certifications)",
			user:      "pilot",
			target:    sectorPath(anvil, "missions"),
			// clearance 3, certs deep_space + salvage: hauler (2, none), corvid (3, salvage), courier (1, none),
			// tow (1, none), quarantine (2, none). Convoy (4), pod (5, hazmat), bullion (3, escort) fall out.
			wantIDs: []string{missionHaulerID, missionCorvidID, missionCourierID, missionTowID, missionQuarantineID},
		},
		{
			name:      "pilot: != on a one-hop join-path attribute",
			construct: "hangarZone != 'quarantine'",
			user:      "pilot",
			target:    sectorPath(anvil, "ships"),
			wantIDs:   []string{shipKingfisherID, shipStubbornMuleID, shipGoodSamaritanID, shipRustyAnchorID}, // the Lantern sits in Quarantine Bay
		},
		{
			name:      "lead: domain subject set OR subject scalar",
			construct: "assignedSquadron IN subject.squadrons OR bookedBy = subject",
			user:      "lead",
			target:    sectorPath(anvil, "missions"),
			wantIDs:   []string{missionCorvidID, missionConvoyID, missionPodID}, // Hammer's three; nothing booked by lead
		},
		{
			name:      "archivist: terminal-state row suppression on the root",
			construct: "state IN ('completed', 'failed', 'stood_down')",
			user:      "archivist",
			target:    sectorPath(anvil, "missions"),
			wantIDs:   []string{missionPodID, missionTowID, missionBullionID},
		},
		{
			name:      "archivist: the same text one hop down on sorties",
			construct: "state IN (...) on a member",
			user:      "archivist",
			target:    sectorPath(anvil, "sorties"),
			wantIDs:   []string{sortiePodID, sortieTowID},
		},
		{
			name:      "archivist: the same text two hops down on sortie expenses",
			construct: "state IN (...) two hops deep",
			user:      "archivist",
			target:    sectorPath(anvil, "sortie-expenses"),
			wantIDs:   []string{expensePodTowGearID},
		},
		{
			name:      "cadet: subject scalar on distress calls",
			construct: "filedBy = subject",
			user:      "cadet",
			target:    sectorPath(anvil, "distress-calls"),
			wantIDs:   []string{callDebrisID},
		},
		{
			name:      "hazard analyst: row-free now on a computed resource admits the board today",
			construct: "now < '2027-01-01T00:00:00Z'",
			user:      "hazards",
			target:    sectorPath(anvil, "sector-hazard-boards"),
			wantIDs:   nil, // the board keys are compound; presence is asserted below
		},
		{
			name:      "client browser: bool attribute",
			construct: "trusted = true",
			user:      "booking",
			target:    "/api/clients",
			wantIDs:   []string{clientHalvardID, clientMeridianID, clientVellumID},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, tt.target, "")
			assertStatus(t, status, http.StatusOK, body)
			rows := decodeRows(t, body)
			if tt.wantIDs == nil {
				if len(rows) == 0 {
					t.Fatalf("%s: no rows: %s", tt.construct, body)
				}

				return
			}
			want := slices.Clone(tt.wantIDs)
			slices.Sort(want)
			if got := idsOf(t, rows); !slices.Equal(got, want) {
				t.Errorf("%s: rows = %v, want %v", tt.construct, got, want)
			}
		})
	}
}

// TestConditionLanguageWrites pins the write-side constructs both ways: the insert
// image against a subject value, the post-image inside an Update, IS NULL / IS NOT
// NULL on updates, and the date literal on a delete.
func TestConditionLanguageWrites(t *testing.T) {
	t.Parallel()

	_, _, h := demoWorld(t)

	tests := []struct {
		name       string
		construct  string
		user       accesstypes.User
		body       string
		wantStatus int
	}{
		{
			name:       "booking: insert image within the fee limit is admitted",
			construct:  "new.fee <= subject.feeLimit",
			user:       "booking",
			body:       `[{"op":"add","path":"` + opPath(anvil, "missions") + `","value":{"clientId":"` + clientHalvardID + `","kindId":"courier","title":"Within limit","hazard":1,"fee":24000,"deadline":"2027-01-01T00:00:00Z"}}]`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "booking: insert image over the fee limit is refused",
			construct:  "new.fee <= subject.feeLimit (refused)",
			user:       "booking",
			body:       `[{"op":"add","path":"` + opPath(anvil, "missions") + `","value":{"clientId":"` + clientHalvardID + `","kindId":"courier","title":"Over limit","hazard":1,"fee":26000,"deadline":"2027-01-01T00:00:00Z"}}]`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "booking: post-image within the limit on an open mission is admitted",
			construct:  "state = 'open' AND new.fee <= subject.feeLimit",
			user:       "booking",
			body:       `[{"op":"patch","path":"` + opPath(anvil, "missions/"+missionHaulerID) + `","value":{"fee":9000}}]`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "booking: post-image over the limit is refused",
			construct:  "new.fee <= subject.feeLimit inside an Update (refused)",
			user:       "booking",
			body:       `[{"op":"patch","path":"` + opPath(anvil, "missions/"+missionHaulerID) + `","value":{"fee":30000}}]`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "booking: a fee change on a claimed mission is refused by the state term",
			construct:  "state = 'open' (refused)",
			user:       "booking",
			body:       `[{"op":"patch","path":"` + opPath(anvil, "missions/"+missionCorvidID) + `","value":{"fee":1000}}]`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "engineer: IS NOT NULL admits the estimate after inspection",
			construct:  "inspectedAt IS NOT NULL",
			user:       "engineer",
			body:       `[{"op":"patch","path":"` + opPath(anvil, "refits/"+refitMuleID) + `","value":{"estimate":12500}}]`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "engineer: IS NOT NULL refuses the estimate before inspection",
			construct:  "inspectedAt IS NOT NULL (refused)",
			user:       "engineer",
			body:       `[{"op":"patch","path":"` + opPath(anvil, "refits/"+refitLanternID) + `","value":{"estimate":1000}}]`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "supercargo: IS NULL admits a correction while in bond",
			construct:  "releasedAt IS NULL",
			user:       "supercargo",
			body:       `[{"op":"patch","path":"` + opPath(anvil, "consignments/"+consignmentPodID) + `","value":{"description":"Sealed cargo pod, medical supplies (recounted)"}}]`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "supercargo: IS NULL refuses a correction once released",
			construct:  "releasedAt IS NULL (refused)",
			user:       "supercargo",
			body:       `[{"op":"patch","path":"` + opPath(anvil, "consignments/"+consignmentBullionID) + `","value":{"description":"Too late"}}]`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "supercargo: the date literal admits disposing of expired bond",
			construct:  "expiresOn < '2026-09-01'",
			user:       "supercargo",
			body:       `[{"op":"remove","path":"` + opPath(anvil, "consignments/"+consignmentDronesID) + `"}]`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "supercargo: the date literal refuses disposing of live bond",
			construct:  "expiresOn < '2026-09-01' (refused)",
			user:       "supercargo",
			body:       `[{"op":"remove","path":"` + opPath(anvil, "consignments/"+consignmentPodID) + `"}]`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "booking: delete by base decision admits an open mission",
			construct:  "state = 'open' on Delete",
			user:       "booking",
			body:       `[{"op":"remove","path":"` + opPath(anvil, "missions/"+missionQuarantineID) + `"}]`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "booking: delete by base decision refuses a stood-down mission",
			construct:  "state = 'open' on Delete (refused)",
			user:       "booking",
			body:       `[{"op":"remove","path":"` + opPath(anvil, "missions/"+missionBullionID) + `"}]`,
			wantStatus: http.StatusForbidden,
		},
	}

	// The cases share seeded rows and run in order: the admitted writes above change
	// the rows later cases address (the hauler's fee, the pod's description).
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, body := doRequestAs(t, h, tt.user, http.MethodPatch, "/api/resources", tt.body)
			if status != tt.wantStatus {
				t.Fatalf("%s: status = %d, want %d: %s", tt.construct, status, tt.wantStatus, body)
			}
		})
	}
}
