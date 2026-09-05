package integration

// This suite pins the server-owned field machinery on DistressCalls: output_only +
// default_create_fn (CaseNumber, FiledBy), input_only (Transcript), plus the
// create-side defaults and validator on Missions, the update-side pair on Refits, and
// immutable enforcement on Ships.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
)

const callsResource = accesstypes.Resource("DistressCalls")

func TestDistressCallServerFields(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestApp(db, grants{
		accesstypes.Create: withFields(callsResource, "summary", "severity", "callerContact", "transcript"),
		accesstypes.Read:   withFields(callsResource, "summary", "severity", "caseNumber", "filedBy"),
	})

	// Supplying the output_only case number is rejected, not ignored.
	status, body := doRequest(t, h, http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":%q,"value":{"summary":"Forged","severity":2,"caseNumber":"DC-FORGED"}}]`, opPath(anvil, "distress-calls")))
	if status != http.StatusBadRequest {
		t.Fatalf("client-supplied caseNumber: status = %d, want 400: %s", status, body)
	}

	// A clean create gets a server-issued case number and the session's user as
	// filedBy; the input_only transcript lands in the row but never serializes back.
	status, body = doRequestAs(t, h, "caller-cal", http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":%q,"value":{"summary":"Hull breach reported","severity":4,"callerContact":"cal@relay.example","transcript":"Mayday, mayday"}}]`, opPath(anvil, "distress-calls")))
	assertStatus(t, status, http.StatusOK, body)
	ids, _ := decodeRow(t, body)["distressCalls"].([]any)
	if len(ids) != 1 {
		t.Fatalf("created ids = %v, want one call id: %s", ids, body)
	}
	id, _ := ids[0].(string)

	if got := readColumn[string](ctx, t, db, "DistressCalls", spanner.Key{id}, "CaseNumber"); !strings.HasPrefix(got, "DC-") {
		t.Errorf("CaseNumber = %q, want a server-issued DC- number", got)
	}
	if got := readColumn[string](ctx, t, db, "DistressCalls", spanner.Key{id}, "FiledBy"); got != "caller-cal" {
		t.Errorf("FiledBy = %q, want the session user", got)
	}
	if got := readColumn[spanner.NullString](ctx, t, db, "DistressCalls", spanner.Key{id}, "Transcript"); got.StringVal != "Mayday, mayday" {
		t.Errorf("Transcript = %q, want the submitted transcript stored", got.StringVal)
	}

	status, body = doRequest(t, h, http.MethodGet, sectorPath(anvil, "distress-calls/"+id), "")
	assertStatus(t, status, http.StatusOK, body)
	if _, leaked := decodeRow(t, body)["transcript"]; leaked {
		t.Errorf("input_only transcript serialized on read: %s", body)
	}
}

func TestMissionCreateDefaultsAndValidator(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestApp(db, grants{accesstypes.Create: withFields(missionsResource, "clientId", "kindId", "title", "hazard", "fee", "deadline")})

	// The validator rejects a deadline that has passed — someone is waiting.
	status, body := doRequest(t, h, http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":%q,"value":{"clientId":%q,"kindId":"rescue","title":"Too late","hazard":2,"fee":100,"deadline":"2020-01-01T00:00:00Z"}}]`, opPath(anvil, "missions"), clientHalvardID))
	if status != http.StatusBadRequest {
		t.Fatalf("past deadline: status = %d, want 400: %s", status, body)
	}
	status, body = doRequest(t, h, http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":%q,"value":{"clientId":%q,"kindId":"rescue","title":"Too hazardous","hazard":9,"fee":100,"deadline":"2027-01-01T00:00:00Z"}}]`, opPath(anvil, "missions"), clientHalvardID))
	if status != http.StatusBadRequest {
		t.Fatalf("hazard 9: status = %d, want 400: %s", status, body)
	}

	// The defaults type fills an omitted hazard at the lowest class.
	status, body = doRequest(t, h, http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"add","path":%q,"value":{"clientId":%q,"kindId":"courier","title":"Defaulted hazard","fee":100,"deadline":"2027-01-01T00:00:00Z"}}]`, opPath(anvil, "missions"), clientHalvardID))
	assertStatus(t, status, http.StatusOK, body)
	ids, _ := decodeRow(t, body)["missions"].([]any)
	id, _ := ids[0].(string)
	if got := readColumn[int64](ctx, t, db, "Missions", spanner.Key{id}, "Hazard"); got != 1 {
		t.Errorf("Hazard = %d, want the default 1", got)
	}
}

func TestRefitUpdateDefaultsAndValidator(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestApp(db, grants{accesstypes.Update: withFields("Refits", "estimate", "notes")})

	// The update defaults type rounds the estimate to whole credits.
	status, body := doRequest(t, h, http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"estimate":12345.67}}]`, opPath(anvil, "refits/"+refitMuleID)))
	assertStatus(t, status, http.StatusOK, body)
	if got := readColumn[spanner.NullNumeric](ctx, t, db, "Refits", spanner.Key{refitMuleID}, "Estimate"); !got.Valid || got.Numeric.FloatString(0) != "12346" {
		t.Errorf("Estimate = %v, want rounded 12346", got)
	}

	// The validator rejects a negative estimate.
	status, body = doRequest(t, h, http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":%q,"value":{"estimate":-5}}]`, opPath(anvil, "refits/"+refitMuleID)))
	if status != http.StatusBadRequest {
		t.Fatalf("negative estimate: status = %d, want 400: %s", status, body)
	}
}

func TestImmutableField(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestApp(db, grants{accesstypes.Update: withFields("Ships", "registry", "name")})

	tests := []struct {
		name       string
		value      string
		wantStatus int
	}{
		{name: "the immutable registry refuses updates", value: `{"registry":"LS-999"}`, wantStatus: http.StatusBadRequest},
		{name: "an ordinary granted field updates", value: `{"name":"Kingfisher II"}`, wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequest(t, h, http.MethodPatch, "/api/resources",
				fmt.Sprintf(`[{"op":"patch","path":%q,"value":%s}]`, opPath(anvil, "ships/"+shipKingfisherID), tt.value))
			assertStatus(t, status, tt.wantStatus, body)
		})
	}
}
