// Demonstrates: @rowsOf, @rowsOf.same-row, @rowsOf.association, list.row-route.
package integration

// This suite pins the server side of the list pages over declared views. The view
// declares its backing table, a create goes into the table, and the new row shows up
// in the view on the next list because the view's SQL reads that table: each view lists
// as the personas the console walk uses and refuses the cadet, a membership created and
// deleted through SquadronMemberships appears in and leaves both views over it, and the
// generated metadata carries each view's rowsOf and the row routes' targets.

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

const pilotLeadID = "30000000-0000-4000-8000-000000000006"

// TestRowsOfViews lists each declared view as the personas the console walk uses: the
// marshal and the dispatcher hold List on all three, the cadet on none.
func TestRowsOfViews(t *testing.T) {
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
		{
			// Every Anvil mission, one row each, keyed by the mission's own id and
			// carrying what Missions lacks: the client's and the squadron's names.
			name: "the marshal lists the mission board of Anvil", user: "marshal",
			target: sectorPath(anvil, "mission-boards?columns=id,title,clientName,squadronName,daysLeft&limit=200"), wantStatus: http.StatusOK, wantRows: 30,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				rows := rowsByID(t, decodeRows(t, respBody), "id")
				if got := rows[missionHaulerID]["clientName"]; got != "Halvard Freight" {
					t.Errorf("hauler clientName = %v, want Halvard Freight", got)
				}
				if got := rows[missionConvoyID]["squadronName"]; got != "Hammer" {
					t.Errorf("convoy squadronName = %v, want Hammer", got)
				}
				if _, ok := rows[missionHaulerID]["daysLeft"].(float64); !ok {
					t.Errorf("hauler daysLeft = %v, want a day count computed in the SQL", rows[missionHaulerID]["daysLeft"])
				}
			},
		},
		{
			// A view lists and never reads: a board row opens the mission's own page.
			name: "the board has no read route", user: "marshal",
			target: sectorPath(anvil, "mission-boards/"+missionHaulerID), wantStatus: http.StatusNotFound,
		},
		{
			// The roster's key is SquadronMemberships' key under its own column names,
			// and the request names both, so a row can be deleted from the list.
			name: "the marshal lists Hammer's roster with the pilots' names and ids", user: "marshal",
			target: sectorPath(anvil, "squadron-rosters?filter=squadronId:eq:"+squadronHammerID+"&columns=squadronId,userId,squadronName,pilotName,pilotId"), wantStatus: http.StatusOK, wantRows: 4,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				rows := rowsByID(t, decodeRows(t, respBody), "userId")
				if got := rows["lead"]["pilotName"]; got != "Flight Lead Lior" {
					t.Errorf("lead pilotName = %v, want Flight Lead Lior", got)
				}
				if got := rows["lead"]["pilotId"]; got != pilotLeadID {
					t.Errorf("lead pilotId = %v, want %s", got, pilotLeadID)
				}
				if got := rows["lead"]["squadronId"]; got != squadronHammerID {
					t.Errorf("lead squadronId = %v, want %s", got, squadronHammerID)
				}
			},
		},
		{
			// The dispatcher flies with Hammer and Tongs at Anvil and Portcullis at
			// Bastion; the Anvil partition lists the two.
			name: "the marshal lists the dispatcher's assignments in Anvil", user: "marshal",
			target: sectorPath(anvil, "pilot-assignments?filter=userId:eq:dispatcher&columns=squadronId,userId,squadronName,wingName"), wantStatus: http.StatusOK, wantRows: 2,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				rows := rowsByID(t, decodeRows(t, respBody), "squadronId")
				if got := rows[squadronHammerID]["wingName"]; got != "Forge Wing" {
					t.Errorf("Hammer wingName = %v, want Forge Wing", got)
				}
				if got := rows[squadronTongsID]["squadronName"]; got != "Tongs" {
					t.Errorf("Tongs squadronName = %v, want Tongs", got)
				}
			},
		},
		{name: "the cadet holds no List on the board, so the page's request is refused", user: "cadet", target: sectorPath(anvil, "mission-boards?columns=id,title"), wantStatus: http.StatusForbidden},
		{name: "the cadet holds no List on the roster either", user: "cadet", target: sectorPath(anvil, "squadron-rosters?columns=squadronId,userId"), wantStatus: http.StatusForbidden},
		{name: "the dispatcher, who edits missions, lists the board", user: "dispatcher", target: sectorPath(anvil, "mission-boards?columns=id,title&limit=200"), wantStatus: http.StatusOK, wantRows: 30},
		{name: "the dispatcher lists the roster", user: "dispatcher", target: sectorPath(anvil, "squadron-rosters?columns=squadronId,userId,pilotName"), wantStatus: http.StatusOK, wantRows: 6},
		{name: "the booking agent, who books missions, lists the board", user: "booking", target: sectorPath(anvil, "mission-boards?columns=id,title&limit=200"), wantStatus: http.StatusOK, wantRows: 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodGet, tt.target, "")
			assertStatus(t, status, tt.wantStatus, body)
			if status != http.StatusOK {
				return
			}
			if tt.wantRows > 0 {
				if rows := decodeRows(t, body); len(rows) != tt.wantRows {
					t.Errorf("rows = %d, want %d: %s", len(rows), tt.wantRows, body)
				}
			}
			if tt.check != nil {
				tt.check(t, body)
			}
		})
	}
}

// TestRowsOfWrites creates and deletes a membership through the table and lists both
// views over it in between: the create goes into the table, and the new row shows up in
// the views on the next list because their SQL reads that table. The steps share one
// row and run in order.
func TestRowsOfWrites(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	h := newTestApp(db, grants{
		accesstypes.List: append(
			withFields("SquadronRosters", "squadronName", "pilotName", "pilotId"),
			withFields("PilotAssignments", "squadronName", "wingName")...,
		),
		accesstypes.Create: {"SquadronMemberships"},
		accesstypes.Delete: {"SquadronMemberships"},
	})

	membership := opPath(anvil, "squadron-memberships/"+squadronTongsID+"/cadet")
	roster := sectorPath(anvil, "squadron-rosters?filter=squadronId:eq:"+squadronTongsID+"&columns=squadronId,userId,pilotName")
	assignments := sectorPath(anvil, "pilot-assignments?filter=userId:eq:cadet&columns=squadronId,userId,squadronName,wingName")

	steps := []struct {
		name       string
		method     string
		target     string
		body       string
		wantStatus int
		wantRows   int
		check      func(t *testing.T, respBody []byte)
	}{
		{name: "Tongs' roster is the dispatcher and the pilot", method: http.MethodGet, target: roster, wantStatus: http.StatusOK, wantRows: 2},
		{name: "the cadet is assigned nowhere", method: http.MethodGet, target: assignments, wantStatus: http.StatusOK, wantRows: 0},
		{
			name: "a create goes into the table, its compound key in the operation's path", method: http.MethodPatch, target: "/api/resources",
			body: fmt.Sprintf(`[{"op":"add","path":%q,"value":{}}]`, membership), wantStatus: http.StatusOK,
		},
		{
			name: "the new row shows up in the roster on the next list", method: http.MethodGet, target: roster, wantStatus: http.StatusOK, wantRows: 3,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				if got := rowsByID(t, decodeRows(t, respBody), "userId")["cadet"]["pilotName"]; got != "Cadet Cass" {
					t.Errorf("cadet pilotName = %v, want Cadet Cass", got)
				}
			},
		},
		{
			name: "and in the pilot's assignments", method: http.MethodGet, target: assignments, wantStatus: http.StatusOK, wantRows: 1,
			check: func(t *testing.T, respBody []byte) {
				t.Helper()
				if got := decodeRows(t, respBody)[0]["squadronName"]; got != "Tongs" {
					t.Errorf("squadronName = %v, want Tongs", got)
				}
			},
		},
		{
			name: "a delete removes the row through the table, lifting the key the view carries", method: http.MethodPatch, target: "/api/resources",
			body: fmt.Sprintf(`[{"op":"remove","path":%q}]`, membership), wantStatus: http.StatusOK,
		},
		{name: "the roster is back to two", method: http.MethodGet, target: roster, wantStatus: http.StatusOK, wantRows: 2},
		{name: "and the cadet is assigned nowhere again", method: http.MethodGet, target: assignments, wantStatus: http.StatusOK, wantRows: 0},
	}

	for _, tt := range steps {
		t.Run(tt.name, func(t *testing.T) {
			status, body := doRequest(t, h, tt.method, tt.target, tt.body)
			assertStatus(t, status, tt.wantStatus, body)
			if tt.method != http.MethodGet {
				return
			}
			if rows := decodeRows(t, body); len(rows) != tt.wantRows {
				t.Errorf("rows = %d, want %d: %s", len(rows), tt.wantRows, body)
			}
			if tt.check != nil {
				tt.check(t, body)
			}
		})
	}
}

// TestRowsOfMetadata pins the console's generated metadata: each declared view names
// its table in rowsOf, an undeclared view names nothing, and the fields the pages'
// row routes name carry their targets.
func TestRowsOfMetadata(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "web", "console", "src", "app", "core", "service", "zz_gen_resources.ts"))
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}

	tests := []struct {
		name    string
		want    string
		absence bool
	}{
		{name: "the mission board names Missions", want: "[Resources.MissionBoards]: {\n    route: 'sectors/{sectorID}/mission-boards',\n    rowsOf: Resources.Missions,"},
		{name: "the roster names SquadronMemberships", want: "[Resources.SquadronRosters]: {\n    route: 'sectors/{sectorID}/squadron-rosters',\n    rowsOf: Resources.SquadronMemberships,"},
		{name: "the assignments name SquadronMemberships too", want: "[Resources.PilotAssignments]: {\n    route: 'sectors/{sectorID}/pilot-assignments',\n    rowsOf: Resources.SquadronMemberships,"},
		{name: "an undeclared view names nothing and stays a read-only list", want: "[Resources.OpenMissionsBySquadrons]: {\n    route: 'sectors/{sectorID}/open-missions-by-squadrons',\n    readDisabled: true,"},
		{name: "the roster's pilot id names Pilots, the row route's target", want: "{ fieldName: 'pilotId', displayType: 'enumerated', required: false, isIndex: false, enumeratedResource: Resources.Pilots }"},
		{name: "the assignments' squadron key names Squadrons, the row route's target", want: "{ fieldName: 'squadronId', primaryKey: { ordinalPosition: 0 }, displayType: 'enumerated', required: false, isIndex: true, filterable: 'always', enumeratedResource: Resources.Squadrons }"},
		{name: "the roster carries the table's compound key", want: "{ fieldName: 'squadronId', primaryKey: { ordinalPosition: 0 }, displayType: 'uuid', required: false, isIndex: true, filterable: 'always' },\n      { fieldName: 'userId', primaryKey: { ordinalPosition: 1 }, displayType: 'string', required: true, isIndex: true, filterable: 'always' },"},
		{name: "the table names no view", want: "rowsOf: Resources.SquadronRosters", absence: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := strings.Contains(string(source), tt.want); got == tt.absence {
				t.Errorf("zz_gen_resources.ts contains %q = %v, want %v", tt.want, got, !tt.absence)
			}
		})
	}
}
