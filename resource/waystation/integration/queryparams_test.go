package integration

// This suite covers the reserved query parameters over the demo world: filter syntax
// and its indexed/allow_filter gating, PII filter placement (URL rejected, POST body
// accepted), sort, limit, offset, and rejection of unknown parameters. InventoryLots
// carries the domain-route cases (including the deliberate E2-gap pin: the domain
// partitions permissions, not rows) and Suppliers carries the PII placement cases
// (ContactEmail is pii + allow_filter; Name is filterable through its unique index).
//
// Seed facts (schema/demoseed): ws-alpha lots A-01 (quantity 42), H-03 (quantity 6),
// B-02 (quantity 3); ws-beta lot BA-1 (quantity 10). Suppliers Helion and Kuiper are
// active, Redline (dex@redline.example) is not.

import (
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

const (
	inventoryLotsResource = accesstypes.Resource("InventoryLots")
	suppliersResource     = accesstypes.Resource("Suppliers")
)

func TestQueryParameters(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	listGrants := grants{accesstypes.List: {
		inventoryLotsResource,
		fieldResource(inventoryLotsResource, "binLocation"),
		fieldResource(inventoryLotsResource, "quantity"),
		suppliersResource,
		fieldResource(suppliersResource, "name"),
		fieldResource(suppliersResource, "active"),
		fieldResource(suppliersResource, "contactEmail"),
	}}

	tests := []struct {
		name          string
		method        string // defaults to GET
		target        string
		body          string
		wantStatus    int
		wantRows      int
		wantLocations []string // binLocation values asserted in response order when non-nil
	}{
		{
			name:       "filter eq on indexed field",
			target:     "/api/waystations/ws-alpha/inventory-lots?filter=binLocation:eq:A-01",
			wantStatus: http.StatusOK,
			wantRows:   1,
		},
		{
			name:       "filter eq matching nothing returns empty list",
			target:     "/api/waystations/ws-alpha/inventory-lots?filter=binLocation:eq:ZZ-99",
			wantStatus: http.StatusOK,
			wantRows:   0,
		},
		{
			name:       "filter allow_filter field combined with indexed field",
			target:     "/api/waystations/ws-alpha/inventory-lots?filter=binLocation:in:(A-01,H-03,B-02),quantity:gt:5",
			wantStatus: http.StatusOK,
			wantRows:   2,
		},
		{
			name:       "filter with only allow_filter fields is rejected",
			target:     "/api/waystations/ws-alpha/inventory-lots?filter=quantity:gt:5",
			wantStatus: http.StatusBadRequest,
		},
		{
			// ExpiresOn carries neither an index nor allow_filter. (Foreign-key
			// columns like catalogItemId ARE filterable: Spanner FKs create backing
			// indexes and the generator derives filterability from the schema.)
			name:       "filter on field without index or allow_filter is rejected",
			target:     "/api/waystations/ws-alpha/inventory-lots?filter=expiresOn:isnull",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "filter or across conditions",
			target:     "/api/waystations/ws-alpha/inventory-lots?filter=binLocation:eq:A-01|binLocation:eq:B-02",
			wantStatus: http.StatusOK,
			wantRows:   2,
		},
		{
			// Structural row tenancy (E2): the ws-beta route cannot reach
			// ws-alpha's lot even with an unconditional grant and a filter that
			// matches it — the injected tenant predicate partitions the rows, and
			// a narrowed list is empty, never a 403.
			name:       "filter cannot reach another station's rows (row tenancy)",
			target:     "/api/waystations/ws-beta/inventory-lots?filter=binLocation:eq:A-01",
			wantStatus: http.StatusOK,
			wantRows:   0,
		},
		{
			// Three rows: ws-beta's BA-1 lot never appears on the ws-alpha route —
			// the tenant predicate is in the WHERE before sorting.
			name:          "sort ascending on untagged field",
			target:        "/api/waystations/ws-alpha/inventory-lots?sort=quantity",
			wantStatus:    http.StatusOK,
			wantRows:      3,
			wantLocations: []string{"B-02", "H-03", "A-01"},
		},
		{
			name:          "sort descending",
			target:        "/api/waystations/ws-alpha/inventory-lots?sort=quantity:desc",
			wantStatus:    http.StatusOK,
			wantRows:      3,
			wantLocations: []string{"A-01", "H-03", "B-02"},
		},
		{
			name:       "sort with unknown field is rejected",
			target:     "/api/waystations/ws-alpha/inventory-lots?sort=warpFactor",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:          "limit caps the row count",
			target:        "/api/waystations/ws-alpha/inventory-lots?sort=quantity&limit=1",
			wantStatus:    http.StatusOK,
			wantRows:      1,
			wantLocations: []string{"B-02"},
		},
		{
			name:          "offset skips rows",
			target:        "/api/waystations/ws-alpha/inventory-lots?sort=quantity&limit=1&offset=1",
			wantStatus:    http.StatusOK,
			wantRows:      1,
			wantLocations: []string{"H-03"},
		},
		{
			name:       "non-numeric limit is rejected",
			target:     "/api/waystations/ws-alpha/inventory-lots?limit=all",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "negative offset is rejected",
			target:     "/api/waystations/ws-alpha/inventory-lots?offset=-1",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown query parameter is rejected",
			target:     "/api/waystations/ws-alpha/inventory-lots?warp=9",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "filter on pii field in url is rejected",
			target:     "/api/suppliers?filter=name:eq:Redline%20Salvage,contactEmail:eq:dex@redline.example",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "filter on pii field in post body is allowed",
			method:     http.MethodPost,
			target:     "/api/suppliers",
			body:       `{"filter":"name:eq:Redline Salvage,contactEmail:eq:dex@redline.example"}`,
			wantStatus: http.StatusOK,
			wantRows:   1,
		},
		{
			name:       "filter in both query and body is rejected",
			method:     http.MethodPost,
			target:     "/api/suppliers?filter=name:eq:Redline%20Salvage",
			body:       `{"filter":"name:eq:Redline Salvage"}`,
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
			if tt.wantLocations != nil {
				for i, want := range tt.wantLocations {
					if got := rows[i]["binLocation"]; got != want {
						t.Errorf("row %d binLocation = %v, want %q", i, got, want)
					}
				}
			}
		})
	}
}
