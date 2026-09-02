package integration

// This suite pins structural permission enforcement over the demo world: field
// permissions are fail-closed (a resource-only grant exposes nothing beyond the
// primary key), explicitly granted fields shape the response exactly, and the
// domain guard 404s unknown tenants before permissions are consulted. Team is the
// deliberately minimal resource — no vocabulary beyond the mandatory @domain
// binding — so nothing here depends on authored attributes.

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

const (
	teamsResource      = accesstypes.Resource("Teams")
	workOrdersResource = accesstypes.Resource("WorkOrders")
)

func fieldResource(res accesstypes.Resource, tag string) accesstypes.Resource {
	return accesstypes.Resource(string(res) + "." + tag)
}

func TestStructuralFailClosed(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		grants     grants
		target     string
		wantStatus int
		wantRows   int
		wantKeys   []string
	}{
		{
			name:       "list with no grants at all conceals the domain",
			grants:     nil,
			target:     "/api/waystations/ws-alpha/teams",
			wantStatus: http.StatusNotFound, // zero grants in the domain: concealment makes it indistinguishable from unknown
		},
		{
			name:       "list with resource-only grant returns exactly the primary key",
			grants:     grants{accesstypes.List: {teamsResource}},
			target:     "/api/waystations/ws-alpha/teams",
			wantStatus: http.StatusOK,
			wantRows:   2, // ws-alpha's teams only: row tenancy partitions the list
			wantKeys:   []string{"id"},
		},
		{
			name: "list with field grants returns exactly the granted fields",
			grants: grants{accesstypes.List: {
				teamsResource,
				fieldResource(teamsResource, "name"),
				fieldResource(teamsResource, "specialty"),
			}},
			target:     "/api/waystations/ws-alpha/teams",
			wantStatus: http.StatusOK,
			wantRows:   2,
			wantKeys:   []string{"id", "name", "specialty"},
		},
		{
			name: "explicitly requesting an ungranted column is forbidden",
			grants: grants{accesstypes.List: {
				teamsResource,
				fieldResource(teamsResource, "name"),
			}},
			target:     "/api/waystations/ws-alpha/teams?columns=name,waystationId",
			wantStatus: http.StatusForbidden,
		},
		{
			name: "read follows the same field shaping",
			grants: grants{accesstypes.Read: {
				teamsResource,
				fieldResource(teamsResource, "name"),
			}},
			target:     "/api/waystations/ws-alpha/teams/" + teamAlphaMechID,
			wantStatus: http.StatusOK,
			wantKeys:   []string{"id", "name"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newTestApp(db, tt.grants)
			status, body := doRequest(t, h, http.MethodGet, tt.target, "")
			assertStatus(t, status, tt.wantStatus, body)
			if tt.wantStatus != http.StatusOK {
				return
			}

			if tt.wantRows > 0 {
				rows := decodeRows(t, body)
				if len(rows) != tt.wantRows {
					t.Fatalf("rows = %d, want %d: %s", len(rows), tt.wantRows, body)
				}
				assertKeys(t, rows[0], tt.wantKeys)

				return
			}
			assertKeys(t, decodeRow(t, body), tt.wantKeys)
		})
	}
}

func TestDomainGuard(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	fullTeams := grants{accesstypes.List: {
		teamsResource,
		fieldResource(teamsResource, "waystationId"),
		fieldResource(teamsResource, "name"),
		fieldResource(teamsResource, "specialty"),
	}}

	tests := []struct {
		name       string
		grants     grants
		target     string
		wantStatus int
	}{
		{
			name:       "unknown waystation is 404",
			grants:     nil,
			target:     "/api/waystations/ws-nowhere/teams",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "known waystation without a foothold is indistinguishable from unknown",
			grants:     nil,
			target:     "/api/waystations/ws-ceres/teams",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "known waystation with a grant serves",
			grants:     fullTeams,
			target:     "/api/waystations/ws-alpha/teams",
			wantStatus: http.StatusOK,
		},
		{
			name:       "global routes carry no domain guard",
			grants:     grants{accesstypes.List: {accesstypes.Resource("Waystations")}},
			target:     "/api/waystations",
			wantStatus: http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newTestApp(db, tt.grants)
			status, body := doRequest(t, h, http.MethodGet, tt.target, "")
			assertStatus(t, status, tt.wantStatus, body)
		})
	}
}

func TestOutletExclusivity(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	// SensorReadings and IngestSensorBatch live on the automation outlet only; the
	// generated test router serves the default outlet, so their default-outlet paths
	// must not exist even with every permission granted.
	allSensor := grants{
		accesstypes.List:    {accesstypes.Resource("SensorReadings")},
		accesstypes.Execute: {accesstypes.Resource("IngestSensorBatch")},
	}
	h := newTestApp(db, allSensor)

	for _, target := range []string{
		"/api/waystations/ws-alpha/sensor-readings",
		"/api/waystations/ws-alpha/ingest-sensor-batch",
	} {
		status, body := doRequest(t, h, http.MethodGet, target, "")
		if status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
			t.Errorf("%s on the default outlet: status = %d, want 404/405: %s", target, status, body)
		}
	}
}

func TestSuppressedReadHandler(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	// FleetSummary suppresses its read handler: the list route exists (global,
	// computed) while the read route does not.
	fleet := grants{
		accesstypes.List: {accesstypes.Resource("FleetSummaries")},
		accesstypes.Read: {accesstypes.Resource("FleetSummaries")},
	}
	h := newTestApp(db, fleet)

	status, body := doRequest(t, h, http.MethodGet, "/api/fleet-summaries", "")
	assertStatus(t, status, http.StatusOK, body)

	status, body = doRequest(t, h, http.MethodGet, fmt.Sprintf("/api/fleet-summaries/%s", wsAlpha), "")
	if status != http.StatusNotFound {
		t.Errorf("suppressed read route: status = %d, want 404: %s", status, body)
	}
}
