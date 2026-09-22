// Demonstrates: condition.subject-scalar, condition.not-in, condition.prefix-not, @attribute.bool, @attribute.nullable-bool, @attribute.date, @attribute.decimal, @attribute.nullable-fk, @attribute.join-path, @subjectSet.domain, @subjectSet.global, @subjectSet.dotted-value, @subjectValue, @subjectValue.two-per-anchor, immutable, @attribute, @attribute.join-path-global, condition.now, execute-condition.
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
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
)

// missionIDs names seeded missions by ordinal.
func missionIDs(ns ...int) []string {
	ids := make([]string, 0, len(ns))
	for _, n := range ns {
		ids = append(ids, missionID(n))
	}

	return ids
}

func TestConditionLanguage(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	// The List grants this suite proves, each pinned to the roles file (conditions-proven).
	provesGrant(t, crew.RolesPath, "ClientBrowser", "List", "Clients", "trusted = true")
	provesGrant(t, crew.RolesPath, "SalvageDesk", "List", "Clients", "insured IS NULL OR insured = true")
	provesGrant(t, crew.RolesPath, "Cadet", "List", "Missions", "hazard IN (1, 2)")
	provesGrant(t, crew.RolesPath, "Cadet", "List", "DistressCalls", "filedBy = subject")
	provesGrant(t, crew.RolesPath, "Pilot", "List", "Missions", "hazard <= subject.clearance AND (requiredCert IS NULL OR requiredCert IN subject.certifications)")
	provesGrant(t, crew.RolesPath, "Pilot", "List", "Ships", "hangarZone != 'quarantine'")
	provesGrant(t, crew.RolesPath, "Veteran", "List", "Missions", "NOT (hazard IN (1, 2) OR fee < 5000)")
	provesGrant(t, crew.RolesPath, "FlightLead", "List", "Missions", "assignedSquadron IN subject.squadrons OR bookedBy = subject")
	provesGrant(t, crew.RolesPath, "Dispatcher", "List", "Missions", "state NOT IN ('completed', 'failed', 'stood_down')")
	provesGrant(t, crew.RolesPath, "Overseer", "List", "Missions", "deadline < now AND state NOT IN ('completed', 'failed', 'stood_down')")
	provesGrant(t, crew.RolesPath, "BookingAgent", "List", "Missions", "fee > 10000 OR bookedBy = subject")
	provesGrant(t, crew.RolesPath, "WingCommander", "List", "Missions", "hazard >= 4")
	provesGrant(t, crew.RolesPath, "WingCommander", "List", "Squadrons", "wing IN subject.wings")
	provesGrant(t, crew.RolesPath, "Archivist", "List", "Missions", "state IN ('completed', 'failed', 'stood_down')")
	provesGrant(t, crew.RolesPath, "Archivist", "List", "Sorties", "state IN ('completed', 'failed', 'stood_down')")
	provesGrant(t, crew.RolesPath, "Archivist", "List", "SortieExpenses", "state IN ('completed', 'failed', 'stood_down')")

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
			target:    sectorPath(anvil, "missions?limit=200"),
			wantIDs:   append([]string{missionHaulerID, missionCourierID, missionTowID, missionQuarantineID}, missionIDs(12, 13, 17, 18, 22, 23, 27, 28, 32, 33)...),
		},
		{
			name:      "veteran: prefix NOT over a parenthesised OR drops routine, low-fee work",
			construct: "NOT (hazard IN (1, 2) OR fee < 5000)",
			user:      "veteran",
			target:    sectorPath(anvil, "missions?limit=200"),
			wantIDs:   append([]string{missionCorvidID, missionConvoyID, missionPodID, missionBullionID}, missionIDs(14, 15, 16, 19, 24, 25, 26, 30, 31)...),
		},
		{
			name:      "dispatcher: NOT IN over the terminal states",
			construct: "state NOT IN ('completed', 'failed', 'stood_down')",
			user:      "dispatcher",
			target:    sectorPath(anvil, "missions?limit=200"),
			wantIDs:   append([]string{missionHaulerID, missionCorvidID, missionConvoyID, missionCourierID, missionQuarantineID}, missionIDs(12, 13, 14, 15, 16, 17, 18, 19, 21, 22, 23, 24, 26, 27, 28, 29, 31, 32)...),
		},
		{
			name:      "overseer: now as a right-side operand — the overdue desk",
			construct: "deadline < now AND state NOT IN (...)",
			user:      "overseer",
			target:    sectorPath(anvil, "missions?limit=200"),
			wantIDs:   []string{missionQuarantineID}, // the Corvid deadline is bootstrap+3m, still ahead at test time; the filler's open deadlines are weeks ahead of seed time
		},
		{
			name:      "booking: > over a decimal OR subject scalar",
			construct: "fee > 10000 OR bookedBy = subject",
			user:      "booking",
			target:    sectorPath(anvil, "missions?limit=200"),
			wantIDs:   append([]string{missionHaulerID, missionCorvidID, missionConvoyID, missionCourierID, missionPodID, missionBullionID, missionQuarantineID}, missionIDs(12, 15, 16, 17, 18, 19, 21, 22, 24, 25, 26, 27, 28, 29, 31, 32, 33)...),
		},
		{
			name:      ">= over an int",
			construct: "hazard >= 4",
			user:      "wingco",
			target:    sectorPath(anvil, "missions?limit=200"),
			wantIDs:   append([]string{missionConvoyID, missionPodID}, missionIDs(15, 16, 20, 21, 25, 26, 30, 31)...),
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
			target:    sectorPath(anvil, "missions?limit=200"),
			// clearance 3, certs deep_space + salvage: hauler (2, none), corvid (3, salvage), courier (1, none),
			// tow (1, none), quarantine (2, none). Convoy (4), pod (5, hazmat), bullion (3, escort) fall out,
			// as does every escort and every hazard 4 or 5 among the filler.
			wantIDs: append([]string{missionHaulerID, missionCorvidID, missionCourierID, missionTowID, missionQuarantineID}, missionIDs(12, 13, 17, 19, 23, 24, 27, 28, 29, 32, 33)...),
		},
		{
			name:      "pilot: != on a one-hop join-path attribute",
			construct: "hangarZone != 'quarantine'",
			user:      "pilot",
			target:    sectorPath(anvil, "ships"),
			wantIDs:   []string{shipKingfisherID, shipStubbornMuleID, shipGoodSamaritanID, shipRustyAnchorID, shipTinWhistleID, shipPatientHeronID, shipSecondChanceID, shipBrassCompassID}, // the Lantern and the Slow Boat sit in Quarantine Bay
		},
		{
			name:      "lead: domain subject set OR subject scalar",
			construct: "assignedSquadron IN subject.squadrons OR bookedBy = subject",
			user:      "lead",
			target:    sectorPath(anvil, "missions?limit=200"),
			wantIDs:   append([]string{missionCorvidID, missionConvoyID, missionPodID}, missionIDs(14, 18, 20, 22, 28)...), // Hammer's missions; nothing booked by lead
		},
		{
			name:      "archivist: terminal-state row suppression on the root",
			construct: "state IN ('completed', 'failed', 'stood_down')",
			user:      "archivist",
			target:    sectorPath(anvil, "missions?limit=200"),
			wantIDs:   append([]string{missionPodID, missionTowID, missionBullionID}, missionIDs(20, 25, 30, 33)...),
		},
		{
			name:      "archivist: the same text one hop down on sorties",
			construct: "state IN (...) on a member",
			user:      "archivist",
			target:    sectorPath(anvil, "sorties"),
			wantIDs:   []string{sortiePodID, sortieTowID, "90000000-0000-4000-8000-000000000006", "90000000-0000-4000-8000-000000000007", "90000000-0000-4000-8000-000000000008", "90000000-0000-4000-8000-000000000012"},
		},
		{
			name:      "archivist: the same text two hops down on sortie expenses",
			construct: "state IN (...) two hops deep",
			user:      "archivist",
			target:    sectorPath(anvil, "sortie-expenses"),
			wantIDs:   []string{expensePodTowGearID, "91000000-0000-4000-8000-000000000007", "91000000-0000-4000-8000-000000000008", "91000000-0000-4000-8000-000000000012"},
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
			construct: "now < '2099-01-01T00:00:00Z'",
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
		{
			name:      "salvor: IS NULL OR = true on a nullable bool admits the undecided outfits",
			construct: "insured IS NULL OR insured = true",
			user:      "salvor",
			target:    "/api/clients",
			wantIDs:   []string{clientHalvardID, clientMeridianID, clientBastionRelayID}, // Vellum's cover is refused (false); Bastion Relay is untrusted but undecided
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

// TestConditionLanguageReads pins the read side of the row conditions: for each role's
// conditional Read grant, a seeded row the condition admits answers and a row it refuses
// is not found, the concealment an unknown key gets, since the condition is folded into the
// row's own WHERE clause. The rows are the ones the List cases above pin, read one at a
// time; the archivist's second grant shows the fee on a completed mission alone.
func TestConditionLanguageReads(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	// The Read grants this suite proves, each pinned to the roles file (conditions-proven).
	provesGrant(t, crew.RolesPath, "Cadet", "Read", "Missions", "hazard IN (1, 2)")
	provesGrant(t, crew.RolesPath, "Cadet", "Read", "DistressCalls", "filedBy = subject")
	provesGrant(t, crew.RolesPath, "Pilot", "Read", "Missions", "hazard <= subject.clearance AND (requiredCert IS NULL OR requiredCert IN subject.certifications)")
	provesGrant(t, crew.RolesPath, "Pilot", "Read", "Ships", "hangarZone != 'quarantine'")
	provesGrant(t, crew.RolesPath, "Veteran", "Read", "Missions", "NOT (hazard IN (1, 2) OR fee < 5000)")
	provesGrant(t, crew.RolesPath, "FlightLead", "Read", "Missions", "assignedSquadron IN subject.squadrons OR bookedBy = subject")
	provesGrant(t, crew.RolesPath, "Dispatcher", "Read", "Missions", "state NOT IN ('completed', 'failed', 'stood_down')")
	provesGrant(t, crew.RolesPath, "Overseer", "Read", "Missions", "deadline < now AND state NOT IN ('completed', 'failed', 'stood_down')")
	provesGrant(t, crew.RolesPath, "BookingAgent", "Read", "Missions", "fee > 10000 OR bookedBy = subject")
	provesGrant(t, crew.RolesPath, "WingCommander", "Read", "Missions", "hazard >= 4")
	provesGrant(t, crew.RolesPath, "WingCommander", "Read", "Squadrons", "wing IN subject.wings")
	provesGrant(t, crew.RolesPath, "Archivist", "Read", "Missions", "state IN ('completed', 'failed', 'stood_down')")
	provesGrant(t, crew.RolesPath, "Archivist", "Read", "Missions", "state = 'completed'")
	provesGrant(t, crew.RolesPath, "Archivist", "Read", "Sorties", "state IN ('completed', 'failed', 'stood_down')")
	provesGrant(t, crew.RolesPath, "Archivist", "Read", "SortieExpenses", "state IN ('completed', 'failed', 'stood_down')")

	tests := []struct {
		name       string
		user       accesstypes.User
		target     string
		wantStatus int
		// present and absent name fields the row must and must not carry.
		present []string
		absent  []string
	}{
		{name: "cadet: a hazard-2 mission", user: "cadet", target: sectorPath(anvil, "missions/"+missionHaulerID), wantStatus: http.StatusOK},
		{name: "cadet: a hazard-3 mission is not found", user: "cadet", target: sectorPath(anvil, "missions/"+missionCorvidID), wantStatus: http.StatusNotFound},
		{name: "cadet: the call he filed", user: "cadet", target: sectorPath(anvil, "distress-calls/"+callDebrisID), wantStatus: http.StatusOK},
		{name: "cadet: the client's call is not found", user: "cadet", target: sectorPath(anvil, "distress-calls/"+callBeaconID), wantStatus: http.StatusNotFound},
		{name: "pilot: a mission within clearance needing no certificate", user: "pilot", target: sectorPath(anvil, "missions/"+missionHaulerID), wantStatus: http.StatusOK},
		{name: "pilot: the hazard-4 convoy is not found", user: "pilot", target: sectorPath(anvil, "missions/"+missionConvoyID), wantStatus: http.StatusNotFound},
		{name: "pilot: a docked ship", user: "pilot", target: sectorPath(anvil, "ships/"+shipKingfisherID), wantStatus: http.StatusOK},
		{name: "pilot: the quarantined Lantern is not found", user: "pilot", target: sectorPath(anvil, "ships/"+shipLanternID), wantStatus: http.StatusNotFound},
		{name: "veteran: a hazard-3, well-paid mission", user: "veteran", target: sectorPath(anvil, "missions/"+missionCorvidID), wantStatus: http.StatusOK},
		{name: "veteran: routine work is not found", user: "veteran", target: sectorPath(anvil, "missions/"+missionHaulerID), wantStatus: http.StatusNotFound},
		{name: "lead: own squadron's convoy", user: "lead", target: sectorPath(anvil, "missions/"+missionConvoyID), wantStatus: http.StatusOK},
		{name: "lead: another squadron's courier, booked by someone else, is not found", user: "lead", target: sectorPath(anvil, "missions/"+missionCourierID), wantStatus: http.StatusNotFound},
		{name: "dispatcher: a live mission", user: "dispatcher", target: sectorPath(anvil, "missions/"+missionHaulerID), wantStatus: http.StatusOK},
		{name: "dispatcher: a completed mission is not found", user: "dispatcher", target: sectorPath(anvil, "missions/"+missionPodID), wantStatus: http.StatusNotFound},
		{name: "overseer: the overdue courier", user: "overseer", target: sectorPath(anvil, "missions/"+missionQuarantineID), wantStatus: http.StatusOK},
		{name: "overseer: a mission still within its deadline is not found", user: "overseer", target: sectorPath(anvil, "missions/"+missionHaulerID), wantStatus: http.StatusNotFound},
		{name: "booking: a mission over the fee threshold", user: "booking", target: sectorPath(anvil, "missions/"+missionCorvidID), wantStatus: http.StatusOK},
		{name: "booking: a cheap mission someone else booked is not found", user: "booking", target: sectorPath(anvil, "missions/"+missionTowID), wantStatus: http.StatusNotFound},
		{name: "wingco: a hazard-4 mission", user: "wingco", target: sectorPath(anvil, "missions/"+missionConvoyID), wantStatus: http.StatusOK},
		{name: "wingco: a hazard-2 mission is not found", user: "wingco", target: sectorPath(anvil, "missions/"+missionHaulerID), wantStatus: http.StatusNotFound},
		// The seed has no Anvil squadron outside Forge Wing; the List case above pins the
		// exact set the condition admits.
		{name: "wingco: a squadron of the wing", user: "wingco", target: sectorPath(anvil, "squadrons/"+squadronHammerID), wantStatus: http.StatusOK},
		{name: "archivist: a completed mission carries its fee", user: "archivist", target: sectorPath(anvil, "missions/"+missionPodID), wantStatus: http.StatusOK, present: []string{"fee"}},
		{name: "archivist: a failed mission is read with the fee masked", user: "archivist", target: sectorPath(anvil, "missions/"+missionTowID), wantStatus: http.StatusOK, present: []string{"title"}, absent: []string{"fee"}},
		{name: "archivist: an open mission is not found", user: "archivist", target: sectorPath(anvil, "missions/"+missionHaulerID), wantStatus: http.StatusNotFound},
		{name: "archivist: a sortie of a completed mission", user: "archivist", target: sectorPath(anvil, "sorties/"+sortiePodID), wantStatus: http.StatusOK},
		{name: "archivist: a sortie of the underway convoy is not found", user: "archivist", target: sectorPath(anvil, "sorties/"+sortieConvoyID), wantStatus: http.StatusNotFound},
		{name: "archivist: an expense two hops under a completed mission", user: "archivist", target: sectorPath(anvil, "sortie-expenses/"+expensePodTowGearID), wantStatus: http.StatusOK},
		{name: "archivist: an expense under the underway convoy is not found", user: "archivist", target: sectorPath(anvil, "sortie-expenses/"+expenseConvoyFuelID), wantStatus: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, tt.target, "")
			assertStatus(t, status, tt.wantStatus, body)
			if status != http.StatusOK {
				return
			}
			row := decodeRow(t, body)
			for _, key := range tt.present {
				if _, ok := row[key]; !ok {
					t.Errorf("row lacks %s: %s", key, body)
				}
			}
			for _, key := range tt.absent {
				if _, ok := row[key]; ok {
					t.Errorf("row carries %s, which the grant masks here: %s", key, body)
				}
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

	// The write grants this suite proves, each pinned to the roles file (conditions-proven).
	provesGrant(t, crew.RolesPath, "BookingAgent", "Create", "Missions", "new.fee <= subject.feeLimit")
	provesGrant(t, crew.RolesPath, "BookingAgent", "Update", "Missions", "state = 'open' AND new.fee <= subject.feeLimit")
	provesGrant(t, crew.RolesPath, "BookingAgent", "Delete", "Missions", "state = 'open'")
	provesGrant(t, crew.RolesPath, "Engineer", "Update", "Refits", "inspectedAt IS NOT NULL")
	provesGrant(t, crew.RolesPath, "Engineer", "Update", "RefitTasks", "state = 'in_refit'")
	provesGrant(t, crew.RolesPath, "FlightLead", "Update", "Sorties", "state = 'underway'")
	provesGrant(t, crew.RolesPath, "Supercargo", "Update", "Consignments", "releasedAt IS NULL")
	provesGrant(t, crew.RolesPath, "Supercargo", "Delete", "Consignments", "expiresOn < '2026-09-01'")

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
			body:       `[{"op":"add","path":"` + opPath(anvil, "missions") + `","value":{"clientId":"` + clientHalvardID + `","kindId":"courier","title":"Within limit","hazard":1,"fee":24000,"deadline":"` + deadline(365) + `"}}]`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "booking: insert image over the fee limit is refused",
			construct:  "new.fee <= subject.feeLimit (refused)",
			user:       "booking",
			body:       `[{"op":"add","path":"` + opPath(anvil, "missions") + `","value":{"clientId":"` + clientHalvardID + `","kindId":"courier","title":"Over limit","hazard":1,"fee":26000,"deadline":"` + deadline(365) + `"}}]`,
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
		{
			name:       "lead: a sortie's state is its mission's, and underway admits the debrief",
			construct:  "state = 'underway' one hop up, on Update",
			user:       "lead",
			body:       `[{"op":"patch","path":"` + opPath(anvil, "sorties/"+sortieConvoyID) + `","value":{"debrief":"Convoy escorted without incident"}}]`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "lead: the held courier's sortie refuses the debrief",
			construct:  "state = 'underway' one hop up, on Update (refused)",
			user:       "lead",
			body:       `[{"op":"patch","path":"` + opPath(anvil, "sorties/"+sortieCourierID) + `","value":{"debrief":"Nothing to report"}}]`,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "engineer: a task of a ship in the refit bay is ticked done",
			construct:  "state = 'in_refit' on the task's refit, on Update",
			user:       "engineer",
			body:       `[{"op":"patch","path":"` + opPath(anvil, "refit-tasks/"+refitSamaritanID+"/2") + `","value":{"done":true}}]`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "engineer: a task of an inspected ship not yet in refit is refused",
			construct:  "state = 'in_refit' on the task's refit, on Update (refused)",
			user:       "engineer",
			body:       `[{"op":"patch","path":"` + opPath(anvil, "refit-tasks/"+refitMuleID+"/1") + `","value":{"done":true}}]`,
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
