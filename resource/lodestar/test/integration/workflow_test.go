package integration

// This suite pins the stateful-resource pattern's structural half with a scripted
// permission engine (the grants-only half lives in the persona suites against the
// real engine): the state column is unwritable from the wire, transitions run only
// through the Execute-gated RPC methods and enforce edge legality, launching creates
// the first sortie, completing settles and brings the sorties home, and the refit
// workflow's join-path tenancy answers NotFound across sectors.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
)

// workflowGrants scripts everything the mission walk needs; enforcement nuances are
// not under test here.
func workflowGrants() grants {
	return grants{
		accesstypes.Create: withFields(missionsResource, "clientId", "kindId", "title", "hazard", "fee", "deadline"),
		accesstypes.Read:   withFields(missionsResource, "statusId", "bookedBy", "title", "settlement", "assignedSquadronId"),
		accesstypes.List:   withFields("Sorties", "missionId", "shipId", "pilotUserId", "returnedAt"),
		accesstypes.Execute: {
			"ClaimMission", "LaunchMission", "HoldMission", "ResumeMission", "CompleteMission", "FailMission", "StandDownMission",
			"InspectShip", "BeginRefit", "StartFlightTest", "PassFlightTest", "FailFlightTest", "ScrapShip",
		},
	}
}

func TestMissionWorkflow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestApp(db, workflowGrants())

	// Deliberately not a table: the walk mutates one mission through its lifecycle,
	// so each section depends on the state the previous one left behind.

	// The wire cannot express a state write: the patch decode is closed over statusId.
	status, body := doRequest(t, h, http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":%q,"value":{"clientId":%q,"kindId":"rescue","title":"Smuggled state","hazard":1,"fee":100,"deadline":"2027-01-01T00:00:00Z","statusId":"completed"}}]`, opPath(anvil, "missions"), clientHalvardID))
	if status != http.StatusBadRequest {
		t.Fatalf("statusId on the wire: status = %d, want 400: %s", status, body)
	}

	// Create as a specific user: BookedBy is server-stamped from the session and the
	// initial state comes from the @state default, never the client.
	status, body = doRequestAs(t, h, "workflow-booker", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":%q,"value":{"clientId":%q,"kindId":"rescue","title":"Drifting lifeboat","hazard":2,"fee":5000,"deadline":"2027-01-01T00:00:00Z"}}]`, opPath(anvil, "missions"), clientHalvardID))
	assertStatus(t, status, http.StatusOK, body)
	ids, _ := decodeRow(t, body)["missions"].([]any)
	if len(ids) != 1 {
		t.Fatalf("created ids = %v, want one mission id: %s", ids, body)
	}
	missionID, _ := ids[0].(string)

	if got := readColumn[string](ctx, t, db, "Missions", spanner.Key{missionID}, "BookedBy"); got != "workflow-booker" {
		t.Errorf("BookedBy = %q, want the session user", got)
	}
	if got := readColumn[string](ctx, t, db, "Missions", spanner.Key{missionID}, "StatusId"); got != "open" {
		t.Errorf("StatusId = %q, want the @state default open", got)
	}

	// Illegal edge: an open mission cannot launch. The declared transition answers
	// one uniform Forbidden naming the method and the row.
	status, body = doRequest(t, h, http.MethodPost, sectorPath(anvil, "launch-mission"),
		fmt.Sprintf(`{"missionId":%q,"shipId":%q,"pilotUserId":"lead"}`, missionID, shipKingfisherID))
	if status != http.StatusForbidden {
		t.Fatalf("launch an open mission: status = %d, want 403: %s", status, body)
	}
	if !strings.Contains(string(body), "LaunchMission may not run against Mission") {
		t.Fatalf("launch an open mission: refusal must name the method and the row, nothing more: %s", body)
	}

	// Legal walk: claim -> launch -> hold -> resume -> complete.
	status, body = doRequest(t, h, http.MethodPost, sectorPath(anvil, "claim-mission"),
		fmt.Sprintf(`{"missionId":%q,"squadronId":%q}`, missionID, squadronHammerID))
	assertStatus(t, status, http.StatusOK, body)
	if got := readColumn[spanner.NullString](ctx, t, db, "Missions", spanner.Key{missionID}, "AssignedSquadronId"); got.StringVal != squadronHammerID {
		t.Errorf("AssignedSquadronId after claim = %v, want Hammer", got)
	}

	status, body = doRequest(t, h, http.MethodPost, sectorPath(anvil, "launch-mission"),
		fmt.Sprintf(`{"missionId":%q,"shipId":%q,"pilotUserId":"lead"}`, missionID, shipKingfisherID))
	assertStatus(t, status, http.StatusOK, body)

	// Launching created the first sortie in the same transaction.
	status, body = doRequest(t, h, http.MethodGet, sectorPath(anvil, "sorties?filter=missionId:eq:"+missionID), "")
	assertStatus(t, status, http.StatusOK, body)
	sorties := decodeRows(t, body)
	if len(sorties) != 1 || sorties[0]["pilotUserId"] != "lead" {
		t.Fatalf("sorties after launch = %v, want one flown by lead", sorties)
	}

	status, body = doRequest(t, h, http.MethodPost, sectorPath(anvil, "hold-mission"),
		fmt.Sprintf(`{"missionId":%q,"reason":"solar weather"}`, missionID))
	assertStatus(t, status, http.StatusOK, body)
	if got := readColumn[string](ctx, t, db, "Missions", spanner.Key{missionID}, "StatusId"); got != "on_hold" {
		t.Errorf("StatusId after hold = %q, want on_hold", got)
	}

	// The loop: on_hold is left again.
	status, body = doRequest(t, h, http.MethodPost, sectorPath(anvil, "resume-mission"),
		fmt.Sprintf(`{"missionId":%q}`, missionID))
	assertStatus(t, status, http.StatusOK, body)

	status, body = doRequest(t, h, http.MethodPost, sectorPath(anvil, "complete-mission"),
		fmt.Sprintf(`{"missionId":%q}`, missionID))
	assertStatus(t, status, http.StatusOK, body)

	if got := readColumn[string](ctx, t, db, "Missions", spanner.Key{missionID}, "StatusId"); got != "completed" {
		t.Errorf("StatusId after the walk = %q, want completed", got)
	}
	// Completing settles: fee minus (no) expenses, and the sortie came home.
	if got := readColumn[spanner.NullNumeric](ctx, t, db, "Missions", spanner.Key{missionID}, "Settlement"); !got.Valid || got.Numeric.FloatString(0) != "5000" {
		t.Errorf("Settlement = %v, want 5000", got)
	}
	sortieID, _ := sorties[0]["id"].(string)
	if got := readColumn[spanner.NullTime](ctx, t, db, "Sorties", spanner.Key{sortieID}, "ReturnedAt"); !got.Valid {
		t.Error("Sorties.ReturnedAt not stamped by CompleteMission")
	}

	// A completed mission has no edges left.
	status, body = doRequest(t, h, http.MethodPost, sectorPath(anvil, "stand-down-mission"),
		fmt.Sprintf(`{"missionId":%q}`, missionID))
	if status != http.StatusForbidden {
		t.Fatalf("stand down a completed mission: status = %d, want 403: %s", status, body)
	}
}

func TestRefitWorkflow(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestApp(db, workflowGrants())

	// Deliberately not a table: the walk moves the Lantern's refit bay by bay.

	// Inspect stamps InspectedAt explicitly — transition-owned domain data.
	if got := readColumn[spanner.NullTime](ctx, t, db, "Refits", spanner.Key{refitLanternID}, "InspectedAt"); got.Valid {
		t.Fatalf("seeded InspectedAt = %v, want unset", got)
	}
	status, body := doRequest(t, h, http.MethodPost, sectorPath(anvil, "inspect-ship"), fmt.Sprintf(`{"refitId":%q}`, refitLanternID))
	assertStatus(t, status, http.StatusOK, body)
	if got := readColumn[spanner.NullTime](ctx, t, db, "Refits", spanner.Key{refitLanternID}, "InspectedAt"); !got.Valid {
		t.Error("Refits.InspectedAt not stamped by InspectShip")
	}

	status, body = doRequest(t, h, http.MethodPost, sectorPath(anvil, "begin-refit"), fmt.Sprintf(`{"refitId":%q}`, refitLanternID))
	assertStatus(t, status, http.StatusOK, body)
	status, body = doRequest(t, h, http.MethodPost, sectorPath(anvil, "start-flight-test"), fmt.Sprintf(`{"refitId":%q}`, refitLanternID))
	assertStatus(t, status, http.StatusOK, body)

	// The backward edge: a failed test slides the ship back a bay, then forward again.
	status, body = doRequest(t, h, http.MethodPost, sectorPath(anvil, "fail-flight-test"), fmt.Sprintf(`{"refitId":%q}`, refitLanternID))
	assertStatus(t, status, http.StatusOK, body)
	if got := readColumn[string](ctx, t, db, "Refits", spanner.Key{refitLanternID}, "StatusId"); got != "in_refit" {
		t.Errorf("StatusId after a failed test = %q, want in_refit", got)
	}
	status, body = doRequest(t, h, http.MethodPost, sectorPath(anvil, "start-flight-test"), fmt.Sprintf(`{"refitId":%q}`, refitLanternID))
	assertStatus(t, status, http.StatusOK, body)

	// Pass stamps the ship's LastRefitAt — the edge is the field's only writer.
	if got := readColumn[spanner.NullTime](ctx, t, db, "Ships", spanner.Key{shipLanternID}, "LastRefitAt"); got.Valid {
		t.Fatalf("seeded LastRefitAt = %v, want unset", got)
	}
	status, body = doRequest(t, h, http.MethodPost, sectorPath(anvil, "pass-flight-test"), fmt.Sprintf(`{"refitId":%q}`, refitLanternID))
	assertStatus(t, status, http.StatusOK, body)
	if got := readColumn[string](ctx, t, db, "Refits", spanner.Key{refitLanternID}, "StatusId"); got != "cleared" {
		t.Errorf("StatusId after the walk = %q, want cleared", got)
	}
	if got := readColumn[spanner.NullTime](ctx, t, db, "Ships", spanner.Key{shipLanternID}, "LastRefitAt"); !got.Valid {
		t.Error("Ships.LastRefitAt not stamped by PassFlightTest")
	}

	// Join-path tenancy on a transition root (c2b62ab): a Bastion refit addressed
	// through Anvil's route is NotFound, never Forbidden, even with every grant.
	status, body = doRequest(t, h, http.MethodPost, sectorPath(anvil, "pass-flight-test"), fmt.Sprintf(`{"refitId":%q}`, refitBastionWatchID))
	if status != http.StatusNotFound {
		t.Fatalf("cross-sector refit: status = %d, want 404: %s", status, body)
	}
	if got := readColumn[string](ctx, t, db, "Refits", spanner.Key{refitBastionWatchID}, "StatusId"); got != "flight_test" {
		t.Errorf("Bastion Watch refit moved to %q through Anvil's route", got)
	}
}
