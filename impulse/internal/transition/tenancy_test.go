package transition

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
)

const beaconConfig = `package config

import (
	"context"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/go-playground/errors/v5"
)

// DataConfiguration is the second level.
type DataConfiguration struct {
	spannerClient *cloudspanner.Client
	access        *access.Client
}

// NewDataConfiguration opens the clients.
func NewDataConfiguration(ctx context.Context) (*DataConfiguration, error) {
	spannerClient, accessClient, err := open(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "open()")
	}

	return &DataConfiguration{
		spannerClient: spannerClient,
		access:        accessClient,
	}, nil
}
`

const beaconApp = `package app

// Configurer carries the dependencies for an App.
type Configurer interface {
	Access() string
	ConsoleDist() string
}

// App implements the handlers.
type App struct {
	access      string
	consoleDist string
}

// New constructs an App.
func New(cfg Configurer) *App {
	return &App{
		access:      cfg.Access(),
		consoleDist: cfg.ConsoleDist(),
	}
}
`

func tenancyFiles() map[string]string {
	return map[string]string{
		"pkg/config/data.go": beaconConfig,
		"app/app.go":         beaconApp,
		"schema/migrations/000001_Sessions.up.sql":   "CREATE TABLE Sessions (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n",
		"schema/migrations/000001_Sessions.down.sql": "DROP TABLE Sessions;\n",
		"schema/migrations/000002_Users.up.sql":      "CREATE TABLE SessionUsers (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n",
		"schema/migrations/000002_Users.down.sql":    "DROP TABLE SessionUsers;\n",
		"web/console/src/app/core/api/api.ts":        "export const api = 1;\n",
	}
}

func TestTenancyValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		table   string
		wantErr string
	}{
		{name: "the default table", table: "Tenants"},
		{name: "a two-word table", table: "BusinessUnits"},
		{name: "a singular table", table: "Tenant", wantErr: `table "Tenant"`},
		{name: "a lower-case table", table: "tenants", wantErr: `table "tenants"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := Tenancy{Table: tt.table}.Validate(beacon(t, tenancyFiles()))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestTenancyNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		table, segment, record string
	}{
		{"Tenants", "tenants", "Tenant"},
		{"BusinessUnits", "business-units", "BusinessUnit"},
		{"Companies", "companies", "Company"},
		{"Addresses", "addresses", "Address"},
		{"Branches", "branches", "Branch"},
	}
	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			t.Parallel()

			tn := Tenancy{Table: tt.table}
			if got := tn.Segment(); got != tt.segment {
				t.Errorf("Segment() = %q, want %q", got, tt.segment)
			}
			if got := tn.Record(); got != tt.record {
				t.Errorf("Record() = %q, want %q", got, tt.record)
			}
		})
	}
}

func TestTenancyApply(t *testing.T) {
	t.Parallel()

	const program = "cmd/generate/resourcegenerator/main.go"
	wantProgram := strings.Replace(beaconProgram,
		"\t\tgeneration.GenerateRoutes(\"pkg/router\", \"api\"),\n",
		"\t\tgeneration.GenerateRoutes(\"pkg/router\", \"api\"),\n\t\tgeneration.WithDomainRoute(\"tenants\"),\n\t\tgeneration.WithConcealedDomains(),\n", 1)
	wantConfig := strings.Replace(beaconConfig, "\taccess        *access.Client\n}", "\taccess        *access.Client\n\ttenants       tenantRoster\n}", 1)
	wantConfig = strings.Replace(wantConfig,
		"\treturn &DataConfiguration{\n\t\tspannerClient: spannerClient,\n\t\taccess:        accessClient,\n\t}, nil\n",
		"\tconf := &DataConfiguration{\n\t\tspannerClient: spannerClient,\n\t\taccess:        accessClient,\n\t}\n\tif err := conf.loadTenants(ctx); err != nil {\n\t\treturn nil, errors.Wrap(err, \"loadTenants()\")\n\t}\n\n\treturn conf, nil\n", 1)
	wantApp := strings.Replace(beaconApp, "\tConsoleDist() string\n}", "\tConsoleDist() string\n\tTenancyConfigurer\n}", 1)
	wantApp = strings.Replace(wantApp, "\taccess      string\n\tconsoleDist string\n}", "\taccess        string\n\tconsoleDist   string\n\tdomainVisible DomainVisibleFunc\n}", 1)
	wantApp = strings.Replace(wantApp, "\t\tconsoleDist: cfg.ConsoleDist(),\n\t}", "\t\tconsoleDist:   cfg.ConsoleDist(),\n\t\tdomainVisible: cfg.DomainVisible,\n\t}", 1)
	wantApp = strings.Replace(wantApp, "\t\taccess:      cfg.Access(),", "\t\taccess:        cfg.Access(),", 1)

	tests := []struct {
		name        string
		files       map[string]string
		wantDid     []string
		wantSkipped []string
		check       func(t *testing.T, a *app.App)
	}{
		{
			name:  "the skeleton shape",
			files: tenancyFiles(),
			wantDid: []string{
				`cmd/generate/resourcegenerator/main.go: added WithDomainRoute("tenants") and WithConcealedDomains()`,
				"schema/migrations/000003_Tenants.up.sql and .down.sql: the Tenants table (a slug primary key and a unique Name)",
				"schema/devseed/000001_dev_tenants.up.sql and .down.sql: the development tenants north and south, a data migration for the bootstrap to apply before the roles",
				"pkg/resources/tenants.go: the Tenant resource struct, global, keyed by slug",
				"pkg/config/tenancy.go: the tenant roster (tenantRoster, loaded from Tenants at startup), Domains(), and DomainVisible() on DataConfiguration; pkg/config/data.go gained the field and the load",
				"app/tenancy.go: TenancyConfigurer (embedded in Configurer), DomainVisibleFunc, and App.DomainVisible; app/app.go gained the field and its assignment",
				"web/console/src/app/core/tenant/tenant.service.ts: the tenant service (the selected tenant, the session's tenant list, and the digest scoped to it) from the reference; the header's tenant picker and the pages that read it are yours",
				"ran go generate ./..., which emitted the Tenant resource and the tenant segment pair under /tenants/{tenantID}",
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				if diff := cmp.Diff(wantConfig, read(t, a, "pkg/config/data.go")); diff != "" {
					t.Errorf("data.go mismatch (-want +got):\n%s", diff)
				}
				if diff := cmp.Diff(wantApp, read(t, a, "app/app.go")); diff != "" {
					t.Errorf("app.go mismatch (-want +got):\n%s", diff)
				}
				for rel, want := range map[string]string{
					"schema/migrations/000003_Tenants.up.sql":           "CREATE TABLE Tenants (",
					"schema/migrations/000003_Tenants.down.sql":         "DROP TABLE Tenants;",
					"schema/devseed/000001_dev_tenants.up.sql":          "INSERT INTO Tenants (Id, Name) VALUES ('north', 'North');",
					"pkg/resources/tenants.go":                          "package resources\n\ntype (\n\t// Tenant is the tenant record",
					"pkg/config/tenancy.go":                             "package config\n",
					"app/tenancy.go":                                    "package app\n",
					"web/console/src/app/core/tenant/tenant.service.ts": "export class TenantService",
				} {
					if got := read(t, a, rel); !strings.Contains(got, want) {
						t.Errorf("%s lacks %q:\n%s", rel, want, got)
					}
				}
				config := read(t, a, "pkg/config/tenancy.go")
				for _, want := range []string{"c.spannerClient.Single().Query", "c.access.UserHasGrants", `SQL: "SELECT Id FROM Tenants ORDER BY Id"`} {
					if !strings.Contains(config, want) {
						t.Errorf("tenancy.go lacks %q", want)
					}
				}
			},
		},
		{
			name: "an application without the skeleton's seams",
			files: map[string]string{
				"schema/migrations/000001_Sessions.up.sql":   "CREATE TABLE Sessions (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n",
				"schema/migrations/000001_Sessions.down.sql": "DROP TABLE Sessions;\n",
				"schema/devseed/000001_seed.up.sql":          "INSERT INTO Sessions (Id) VALUES ('x');\n",
			},
			wantDid: []string{
				`cmd/generate/resourcegenerator/main.go: added WithDomainRoute("tenants") and WithConcealedDomains()`,
				"schema/migrations/000002_Tenants.up.sql and .down.sql: the Tenants table (a slug primary key and a unique Name)",
				"pkg/resources/tenants.go: the Tenant resource struct, global, keyed by slug",
				"ran go generate ./..., which emitted the Tenant resource and the tenant segment pair under /tenants/{tenantID}",
			},
			wantSkipped: []string{
				"schema/devseed already exists, so no development tenants were seeded; add two to it",
				"no file declares a DataConfiguration struct, so the tenant roster and DomainVisible were not added to the data level; add a roster read from Tenants at startup, Domains() listing it, and DomainVisible(ctx, user, domain) answering roster membership AND access.UserHasGrants",
				"no file declares a Configurer interface, so DomainVisible was not exposed on the app; the generated code needs DomainVisible(ctx, user, domain) (bool, error) on the handlers",
				"web/angular.json has no project rooted over console/src/app/core/service, so the tenant service was not copied in; a browser app needs a selected tenant to scope its requests and digest by",
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				if _, err := os.Stat(a.Abs("pkg/config/tenancy.go")); !errors.Is(err, os.ErrNotExist) {
					t.Error("pkg/config/tenancy.go was written without a DataConfiguration")
				}
			},
		},
	}
	// The base as impulse new renders it: the data level holds the engine through the
	// auth package (*staff.Auth), not an *access.Client field.
	authShape := tenancyFiles()
	authShape["pkg/config/data.go"] = authConfig
	authShape["pkg/auth/staff/staff.go"] = staffPackage(t)
	tests = append(tests, struct {
		name        string
		files       map[string]string
		wantDid     []string
		wantSkipped []string
		check       func(t *testing.T, a *app.App)
	}{
		name:    "the base with an auth package",
		files:   authShape,
		wantDid: tests[0].wantDid,
		check: func(t *testing.T, a *app.App) {
			t.Helper()
			config := read(t, a, "pkg/config/tenancy.go")
			for _, want := range []string{"c.spannerClient.Single().Query", "c.staff.Access().UserHasGrants"} {
				if !strings.Contains(config, want) {
					t.Errorf("tenancy.go lacks %q:\n%s", want, config)
				}
			}
			if data := read(t, a, "pkg/config/data.go"); !strings.Contains(data, "\ttenants       tenantRoster\n") || !strings.Contains(data, "conf.loadTenants(ctx)") {
				t.Errorf("data.go = %q", data)
			}
		},
	})
	// A data level whose constructor returns a configuration built elsewhere: the roster
	// field is added, the load is an obligation, and the file keeps its contents.
	builtElsewhere := tenancyFiles()
	builtElsewhere["pkg/config/data.go"] = strings.Replace(beaconConfig,
		"\treturn &DataConfiguration{\n\t\tspannerClient: spannerClient,\n\t\taccess:        accessClient,\n\t}, nil\n",
		"\treturn assemble(spannerClient, accessClient), nil\n", 1)
	wantBuiltElsewhere := strings.Replace(builtElsewhere["pkg/config/data.go"], "\taccess        *access.Client\n}", "\taccess        *access.Client\n\ttenants       tenantRoster\n}", 1)
	wantBuiltElsewhereDid := slices.Clone(tests[0].wantDid)
	wantBuiltElsewhereDid[4] = strings.Replace(wantBuiltElsewhereDid[4], "gained the field and the load", "gained the field", 1)
	tests = append(tests, struct {
		name        string
		files       map[string]string
		wantDid     []string
		wantSkipped []string
		check       func(t *testing.T, a *app.App)
	}{
		name:        "a constructor without the returned literal keeps the file and gains the field",
		files:       builtElsewhere,
		wantDid:     wantBuiltElsewhereDid,
		wantSkipped: []string{"pkg/config/data.go: NewDataConfiguration does not end in \"return &DataConfiguration{...}, nil\", so the roster load was not inserted; call conf.loadTenants(ctx) once the configuration is built"},
		check: func(t *testing.T, a *app.App) {
			t.Helper()
			if diff := cmp.Diff(wantBuiltElsewhere, read(t, a, "pkg/config/data.go")); diff != "" {
				t.Errorf("data.go mismatch (-want +got):\n%s", diff)
			}
		},
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := beacon(t, tt.files)
			if tt.name == "an application without the skeleton's seams" {
				// A workspace whose project does not own the client target.
				if err := os.WriteFile(a.Abs("web/angular.json"), []byte(`{"version": 1, "projects": {"other": {"root": "other", "sourceRoot": "other/src"}}}`), 0o600); err != nil {
					t.Fatal(err)
				}
				var err error
				if a, err = app.Discover(a.Root); err != nil {
					t.Fatal(err)
				}
			}
			exec := &fakeExec{}
			ch, err := Tenancy{Table: "Tenants"}.Apply(t.Context(), a, exec)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDid, ch.Did); diff != "" {
				t.Errorf("Did mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantSkipped, ch.Skipped); diff != "" {
				t.Errorf("Skipped mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(wantProgram, read(t, a, program)); diff != "" {
				t.Errorf("program mismatch (-want +got):\n%s", diff)
			}
			if tt.check != nil {
				tt.check(t, a)
			}
		})
	}
}
