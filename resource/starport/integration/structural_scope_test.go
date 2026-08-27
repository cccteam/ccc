package integration

// This suite pins the structural-scope security property that retired the
// reserved-marker guard: no URL or operation-path domain value can address the
// global partition, because the generated handlers wrap the parameter in
// accesstypes.DomainScope — a tenant scope by construction. The harness makes
// DomainExists deliberately permissive (recognizes every value — the
// misconfigured-tenant-list scenario) and keys full grants under the global
// scope: a spoofed segment like "access:global" lands in an ordinary, empty
// tenant partition and is forbidden, with nothing to guard against and no
// rejection rule for callers to know.

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/starport/app"
	"github.com/cccteam/ccc/resource/starport/pkg/router"
	initiator "github.com/cccteam/db-initiator"
)

func newPermissiveDomainApp(db *initiator.SpannerDB, g domainGrants) http.Handler {
	return router.NewTestRouter(app.New(&testConfigurer{
		db:     db,
		access: &domainAccess{byDomain: g},
		// A deliberately misconfigured tenant list that recognizes everything: even
		// then, no URL value can reach the global partition.
		domainExists: func(context.Context, accesstypes.Domain) (bool, error) {
			return true, nil
		},
	}))
}

func TestSpoofedGlobalScopeIsInert(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, "file://../schema/migrations", "file://testdata/seed")
	if err != nil {
		t.Fatal(err)
	}

	berthListGrants := grants{accesstypes.List: {
		berthsResource,
		fieldResource(berthsResource, "designation"),
		fieldResource(berthsResource, "sizeClass"),
		fieldResource(berthsResource, "occupied"),
	}}

	tests := []struct {
		name       string
		grants     domainGrants
		method     string
		target     string
		body       string
		wantStatus int
		verify     func(t *testing.T)
	}{
		{
			name:       "list under a spoofed sentinel URL is an empty tenant partition, never the global one",
			grants:     domainGrants{globalScope: berthListGrants},
			method:     http.MethodGet,
			target:     "/api/stations/access:global/berths",
			wantStatus: http.StatusForbidden,
		},
		{
			name: "patch under a spoofed sentinel URL is forbidden before the transaction",
			grants: domainGrants{globalScope: {accesstypes.Update: {
				berthsResource,
				fieldResource(berthsResource, "occupied"),
			}}},
			method:     http.MethodPatch,
			target:     "/api/stations/access:global/berths",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"occupied":true}}]`, berthD7ID),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "rpc under a spoofed sentinel URL is forbidden despite a global execute grant",
			grants:     domainGrants{globalScope: {accesstypes.Execute: {authorizeDockingResource}}},
			method:     http.MethodPost,
			target:     "/api/stations/access:global/authorize-docking",
			body:       fmt.Sprintf(`{"berthId":%q,"dockingCode":"dock-42"}`, berthD7ID),
			wantStatus: http.StatusForbidden,
		},
		{
			name: "consolidated operation under a spoofed sentinel path stays in its empty tenant partition",
			grants: domainGrants{globalScope: {accesstypes.Update: {
				gantryCranesResource,
				fieldResource(gantryCranesResource, "operational"),
			}}},
			method:     http.MethodPatch,
			target:     "/api/resources",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/stations/access:global/gantry-cranes/%s","value":{"operational":false}}]`, craneGC1ID),
			wantStatus: http.StatusForbidden,
			verify: func(t *testing.T) {
				t.Helper()
				if operational := readColumn[bool](ctx, t, db, "GantryCranes", spanner.Key{craneGC1ID}, "Operational"); !operational {
					t.Error("crane GC-1 Operational changed, want unchanged true")
				}
			},
		},
		{
			name: "a tenant literally named like the sentinel is ordinary data with its own partition",
			grants: domainGrants{
				accesstypes.DomainScope("access:global"): berthListGrants,
			},
			method:     http.MethodGet,
			target:     "/api/stations/access:global/berths",
			wantStatus: http.StatusOK,
		},
		{
			name:       "an ordinary tenant sails through the permissive tenant list",
			grants:     domainGrants{stationGamma: berthListGrants},
			method:     http.MethodGet,
			target:     "/api/stations/station-gamma/berths",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			testApp := newPermissiveDomainApp(db, tt.grants)

			status, body := doRequest(t, testApp, tt.method, tt.target, tt.body)
			assertStatus(t, status, tt.wantStatus, body)
			if tt.verify != nil {
				tt.verify(t)
			}
		})
	}
}
