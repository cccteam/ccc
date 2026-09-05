package integration

// This suite covers the reserved query parameters over the demo world: filter syntax
// and its indexed/allow_filter gating, PII filter placement (URL rejected, POST body
// accepted), sort, limit, offset, and rejection of unknown parameters. Consignments
// carry the sector-route cases (BondCode is unique-indexed, Mass is allow_filter) and
// Clients the PII placement cases (ContactEmail is pii + allow_filter).

import (
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

func TestQueryParameters(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}

	listGrants := grants{accesstypes.List: append(
		withFields("Consignments", "bondCode", "mass", "expiresOn"),
		withFields("Clients", "name", "trusted", "contactEmail")...,
	)}

	tests := []struct {
		name       string
		method     string
		target     string
		body       string
		wantStatus int
		wantRows   int
		wantCodes  []string
	}{
		{name: "filter eq on indexed field", target: sectorPath(anvil, "consignments?filter=bondCode:eq:BND-ANV-0001"), wantStatus: http.StatusOK, wantRows: 1},
		{name: "filter eq matching nothing returns empty list", target: sectorPath(anvil, "consignments?filter=bondCode:eq:BND-NONE"), wantStatus: http.StatusOK, wantRows: 0},
		{name: "filter allow_filter field combined with indexed field", target: sectorPath(anvil, "consignments?filter=bondCode:in:(BND-ANV-0001,BND-ANV-0002,BND-ANV-0003),mass:gt:100"), wantStatus: http.StatusOK, wantRows: 2},
		{name: "filter with only allow_filter fields is rejected", target: sectorPath(anvil, "consignments?filter=mass:gt:100"), wantStatus: http.StatusBadRequest},
		{name: "filter on field without index or allow_filter is rejected", target: sectorPath(anvil, "consignments?filter=description:eq:x"), wantStatus: http.StatusBadRequest},
		{name: "filter or across conditions", target: sectorPath(anvil, "consignments?filter=bondCode:eq:BND-ANV-0001|bondCode:eq:BND-ANV-0002"), wantStatus: http.StatusOK, wantRows: 2},
		{name: "filter cannot reach another sector's rows (row tenancy)", target: sectorPath(bastion, "consignments?filter=bondCode:eq:BND-ANV-0001"), wantStatus: http.StatusOK, wantRows: 0},
		{name: "sort ascending on untagged field", target: sectorPath(anvil, "consignments?sort=expiresOn"), wantStatus: http.StatusOK, wantRows: 3, wantCodes: []string{"BND-ANV-0002", "BND-ANV-0001", "BND-ANV-0003"}},
		{name: "sort descending", target: sectorPath(anvil, "consignments?sort=expiresOn:desc"), wantStatus: http.StatusOK, wantRows: 3, wantCodes: []string{"BND-ANV-0003", "BND-ANV-0001", "BND-ANV-0002"}},
		{name: "sort with unknown field is rejected", target: sectorPath(anvil, "consignments?sort=warpFactor"), wantStatus: http.StatusBadRequest},
		{name: "limit caps the row count", target: sectorPath(anvil, "consignments?sort=expiresOn&limit=1"), wantStatus: http.StatusOK, wantRows: 1, wantCodes: []string{"BND-ANV-0002"}},
		{name: "offset skips rows", target: sectorPath(anvil, "consignments?sort=expiresOn&limit=1&offset=1"), wantStatus: http.StatusOK, wantRows: 1, wantCodes: []string{"BND-ANV-0001"}},
		{name: "non-numeric limit is rejected", target: sectorPath(anvil, "consignments?limit=all"), wantStatus: http.StatusBadRequest},
		{name: "negative offset is rejected", target: sectorPath(anvil, "consignments?offset=-1"), wantStatus: http.StatusBadRequest},
		{name: "unknown query parameter is rejected", target: sectorPath(anvil, "consignments?warp=9"), wantStatus: http.StatusBadRequest},
		{name: "filter on pii field in url is rejected", target: "/api/clients?filter=name:eq:Halvard%20Freight,contactEmail:eq:cleo@halvard.example", wantStatus: http.StatusBadRequest},
		{name: "filter on pii field in post body is allowed", method: http.MethodPost, target: "/api/clients", body: `{"filter":"name:eq:Halvard Freight,contactEmail:eq:cleo@halvard.example"}`, wantStatus: http.StatusOK, wantRows: 1},
		{name: "filter in both query and body is rejected", method: http.MethodPost, target: "/api/clients?filter=name:eq:Halvard%20Freight", body: `{"filter":"name:eq:Halvard Freight"}`, wantStatus: http.StatusBadRequest},
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
			for i, want := range tt.wantCodes {
				if got := rows[i]["bondCode"]; got != want {
					t.Errorf("row %d bondCode = %v, want %q", i, got, want)
				}
			}
		})
	}
}
