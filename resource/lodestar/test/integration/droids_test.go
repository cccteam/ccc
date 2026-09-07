package integration

import (
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// TestIngestDroidReportsBatch drives the one RPC method with a nested request: the
// generated handler decodes the batch into its local mirror and builds
// rpc.IngestDroidReports through the pinned view, and every reading lands in the
// same transaction. The droids outlet is served under its own prefix by the test
// router, with no API-key middleware in front of it.
func TestIngestDroidReportsBatch(t *testing.T) {
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

	tests := []struct {
		name       string
		body       string
		wantStatus int
		// wantRecent is the reactor board's recent values after the call, newest first.
		wantRecent []float64
	}{
		{
			// The seed holds one reactor reading, 0.20 at 11:00.
			name:       "a batch of two readings lands as one transaction",
			body:       `{"shipId":"` + shipKingfisherID + `","readings":[{"subsystem":"reactor","reading":0.55,"recordedAt":"2026-09-02T12:00:00Z"},{"subsystem":"reactor","reading":0.35,"recordedAt":"2026-09-02T11:00:00Z"}]}`,
			wantStatus: http.StatusOK,
			wantRecent: []float64{0.55, 0.35, 0.20},
		},
		{
			name:       "an empty batch is refused by the body",
			body:       `{"shipId":"` + shipKingfisherID + `","readings":[]}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "a reading without a subsystem is refused by the body",
			body:       `{"shipId":"` + shipKingfisherID + `","readings":[{"reading":0.9}]}`,
			wantStatus: http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// The refusals write nothing, so the case that writes reads its own board.
			status, body := doRequest(t, h, http.MethodPost, ingest, tt.body)
			assertStatus(t, status, tt.wantStatus, body)
			if tt.wantRecent == nil {
				return
			}

			status, body = doRequest(t, h, http.MethodGet, sectorPath(anvil, "sector-hazard-boards/"+shipKingfisherID+"/reactor"), "")
			assertStatus(t, status, http.StatusOK, body)
			row := decodeRow(t, body)
			if got := row["worstReading"]; got != tt.wantRecent[0] {
				t.Errorf("worstReading = %v, want %v", got, tt.wantRecent[0])
			}
			recent, ok := row["recent"].([]any)
			if !ok || len(recent) != len(tt.wantRecent) {
				t.Fatalf("recent = %v, want %d readings", row["recent"], len(tt.wantRecent))
			}
			for i, want := range tt.wantRecent {
				reading, ok := recent[i].(map[string]any)
				if !ok || reading["value"] != want {
					t.Errorf("recent[%d] = %v, want value %v", i, recent[i], want)
				}
			}
		})
	}
}

// TestInspectShipAnswers drives the one method that returns a result: InspectShip
// answers with a RefitReport three levels deep (report, subsystem, reading). The
// generated handler captures it inside the transaction and encodes it through the
// mirrors after the commit; the droid batch ingested first is what the report finds.
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
	// hull readings and one reactor reading through the droid channel.
	batch := `{"shipId":"` + shipLanternID + `","readings":[` +
		`{"subsystem":"hull","reading":0.30,"recordedAt":"2026-09-03T10:00:00Z"},` +
		`{"subsystem":"reactor","reading":0.15,"recordedAt":"2026-09-03T10:05:00Z"},` +
		`{"subsystem":"hull","reading":0.45,"recordedAt":"2026-09-03T11:00:00Z"}]}`
	status, body := doRequest(t, h, http.MethodPost, "/droids/sectors/"+anvil+"/ingest-droid-reports", batch)
	assertStatus(t, status, http.StatusOK, body)

	status, body = doRequest(t, h, http.MethodPost, sectorPath(anvil, "inspect-ship"), `{"refitId":"`+refitLanternID+`"}`)
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
