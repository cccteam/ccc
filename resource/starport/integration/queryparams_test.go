package integration

// This suite covers the reserved query parameters (columns is covered by the permission
// suites): filter syntax and its indexed/allow_filter gating, PII filter placement,
// sort, limit, offset, and rejection of unknown parameters. It runs read-only against
// the SupplyCrates seed data (see supply_crates.go for the field/tag layout).

import (
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

const supplyCratesResource = accesstypes.Resource("SupplyCrates")

func TestQueryParameters(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, "file://../schema/migrations", "file://testdata/seed")
	if err != nil {
		t.Fatal(err)
	}

	listGrants := grants{accesstypes.List: {
		supplyCratesResource,
		fieldResource(supplyCratesResource, "label"),
		fieldResource(supplyCratesResource, "priority"),
		fieldResource(supplyCratesResource, "status"),
		fieldResource(supplyCratesResource, "inspectorBadge"),
	}}

	tests := []struct {
		name       string
		method     string // defaults to GET
		target     string
		body       string
		wantStatus int
		wantRows   int
		wantLabels []string // asserted in response order when non-nil
	}{
		{
			name:       "filter eq on indexed field",
			target:     "/api/supply-crates?filter=label:eq:Coolant%20Cells",
			wantStatus: http.StatusOK,
			wantRows:   2,
		},
		{
			name:       "filter eq matching nothing returns empty list",
			target:     "/api/supply-crates?filter=label:eq:Phantom%20Cargo",
			wantStatus: http.StatusOK,
			wantRows:   0,
		},
		{
			name:       "filter allow_filter field combined with indexed field",
			target:     "/api/supply-crates?filter=label:eq:Coolant%20Cells,quantity:gt:20",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantLabels: []string{"Coolant Cells"},
		},
		{
			// Filter fields are not checked against field grants: quantity carries no
			// List grant here, yet filtering on it succeeds. This pins current behavior.
			name:       "filter field does not require a field grant",
			target:     "/api/supply-crates?filter=label:eq:Coolant%20Cells,quantity:lte:12",
			wantStatus: http.StatusOK,
			wantRows:   1,
		},
		{
			name:       "filter with only allow_filter fields is rejected",
			target:     "/api/supply-crates?filter=quantity:gt:20",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "filter on field without index or allow_filter is rejected",
			target:     "/api/supply-crates?filter=priority:eq:1",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "filter with unknown operator is rejected",
			target:     "/api/supply-crates?filter=label:like:Coolant",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "filter or across conditions",
			target:     "/api/supply-crates?filter=label:eq:Ration%20Packs|label:eq:Coolant%20Cells",
			wantStatus: http.StatusOK,
			wantRows:   3,
		},
		{
			name:       "filter in operator with value list",
			target:     "/api/supply-crates?filter=label:in:(Ration%20Packs,Coolant%20Cells)",
			wantStatus: http.StatusOK,
			wantRows:   3,
		},
		{
			name:       "filter isnull on indexed nullable field",
			target:     "/api/supply-crates?filter=assignedShipId:isnull",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantLabels: []string{"Ration Packs"},
		},
		{
			name:       "filter on pii field in url is rejected",
			target:     "/api/supply-crates?filter=label:eq:Coolant%20Cells,inspectorBadge:eq:INSP-77",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "filter on pii field in post body is allowed",
			method:     http.MethodPost,
			target:     "/api/supply-crates",
			body:       `{"filter":"assignedShipId:isnotnull,inspectorBadge:eq:INSP-77"}`,
			wantStatus: http.StatusOK,
			wantRows:   1,
		},
		{
			name:       "filter in both query and body is rejected",
			method:     http.MethodPost,
			target:     "/api/supply-crates?filter=label:eq:Coolant%20Cells",
			body:       `{"filter":"label:eq:Coolant Cells"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			// sort requires no index or allow_filter tag on the field.
			name:       "sort ascending on untagged field",
			target:     "/api/supply-crates?sort=priority",
			wantStatus: http.StatusOK,
			wantRows:   3,
			wantLabels: []string{"Ration Packs", "Coolant Cells", "Coolant Cells"},
		},
		{
			name:       "sort descending",
			target:     "/api/supply-crates?sort=priority:desc",
			wantStatus: http.StatusOK,
			wantRows:   3,
			wantLabels: []string{"Coolant Cells", "Coolant Cells", "Ration Packs"},
		},
		{
			name:       "sort with unknown field is rejected",
			target:     "/api/supply-crates?sort=warpFactor",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "sort with invalid direction is rejected",
			target:     "/api/supply-crates?sort=priority:sideways",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "limit caps the row count",
			target:     "/api/supply-crates?sort=priority&limit=1",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantLabels: []string{"Ration Packs"},
		},
		{
			name:       "offset skips rows",
			target:     "/api/supply-crates?sort=priority&limit=1&offset=1",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantLabels: []string{"Coolant Cells"},
		},
		{
			name:       "non-numeric limit is rejected",
			target:     "/api/supply-crates?limit=all",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "negative offset is rejected",
			target:     "/api/supply-crates?offset=-1",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown query parameter is rejected",
			target:     "/api/supply-crates?warp=9",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			testApp := newTestApp(db, listGrants)

			method := tt.method
			if method == "" {
				method = http.MethodGet
			}

			status, body := doRequest(t, testApp, method, tt.target, tt.body)
			assertStatus(t, status, tt.wantStatus, body)
			if tt.wantStatus != http.StatusOK {
				return
			}

			rows := decodeRows(t, body)
			if len(rows) != tt.wantRows {
				t.Fatalf("row count = %d, want %d: %s", len(rows), tt.wantRows, body)
			}
			if tt.wantLabels != nil {
				for i, want := range tt.wantLabels {
					if got := rows[i]["label"]; got != want {
						t.Errorf("row %d label = %v, want %q", i, got, want)
					}
				}
			}
		})
	}
}
