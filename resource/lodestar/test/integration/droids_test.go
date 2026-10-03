package integration

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/telemetry"
	"github.com/go-playground/errors/v5"
)

// TestIngestDroidReports drives the machine-only RPC: the payload is flat, one reading per
// call (a batch is the droid script calling it in a loop), and the tenant column comes
// from the ship's hangar. The droids outlet is served under its own prefix by the test
// router, with no API-key middleware in front of it. The first reading carries its raw
// frame, a telemetry.Frame typed in the droid link's own package, which the generator
// writes the JSON and Spanner methods for (WithTypes): the column holds the JSON the
// droid sent, the droid's list reads it back as that JSON, and the reading sent without
// one leaves the column NULL and the row's frame null.
//
// Demonstrates: outlet.exclusive, machine-identity, rpc.typed-result, rpc.nested-shape, rpc.row-free, typescript.types-package.
func TestIngestDroidReports(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	h := newTestApp(db, grants{
		accesstypes.Execute: {accesstypes.Resource("IngestDroidReports")},
		accesstypes.List:    append(withFields("SectorHazardBoards", "worstReading", "recent"), withFields("DroidReports", "shipId", "subsystem", "reading", "frame")...),
		accesstypes.Read:    withFields("SectorHazardBoards", "worstReading", "recent"),
	})
	ingest := "/droids/sectors/" + anvil + "/ingest-droid-reports"

	// The Lantern has no seeded telemetry: two readings land one call at a time, the
	// first with the firmware's raw frame, the second without, both dated after every
	// seeded reading so the droid's first page, newest first, holds them.
	frame := map[string]any{"fw": "7.2", "reactor": map[string]any{"flux": 0.55, "coils": []any{float64(1), float64(4)}}}
	frameJSON, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"shipId":"` + shipLanternID + `","subsystem":"reactor","reading":0.55,"recordedAt":"2026-11-30T12:00:00Z","frame":` + string(frameJSON) + `}`,
		`{"shipId":"` + shipLanternID + `","subsystem":"reactor","reading":0.35,"recordedAt":"2026-11-30T11:00:00Z"}`,
	} {
		status, respBody := doRequest(t, h, http.MethodPost, ingest, body)
		assertStatus(t, status, http.StatusOK, respBody)
	}

	// The droid's list carries the frame back as the JSON it sent, and null for the
	// reading that sent none; the column holds the same JSON, and NULL.
	status, body := doRequest(t, h, http.MethodGet, "/droids/sectors/"+anvil+"/droid-reports", "")
	assertStatus(t, status, http.StatusOK, body)
	frames := map[float64]any{}
	for _, row := range decodeRows(t, body) {
		if row["shipId"] != shipLanternID {
			continue
		}
		if reading, ok := row["reading"].(float64); ok {
			frames[reading] = row["frame"]
		}
	}
	if len(frames) != 2 {
		t.Fatalf("the Lantern's readings on the first page = %v, want the two just ingested: %s", frames, body)
	}
	if got := frames[0.55]; !reflect.DeepEqual(got, frame) {
		t.Errorf("frame of the reading sent with one = %v, want %v", got, frame)
	}
	if got, present := frames[0.35]; !present || got != nil {
		t.Errorf("frame of the reading sent without one = %v (present %v), want a null", got, present)
	}
	var stored []spanner.NullJSON
	iter := db.Single().Query(ctx, spanner.Statement{
		SQL:    "SELECT Frame FROM DroidReports WHERE ShipId = @ship AND Subsystem = 'reactor' ORDER BY RecordedAt DESC",
		Params: map[string]any{"ship": shipLanternID},
	})
	defer iter.Stop()
	if err := iter.Do(func(row *spanner.Row) error {
		var cell spanner.NullJSON
		if err := row.Columns(&cell); err != nil {
			return errors.Wrap(err, "spanner.Row.Columns()")
		}
		stored = append(stored, cell)

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(stored) != 2 || !stored[0].Valid || !reflect.DeepEqual(stored[0].Value, frame) || stored[1].Valid {
		t.Errorf("Frame column, newest first = %v, want the frame %v then NULL", stored, frame)
	}
	// The type is its declaration and its annotation alone: the JSON pair that keeps it
	// JSON on the wire is generated into the telemetry package.
	var _ json.Marshaler = telemetry.Frame(nil)
	var _ json.Unmarshaler = (*telemetry.Frame)(nil)

	status, body = doRequest(t, h, http.MethodGet, sectorPath(anvil, "sector-hazard-boards/"+shipLanternID+"/reactor"), "")
	assertStatus(t, status, http.StatusOK, body)
	row := decodeRow(t, body)
	if got := row["worstReading"]; got != 0.55 {
		t.Errorf("worstReading = %v, want 0.55", got)
	}
	recent, ok := row["recent"].([]any)
	if !ok || len(recent) != 2 {
		t.Fatalf("recent = %v, want two readings", row["recent"])
	}
	for i, want := range []float64{0.55, 0.35} {
		reading, ok := recent[i].(map[string]any)
		if !ok || reading["value"] != want {
			t.Errorf("recent[%d] = %v, want value %v", i, recent[i], want)
		}
	}

	// A reading without a subsystem is refused by the body.
	status, body = doRequest(t, h, http.MethodPost, ingest, `{"shipId":"`+shipLanternID+`","reading":0.9}`)
	assertStatus(t, status, http.StatusBadRequest, body)
}

// TestInspectShipAnswers drives the one method that returns a result: InspectShip
// answers with a RefitReport three levels deep (report, subsystem, reading). The
// generated handler captures it inside the transaction and encodes it through the
// mirrors after the commit; the droid readings ingested first are what the report finds.
func TestInspectShipAnswers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	h := newTestApp(db, grants{
		accesstypes.Execute: {accesstypes.Resource("IngestDroidReports"), accesstypes.Resource("InspectShip")},
	})

	// The Lantern (the docked refit's ship) has no seeded telemetry; give it two
	// hull readings and one reactor reading through the droid channel, one per call.
	for _, reading := range []string{
		`{"shipId":"` + shipLanternID + `","subsystem":"hull","reading":0.30,"recordedAt":"2026-09-03T10:00:00Z"}`,
		`{"shipId":"` + shipLanternID + `","subsystem":"reactor","reading":0.15,"recordedAt":"2026-09-03T10:05:00Z"}`,
		`{"shipId":"` + shipLanternID + `","subsystem":"hull","reading":0.45,"recordedAt":"2026-09-03T11:00:00Z"}`,
	} {
		status, body := doRequest(t, h, http.MethodPost, "/droids/sectors/"+anvil+"/ingest-droid-reports", reading)
		assertStatus(t, status, http.StatusOK, body)
	}

	status, body := doRequest(t, h, http.MethodPost, sectorPath(anvil, "inspect-ship"), `{"refitId":"`+refitLanternID+`"}`)
	assertStatus(t, status, http.StatusOK, body)

	report := decodeRow(t, body)
	if got := report["refitId"]; got != refitLanternID {
		t.Errorf("refitId = %v, want %v", got, refitLanternID)
	}
	if got := report["shipId"]; got != shipLanternID {
		t.Errorf("shipId = %v, want %v", got, shipLanternID)
	}
	if got := report["shipName"]; got != "Lantern" {
		t.Errorf("shipName = %v, want Lantern", got)
	}
	subsystems, ok := report["subsystems"].([]any)
	if !ok || len(subsystems) != 2 {
		t.Fatalf("subsystems = %v, want hull and reactor: %s", report["subsystems"], body)
	}
	want := map[string][]float64{"hull": {0.45, 0.30}, "reactor": {0.15}}
	for _, s := range subsystems {
		subsystem, ok := s.(map[string]any)
		if !ok {
			t.Fatalf("subsystem = %T, want an object", s)
		}
		name, _ := subsystem["name"].(string)
		readings, ok := subsystem["readings"].([]any)
		if !ok || len(readings) != len(want[name]) {
			t.Fatalf("%s readings = %v, want %v", name, subsystem["readings"], want[name])
		}
		for i, r := range readings {
			reading, ok := r.(map[string]any)
			if !ok || reading["value"] != want[name][i] {
				t.Errorf("%s readings[%d] = %v, want value %v", name, i, r, want[name][i])
			}
		}
	}

	// The transition still happened: a second inspection finds the refit moved on.
	status, body = doRequest(t, h, http.MethodPost, sectorPath(anvil, "inspect-ship"), `{"refitId":"`+refitLanternID+`"}`)
	assertStatus(t, status, http.StatusForbidden, body)
}
