package integration

// This suite pins the fail-closed structural enforcement of field permissions on
// annotation-free resources: every non-primary-key field requires the endpoint's
// permission on its field resource, and a resource-only grant exposes nothing beyond
// the primary key. It deliberately rewrote the pre-migration fail-open suite — the
// rewrite documents the behavior change. Contrast with permissions_invariant_test.go,
// which must survive the migration untouched.

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
	dockingBaysResource    = accesstypes.Resource("DockingBays")
	cargoManifestsResource = accesstypes.Resource("CargoManifests")
)

func TestFailClosedQuery(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	db, err := prepareDatabase(ctx, t, "file://../schema/migrations", "file://testdata/seed")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		grants     grants
		target     string
		wantStatus int
		wantRows   int
		wantKeys   []string
	}{
		{
			name:       "the resource grant is still required",
			grants:     nil,
			target:     "/api/docking-bays",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "resource-only grant returns only the primary key",
			grants:     grants{accesstypes.List: {dockingBaysResource}},
			target:     "/api/docking-bays",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantKeys:   []string{"id"},
		},
		{
			name:       "requested columns without field grants are forbidden",
			grants:     grants{accesstypes.List: {dockingBaysResource}},
			target:     "/api/docking-bays?columns=name,deckLevel",
			wantStatus: http.StatusForbidden,
		},
		{
			name: "requested columns with field grants are returned",
			grants: grants{accesstypes.List: {
				dockingBaysResource,
				fieldResource(dockingBaysResource, "name"),
				fieldResource(dockingBaysResource, "deckLevel"),
			}},
			target:     "/api/docking-bays?columns=name,deckLevel",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantKeys:   []string{"name", "deckLevel"},
		},
		{
			name:       "resource-only grant on a composite-key resource returns only the key fields",
			grants:     grants{accesstypes.List: {cargoManifestsResource}},
			target:     "/api/cargo-manifests",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantKeys:   []string{"shipId", "lineNumber"},
		},
		{
			name: "field grants add exactly the granted fields",
			grants: grants{accesstypes.List: {
				cargoManifestsResource,
				fieldResource(cargoManifestsResource, "declaredValue"),
			}},
			target:     "/api/cargo-manifests",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantKeys:   []string{"shipId", "lineNumber", "declaredValue"},
		},
		{
			name: "formerly-untagged fields require grants like any other field",
			grants: grants{accesstypes.List: {
				cargoManifestsResource,
				fieldResource(cargoManifestsResource, "details"),
				fieldResource(cargoManifestsResource, "quantity"),
			}},
			target:     "/api/cargo-manifests",
			wantStatus: http.StatusOK,
			wantRows:   1,
			wantKeys:   []string{"shipId", "lineNumber", "details", "quantity"},
		},
		{
			name:       "requested formerly-untagged column without its grant is forbidden",
			grants:     grants{accesstypes.List: {cargoManifestsResource}},
			target:     "/api/cargo-manifests?columns=details",
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "requested declaredValue without its grant is forbidden",
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

func TestFailClosedMutation(t *testing.T) {
	t.Parallel()

	// Each case prepares its own seeded database, fully isolating the cases from one
	// another.
	tests := []struct {
		name       string
		grants     grants
		body       string
		wantStatus int
		verify     func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, respBody []byte)
	}{
		{
			name:       "create with resource-only grant is forbidden",
			grants:     grants{accesstypes.Create: {dockingBaysResource}},
			body:       `[{"op":"add","path":"/docking-bays","value":{"name":"Bay Beta","deckLevel":5,"maxTonnage":90000}}]`,
			wantStatus: http.StatusForbidden,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				if count := countRows(ctx, t, db, "DockingBays"); count != 1 {
					t.Errorf("DockingBays row count = %d, want the seeded 1 (forbidden create must roll back)", count)
				}
			},
		},
		{
			name: "create with field grants sets every granted field",
			grants: grants{accesstypes.Create: {
				dockingBaysResource,
				fieldResource(dockingBaysResource, "name"),
				fieldResource(dockingBaysResource, "deckLevel"),
				fieldResource(dockingBaysResource, "maxTonnage"),
			}},
			body:       `[{"op":"add","path":"/docking-bays","value":{"name":"Bay Beta","deckLevel":5,"maxTonnage":90000}}]`,
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, respBody []byte) {
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
			},
		},
		{
			name:       "update formerly-untagged field without its grant is forbidden",
			grants:     grants{accesstypes.Update: {cargoManifestsResource}},
			body:       fmt.Sprintf(`[{"op":"patch","path":"/cargo-manifests/%s/1","value":{"details":"Hull plating (recount)"}}]`, shipVantaID),
			wantStatus: http.StatusForbidden,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				details := readColumn[string](ctx, t, db, "CargoManifests", spanner.Key{shipVantaID, 1}, "Details")
				if details != "Hull plating" {
					t.Errorf("manifest Details = %q, want unchanged %q", details, "Hull plating")
				}
			},
		},
		{
			name: "update formerly-untagged field with its grant is allowed",
			grants: grants{accesstypes.Update: {
				cargoManifestsResource,
				fieldResource(cargoManifestsResource, "details"),
			}},
			body:       fmt.Sprintf(`[{"op":"patch","path":"/cargo-manifests/%s/1","value":{"details":"Hull plating (recount)"}}]`, shipVantaID),
			wantStatus: http.StatusOK,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				details := readColumn[string](ctx, t, db, "CargoManifests", spanner.Key{shipVantaID, 1}, "Details")
				if details != "Hull plating (recount)" {
					t.Errorf("manifest Details = %q, want %q", details, "Hull plating (recount)")
				}
			},
		},
		{
			name:       "update declaredValue still requires the field grant",
			grants:     grants{accesstypes.Update: {cargoManifestsResource}},
			body:       fmt.Sprintf(`[{"op":"patch","path":"/cargo-manifests/%s/1","value":{"declaredValue":95000}}]`, shipVantaID),
			wantStatus: http.StatusForbidden,
			verify: func(ctx context.Context, t *testing.T, db *initiator.SpannerDB, _ []byte) {
				declaredValue := readColumn[int64](ctx, t, db, "CargoManifests", spanner.Key{shipVantaID, 1}, "DeclaredValue")
				if declaredValue != 90000 {
					t.Errorf("manifest DeclaredValue = %d, want unchanged %d", declaredValue, 90000)
				}
			},
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

// countRows counts the rows of a table directly in the database.
func countRows(ctx context.Context, t *testing.T, db *initiator.SpannerDB, table string) int64 {
	t.Helper()

	iter := db.Single().Query(ctx, spanner.Statement{SQL: "SELECT COUNT(*) FROM " + table})
	defer iter.Stop()

	row, err := iter.Next()
	if err != nil {
		t.Fatalf("Query(COUNT %s): %v", table, err)
	}

	var count int64
	if err := row.Columns(&count); err != nil {
		t.Fatalf("row.Columns(count): %v", err)
	}

	return count
}
