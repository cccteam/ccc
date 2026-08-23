package integration

// This suite pins the structural reserved-marker guard: a URL or operation-path domain
// carrying ':' (e.g. a spoofed "access:global") is rejected BEFORE DomainExists is
// consulted. The harness makes DomainExists deliberately permissive (recognizes every
// value — the misconfigured-tenant-list scenario), and keys full grants under
// accesstypes.GlobalDomain: without the guard, the spoofed segment would compare equal
// to the GlobalDomain sentinel and the request would be authorized out of the global
// partition. The rejections below are therefore the guard's alone.

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/starport/app"
	"github.com/cccteam/ccc/resource/starport/pkg/rpc"
	initiator "github.com/cccteam/db-initiator"
	"github.com/go-playground/validator/v10"
)

func newPermissiveDomainApp(db *initiator.SpannerDB, g domainGrants) *app.App {
	return app.New(app.Config{
		ResourceClient: resource.NewSpannerClient(db.Client),
		RPCClient:      rpc.NewClient(),
		UserPermissions: func(*http.Request) resource.UserPermissions {
			return &domainUserPermissions{byDomain: g}
		},
		// A deliberately misconfigured tenant list that recognizes everything: the
		// reserved-marker guard must reject before this is ever consulted.
		DomainExists: func(context.Context, accesstypes.Domain) (bool, error) {
			return true, nil
		},
		Validator: validator.New(),
	})
}

func TestReservedMarkerDomainRejected(t *testing.T) {
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
			name:       "list under the spoofed global sentinel is not found, not served from the global partition",
			grants:     domainGrants{accesstypes.GlobalDomain: berthListGrants},
			method:     http.MethodGet,
			target:     "/api/stations/access:global/berths",
			wantStatus: http.StatusNotFound,
		},
		{
			name: "patch under the spoofed global sentinel is not found before the transaction",
			grants: domainGrants{accesstypes.GlobalDomain: {accesstypes.Update: {
				berthsResource,
				fieldResource(berthsResource, "occupied"),
			}}},
			method:     http.MethodPatch,
			target:     "/api/stations/access:global/berths",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/berths/%s","value":{"occupied":true}}]`, berthD7ID),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "rpc under the spoofed global sentinel is not found despite a global execute grant",
			grants:     domainGrants{accesstypes.GlobalDomain: {accesstypes.Execute: {authorizeDockingResource}}},
			method:     http.MethodPost,
			target:     "/api/stations/access:global/authorize-docking",
			body:       fmt.Sprintf(`{"berthId":%q,"dockingCode":"dock-42"}`, berthD7ID),
			wantStatus: http.StatusNotFound,
		},
		{
			name: "consolidated operation under the spoofed global sentinel is a bad request",
			grants: domainGrants{accesstypes.GlobalDomain: {accesstypes.Update: {
				gantryCranesResource,
				fieldResource(gantryCranesResource, "operational"),
			}}},
			method:     http.MethodPatch,
			target:     "/api/resources",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/stations/access:global/gantry-cranes/%s","value":{"operational":false}}]`, craneGC1ID),
			wantStatus: http.StatusBadRequest,
			verify: func(t *testing.T) {
				t.Helper()
				if operational := readColumn[bool](ctx, t, db, "GantryCranes", spanner.Key{craneGC1ID}, "Operational"); !operational {
					t.Error("crane GC-1 Operational changed, want unchanged true")
				}
			},
		},
		{
			name:       "a marker-free domain sails through the permissive tenant list",
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
