package integration

// This suite covers the field-direction conditions on SupplyCrate:
//   - conditions:"input_only" (Notes): accepted and stored on create/update, but never
//     serialized in read or list responses.
//   - conditions:"output_only" (Barcode): present in read responses, but rejected with a
//     400 when supplied in any mutation body.

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	initiator "github.com/cccteam/db-initiator"
)

func TestFieldDirection(t *testing.T) {
	t.Parallel()

	crateReadGrants := grants{accesstypes.Read: {
		supplyCratesResource,
		fieldResource(supplyCratesResource, "label"),
		fieldResource(supplyCratesResource, "quantity"),
		fieldResource(supplyCratesResource, "priority"),
		fieldResource(supplyCratesResource, "status"),
		fieldResource(supplyCratesResource, "barcode"),
		fieldResource(supplyCratesResource, "inspectorBadge"),
		fieldResource(supplyCratesResource, "assignedShipId"),
	}}

	crateListGrants := grants{accesstypes.List: crateReadGrants[accesstypes.Read]}

	crateCreateGrants := grants{accesstypes.Create: {
		supplyCratesResource,
		fieldResource(supplyCratesResource, "label"),
		fieldResource(supplyCratesResource, "quantity"),
		fieldResource(supplyCratesResource, "priority"),
		fieldResource(supplyCratesResource, "notes"),
	}}

	crateUpdateGrants := grants{accesstypes.Update: {
		supplyCratesResource,
		fieldResource(supplyCratesResource, "quantity"),
		fieldResource(supplyCratesResource, "notes"),
	}}

	tests := []struct {
		name       string
		grants     grants
		method     string
		target     string
		body       string
		wantStatus int
		wantKeys   []string
		verify     func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, respBody []byte)
	}{
		{
			name:       "input_only field is stored on create",
			grants:     crateCreateGrants,
			method:     http.MethodPatch,
			target:     "/api/resources",
			body:       `[{"op":"add","path":"/supply-crates","value":{"label":"Med Kits","quantity":30,"priority":1,"notes":"Fragile"}}]`,
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, respBody []byte) {
				id := createdID(t, respBody, "supplyCrates")
				notes := readColumn[spanner.NullString](ctx, t, db, "SupplyCrates", spanner.Key{id}, "Notes")
				if !notes.Valid || notes.StringVal != "Fragile" {
					t.Errorf("created crate Notes = %v, want %q", notes, "Fragile")
				}
			},
		},
		{
			name:       "input_only field is stored on update",
			grants:     crateUpdateGrants,
			method:     http.MethodPatch,
			target:     "/api/resources",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/supply-crates/%s","value":{"notes":"Recounted"}}]`, crateRationsID),
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				notes := readColumn[spanner.NullString](ctx, t, db, "SupplyCrates", spanner.Key{crateRationsID}, "Notes")
				if !notes.Valid || notes.StringVal != "Recounted" {
					t.Errorf("updated crate Notes = %v, want %q", notes, "Recounted")
				}
			},
		},
		{
			// The seeded crate has Notes set; the read response must not contain it,
			// while the output_only barcode must be present.
			name:       "read returns output_only field but never input_only field",
			grants:     crateReadGrants,
			method:     http.MethodGet,
			target:     "/api/supply-crates/" + crateCoolant40ID,
			wantStatus: http.StatusOK,
			wantKeys:   []string{"id", "label", "quantity", "priority", "status", "barcode", "inspectorBadge", "assignedShipId"},
		},
		{
			name:       "list never returns input_only field",
			grants:     crateListGrants,
			method:     http.MethodGet,
			target:     "/api/supply-crates?filter=label:eq:Coolant%20Cells",
			wantStatus: http.StatusOK,
			verify: func(_ context.Context, t *testing.T, _ *initiator.SpannerDB, respBody []byte) {
				for _, row := range decodeRows(t, respBody) {
					assertKeys(t, row, []string{"id", "label", "quantity", "priority", "status", "barcode", "inspectorBadge", "assignedShipId"})
				}
			},
		},
		{
			name:       "output_only field is rejected on create",
			grants:     crateCreateGrants,
			method:     http.MethodPatch,
			target:     "/api/resources",
			body:       `[{"op":"add","path":"/supply-crates","value":{"label":"Med Kits","quantity":30,"priority":1,"barcode":"BC-FORGED"}}]`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "output_only field is rejected on update",
			grants:     crateUpdateGrants,
			method:     http.MethodPatch,
			target:     "/api/resources",
			body:       fmt.Sprintf(`[{"op":"patch","path":"/supply-crates/%s","value":{"quantity":41,"barcode":"BC-FORGED"}}]`, crateCoolant40ID),
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

			status, respBody := doRequest(t, testApp, tt.method, tt.target, tt.body)
			assertStatus(t, status, tt.wantStatus, respBody)
			if tt.wantStatus != http.StatusOK {
				return
			}
			if tt.wantKeys != nil {
				assertKeys(t, decodeRow(t, respBody), tt.wantKeys)
			}
			if tt.verify != nil {
				tt.verify(ctx, t, db, respBody)
			}
		})
	}
}
