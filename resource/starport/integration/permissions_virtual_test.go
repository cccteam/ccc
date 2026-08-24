package integration

// This suite asserts permission behavior of the virtual resources: list-only
// projections backed by embedded subqueries instead of table metadata. Their primary
// keys come from primarykey field annotations, so the perm:"-" exemption in the
// generated list request structs flows from annotations rather than the schema —
// key fields follow the resource-level grant, and every other field requires its own
// field grant. ShipCargoSummaries has a single-field annotated key; ManifestLines has
// a compound annotated key, so both of its key fields are exempt. Both resources are
// fully structural, so these assertions are as invariant as the invariant suite.

import (
	"net/http"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

const (
	shipCargoSummariesResource = accesstypes.Resource("ShipCargoSummaries")
	manifestLinesResource      = accesstypes.Resource("ManifestLines")
)

func TestVirtualResourceQuery(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, "file://../schema/migrations", "file://testdata/seed")
	if err != nil {
		t.Fatal(err)
	}

	summaryFieldsList := grants{accesstypes.List: {
		shipCargoSummariesResource,
		fieldResource(shipCargoSummariesResource, "shipName"),
		fieldResource(shipCargoSummariesResource, "dockingBayName"),
		fieldResource(shipCargoSummariesResource, "manifestLines"),
		fieldResource(shipCargoSummariesResource, "totalDeclaredValue"),
	}}

	tests := []struct {
		name       string
		grants     grants
		target     string
		wantStatus int
		wantRows   int
		wantKeys   []string
	}{
		{
			name:       "list without resource grant is forbidden",
			grants:     nil,
			target:     "/api/ship-cargo-summaries",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "list with resource-only grant returns exactly the annotated primary key",
			grants:     grants{accesstypes.List: {shipCargoSummariesResource}},
			target:     "/api/ship-cargo-summaries",
			wantStatus: http.StatusOK,
			wantRows:   2,
			wantKeys:   []string{"shipId"},
		},
		{
			name: "list with field grants returns the granted fields plus the key",
			grants: grants{accesstypes.List: {
				shipCargoSummariesResource,
				fieldResource(shipCargoSummariesResource, "shipName"),
			}},
			target:     "/api/ship-cargo-summaries",
			wantStatus: http.StatusOK,
			wantRows:   2,
			wantKeys:   []string{"shipId", "shipName"},
		},
		{
			name:       "requested key column needs no field grant",
			grants:     grants{accesstypes.List: {shipCargoSummariesResource}},
			target:     "/api/ship-cargo-summaries?columns=shipId",
			wantStatus: http.StatusOK,
			wantRows:   2,
			wantKeys:   []string{"shipId"},
		},
		{
			name:       "requested column without field grant is forbidden",
			grants:     grants{accesstypes.List: {shipCargoSummariesResource}},
			target:     "/api/ship-cargo-summaries?columns=totalDeclaredValue",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "filter on an indexed virtual field",
			grants:     summaryFieldsList,
			target:     "/api/ship-cargo-summaries?filter=shipName:eq:Vanta",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantKeys:   []string{"shipId", "shipName", "dockingBayName", "manifestLines", "totalDeclaredValue"},
		},
		{
			name:       "compound-key list with resource-only grant returns exactly the two annotated key fields",
			grants:     grants{accesstypes.List: {manifestLinesResource}},
			target:     "/api/manifest-lines",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantKeys:   []string{"shipId", "lineNumber"},
		},
		{
			name: "compound-key list with field grants returns the granted fields plus both key fields",
			grants: grants{accesstypes.List: {
				manifestLinesResource,
				fieldResource(manifestLinesResource, "shipName"),
				fieldResource(manifestLinesResource, "details"),
			}},
			target:     "/api/manifest-lines",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantKeys:   []string{"shipId", "lineNumber", "shipName", "details"},
		},
		{
			name:       "compound-key non-key column without field grant is forbidden",
			grants:     grants{accesstypes.List: {manifestLinesResource}},
			target:     "/api/manifest-lines?columns=details",
			wantStatus: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			testApp := newTestApp(db, tt.grants)

			status, body := doRequest(t, testApp, http.MethodGet, tt.target, "")
			assertStatus(t, status, tt.wantStatus, body)
			if tt.wantStatus != http.StatusOK {
				return
			}

			rows := decodeRows(t, body)
			if len(rows) != tt.wantRows {
				t.Fatalf("row count = %d, want %d: %s", len(rows), tt.wantRows, body)
			}
			for _, row := range rows {
				assertKeys(t, row, tt.wantKeys)
			}
		})
	}
}

// TestVirtualResourceSubquery asserts the subquery data path end to end: the rollup
// aggregates the seeded base tables correctly, including a ship with no docking bay
// and no manifest lines.
func TestVirtualResourceSubquery(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, "file://../schema/migrations", "file://testdata/seed")
	if err != nil {
		t.Fatal(err)
	}

	testApp := newTestApp(db, grants{accesstypes.List: {
		shipCargoSummariesResource,
		fieldResource(shipCargoSummariesResource, "shipName"),
		fieldResource(shipCargoSummariesResource, "dockingBayName"),
		fieldResource(shipCargoSummariesResource, "manifestLines"),
		fieldResource(shipCargoSummariesResource, "totalDeclaredValue"),
	}})

	status, body := doRequest(t, testApp, http.MethodGet, "/api/ship-cargo-summaries?sort=shipName", "")
	assertStatus(t, status, http.StatusOK, body)

	rows := decodeRows(t, body)
	if len(rows) != 2 {
		t.Fatalf("row count = %d, want 2: %s", len(rows), body)
	}

	// Sorted by shipName: Comost (undocked, no manifests), then Vanta (Bay Alpha, one line).
	comost, vanta := rows[0], rows[1]

	if got := comost["shipName"]; got != "Comost" {
		t.Errorf("rows[0].shipName = %v, want Comost", got)
	}
	if got := comost["dockingBayName"]; got != nil {
		t.Errorf("Comost dockingBayName = %v, want null", got)
	}
	if got := comost["manifestLines"]; got != float64(0) {
		t.Errorf("Comost manifestLines = %v, want 0", got)
	}
	if got := comost["totalDeclaredValue"]; got != float64(0) {
		t.Errorf("Comost totalDeclaredValue = %v, want 0", got)
	}

	if got := vanta["shipName"]; got != "Vanta" {
		t.Errorf("rows[1].shipName = %v, want Vanta", got)
	}
	if got := vanta["dockingBayName"]; got != "Bay Alpha" {
		t.Errorf("Vanta dockingBayName = %v, want Bay Alpha", got)
	}
	if got := vanta["manifestLines"]; got != float64(1) {
		t.Errorf("Vanta manifestLines = %v, want 1", got)
	}
	if got := vanta["totalDeclaredValue"]; got != float64(90000) {
		t.Errorf("Vanta totalDeclaredValue = %v, want 90000", got)
	}
}
