package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

const (
	tenantScopedResource = `package resources

type (
	// Announcement is posted within one tenant.
	//
	// @resource
	// @permissionScope(domain)
	Announcement struct {
		ID string ` + "`spanner:\"Id\"`" + `
	}
)
`
	globalResource = `package resources

// Lens is global.
//
// @resource
type Lens struct {
	ID string ` + "`spanner:\"Id\"`" + `
}
`
	tenantsMigration = "CREATE TABLE Tenants (\n  Id STRING(64) NOT NULL,\n) PRIMARY KEY (Id);\n"
)

func TestTenancyWired(t *testing.T) {
	t.Parallel()

	tenanted := program("pkg/resources",
		`generation.GenerateHandlers("app"),`,
		`generation.GenerateRoutes("pkg/router", "api"),`,
		`generation.WithDomainRoute("tenants"),`,
	)
	untenanted := program("pkg/resources",
		`generation.GenerateHandlers("app"),`,
		`generation.GenerateRoutes("pkg/router", "api"),`,
	)

	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "tenanted and wired",
			files: map[string]string{
				"cmd/generate/main.go":                    tenanted,
				"pkg/resources/announcements.go":          tenantScopedResource,
				"schema/migrations/000005_Tenants.up.sql": tenantsMigration,
			},
			wantStatus:  Pass,
			wantSummary: "tenant record Tenants (schema/migrations/000005_Tenants.up.sql); 1 tenant-scoped resource(s)",
		},
		{
			name: "tenanted with nothing behind it",
			files: map[string]string{
				"cmd/generate/main.go":              tenanted,
				"pkg/resources/lenses.go":           globalResource,
				"schema/migrations/000001_X.up.sql": "CREATE TABLE Lenses (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n",
			},
			wantStatus:  Fail,
			wantSummary: "2 tenancy wiring problem(s)",
			wantDetails: []string{
				`WithDomainRoute("tenants"): no migration creates a table named like it (the tenant-record table)`,
				"no struct is annotated @permissionScope(domain): every resource is global, and the tenant segment serves nothing",
			},
		},
		{
			name: "tenanted, the table created in another spelling",
			files: map[string]string{
				"cmd/generate/main.go": program("pkg/resources",
					`generation.GenerateHandlers("app"),`,
					`generation.GenerateRoutes("pkg/router", "api"),`,
					`generation.WithDomainRoute("space-stations"),`,
				),
				"pkg/resources/announcements.go":                tenantScopedResource,
				"schema/migrations/000005_SpaceStations.up.sql": "create table if not exists `SpaceStations` (Id STRING(64) NOT NULL) PRIMARY KEY (Id);\n",
			},
			wantStatus:  Pass,
			wantSummary: "tenant record SpaceStations (schema/migrations/000005_SpaceStations.up.sql); 1 tenant-scoped resource(s)",
		},
		{
			name: "untenanted and clean",
			files: map[string]string{
				"cmd/generate/main.go":    untenanted,
				"pkg/resources/lenses.go": globalResource,
			},
			wantStatus:  Pass,
			wantSummary: "not tenanted: no tenant-scoped resources",
		},
		{
			name: "untenanted with a tenant-scoped resource lying around",
			files: map[string]string{
				"cmd/generate/main.go":           untenanted,
				"pkg/resources/announcements.go": tenantScopedResource,
			},
			wantStatus:  Fail,
			wantSummary: "1 tenancy wiring problem(s) in an untenanted application",
			wantDetails: []string{
				"pkg/resources/announcements.go:8: Announcement is @permissionScope(domain), but no WithDomainRoute names the tenant segment; it is served under the default /domain/{domain}/ pair",
			},
		},
		{
			name: "no site generator",
			files: map[string]string{
				"cmd/generate/main.go": program("pkg/sharedresources", `generation.GenerateTypescript("web/src"),`),
			},
			wantStatus:  Skip,
			wantSummary: "no site generator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			write := func(rel, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write("go.mod", "module example.com/harbor\n\ngo 1.26.6\n")
			for rel, content := range tt.files {
				write(rel, content)
			}

			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := tenancyWired{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: tenancyWired{}.Name(), Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
