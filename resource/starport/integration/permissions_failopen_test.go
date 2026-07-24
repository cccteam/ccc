package integration

// This suite pins the CURRENT fail-open behavior of untagged fields: a field with no
// perm tag is accessible to any user holding the resource-level grant. Existing
// applications depend on this behavior today.
//
// When field permissions migrate from fail open to fail closed, this suite is expected
// to be deliberately rewritten — that rewrite documents the behavior change. Contrast
// with permissions_invariant_test.go, which must survive the migration untouched.

import (
	"fmt"
	"net/http"
	"testing"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
)

const (
	dockingBaysResource    = accesstypes.Resource("DockingBays")
	cargoManifestsResource = accesstypes.Resource("CargoManifests")
)

func TestFailOpenQuery(t *testing.T) {
	// No t.Parallel(): all integration tests share one Spanner emulator instance, and
	// concurrent database creation/DDL across tests is unreliable on the emulator.
	ctx := t.Context()

	db, err := prepareDatabase(ctx, t)
	if err != nil {
		t.Fatal(err)
	}
	seedDatabase(ctx, t, db)

	tests := []struct {
		name       string
		grants     grants
		target     string
		wantStatus int
		wantRows   int
		wantKeys   []string
	}{
		{
			name:       "untagged resource still requires the resource grant",
			grants:     nil,
			target:     "/api/docking-bays",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "resource-only grant returns every untagged field",
			grants:     grants{accesstypes.List: {dockingBaysResource}},
			target:     "/api/docking-bays",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantKeys:   []string{"id", "name", "deckLevel", "maxTonnage"},
		},
		{
			name:       "untagged requested columns require no field grants",
			grants:     grants{accesstypes.List: {dockingBaysResource}},
			target:     "/api/docking-bays?columns=name,deckLevel",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantKeys:   []string{"name", "deckLevel"},
		},
		{
			name:       "mixed resource returns untagged fields and filters denied tagged fields",
			grants:     grants{accesstypes.List: {cargoManifestsResource}},
			target:     "/api/cargo-manifests",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantKeys:   []string{"shipId", "lineNumber", "details", "quantity"},
		},
		{
			name: "mixed resource includes tagged fields with a field grant",
			grants: grants{accesstypes.List: {
				cargoManifestsResource,
				fieldResource(cargoManifestsResource, "declaredValue"),
			}},
			target:     "/api/cargo-manifests",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantKeys:   []string{"shipId", "lineNumber", "details", "quantity", "declaredValue"},
		},
		{
			name:       "tagged requested column in a mixed resource is still forbidden without a field grant",
			grants:     grants{accesstypes.List: {cargoManifestsResource}},
			target:     "/api/cargo-manifests?columns=declaredValue",
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

func TestFailOpenMutation(t *testing.T) {
	// No t.Parallel(): all integration tests share one Spanner emulator instance, and
	// concurrent database creation/DDL across tests is unreliable on the emulator.
	ctx := t.Context()

	db, err := prepareDatabase(ctx, t)
	if err != nil {
		t.Fatal(err)
	}
	seedDatabase(ctx, t, db)

	// The subtests below share one database and run sequentially: each case targets rows
	// that earlier cases do not modify.

	t.Run("create with resource-only grant may set every untagged field", func(t *testing.T) {
		testApp := newTestApp(db, grants{accesstypes.Create: {dockingBaysResource}})
		body := `[{"op":"add","path":"/docking-bays","value":{"name":"Bay Beta","deckLevel":5,"maxTonnage":90000}}]`
		status, respBody := doRequest(t, testApp, http.MethodPatch, "/api/resources", body)
		assertStatus(t, status, http.StatusOK, respBody)

		resp := decodeRow(t, respBody)
		created, ok := resp["dockingBays"].([]any)
		if !ok || len(created) != 1 {
			t.Fatalf("expected one created docking bay id, got: %s", respBody)
		}
		createdID, ok := created[0].(string)
		if !ok {
			t.Fatalf("created docking bay id is not a string: %s", respBody)
		}
		name := readColumn[string](ctx, t, db, "DockingBays", spanner.Key{createdID}, "Name")
		if name != "Bay Beta" {
			t.Errorf("created docking bay Name = %q, want %q", name, "Bay Beta")
		}
	})

	t.Run("update untagged field with resource-only grant is allowed", func(t *testing.T) {
		testApp := newTestApp(db, grants{accesstypes.Update: {cargoManifestsResource}})
		body := fmt.Sprintf(`[{"op":"patch","path":"/cargo-manifests/%s/1","value":{"details":"Hull plating (recount)"}}]`, shipVantaID)
		status, respBody := doRequest(t, testApp, http.MethodPatch, "/api/resources", body)
		assertStatus(t, status, http.StatusOK, respBody)

		details := readColumn[string](ctx, t, db, "CargoManifests", spanner.Key{shipVantaID, 1}, "Details")
		if details != "Hull plating (recount)" {
			t.Errorf("manifest Details = %q, want %q", details, "Hull plating (recount)")
		}
	})

	t.Run("update tagged field still requires the field grant", func(t *testing.T) {
		testApp := newTestApp(db, grants{accesstypes.Update: {cargoManifestsResource}})
		body := fmt.Sprintf(`[{"op":"patch","path":"/cargo-manifests/%s/1","value":{"declaredValue":95000}}]`, shipVantaID)
		status, respBody := doRequest(t, testApp, http.MethodPatch, "/api/resources", body)
		assertStatus(t, status, http.StatusForbidden, respBody)

		declaredValue := readColumn[int64](ctx, t, db, "CargoManifests", spanner.Key{shipVantaID, 1}, "DeclaredValue")
		if declaredValue != 90000 {
			t.Errorf("manifest DeclaredValue = %d, want unchanged %d", declaredValue, 90000)
		}
	})
}
