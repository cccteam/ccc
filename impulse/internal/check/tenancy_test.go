package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// deployFile renders a deployment step calling access.MigrateRoles with the given
// trailing arguments after the four fixed ones.
func deployFile(trailing string) string {
	call := `access.MigrateRoles(ctx, manager, router.Collection(), roles` + trailing + `)`

	return `package deploy

import (
	"context"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"

	"example.com/harbor/pkg/router"
)

func MigrateRoles(ctx context.Context, manager access.UserManager, roles *access.RoleConfig, domains ...accesstypes.Domain) error {
	return ` + call + `
}
`
}

// bootstrapFile renders a caller of the deploy package's MigrateRoles wrapper with the
// given trailing arguments after the wrapper's three fixed ones.
func bootstrapFile(trailing string) string {
	return `package main

import (
	"context"

	"github.com/cccteam/access"

	"example.com/harbor/pkg/deploy"
)

func run(ctx context.Context, manager access.UserManager, roles *access.RoleConfig) error {
	return deploy.MigrateRoles(ctx, manager, roles` + trailing + `)
}
`
}

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
				"pkg/deploy/deploy.go":                    deployFile(", domains..."),
				"schema/migrations/000005_Tenants.up.sql": tenantsMigration,
			},
			wantStatus:  Pass,
			wantSummary: "tenant record Tenants (schema/migrations/000005_Tenants.up.sql); 1 tenant-scoped resource(s); roles provisioned per tenant in 1 place(s)",
		},
		{
			name: "tenanted with nothing behind it",
			files: map[string]string{
				"cmd/generate/main.go":              tenanted,
				"pkg/resources/lenses.go":           globalResource,
				"pkg/deploy/deploy.go":              deployFile(""),
				"schema/migrations/000001_X.up.sql": "CREATE TABLE Lenses (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n",
			},
			wantStatus:  Fail,
			wantSummary: "3 tenancy wiring problem(s)",
			wantDetails: []string{
				`WithDomainRoute("tenants"): no migration creates a table named like it (the tenant-record table)`,
				"no struct is annotated @permissionScope(domain): every resource is global, and the tenant segment serves nothing",
				"pkg/deploy/deploy.go:13: access.MigrateRoles is called without domains; roles never reach the tenants",
			},
		},
		{
			name: "tenanted without a role migration and with an unreadable source",
			files: map[string]string{
				"cmd/generate/main.go": program("pkg/resources",
					`generation.GenerateHandlers("app"),`,
					`generation.GenerateRoutes("pkg/router", "api"),`,
					`generation.WithDomainRoute("space-stations"),`,
				),
				"pkg/resources/announcements.go":                tenantScopedResource,
				"schema/migrations/000005_SpaceStations.up.sql": "create table if not exists `SpaceStations` (Id STRING(64) NOT NULL) PRIMARY KEY (Id);\n",
			},
			wantStatus:  Fail,
			wantSummary: "1 tenancy wiring problem(s)",
			wantDetails: []string{
				"no access.MigrateRoles call outside tests: roles are never provisioned",
			},
		},
		{
			name: "untenanted and clean, with the wrapper's empty spread passed through",
			files: map[string]string{
				"cmd/generate/main.go":    untenanted,
				"pkg/resources/lenses.go": globalResource,
				"pkg/deploy/deploy.go":    deployFile(", domains..."),
			},
			wantStatus:  Pass,
			wantSummary: "not tenanted: no tenant-scoped resources, roles provisioned globally",
		},
		{
			name: "untenanted with tenancy halves lying around",
			files: map[string]string{
				"cmd/generate/main.go":           untenanted,
				"pkg/resources/announcements.go": tenantScopedResource,
				"pkg/deploy/deploy.go":           deployFile(`, "north", "south"`),
			},
			wantStatus:  Fail,
			wantSummary: "2 tenancy wiring problem(s) in an untenanted application",
			wantDetails: []string{
				"pkg/resources/announcements.go:8: Announcement is @permissionScope(domain), but no WithDomainRoute names the tenant segment; it is served under the default /domain/{domain}/ pair",
				"pkg/deploy/deploy.go:13: access.MigrateRoles receives 2 domain(s), but the application is not tenanted",
			},
		},
		{
			name: "tenanted, with the wrapper's callers passing the roster",
			files: map[string]string{
				"cmd/generate/main.go":                    tenanted,
				"pkg/resources/announcements.go":          tenantScopedResource,
				"pkg/deploy/deploy.go":                    deployFile(", domains..."),
				"cmd/bootstrap/main.go":                   bootstrapFile(", domains..."),
				"cmd/deployment/migrate/main.go":          bootstrapFile(`, "north"`),
				"schema/migrations/000005_Tenants.up.sql": tenantsMigration,
			},
			wantStatus:  Pass,
			wantSummary: "tenant record Tenants (schema/migrations/000005_Tenants.up.sql); 1 tenant-scoped resource(s); roles provisioned per tenant in 3 place(s)",
		},
		{
			name: "tenanted, with a wrapper caller passing no domains",
			files: map[string]string{
				"cmd/generate/main.go":                    tenanted,
				"pkg/resources/announcements.go":          tenantScopedResource,
				"pkg/deploy/deploy.go":                    deployFile(", domains..."),
				"cmd/bootstrap/main.go":                   bootstrapFile(""),
				"schema/migrations/000005_Tenants.up.sql": tenantsMigration,
			},
			wantStatus:  Fail,
			wantSummary: "1 tenancy wiring problem(s)",
			wantDetails: []string{
				"cmd/bootstrap/main.go:12: deploy.MigrateRoles is called without domains; roles never reach the tenants",
			},
		},
		{
			name: "untenanted, with a wrapper caller passing domains",
			files: map[string]string{
				"cmd/generate/main.go":    untenanted,
				"pkg/resources/lenses.go": globalResource,
				"pkg/deploy/deploy.go":    deployFile(", domains..."),
				"cmd/bootstrap/main.go":   bootstrapFile(`, "north"`),
			},
			wantStatus:  Fail,
			wantSummary: "1 tenancy wiring problem(s) in an untenanted application",
			wantDetails: []string{
				"cmd/bootstrap/main.go:12: deploy.MigrateRoles receives 1 domain(s), but the application is not tenanted",
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
