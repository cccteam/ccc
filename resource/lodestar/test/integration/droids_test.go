package integration

import (
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// TestIngestDroidReports drives the machine-only RPC: the payload is flat, one reading per
// call (a batch is the droid script calling it in a loop), and the tenant column comes
// from the ship's hangar. The droids outlet is served under its own prefix by the test
// router, with no API-key middleware in front of it.
//
// Demonstrates: outlet.exclusive, machine-identity, rpc.typed-result, rpc.nested-shape, rpc.row-free.
func TestIngestDroidReports(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	h := newTestApp(db, grants{
		accesstypes.Execute: {accesstypes.Resource("IngestDroidReports")},
		accesstypes.List:    withFields("SectorHazardBoards", "worstReading", "recent"),
		accesstypes.Read:    withFields("SectorHazardBoards", "worstReading", "recent"),
	})
	ingest := "/droids/sectors/" + anvil + "/ingest-droid-reports"

	// The Lantern has no seeded telemetry: two readings land one call at a time.
	for _, body := range []string{
		`{"shipId":"` + shipLanternID + `","subsystem":"reactor","reading":0.55,"recordedAt":"2026-09-02T12:00:00Z"}`,
		`{"shipId":"` + shipLanternID + `","subsystem":"reactor","reading":0.35,"recordedAt":"2026-09-02T11:00:00Z"}`,
	} {
		status, respBody := doRequest(t, h, http.MethodPost, ingest, body)
		assertStatus(t, status, http.StatusOK, respBody)
	}

	status, body := doRequest(t, h, http.MethodGet, sectorPath(anvil, "sector-hazard-boards/"+shipLanternID+"/reactor"), "")
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
