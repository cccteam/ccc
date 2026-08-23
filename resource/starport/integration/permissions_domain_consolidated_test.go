package integration

// This suite pins the consolidated batch handler's domain semantics: a domain-scoped
// operation carries its station in the operation path exactly as the URL grammar does
// (/stations/{stationID}/gantry-cranes/...), each operation is checked in its own
// station's permission partition, cross-domain batches are legal (the batch is one
// transaction — one forbidden operation rolls back all of them), and the path grammar
// itself rejects domainless domain-scoped operations and domain-prefixed global ones.

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	initiator "github.com/cccteam/db-initiator"
)

const (
	gantryCranesResource = accesstypes.Resource("GantryCranes")

	// Seeded gantry crane identifiers, matching testdata/seed.
	craneGC1ID = "4e5f6a7b-8c9d-4e0f-a1b2-c3d4e5f6a7b8" // LiftTonnage 250, Operational true
	craneGC9ID = "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d" // LiftTonnage 400, Operational false
)

func TestDomainPartitionConsolidatedMutation(t *testing.T) {
	t.Parallel()

	craneUpdateGrants := grants{accesstypes.Update: {
		gantryCranesResource,
		fieldResource(gantryCranesResource, "operational"),
		fieldResource(gantryCranesResource, "liftTonnage"),
	}}

	// Each case prepares its own seeded database, fully isolating the cases from one
	// another.
	tests := []struct {
		name       string
		grants     domainGrants
		body       string
		wantStatus int
		verify     func(ctx context.Context, t *testing.T, db *initiator.SpannerDB)
	}{
		{
			name:       "operation with grant in its own station is applied",
			grants:     domainGrants{stationAlpha: craneUpdateGrants},
			body:       fmt.Sprintf(`[{"op":"patch","path":"/stations/station-alpha/gantry-cranes/%s","value":{"operational":false}}]`, craneGC1ID),
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB) {
				if operational := readColumn[bool](ctx, t, db, "GantryCranes", spanner.Key{craneGC1ID}, "Operational"); operational {
					t.Error("crane GC-1 Operational = true, want updated to false")
				}
			},
		},
		{
			name:       "the same grant in another station is forbidden",
			grants:     domainGrants{stationBeta: craneUpdateGrants},
			body:       fmt.Sprintf(`[{"op":"patch","path":"/stations/station-alpha/gantry-cranes/%s","value":{"operational":false}}]`, craneGC1ID),
			wantStatus: http.StatusForbidden,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB) {
				if operational := readColumn[bool](ctx, t, db, "GantryCranes", spanner.Key{craneGC1ID}, "Operational"); !operational {
					t.Error("crane GC-1 Operational changed, want unchanged true")
				}
			},
		},
		{
			name:       "a global grant does not satisfy a station operation",
			grants:     domainGrants{accesstypes.GlobalDomain: craneUpdateGrants},
			body:       fmt.Sprintf(`[{"op":"patch","path":"/stations/station-alpha/gantry-cranes/%s","value":{"operational":false}}]`, craneGC1ID),
			wantStatus: http.StatusForbidden,
		},
		{
			name: "cross-domain batch with grants in both stations applies every operation",
			grants: domainGrants{
				stationAlpha: craneUpdateGrants,
				stationBeta:  craneUpdateGrants,
			},
			body:       fmt.Sprintf(`[{"op":"patch","path":"/stations/station-alpha/gantry-cranes/%s","value":{"operational":false}},{"op":"patch","path":"/stations/station-beta/gantry-cranes/%s","value":{"liftTonnage":425}}]`, craneGC1ID, craneGC9ID),
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB) {
				if operational := readColumn[bool](ctx, t, db, "GantryCranes", spanner.Key{craneGC1ID}, "Operational"); operational {
					t.Error("crane GC-1 Operational = true, want updated to false")
				}
				if tonnage := readColumn[int64](ctx, t, db, "GantryCranes", spanner.Key{craneGC9ID}, "LiftTonnage"); tonnage != 425 {
					t.Errorf("crane GC-9 LiftTonnage = %d, want updated 425", tonnage)
				}
			},
		},
		{
			name:       "one forbidden operation rolls back the whole cross-domain batch",
			grants:     domainGrants{stationAlpha: craneUpdateGrants},
			body:       fmt.Sprintf(`[{"op":"patch","path":"/stations/station-alpha/gantry-cranes/%s","value":{"operational":false}},{"op":"patch","path":"/stations/station-beta/gantry-cranes/%s","value":{"liftTonnage":425}}]`, craneGC1ID, craneGC9ID),
			wantStatus: http.StatusForbidden,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB) {
				if operational := readColumn[bool](ctx, t, db, "GantryCranes", spanner.Key{craneGC1ID}, "Operational"); !operational {
					t.Error("crane GC-1 Operational changed, want the whole batch rolled back")
				}
				if tonnage := readColumn[int64](ctx, t, db, "GantryCranes", spanner.Key{craneGC9ID}, "LiftTonnage"); tonnage != 400 {
					t.Errorf("crane GC-9 LiftTonnage = %d, want unchanged 400", tonnage)
				}
			},
		},
		{
			name: "a batch mixes global and station operations",
			grants: domainGrants{
				accesstypes.GlobalDomain: {accesstypes.Update: {
					shipsResource,
					fieldResource(shipsResource, "cargoValue"),
				}},
				stationAlpha: craneUpdateGrants,
			},
			body:       fmt.Sprintf(`[{"op":"patch","path":"/ships/%s","value":{"cargoValue":777}},{"op":"patch","path":"/stations/station-alpha/gantry-cranes/%s","value":{"operational":false}}]`, shipVantaID, craneGC1ID),
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB) {
				if cargoValue := readColumn[int64](ctx, t, db, "Ships", spanner.Key{shipVantaID}, "CargoValue"); cargoValue != 777 {
					t.Errorf("ship Vanta CargoValue = %d, want updated 777", cargoValue)
				}
				if operational := readColumn[bool](ctx, t, db, "GantryCranes", spanner.Key{craneGC1ID}, "Operational"); operational {
					t.Error("crane GC-1 Operational = true, want updated to false")
				}
			},
		},
		{
			name: "create carries the station in the operation path",
			grants: domainGrants{stationAlpha: {accesstypes.Create: {
				gantryCranesResource,
				fieldResource(gantryCranesResource, "callsign"),
				fieldResource(gantryCranesResource, "liftTonnage"),
				fieldResource(gantryCranesResource, "operational"),
			}}},
			body:       `[{"op":"add","path":"/stations/station-alpha/gantry-cranes","value":{"callsign":"Crane GC-4","liftTonnage":300,"operational":true}}]`,
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB) {
				if count := countRows(ctx, t, db, "GantryCranes"); count != 3 {
					t.Errorf("GantryCranes row count = %d, want 3 after create", count)
				}
			},
		},
		{
			name:       "a domain-scoped operation without its station segment is a bad request",
			grants:     domainGrants{stationAlpha: craneUpdateGrants},
			body:       fmt.Sprintf(`[{"op":"patch","path":"/gantry-cranes/%s","value":{"operational":false}}]`, craneGC1ID),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "a global operation with a station prefix is a bad request",
			grants:     domainGrants{accesstypes.GlobalDomain: {accesstypes.Update: {shipsResource, fieldResource(shipsResource, "cargoValue")}}},
			body:       fmt.Sprintf(`[{"op":"patch","path":"/stations/station-alpha/ships/%s","value":{"cargoValue":777}}]`, shipVantaID),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "an unknown station in the operation path is a bad request despite grants there",
			grants:     domainGrants{stationGamma: craneUpdateGrants},
			body:       fmt.Sprintf(`[{"op":"patch","path":"/stations/station-gamma/gantry-cranes/%s","value":{"operational":false}}]`, craneGC1ID),
			wantStatus: http.StatusBadRequest,
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

			status, respBody := doRequest(t, testApp, http.MethodPatch, "/api/resources", tt.body)
			assertStatus(t, status, tt.wantStatus, respBody)
			if tt.verify != nil {
				tt.verify(ctx, t, db)
			}
		})
	}
}
