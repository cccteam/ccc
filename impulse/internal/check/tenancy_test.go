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
	// tenantRecord is the tenant record, annotated @tenant (line 8).
	tenantRecord = `package resources

type (
	// Tenant is the tenant record.
	//
	// @resource
	// @tenant
	Tenant struct {
		ID   string ` + "`spanner:\"Id\"`" + `
		Name string ` + "`spanner:\"Name\"`" + `
	}
)
`
	// configHeader opens a data level holding the roster; each shape below constructs it
	// on line 15.
	configHeader = `package config

import (
	"context"

	"example.com/harbor/app"
	"github.com/cccteam/ccc/resource"
)

type DataConfiguration struct {
	tenants *resource.TenantRoster
}

`
	// wiredConfig builds the roster with the generated constructor, hands it the tenants
	// signal, and starts it.
	wiredConfig = configHeader + `func (c *DataConfiguration) startTenants(ctx context.Context, client resource.Client, signals resource.TenantSignals) error {
	c.tenants = app.NewTenantRoster(client, resource.WithTenantSignals(signals))

	return c.tenants.Start(ctx)
}
`
	// bareConfig builds the roster without the signal and never starts it.
	bareConfig = configHeader + `func (c *DataConfiguration) build(client resource.Client) {
	c.tenants = app.NewTenantRoster(client)
}
`
	// returnedConfig returns the roster from a helper, so nothing binds it to a name.
	returnedConfig = configHeader + `func newRoster(client resource.Client, signals resource.TenantSignals) *resource.TenantRoster {
	return app.NewTenantRoster(client, resource.WithTenantSignals(signals))
}
`
)

func TestTenancyWired(t *testing.T) {
	t.Parallel()

	site := program("pkg/resources",
		`generation.GenerateHandlers("app"),`,
		`generation.GenerateRoutes("pkg/router", "api"),`,
	)
	consoleSite := program("apps/console/pkg/resources",
		`generation.GenerateHandlers("apps/console/app"),`,
		`generation.GenerateRoutes("apps/console/pkg/router", "api"),`,
	)
	portalSite := program("apps/portal/pkg/resources",
		`generation.GenerateHandlers("apps/portal/app"),`,
		`generation.GenerateRoutes("apps/portal/pkg/router", "api"),`,
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
				"cmd/generate/main.go":           site,
				"pkg/resources/announcements.go": tenantScopedResource,
				"pkg/resources/tenants.go":       tenantRecord,
				"pkg/config/data.go":             wiredConfig,
			},
			wantStatus:  Pass,
			wantSummary: "tenant record Tenant; 1 tenant-scoped resource(s); roster built by NewTenantRoster at pkg/config/data.go:15",
		},
		{
			name: "tenanted with nothing behind the record",
			files: map[string]string{
				"cmd/generate/main.go":     site,
				"pkg/resources/lenses.go":  globalResource,
				"pkg/resources/tenants.go": tenantRecord,
			},
			wantStatus:  Fail,
			wantSummary: "2 tenancy wiring problem(s)",
			wantDetails: []string{
				"cmd/generate/main.go: no struct in pkg/resources is annotated @permissionScope(domain): every resource is global, and the tenant segment serves nothing",
				"pkg/resources/tenants.go:8: no file outside tests calls NewTenantRoster, the generated constructor of the tenant roster over Tenant; build the roster in the data level (NewTenantRoster(client, resource.WithTenantSignals(<live service>))) and start it (Start) before serving, so the generated DomainGuard knows the tenants and a tenant created on one instance reaches the others",
			},
		},
		{
			name: "a roster handed no signal and never started",
			files: map[string]string{
				"cmd/generate/main.go":           site,
				"pkg/resources/announcements.go": tenantScopedResource,
				"pkg/resources/tenants.go":       tenantRecord,
				"pkg/config/data.go":             bareConfig,
			},
			wantStatus:  Fail,
			wantSummary: "2 tenancy wiring problem(s)",
			wantDetails: []string{
				"pkg/config/data.go:15: NewTenantRoster is handed no resource.WithTenantSignals; pass the live service the data level opens, so a tenant created on another instance reaches this one at once rather than at the roster's backstop",
				"pkg/config/data.go:15: no file in pkg/config calls Start on the roster NewTenantRoster builds (tenants), so it is never loaded and the generated DomainGuard knows no tenant; start it where the data level is built and fail the start on its error",
			},
		},
		{
			name: "a roster returned from a helper is noted, not followed",
			files: map[string]string{
				"cmd/generate/main.go":           site,
				"pkg/resources/announcements.go": tenantScopedResource,
				"pkg/resources/tenants.go":       tenantRecord,
				"pkg/config/data.go":             returnedConfig,
			},
			wantStatus:  Pass,
			wantSummary: "tenant record Tenant; 1 tenant-scoped resource(s); roster built by NewTenantRoster at pkg/config/data.go:15",
			wantDetails: []string{
				"pkg/config/data.go:15: NewTenantRoster's result is not bound to a variable or a field here, so whether Start is called on it was not read",
			},
		},
		{
			name: "sites sharing one roster",
			files: map[string]string{
				"cmd/generate/console/main.go":                consoleSite,
				"cmd/generate/portal/main.go":                 portalSite,
				"apps/console/pkg/resources/announcements.go": tenantScopedResource,
				"apps/console/pkg/resources/tenants.go":       tenantRecord,
				"apps/portal/pkg/resources/announcements.go":  tenantScopedResource,
				"apps/portal/pkg/resources/tenants.go":        tenantRecord,
				"pkg/config/data.go":                          wiredConfig,
			},
			wantStatus:  Pass,
			wantSummary: "tenant record Tenant; 2 tenant-scoped resource(s); roster built by NewTenantRoster at pkg/config/data.go:15",
		},
		{
			name: "untenanted and clean",
			files: map[string]string{
				"cmd/generate/main.go":    site,
				"pkg/resources/lenses.go": globalResource,
			},
			wantStatus:  Pass,
			wantSummary: "not tenanted: no tenant-scoped resources",
		},
		{
			name: "untenanted with a tenant-scoped resource lying around",
			files: map[string]string{
				"cmd/generate/main.go":           site,
				"pkg/resources/announcements.go": tenantScopedResource,
			},
			wantStatus:  Fail,
			wantSummary: "1 tenancy wiring problem(s) in an untenanted application",
			wantDetails: []string{
				"pkg/resources/announcements.go:8: Announcement is @permissionScope(domain), but no struct in pkg/resources is annotated @tenant; a tenant-scoped resource needs a tenant record, the global resource whose rows are the tenants",
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
