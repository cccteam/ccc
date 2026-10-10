package transition

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
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

// liveConfig is the base's data level: a resource client and the live service the roster
// is handed as its signals.
const liveConfig = `package config

import (
	"context"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/resource"
	livefirestore "github.com/cccteam/ccc/resource/live/firestore"
	"github.com/go-playground/errors/v5"
)

// DataConfiguration is the second level.
type DataConfiguration struct {
	spannerClient  *cloudspanner.Client
	resourceClient *resource.SpannerClient
	live           *livefirestore.Service
}

// NewDataConfiguration opens the clients.
func NewDataConfiguration(ctx context.Context) (*DataConfiguration, error) {
	spannerClient, liveService, err := open(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "open()")
	}

	return &DataConfiguration{
		spannerClient:  spannerClient,
		resourceClient: resource.NewSpannerClient(spannerClient),
		live:           liveService,
	}, nil
}
`

// permissionsApp is the base's App: its UserPermissions passes no roster, since an
// untenanted application lists no domain.
const permissionsApp = `package app

import (
	"net/http"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/resource"
)

// Configurer carries the dependencies for an App.
type Configurer interface {
	Access() access.Controller
	ConsoleDist() string
}

// App implements the handlers.
type App struct {
	access      access.Controller
	consoleDist string
}

// New constructs an App.
func New(cfg Configurer) *App {
	return &App{
		access:      cfg.Access(),
		consoleDist: cfg.ConsoleDist(),
	}
}

// UserPermissions returns the permission checker for a request.
func (a *App) UserPermissions(r *http.Request) resource.UserPermissions {
	return resource.SessionPermissions(r.Context(), a.access.ForUser, a.access.ForRole, nil)
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
		"\t\tgeneration.GenerateRoutes(\"pkg/router\", \"api\"),\n\t\tgeneration.WithConcealedDomains(),\n", 1)
	// The data level gains the roster field (and the resource import its type needs) and
	// the start where the configuration is built.
	withRosterField := func(config string) string {
		config = strings.Replace(config, "\t\"github.com/cccteam/access\"\n", "\t\"github.com/cccteam/access\"\n\t\"github.com/cccteam/ccc/resource\"\n", 1)

		return strings.Replace(config, "\taccess        *access.Client\n}", "\taccess        *access.Client\n\ttenants       *resource.TenantRoster\n}", 1)
	}
	wantConfig := strings.Replace(withRosterField(beaconConfig),
		"\treturn &DataConfiguration{\n\t\tspannerClient: spannerClient,\n\t\taccess:        accessClient,\n\t}, nil\n",
		"\tconf := &DataConfiguration{\n\t\tspannerClient: spannerClient,\n\t\taccess:        accessClient,\n\t}\n\tif err := conf.startTenants(ctx); err != nil {\n\t\treturn nil, errors.Wrap(err, \"startTenants()\")\n\t}\n\n\treturn conf, nil\n", 1)
	wantApp := strings.Replace(beaconApp, "package app\n\n", "package app\n\nimport \"github.com/cccteam/ccc/resource\"\n\n", 1)
	wantApp = strings.Replace(wantApp, "\tConsoleDist() string\n}", "\tConsoleDist() string\n\tTenancyConfigurer\n}", 1)
	wantApp = strings.Replace(wantApp, "\taccess      string\n\tconsoleDist string\n}", "\taccess      string\n\tconsoleDist string\n\ttenants     *resource.TenantRoster\n}", 1)
	wantApp = strings.Replace(wantApp, "\t\tconsoleDist: cfg.ConsoleDist(),\n\t}", "\t\tconsoleDist: cfg.ConsoleDist(),\n\t\ttenants:     cfg.TenantRoster(),\n\t}", 1)
	// The fixtures' data level holds no live service and their App composes no session
	// permissions, so the two edits that need them are recorded as the agent's.
	noSignals := "pkg/config/tenancy.go: DataConfiguration holds no *firestore.Service field (resource/live/firestore), so the roster is built without resource.WithTenantSignals and reloads at its backstop alone; pass the live service the level opens, so a tenant created on another instance reaches this one at once"
	noPermissions := "app/app.go: UserPermissions passes no nil roster to resource.SessionPermissions, so the roster's Domains were not handed to it; pass a.tenants.Domains where the session permissions are composed, so a login's tenant list is the roster filtered by its footholds"

	tests := []struct {
		name        string
		files       map[string]string
		wantDid     []string
		wantSkipped []string
		check       func(t *testing.T, a *app.App)
	}{
		{
			name:  "the base's seams without a live service or session permissions",
			files: tenancyFiles(),
			wantDid: []string{
				"cmd/generate/resourcegenerator/main.go: added WithConcealedDomains(); the tenant segment /tenants derives from the Tenant record",
				"schema/migrations/000003_Tenants.up.sql and .down.sql: the Tenants table (a slug primary key and a unique Name)",
				"schema/devseed/000001_dev_tenants.up.sql and .down.sql: the development tenants north and south, a data migration for the bootstrap to apply before the logins",
				"pkg/resources/tenants.go: the Tenant resource struct, global, keyed by slug",
				"pkg/config/tenancy.go: the tenant roster (startTenants builds it with app.NewTenantRoster and starts it) and TenantRoster() on DataConfiguration; pkg/config/data.go gained the field and the start",
				"app/tenancy.go: TenancyConfigurer (embedded in Configurer) and App.TenantRoster(); app/app.go gained the field and its assignment",
				"web/console/src/app/core/tenant/tenant.service.ts: the tenant service (the selected tenant, the session's tenant list, and the digest scoped to it) from the reference; the header's tenant picker and the pages that read it are yours",
				"ran go generate ./..., which emitted the Tenant resource, the tenant segment pair under /tenants/{tenantID}, and the roster constructor NewTenantRoster",
			},
			wantSkipped: []string{noSignals, noPermissions},
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
					"pkg/resources/tenants.go":                          "package resources\n\ntype (\n\t// Tenant is the tenant record (@tenant)",
					"pkg/config/tenancy.go":                             "package config\n",
					"app/tenancy.go":                                    "package app\n",
					"web/console/src/app/core/tenant/tenant.service.ts": "export class TenantService",
				} {
					if got := read(t, a, rel); !strings.Contains(got, want) {
						t.Errorf("%s lacks %q:\n%s", rel, want, got)
					}
				}
				config := read(t, a, "pkg/config/tenancy.go")
				for _, want := range []string{"c.tenants = app.NewTenantRoster(resource.NewSpannerClient(c.spannerClient))", "// No live service field was found on DataConfiguration", "c.tenants.Start(ctx)", "func (c *DataConfiguration) TenantRoster() *resource.TenantRoster"} {
					if !strings.Contains(config, want) {
						t.Errorf("tenancy.go lacks %q:\n%s", want, config)
					}
				}
				if resources := read(t, a, "pkg/resources/tenants.go"); !strings.Contains(resources, "\t// @resource\n\t// @tenant\n\tTenant struct {") {
					t.Errorf("tenants.go does not annotate the record @tenant:\n%s", resources)
				}
			},
		},
		{
			name: "the skeleton's data level and app",
			files: func() map[string]string {
				files := tenancyFiles()
				files["pkg/config/data.go"] = liveConfig
				files["app/app.go"] = permissionsApp

				return files
			}(),
			wantDid: []string{
				"cmd/generate/resourcegenerator/main.go: added WithConcealedDomains(); the tenant segment /tenants derives from the Tenant record",
				"schema/migrations/000003_Tenants.up.sql and .down.sql: the Tenants table (a slug primary key and a unique Name)",
				"schema/devseed/000001_dev_tenants.up.sql and .down.sql: the development tenants north and south, a data migration for the bootstrap to apply before the logins",
				"pkg/resources/tenants.go: the Tenant resource struct, global, keyed by slug",
				"pkg/config/tenancy.go: the tenant roster (startTenants builds it with app.NewTenantRoster and starts it) and TenantRoster() on DataConfiguration; pkg/config/data.go gained the field and the start",
				"app/tenancy.go: TenancyConfigurer (embedded in Configurer) and App.TenantRoster(); app/app.go gained the field and its assignment",
				"app/app.go: UserPermissions passes the roster's Domains to resource.SessionPermissions",
				"web/console/src/app/core/tenant/tenant.service.ts: the tenant service (the selected tenant, the session's tenant list, and the digest scoped to it) from the reference; the header's tenant picker and the pages that read it are yours",
				"ran go generate ./..., which emitted the Tenant resource, the tenant segment pair under /tenants/{tenantID}, and the roster constructor NewTenantRoster",
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				config := read(t, a, "pkg/config/tenancy.go")
				if want := "c.tenants = app.NewTenantRoster(c.resourceClient, resource.WithTenantSignals(c.live))"; !strings.Contains(config, want) {
					t.Errorf("tenancy.go lacks %q:\n%s", want, config)
				}
				data := read(t, a, "pkg/config/data.go")
				for _, want := range []string{"\ttenants        *resource.TenantRoster\n", "\tif err := conf.startTenants(ctx); err != nil {\n\t\treturn nil, errors.Wrap(err, \"startTenants()\")\n\t}\n"} {
					if !strings.Contains(data, want) {
						t.Errorf("data.go lacks %q:\n%s", want, data)
					}
				}
				application := read(t, a, "app/app.go")
				for _, want := range []string{"\tTenancyConfigurer\n}", "\ttenants     *resource.TenantRoster\n", "\t\ttenants:     cfg.TenantRoster(),\n", "resource.SessionPermissions(r.Context(), a.access.ForUser, a.access.ForRole, a.tenants.Domains)"} {
					if !strings.Contains(application, want) {
						t.Errorf("app.go lacks %q:\n%s", want, application)
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
				"cmd/generate/resourcegenerator/main.go: added WithConcealedDomains(); the tenant segment /tenants derives from the Tenant record",
				"schema/migrations/000002_Tenants.up.sql and .down.sql: the Tenants table (a slug primary key and a unique Name)",
				"pkg/resources/tenants.go: the Tenant resource struct, global, keyed by slug",
				"ran go generate ./..., which emitted the Tenant resource, the tenant segment pair under /tenants/{tenantID}, and the roster constructor NewTenantRoster",
			},
			wantSkipped: []string{
				"schema/devseed already exists, so no development tenants were seeded; add two to it",
				"no file declares a DataConfiguration struct, so the tenant roster was not added to the data level; build it where the database is opened with app.NewTenantRoster(client, resource.WithTenantSignals(<the live service>)), start it (Start) and fail the start on its error, and expose it as TenantRoster()",
				"no file declares a Configurer interface, so the tenant roster was not exposed on the app; the generated code needs TenantRoster() *resource.TenantRoster on the handlers, answering the roster the data level built",
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
		name:        "the base with an auth package",
		files:       authShape,
		wantDid:     tests[0].wantDid,
		wantSkipped: []string{noSignals, noPermissions},
		check: func(t *testing.T, a *app.App) {
			t.Helper()
			config := read(t, a, "pkg/config/tenancy.go")
			if want := "c.tenants = app.NewTenantRoster(resource.NewSpannerClient(c.spannerClient))"; !strings.Contains(config, want) {
				t.Errorf("tenancy.go lacks %q:\n%s", want, config)
			}
			if data := read(t, a, "pkg/config/data.go"); !strings.Contains(data, "\ttenants       *resource.TenantRoster\n") || !strings.Contains(data, "conf.startTenants(ctx)") {
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
	wantBuiltElsewhere := withRosterField(builtElsewhere["pkg/config/data.go"])
	wantBuiltElsewhereDid := slices.Clone(tests[0].wantDid)
	wantBuiltElsewhereDid[4] = strings.Replace(wantBuiltElsewhereDid[4], "gained the field and the start", "gained the field", 1)
	tests = append(tests, struct {
		name        string
		files       map[string]string
		wantDid     []string
		wantSkipped []string
		check       func(t *testing.T, a *app.App)
	}{
		name:    "a constructor without the returned literal keeps the file and gains the field",
		files:   builtElsewhere,
		wantDid: wantBuiltElsewhereDid,
		wantSkipped: []string{
			"pkg/config/data.go: NewDataConfiguration does not end in \"return &DataConfiguration{...}, nil\", so the roster's start was not inserted; call conf.startTenants(ctx) once the configuration is built, and fail the start on its error",
			noSignals,
			noPermissions,
		},
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

// TestClientExpression names the client the roster reads through, by what the data level
// holds: the database driver's resource client, a resource client of the level's own, a
// Spanner client wrapped as one, or nothing.
func TestClientExpression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "the database driver",
			src:  "package config\n\nimport \"github.com/cccteam/ccc/resource/database/spanner\"\n\ntype DataConfiguration struct {\n\tdatabase *spanner.Driver\n}\n",
			want: "c.database.ResourceClient",
		},
		{
			name: "the database driver under the alias the skeleton binds it by",
			src:  "package config\n\nimport database \"github.com/cccteam/ccc/resource/database/spanner\"\n\ntype DataConfiguration struct {\n\tdatabase *database.Driver\n}\n",
			want: "c.database.ResourceClient",
		},
		{
			name: "a resource client of the level's own",
			src:  "package config\n\nimport \"github.com/cccteam/ccc/resource\"\n\ntype DataConfiguration struct {\n\tresourceClient *resource.SpannerClient\n}\n",
			want: "c.resourceClient",
		},
		{
			name: "a Spanner client wrapped as a resource client",
			src:  "package config\n\nimport cloudspanner \"cloud.google.com/go/spanner\"\n\ntype DataConfiguration struct {\n\tspannerClient *cloudspanner.Client\n}\n",
			want: "resource.NewSpannerClient(c.spannerClient)",
		},
		{
			name: "neither",
			src:  "package config\n\ntype DataConfiguration struct {\n\tname string\n}\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := clientExpression("pkg/config/data.go", []byte(tt.src))
			if err != nil {
				t.Fatalf("clientExpression() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("clientExpression() = %q, want %q", got, tt.want)
			}
		})
	}
}
