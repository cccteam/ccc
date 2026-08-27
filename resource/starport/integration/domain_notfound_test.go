package integration

// TestDomainNotFound pins the generated unknown-domain guard: a domain-scoped request
// naming a station the application does not recognize is a 404, decided before decode
// and permission checks — full grants keyed under the unknown station never rescue it.
// A known station without grants keeps returning 403 (the guard never over-triggers),
// and global routes carry no guard at all.

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

var stationGamma = accesstypes.DomainScope("station-gamma")

func TestDomainNotFound(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, "file://../schema/migrations", "file://testdata/seed")
	if err != nil {
		t.Fatal(err)
	}

	fullBerthGrants := grants{
		accesstypes.List: {
			berthsResource,
			fieldResource(berthsResource, "designation"),
			fieldResource(berthsResource, "sizeClass"),
			fieldResource(berthsResource, "occupied"),
		},
		accesstypes.Read: {
			berthsResource,
			fieldResource(berthsResource, "designation"),
			fieldResource(berthsResource, "sizeClass"),
			fieldResource(berthsResource, "occupied"),
		},
		accesstypes.Update: {
			berthsResource,
			fieldResource(berthsResource, "occupied"),
		},
		accesstypes.Execute: {authorizeDockingResource},
	}

	tests := []struct {
		name       string
		grants     domainGrants
		method     string
		target     string
		body       string
		wantStatus int
	}{
		{
			name:       "list in an unknown station is not found despite full grants there",
			grants:     domainGrants{stationGamma: fullBerthGrants},
			method:     http.MethodGet,
			target:     "/api/stations/station-gamma/berths",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "read in an unknown station is not found despite full grants there",
			grants:     domainGrants{stationGamma: fullBerthGrants},
			method:     http.MethodGet,
			target:     "/api/stations/station-gamma/berths/" + berthD7ID,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "patch in an unknown station is not found before the transaction",
			grants:     domainGrants{stationGamma: fullBerthGrants},
			method:     http.MethodPatch,
			target:     "/api/stations/station-gamma/berths",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/berths/%s","value":{"occupied":true}}]`, berthD7ID),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "rpc in an unknown station is not found despite an execute grant there",
			grants:     domainGrants{stationGamma: fullBerthGrants},
			method:     http.MethodPost,
			target:     "/api/stations/station-gamma/authorize-docking",
			body:       fmt.Sprintf(`{"berthId":%q,"dockingCode":"dock-42"}`, berthD7ID),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "a known station without grants stays forbidden, not not-found",
			grants:     nil,
			method:     http.MethodGet,
			target:     "/api/stations/station-alpha/berths",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "global routes carry no domain guard",
			grants:     domainGrants{globalScope: {accesstypes.List: {shipsResource}}},
			method:     http.MethodGet,
			target:     "/api/ships",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			testApp := newDomainTestApp(db, tt.grants)

			status, body := doRequest(t, testApp, tt.method, tt.target, tt.body)
			assertStatus(t, status, tt.wantStatus, body)
		})
	}
}
