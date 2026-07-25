package integration

// This suite covers server-populated fields and create validation:
//   - default_create_fn (SupplyCrate.Status, SupplyCrate.Barcode): the default runs only
//     when the client omits the field; a client-supplied value wins.
//   - output_only_update_fn (Ship.UpdatedAt): stamped on every update, absent on create,
//     and never accepted from the client.
//   - @validateCreateType (SupplyCrateCreateValidator): runs inside the mutation
//     transaction and surfaces as a 400.

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	initiator "github.com/cccteam/db-initiator"
)

// createdID extracts the single created row id for a resource from a consolidated
// mutation response body.
func createdID(t *testing.T, respBody []byte, resourceKey string) string {
	t.Helper()

	resp := decodeRow(t, respBody)
	created, ok := resp[resourceKey].([]any)
	if !ok || len(created) != 1 {
		t.Fatalf("expected one created %s id, got: %s", resourceKey, respBody)
	}
	id, ok := created[0].(string)
	if !ok {
		t.Fatalf("created %s id is not a string: %s", resourceKey, respBody)
	}

	return id
}

func TestServerPopulatedFields(t *testing.T) {
	t.Parallel()

	crateCreateGrants := grants{accesstypes.Create: {
		supplyCratesResource,
		fieldResource(supplyCratesResource, "label"),
		fieldResource(supplyCratesResource, "quantity"),
		fieldResource(supplyCratesResource, "priority"),
		fieldResource(supplyCratesResource, "status"),
	}}

	shipCreateGrants := grants{accesstypes.Create: {
		shipsResource,
		fieldResource(shipsResource, "registryCode"),
		fieldResource(shipsResource, "name"),
		fieldResource(shipsResource, "cargoValue"),
	}}

	shipUpdateGrants := grants{accesstypes.Update: {
		shipsResource,
		fieldResource(shipsResource, "name"),
	}}

	tests := []struct {
		name       string
		grants     grants
		body       string
		wantStatus int
		verify     func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, respBody []byte)
	}{
		{
			name:       "omitted default_create_fn fields are server-populated",
			grants:     crateCreateGrants,
			body:       `[{"op":"add","path":"/supply-crates","value":{"label":"Spare Fuses","quantity":8,"priority":4}}]`,
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, respBody []byte) {
				id := createdID(t, respBody, "supplyCrates")
				status := readColumn[string](ctx, t, db, "SupplyCrates", spanner.Key{id}, "Status")
				if status != "provisioned" {
					t.Errorf("created crate Status = %q, want default %q", status, "provisioned")
				}
				barcode := readColumn[string](ctx, t, db, "SupplyCrates", spanner.Key{id}, "Barcode")
				if barcode != "BC-UNASSIGNED" {
					t.Errorf("created crate Barcode = %q, want default %q", barcode, "BC-UNASSIGNED")
				}
			},
		},
		{
			name:       "client-supplied value wins over default_create_fn",
			grants:     crateCreateGrants,
			body:       `[{"op":"add","path":"/supply-crates","value":{"label":"Spare Fuses","quantity":8,"priority":4,"status":"expedited"}}]`,
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, respBody []byte) {
				id := createdID(t, respBody, "supplyCrates")
				status := readColumn[string](ctx, t, db, "SupplyCrates", spanner.Key{id}, "Status")
				if status != "expedited" {
					t.Errorf("created crate Status = %q, want client value %q", status, "expedited")
				}
			},
		},
		{
			name:       "validateCreateType rejects invalid create",
			grants:     crateCreateGrants,
			body:       `[{"op":"add","path":"/supply-crates","value":{"label":"Empty Crate","quantity":0,"priority":4}}]`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "create leaves output_only_update_fn field unset",
			grants:     shipCreateGrants,
			body:       `[{"op":"add","path":"/ships","value":{"registryCode":"SSV-3001","name":"Pelican","cargoValue":1000}}]`,
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, respBody []byte) {
				id := createdID(t, respBody, "ships")
				updatedAt := readColumn[spanner.NullTime](ctx, t, db, "Ships", spanner.Key{id}, "UpdatedAt")
				if updatedAt.Valid {
					t.Errorf("created ship UpdatedAt = %v, want NULL", updatedAt.Time)
				}
			},
		},
		{
			name:       "update stamps output_only_update_fn field with commit timestamp",
			grants:     shipUpdateGrants,
			body:       fmt.Sprintf(`[{"op":"patch","path":"/ships/%s","value":{"name":"Vanta Prime"}}]`, shipVantaID),
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				updatedAt := readColumn[spanner.NullTime](ctx, t, db, "Ships", spanner.Key{shipVantaID}, "UpdatedAt")
				if !updatedAt.Valid {
					t.Error("updated ship UpdatedAt is NULL, want commit timestamp")
				}
			},
		},
		{
			name:       "client-supplied output_only_update_fn field is rejected on update",
			grants:     shipUpdateGrants,
			body:       fmt.Sprintf(`[{"op":"patch","path":"/ships/%s","value":{"name":"Vanta Prime","updatedAt":"2026-01-01T00:00:00Z"}}]`, shipVantaID),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "client-supplied output_only_update_fn field is rejected on create",
			grants:     shipCreateGrants,
			body:       `[{"op":"add","path":"/ships","value":{"registryCode":"SSV-3002","name":"Osprey","cargoValue":1000,"updatedAt":"2026-01-01T00:00:00Z"}}]`,
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
			testApp := newTestApp(db, tt.grants)

			status, respBody := doRequest(t, testApp, http.MethodPatch, "/api/resources", tt.body)
			assertStatus(t, status, tt.wantStatus, respBody)
			if tt.verify != nil {
				tt.verify(ctx, t, db, respBody)
			}
		})
	}
}
