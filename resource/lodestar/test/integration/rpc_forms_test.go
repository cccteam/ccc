package integration

// rpc_forms_test (design plan §9): the client-form method runs with no transaction and
// refuses dry-run with 400; its armed reads return the caller's own view (the marshal's
// fees, the archivist's redactions, the cadet's refusal); the release's armed manifest
// read answers a typed receipt for the supercargo and the droid alike, and refuses a
// caller with no Read on the manifest; CompleteMission's settlement lands through the
// Paymaster role and the change event names the lead as that role.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	initiator "github.com/cccteam/db-initiator"
)

// briefingBody is CompileBriefing's answer as the wire carries it.
type briefingBody struct {
	Sector          string   `json:"sector"`
	Missions        int64    `json:"missions"`
	OpenMissions    int64    `json:"openMissions"`
	WorstHazard     int64    `json:"worstHazard"`
	FeesOutstanding string   `json:"feesOutstanding"`
	FeesRedacted    int64    `json:"feesRedacted"`
	Overdue         []string `json:"overdue"`
	HazardBoard     []struct {
		ShipName     string  `json:"shipName"`
		Subsystem    string  `json:"subsystem"`
		WorstReading float64 `json:"worstReading"`
	} `json:"hazardBoard"`
	HazardWithheld bool `json:"hazardWithheld"`
}

// TestCompileBriefing_clientForm drives the one method that runs outside a transaction.
//
// Demonstrates: rpc.client-form, rpc.armed-read, rpc.decision-as-data, rpc.dry-run, cell-masking.
func TestCompileBriefing_clientForm(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	tests := []struct {
		name         string
		user         accesstypes.User
		body         string
		wantStatus   int
		wantMissions int64
		wantOpen     int64
		wantRedacted int64
		wantOverdue  int
		wantHazards  bool
		wantWithheld bool
	}{
		{
			// Every Anvil mission, every fee, one overdue (the quarantine courier; the
			// Corvid's deadline is bootstrap+3m), and the board the marshal may read.
			name: "the marshal's sheet carries every fee and the hazard board", user: "marshal",
			body: `{"includeHazards":true}`, wantStatus: http.StatusOK,
			wantMissions: 30, wantOpen: 23, wantRedacted: 0, wantOverdue: 1, wantHazards: true,
		},
		{
			// The dispatcher's grant admits the live missions; the board is not hers,
			// so the sheet says so instead of folding it.
			name: "the dispatcher's sheet is the live board, hazards withheld", user: "dispatcher",
			body: `{"includeHazards":true}`, wantStatus: http.StatusOK,
			wantMissions: 23, wantOpen: 23, wantRedacted: 0, wantOverdue: 1, wantWithheld: true,
		},
		{
			// The archivist's grant admits the seven closed missions and shows the fee on
			// the three completed ones: four redactions, counted by the body from the row
			// envelope, with no masking code of its own.
			name: "the archivist's sheet counts the redactions her grant makes", user: "archivist",
			body: `{}`, wantStatus: http.StatusOK,
			wantMissions: 7, wantOpen: 0, wantRedacted: 4,
		},
		{name: "a caller without the Execute grant is refused", user: "cadet", body: `{}`, wantStatus: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequestAs(t, h, tt.user, http.MethodPost, sectorPath(anvil, "compile-briefing"), tt.body)
			assertStatus(t, status, tt.wantStatus, body)
			if status != http.StatusOK {
				return
			}
			var got briefingBody
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("json.Unmarshal(%s) error = %v", body, err)
			}
			if got.Sector != anvil || got.Missions != tt.wantMissions || got.OpenMissions != tt.wantOpen || got.FeesRedacted != tt.wantRedacted {
				t.Errorf("briefing = %+v, want %d missions, %d open, %d redacted in %s", got, tt.wantMissions, tt.wantOpen, tt.wantRedacted, anvil)
			}
			if len(got.Overdue) != tt.wantOverdue {
				t.Errorf("overdue = %v, want %d", got.Overdue, tt.wantOverdue)
			}
			if (len(got.HazardBoard) > 0) != tt.wantHazards || got.HazardWithheld != tt.wantWithheld {
				t.Errorf("hazard board = %d lines, withheld %v; want lines %v, withheld %v", len(got.HazardBoard), got.HazardWithheld, tt.wantHazards, tt.wantWithheld)
			}
		})
	}

	t.Run("a dry run is refused: there is no transaction to roll back", func(t *testing.T) {
		t.Parallel()

		status, body := doDryRunAs(t, h, "marshal", sectorPath(anvil, "compile-briefing"), `{}`)
		assertStatus(t, status, http.StatusBadRequest, body)
	})
}

// releasedBody is ReleaseConsignment's receipt as the wire carries it.
type releasedBody struct {
	ConsignmentID string `json:"consignmentId"`
	BondCode      string `json:"bondCode"`
	ReleasedAt    string `json:"releasedAt"`
}

// TestReleaseConsignment_armedManifest drives the release's armed read: the supercargo
// and the droid both get the receipt, from a manifest read under their own grants; a
// caller who may Execute but holds no Read on the manifest is refused before anything is
// stamped.
//
// Demonstrates: rpc.armed-read, rpc.typed-result, execute-condition, machine-identity.
func TestReleaseConsignment_armedManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		user        accesstypes.User
		prefix      string
		consignment string
		wantStatus  int
	}{
		{name: "the supercargo releases bond and gets the receipt", user: "supercargo", prefix: consoleAPI, consignment: consignmentPodID, wantStatus: http.StatusOK},
		{name: "the droid releases bond through its own outlet and gets the receipt", user: droidUser, prefix: "/droids", consignment: consignmentDronesID, wantStatus: http.StatusOK},
		{name: "a second release is the frame's refusal: the grant carries releasedAt IS NULL", user: "supercargo", prefix: consoleAPI, consignment: consignmentBullionID, wantStatus: http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, db, h := demoWorld(t)
			target := tt.prefix + "/sectors/" + anvil + "/release-consignment"
			status, body := doRequestAs(t, h, tt.user, http.MethodPost, target, fmt.Sprintf(`{"consignmentId":%q}`, tt.consignment))
			assertStatus(t, status, tt.wantStatus, body)
			if status != http.StatusOK {
				return
			}
			var got releasedBody
			if err := json.Unmarshal(body, &got); err != nil {
				t.Fatalf("json.Unmarshal(%s) error = %v", body, err)
			}
			if got.ConsignmentID != tt.consignment || !strings.HasPrefix(got.BondCode, "BND-ANV-") || got.ReleasedAt == "" {
				t.Errorf("receipt = %+v, want the bond code and the release instant", got)
			}
			if stamped := readColumn[spanner.NullTime](ctx, t, db, "Consignments", spanner.Key{tt.consignment}, "ReleasedAt"); !stamped.Valid {
				t.Error("ReleasedAt not stamped")
			}
		})
	}

	t.Run("an Execute grant without a Read on the manifest is refused by the armed read", func(t *testing.T) {
		t.Parallel()

		ctx := t.Context()
		db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
		if err != nil {
			t.Fatal(err)
		}
		h := newTestApp(db, grants{accesstypes.Execute: {"ReleaseConsignment"}})
		status, body := doRequest(t, h, http.MethodPost, sectorPath(anvil, "release-consignment"), fmt.Sprintf(`{"consignmentId":%q}`, consignmentPodID))
		assertStatus(t, status, http.StatusForbidden, body)
		if stamped := readColumn[spanner.NullTime](ctx, t, db, "Consignments", spanner.Key{consignmentPodID}, "ReleasedAt"); stamped.Valid {
			t.Error("ReleasedAt stamped although the manifest read was refused")
		}
	})
}

// TestCompleteMission_asPaymaster pins the role a body borrows: the Flight Lead holds no
// Update on Missions.settlement, so the settlement is posted through caller.As over the
// Paymaster role, and the change event names the lead as that role, the same actor-aware
// event an act-as-role session produces.
//
// Demonstrates: rpc.as-role, event-source.
func TestCompleteMission_asPaymaster(t *testing.T) {
	t.Parallel()

	ctx, db, h := demoWorld(t)

	status, body := doRequestAs(t, h, "lead", http.MethodPost, sectorPath(anvil, "complete-mission"), fmt.Sprintf(`{"missionId":%q}`, missionConvoyID))
	assertStatus(t, status, http.StatusOK, body)

	settled := readColumn[spanner.NullNumeric](ctx, t, db, "Missions", spanner.Key{missionConvoyID}, "Settlement")
	if !settled.Valid || settled.Numeric.FloatString(0) != "13500" {
		t.Errorf("Settlement = %v, want 13500 (fee 15000 less 1500 of expenses)", settled)
	}
	source, keys := latestChangeEventTouching(t, db, "Missions", missionConvoyID, "Settlement")
	if !strings.HasPrefix(source, "lead as role Paymaster (") {
		t.Errorf("settlement event source = %q, want 'lead as role Paymaster (...)'", source)
	}
	if len(keys) != 1 || keys[0] != "Settlement" {
		t.Errorf("settlement event changed %v, want exactly [Settlement]", keys)
	}
}

// latestChangeEventTouching returns the newest DataChangeEvents row for the table and
// row whose change set carries the column.
func latestChangeEventTouching(t *testing.T, db *initiator.SpannerDB, tableName, rowID, column string) (eventSource string, changeSetKeys []string) {
	t.Helper()

	iter := db.Single().Query(t.Context(), spanner.Statement{
		SQL: `SELECT EventSource, ChangeSet FROM DataChangeEvents
		       WHERE TableName = @table AND RowId = @row ORDER BY EventTime DESC, Sequence DESC`,
		Params: map[string]any{"table": tableName, "row": rowID},
	})
	defer iter.Stop()
	for {
		row, err := iter.Next()
		if err != nil {
			t.Fatalf("no change event touching %s.%s on %s: %v", tableName, column, rowID, err)
		}
		var source string
		var changeSet spanner.NullJSON
		if err := row.Columns(&source, &changeSet); err != nil {
			t.Fatal(err)
		}
		set, _ := changeSet.Value.(map[string]any)
		if _, ok := set[column]; !ok {
			continue
		}
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}

		return source, keys
	}
}
