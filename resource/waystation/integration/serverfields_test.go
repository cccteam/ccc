package integration

// This suite pins the server-owned field machinery on IncidentReports: output_only +
// default_create_fn (CaseNumber), input_only (RawStatement), the @defaultsUpdateType
// clamp and the @validateUpdateType rejection, plus immutable enforcement on Assets.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
)

const incidentsResource = accesstypes.Resource("IncidentReports")

func incidentGrants() grants {
	return grants{
		accesstypes.Create: {
			incidentsResource,
			fieldResource(incidentsResource, "waystationId"),
			fieldResource(incidentsResource, "summary"),
			fieldResource(incidentsResource, "severity"),
			fieldResource(incidentsResource, "reporterContact"),
			fieldResource(incidentsResource, "rawStatement"),
		},
		accesstypes.Update: {
			incidentsResource,
			fieldResource(incidentsResource, "summary"),
			fieldResource(incidentsResource, "severity"),
		},
		accesstypes.Read: {
			incidentsResource,
			fieldResource(incidentsResource, "summary"),
			fieldResource(incidentsResource, "severity"),
			fieldResource(incidentsResource, "caseNumber"),
		},
	}
}

// TestIncidentServerFields walks one incident from forged create attempt through
// clamped and rejected updates. Deliberately not a table: the created row's id flows
// through every later step.
func TestIncidentServerFields(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	h := newTestApp(db, incidentGrants())

	// Supplying the output_only case number is rejected, not ignored.
	status, body := doRequest(t, h, http.MethodPatch, "/api/resources",
		`[{"op":"add","path":"/waystations/ws-alpha/incident-reports","value":{"waystationId":"ws-alpha","summary":"Vent rattle","severity":2,"reporterContact":"crew@ws-alpha.demo","caseNumber":"IR-FORGED"}}]`)
	if status != http.StatusBadRequest {
		t.Fatalf("client-supplied caseNumber: status = %d, want 400: %s", status, body)
	}

	// A clean create gets a server-issued case number; the input_only raw statement
	// lands in the row but never serializes back out.
	status, body = doRequest(t, h, http.MethodPatch, "/api/resources",
		`[{"op":"add","path":"/waystations/ws-alpha/incident-reports","value":{"waystationId":"ws-alpha","summary":"Vent rattle","severity":2,"reporterContact":"crew@ws-alpha.demo","rawStatement":"It rattles on spin-up."}}]`)
	assertStatus(t, status, http.StatusOK, body)
	ids, _ := decodeRow(t, body)["incidentReports"].([]any)
	if len(ids) != 1 {
		t.Fatalf("created ids = %v, want one incident id: %s", ids, body)
	}
	id, _ := ids[0].(string)

	if got := readColumn[string](ctx, t, db, "IncidentReports", spanner.Key{id}, "CaseNumber"); !strings.HasPrefix(got, "IR-") {
		t.Errorf("CaseNumber = %q, want a server-issued IR- number", got)
	}
	if got := readColumn[spanner.NullString](ctx, t, db, "IncidentReports", spanner.Key{id}, "RawStatement"); got.StringVal != "It rattles on spin-up." {
		t.Errorf("RawStatement = %q, want the submitted statement stored", got.StringVal)
	}

	status, body = doRequest(t, h, http.MethodGet, "/api/waystations/ws-alpha/incident-reports/"+id, "")
	assertStatus(t, status, http.StatusOK, body)
	row := decodeRow(t, body)
	if _, leaked := row["rawStatement"]; leaked {
		t.Errorf("input_only rawStatement serialized on read: %s", body)
	}

	// The update defaults type clamps severity into 1..5.
	status, body = doRequest(t, h, http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":"/waystations/ws-alpha/incident-reports/%s","value":{"severity":9}}]`, id))
	assertStatus(t, status, http.StatusOK, body)
	if got := readColumn[int64](ctx, t, db, "IncidentReports", spanner.Key{id}, "Severity"); got != 5 {
		t.Errorf("Severity = %d, want clamped to 5", got)
	}

	// The update validator rejects a blanked summary.
	status, body = doRequest(t, h, http.MethodPatch, "/api/resources",
		fmt.Sprintf(`[{"op":"patch","path":"/waystations/ws-alpha/incident-reports/%s","value":{"summary":""}}]`, id))
	if status != http.StatusBadRequest {
		t.Fatalf("blank summary: status = %d, want 400: %s", status, body)
	}
}

func TestImmutableField(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	assets := accesstypes.Resource("Assets")
	h := newTestApp(db, grants{accesstypes.Update: {
		assets,
		fieldResource(assets, "serialNumber"),
		fieldResource(assets, "name"),
	}})

	tests := []struct {
		name       string
		value      string
		wantStatus int
	}{
		{
			// SerialNumber is immutable: settable on create only, update is a 400
			// regardless of grants.
			name:       "the immutable serial number refuses updates",
			value:      `{"serialNumber":"AR-9-XXXX"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "an ordinary granted field updates",
			value:      `{"name":"Atmos Recycler AR-9b"}`,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := doRequest(t, h, http.MethodPatch, "/api/resources",
				fmt.Sprintf(`[{"op":"patch","path":"/waystations/ws-alpha/assets/%s","value":%s}]`, assetRecyclerID, tt.value))
			assertStatus(t, status, tt.wantStatus, body)
		})
	}
}
