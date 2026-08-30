package integration

// This suite asserts that domain-scoped resources and RPC methods check permissions
// inside the permission partition named by the URL's station segment, and only there:
// a grant in the URL's station authorizes the request, the same grant in another
// station does not, and grants never bleed between the global domain and any station
// in either direction.
//
// Like the invariant suite, it only exercises fully tagged surfaces (Berths,
// AuthorizeDocking), so these assertions must NOT be updated when the field-permission
// default changes. A failure here means domain partitioning itself broke.

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/starport/app"
	"github.com/cccteam/ccc/resource/starport/pkg/router"
	initiator "github.com/cccteam/db-initiator"
	"google.golang.org/grpc/codes"
)

const (
	berthsResource           = accesstypes.Resource("Berths")
	authorizeDockingResource = accesstypes.Resource("AuthorizeDocking")

	// Seeded berth identifiers, matching testdata/seed.
	berthD7ID = "8c7d6e5f-4a3b-4c2d-9e1f-0a9b8c7d6e5f"
	berthK2ID = "2b3c4d5e-6f7a-4b8c-9d0e-1f2a3b4c5d6e"
)

// Stations are the permission scopes. They exist only as URL values and grant
// partitions: the Berths table is deliberately domain-blind. globalScope is
// the structural global partition.
var (
	stationAlpha = accesstypes.DomainScope("station-alpha")
	stationBeta  = accesstypes.DomainScope("station-beta")
	globalScope  = accesstypes.GlobalScope()
)

// domainGrants is a static permission table partitioned by scope. Global grants live
// under the structural global scope, exactly as they would in a real policy store.
type domainGrants map[accesstypes.Scope]grants

// domainAccess scripts access.Controller's permission checks over a domainGrants
// table. A check consults only the partition of the scope it is called with. Every
// Controller method the pipeline does not consume panics through the embedded nil
// interface, keeping the fake honest about what the pipeline actually draws on.
type domainAccess struct {
	access.Controller
	byDomain domainGrants
}

func (d *domainAccess) CheckUserResources(_ context.Context, _ accesstypes.Environment, _ accesstypes.User, scope accesstypes.Scope, perm accesstypes.Permission, resources ...accesstypes.Resource) (accesstypes.Decisions, error) {
	g := d.byDomain[scope]
	decisions := make(accesstypes.Decisions, len(resources))
	for _, res := range resources {
		if slices.Contains(g[perm], res) {
			decisions[res] = accesstypes.Granted()
		} else {
			decisions[res] = accesstypes.Denied()
		}
	}

	return decisions, nil
}

// newDomainTestApp builds the application with a domain-partitioned permission table
// backing every request, served through the generated test router.
func newDomainTestApp(db *initiator.SpannerDB, g domainGrants) http.Handler {
	return router.NewTestRouter(app.New(&testConfigurer{
		db:           db,
		access:       &domainAccess{byDomain: g},
		domainExists: stationsExist,
	}))
}

func TestDomainPartitionQuery(t *testing.T) {
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
	}}

	tests := []struct {
		name       string
		grants     domainGrants
		target     string
		wantStatus int
		wantRows   int
		wantKeys   []string
	}{
		{
			name:       "list with grant in the URL's station is allowed",
			grants:     domainGrants{stationAlpha: {accesstypes.List: {berthsResource}}},
			target:     "/api/stations/station-alpha/berths",
			wantStatus: http.StatusOK,
			wantRows:   2,
			wantKeys:   []string{"id"},
		},
		{
			name:       "list with field grants in the URL's station returns exactly the granted fields",
			grants:     domainGrants{stationAlpha: berthListGrants},
			target:     "/api/stations/station-alpha/berths",
			wantStatus: http.StatusOK,
			wantRows:   2,
			wantKeys:   []string{"id", "designation", "sizeClass"},
		},
		{
			name:       "list with the same grant in another station is forbidden",
			grants:     domainGrants{stationBeta: {accesstypes.List: {berthsResource}}},
			target:     "/api/stations/station-alpha/berths",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "global grant does not satisfy a station route",
			grants:     domainGrants{globalScope: {accesstypes.List: {berthsResource}}},
			target:     "/api/stations/station-alpha/berths",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "station grant does not satisfy a global route",
			grants:     domainGrants{stationAlpha: {accesstypes.List: {shipsResource}}},
			target:     "/api/ships",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "field grant in another station does not widen the URL station's fields",
			grants:     domainGrants{stationAlpha: {accesstypes.List: {berthsResource}}, stationBeta: berthListGrants},
			target:     "/api/stations/station-alpha/berths",
			wantStatus: http.StatusOK,
			wantRows:   2,
			wantKeys:   []string{"id"},
		},
		{
			name:       "read with grant in the URL's station is allowed",
			grants:     domainGrants{stationAlpha: {accesstypes.Read: {berthsResource, fieldResource(berthsResource, "designation")}}},
			target:     "/api/stations/station-alpha/berths/" + berthD7ID,
			wantStatus: http.StatusOK,
			wantKeys:   []string{"id", "designation"},
		},
		{
			name:       "read with the same grant in another station is forbidden",
			grants:     domainGrants{stationBeta: {accesstypes.Read: {berthsResource}}},
			target:     "/api/stations/station-alpha/berths/" + berthD7ID,
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			testApp := newDomainTestApp(db, tt.grants)

			status, body := doRequest(t, testApp, http.MethodGet, tt.target, "")
			assertStatus(t, status, tt.wantStatus, body)
			if tt.wantStatus != http.StatusOK {
				return
			}

			if tt.wantRows > 0 {
				rows := decodeRows(t, body)
				if len(rows) != tt.wantRows {
					t.Fatalf("row count = %d, want %d: %s", len(rows), tt.wantRows, body)
				}
				for _, row := range rows {
					assertKeys(t, row, tt.wantKeys)
				}
			} else {
				assertKeys(t, decodeRow(t, body), tt.wantKeys)
			}
		})
	}
}

func TestDomainPartitionMutation(t *testing.T) {
	t.Parallel()

	createGrants := grants{accesstypes.Create: {
		berthsResource,
		fieldResource(berthsResource, "designation"),
		fieldResource(berthsResource, "sizeClass"),
		fieldResource(berthsResource, "occupied"),
	}}

	createBody := `[{"op":"add","path":"/","value":{"designation":"Berth Z-9","sizeClass":2,"occupied":false}}]`

	// Berth is excluded from handler consolidation (the consolidated payload cannot
	// carry a domain yet), so all mutations flow through the standalone
	// PATCH /api/stations/{stationID}/berths endpoint. Each case prepares its own
	// seeded database, fully isolating the cases from one another.
	tests := []struct {
		name       string
		grants     domainGrants
		target     string
		body       string
		wantStatus int
		verify     func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, respBody []byte)
	}{
		{
			name:       "create with field grants in the URL's station is allowed",
			grants:     domainGrants{stationAlpha: createGrants},
			target:     "/api/stations/station-alpha/berths",
			body:       createBody,
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, respBody []byte) {
				resp := decodeRow(t, respBody)
				created, ok := resp["iDs"].([]any)
				if !ok || len(created) != 1 {
					t.Fatalf("expected one created berth id, got: %s", respBody)
				}
				createdID, ok := created[0].(string)
				if !ok {
					t.Fatalf("created berth id is not a string: %s", respBody)
				}
				designation := readColumn[string](ctx, t, db, "Berths", spanner.Key{createdID}, "Designation")
				if designation != "Berth Z-9" {
					t.Errorf("created berth Designation = %q, want %q", designation, "Berth Z-9")
				}
			},
		},
		{
			name:       "create with the same grants in another station is forbidden",
			grants:     domainGrants{stationBeta: createGrants},
			target:     "/api/stations/station-alpha/berths",
			body:       createBody,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "create with the same grants in the global domain is forbidden",
			grants:     domainGrants{globalScope: createGrants},
			target:     "/api/stations/station-alpha/berths",
			body:       createBody,
			wantStatus: http.StatusForbidden,
		},
		{
			name: "update with field grant in the URL's station is allowed",
			grants: domainGrants{stationAlpha: {accesstypes.Update: {
				berthsResource,
				fieldResource(berthsResource, "occupied"),
			}}},
			target:     "/api/stations/station-alpha/berths",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"occupied":true}}]`, berthD7ID),
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				occupied := readColumn[bool](ctx, t, db, "Berths", spanner.Key{berthD7ID}, "Occupied")
				if !occupied {
					t.Error("berth Occupied = false, want true")
				}
			},
		},
		{
			name: "update with the same field grant in another station is forbidden",
			grants: domainGrants{stationBeta: {accesstypes.Update: {
				berthsResource,
				fieldResource(berthsResource, "occupied"),
			}}},
			target:     "/api/stations/station-alpha/berths",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"occupied":true}}]`, berthD7ID),
			wantStatus: http.StatusForbidden,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				occupied := readColumn[bool](ctx, t, db, "Berths", spanner.Key{berthD7ID}, "Occupied")
				if occupied {
					t.Error("berth Occupied = true, want unchanged false")
				}
			},
		},
		{
			name: "update immutable field is rejected regardless of grants",
			grants: domainGrants{stationAlpha: {accesstypes.Update: {
				berthsResource,
				fieldResource(berthsResource, "designation"),
			}}},
			target:     "/api/stations/station-alpha/berths",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/%s","value":{"designation":"Berth D-0"}}]`, berthD7ID),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "delete with resource grant in the URL's station is allowed",
			grants:     domainGrants{stationAlpha: {accesstypes.Delete: {berthsResource}}},
			target:     "/api/stations/station-alpha/berths",
			body:       fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, berthK2ID),
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				_, err := db.Single().ReadRow(ctx, "Berths", spanner.Key{berthK2ID}, []string{"Designation"})
				if spanner.ErrCode(err) != codes.NotFound {
					t.Errorf("expected berth to be deleted, ReadRow err = %v", err)
				}
			},
		},
		{
			name:       "delete with the same grant in another station is forbidden",
			grants:     domainGrants{stationBeta: {accesstypes.Delete: {berthsResource}}},
			target:     "/api/stations/station-alpha/berths",
			body:       fmt.Sprintf(`[{"op":"remove","path":"/%s"}]`, berthK2ID),
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()

			db, err := prepareDatabase(ctx, t, "file://../schema/migrations", "file://testdata/seed")
			if err != nil {
				t.Fatal(err)
			}
			testApp := newDomainTestApp(db, tt.grants)

			status, respBody := doRequest(t, testApp, http.MethodPatch, tt.target, tt.body)
			assertStatus(t, status, tt.wantStatus, respBody)
			if tt.verify != nil {
				tt.verify(ctx, t, db, respBody)
			}
		})
	}
}

func TestDomainPartitionRPC(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, "file://../schema/migrations", "file://testdata/seed")
	if err != nil {
		t.Fatal(err)
	}

	executeGrant := grants{accesstypes.Execute: {authorizeDockingResource}}
	body := fmt.Sprintf(`{"berthId":%q,"dockingCode":"dock-42"}`, berthD7ID)

	tests := []struct {
		name       string
		grants     domainGrants
		target     string
		body       string
		wantStatus int
	}{
		{
			name:       "execute with grant in the URL's station is allowed",
			grants:     domainGrants{stationAlpha: executeGrant},
			target:     "/api/stations/station-alpha/authorize-docking",
			body:       body,
			wantStatus: http.StatusOK,
		},
		{
			name:       "execute with the same grant in another station is forbidden",
			grants:     domainGrants{stationBeta: executeGrant},
			target:     "/api/stations/station-alpha/authorize-docking",
			body:       body,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "global execute grant does not satisfy a station route",
			grants:     domainGrants{globalScope: executeGrant},
			target:     "/api/stations/station-alpha/authorize-docking",
			body:       body,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "station execute grant does not satisfy the global RPC route",
			grants:     domainGrants{stationAlpha: {accesstypes.Execute: {authorizeLaunchResource}}},
			target:     "/api/authorize-launch",
			body:       fmt.Sprintf(`{"shipId":%q,"launchCode":"launch-7"}`, shipVantaID),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "execute with grant still enforces request validation",
			grants:     domainGrants{stationAlpha: executeGrant},
			target:     "/api/stations/station-alpha/authorize-docking",
			body:       fmt.Sprintf(`{"berthId":%q,"dockingCode":""}`, berthD7ID),
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			testApp := newDomainTestApp(db, tt.grants)

			status, respBody := doRequest(t, testApp, http.MethodPost, tt.target, tt.body)
			assertStatus(t, status, tt.wantStatus, respBody)
		})
	}
}
