package transition

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

const beaconProgram = `// Package main is the beacon resource generator program.
package main

import (
	"context"
	"log"

	"github.com/cccteam/ccc/resource/generation"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	generator, err := generation.NewResourceGenerator(
		ctx,
		"pkg/resources",
		[]string{"file://schema/migrations"},
		generation.GenerateHandlers("app"),
		generation.GenerateRoutes("pkg/router", "api"),
		generation.GenerateHandlerTests("test/authz"),
		generation.WithSpannerEmulatorVersion("1.5.56"),
		generation.GenerateTypescript("web/console/src/app/core/service",
			generation.GenerateMetadata(),
			generation.GeneratePermissions(),
			generation.GenerateEnums(),
		),
	)
	if err != nil {
		return err
	}
	defer generator.Close()

	return generator.Generate()
}
`

const beaconAngular = `{
  "$schema": "./node_modules/@angular/cli/lib/config/schema.json",
  "version": 1,
  "newProjectRoot": "projects",
  "projects": {
    "console": {
      "projectType": "application",
      "root": "console",
      "sourceRoot": "console/src",
      "prefix": "app",
      "architect": {
        "build": {
          "builder": "@angular-devkit/build-angular:application",
          "options": {
            "outputPath": {
              "base": "dist/console",
              "browser": ""
            },
            "index": "console/src/index.html",
            "browser": "console/src/main.ts",
            "tsConfig": "console/tsconfig.app.json",
            "styles": [
              "@angular/material/prebuilt-themes/azure-blue.css",
              "console/src/styles.scss"
            ],
            "scripts": []
          },
          "configurations": {
            "production": {
              "fileReplacements": [
                {
                  "replace": "console/src/environments/environment.ts",
                  "with": "console/src/environments/environment.prod.ts"
                }
              ]
            }
          }
        },
        "serve": {
          "builder": "@angular-devkit/build-angular:dev-server",
          "options": {
            "servePath": "/",
            "proxyConfig": "console/proxy.conf.js"
          },
          "configurations": {
            "production": {
              "buildTarget": "console:build:production"
            },
            "development": {
              "host": "127.0.0.1",
              "port": 4300,
              "buildTarget": "console:build:development"
            }
          },
          "defaultConfiguration": "development"
        },
        "test": {
          "builder": "@angular/build:unit-test",
          "options": {
            "tsConfig": "console/tsconfig.spec.json"
          }
        }
      }
    }
  },
  "cli": {
    "analytics": false
  }
}
`

const beaconPackage = `{
  "name": "beacon-web",
  "version": "0.0.0",
  "scripts": {
    "ng": "ng",
    "start:console": "ng serve console --no-hmr",
    "build": "ng build console",
    "lint": "ng lint console",
    "test": "ng test console --watch=false",
    "ccclib:local": "./ccclib.sh local"
  },
  "private": true
}
`

const beaconProcfile = `# Development processes.
# console: ng serve with /api proxied.
spanner: podman run --rm -p 127.0.0.1:${SPANNER_EMULATOR_PORT}:9010 gcr.io/cloud-spanner-emulator/emulator:1.5.56
server: bash -c 'go run ./cmd/bootstrap && go run .'
console: bash -c 'cd web && bun install && bun run start:console'
`

// beacon writes a flat application with one console project, plus any extra files, and
// discovers it.
func beacon(t *testing.T, extra map[string]string) *app.App {
	t.Helper()

	root := t.TempDir()
	files := map[string]string{
		"go.mod":                                         "module example.com/acme/beacon\n\ngo 1.26\n",
		"cmd/generate/generate.go":                       "package generate\n\n//go:generate go run ./resourcegenerator\n",
		"cmd/generate/resourcegenerator/main.go":         beaconProgram,
		"pkg/resources/doc.go":                           "package resources\n",
		"app/doc.go":                                     "package app\n",
		"pkg/router/router.go":                           "package router\n",
		"Procfile":                                       beaconProcfile,
		"web/angular.json":                               beaconAngular,
		"web/package.json":                               beaconPackage,
		"web/console/proxy.conf.js":                      "module.exports = {\n  '/api/': {\n    target: 'http://127.0.0.1:8090',\n  },\n};\n",
		"web/console/tsconfig.app.json":                  "{\n  \"compilerOptions\": {\n    \"outDir\": \"../out-tsc/console\"\n  }\n}\n",
		"web/console/tsconfig.spec.json":                 "{\n  \"extends\": \"./tsconfig.app.json\",\n  \"include\": [\"src/**/*.spec.ts\"]\n}\n",
		"web/console/src/app/app.component.spec.ts":      "// the spec the copy carries over\n",
		"web/console/src/index.html":                     "<html>\n  <head>\n    <title>Beacon</title>\n    <base href=\"/\" />\n  </head>\n</html>\n",
		"web/console/src/environments/environment.ts":    "export const environment = {\n  production: false,\n  baseUrl: '',\n  apiUrl: '/api',\n};\n",
		"web/console/src/app/core/api/api.ts":            "import { Api } from '@app/service/zz_gen_api';\nexport const api = new Api();\n",
		"web/console/src/app/core/service/zz_gen_api.ts": "// generated\n",
		"web/console/node_modules/left/index.js":         "// installed\n",
	}
	for rel, content := range extra {
		files[rel] = content
	}
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a, err := app.Discover(root)
	if err != nil {
		t.Fatalf("app.Discover() error = %v", err)
	}

	return a
}

// fakeExec answers go generate: its output, and its failure when err is set.
type fakeExec struct {
	calls []string
	out   string
	err   error
}

func (f *fakeExec) Run(_ context.Context, _ string, _ []string, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if f.err != nil {
		return []byte("generation: boom\n"), f.err
	}

	return []byte(f.out), nil
}

func read(t *testing.T, a *app.App, rel string) string {
	t.Helper()

	data, err := os.ReadFile(a.Abs(rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}

	return string(data)
}

func TestOutletValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		outlet  Outlet
		wantErr string
	}{
		{name: "a session outlet", outlet: Outlet{Name: "portal", Prefix: "portal/api", Sessions: true}},
		{name: "an API-key outlet", outlet: Outlet{Name: "machines", Prefix: "machines"}},
		{name: "the reserved name", outlet: Outlet{Name: "default", Prefix: "x"}, wantErr: `outlet name "default"`},
		{name: "a name that is not an identifier", outlet: Outlet{Name: "my-portal", Prefix: "x"}, wantErr: `outlet name "my-portal"`},
		{name: "a prefix with a leading slash", outlet: Outlet{Name: "portal", Prefix: "/portal"}, wantErr: `outlet prefix "/portal"`},
		{name: "an empty prefix", outlet: Outlet{Name: "portal"}, wantErr: `outlet prefix ""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.outlet.Validate(beacon(t, nil))
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

func TestOutletApply(t *testing.T) {
	t.Parallel()

	const program = "cmd/generate/resourcegenerator/main.go"
	sessionsProgram := strings.Replace(beaconProgram,
		"\t\tgeneration.GenerateRoutes(\"pkg/router\", \"api\"),\n",
		"\t\tgeneration.GenerateRoutes(\"pkg/router\", \"api\"),\n\t\tgeneration.WithRouterOutlet(\"portal\", \"portal/api\", generation.ServesSessions()),\n", 1)
	sessionsProgram = strings.Replace(sessionsProgram,
		"\t\t\tgeneration.GenerateEnums(),\n\t\t),\n\t)\n",
		"\t\t\tgeneration.GenerateEnums(),\n\t\t),\n"+
			"\t\tgeneration.GenerateTypescript(\"web/portal/src/app/core/service\",\n"+
			"\t\t\tgeneration.ForOutlet(\"portal\"),\n"+
			"\t\t\tgeneration.GenerateMetadata(),\n"+
			"\t\t\tgeneration.GeneratePermissions(),\n"+
			"\t\t\tgeneration.GenerateEnums(),\n"+
			"\t\t),\n\t)\n", 1)
	machinesProgram := strings.Replace(beaconProgram,
		"\t\tgeneration.GenerateRoutes(\"pkg/router\", \"api\"),\n",
		"\t\tgeneration.GenerateRoutes(\"pkg/router\", \"api\"),\n\t\tgeneration.WithRouterOutlet(\"machines\", \"machines\"),\n", 1)

	tests := []struct {
		name         string
		outlet       Outlet
		generateErr  error
		generateOut  string
		wantWarnings []string
		wantProgram  string
		wantDid      []string
		wantSkipped  []string
		check        func(t *testing.T, a *app.App)
	}{
		{
			name:        "a session outlet",
			outlet:      Outlet{Name: "portal", Prefix: "portal/api", Sessions: true},
			wantProgram: sessionsProgram,
			wantDid: []string{
				`cmd/generate/resourcegenerator/main.go: added WithRouterOutlet("portal", "portal/api", generation.ServesSessions())`,
				`cmd/generate/resourcegenerator/main.go: added GenerateTypescript("web/portal/src/app/core/service", ForOutlet("portal"), ...) with the default target's options`,
				"copied the console browser project to web/portal, rewriting its API prefix (/api to /portal/api), base path (/portal/), and output paths; its titles still say console",
				"web/angular.json: added the portal project as a copy of console, serving under /portal on port 4301",
				"web/package.json: added start:portal and extended build, lint, and test to the portal project",
				"Procfile: added the portal process, a copy of console running start:portal",
				"ran go generate ./..., which emitted the portal outlet's routes and handlers and its browser client",
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				if got := read(t, a, "web/portal/proxy.conf.js"); !strings.Contains(got, "'/portal/api/': {") {
					t.Errorf("proxy.conf.js = %q", got)
				}
				if got := read(t, a, "web/portal/src/index.html"); !strings.Contains(got, `<base href="/portal/" />`) {
					t.Errorf("index.html = %q", got)
				}
				if got := read(t, a, "web/portal/src/environments/environment.ts"); !strings.Contains(got, "baseUrl: '/portal/',\n  apiUrl: '/portal/api',") {
					t.Errorf("environment.ts = %q", got)
				}
				if got := read(t, a, "web/portal/tsconfig.app.json"); !strings.Contains(got, "out-tsc/portal") {
					t.Errorf("tsconfig.app.json = %q", got)
				}
				// The spec tsconfig and the specs ride along: the copied project tests as the console does.
				if got := read(t, a, "web/portal/tsconfig.spec.json"); !strings.Contains(got, "./tsconfig.app.json") {
					t.Errorf("tsconfig.spec.json = %q", got)
				}
				if _, err := os.Stat(a.Abs("web/portal/src/app/app.component.spec.ts")); err != nil {
					t.Errorf("the console's spec was not copied: %v", err)
				}
				if got := read(t, a, "web/portal/src/app/core/api/api.ts"); !strings.Contains(got, "zz_gen_api") {
					t.Errorf("api.ts = %q", got)
				}
				for _, absent := range []string{"web/portal/src/app/core/service/zz_gen_api.ts", "web/portal/node_modules/left/index.js"} {
					if _, err := os.Stat(a.Abs(absent)); !errors.Is(err, os.ErrNotExist) {
						t.Errorf("%s exists in the copy", absent)
					}
				}

				var workspace struct {
					Projects map[string]struct {
						Root       string `json:"root"`
						SourceRoot string `json:"sourceRoot"`
						Architect  struct {
							Build struct {
								Options struct {
									BaseHref   string `json:"baseHref"`
									OutputPath struct {
										Base string `json:"base"`
									} `json:"outputPath"`
									TsConfig string `json:"tsConfig"`
								} `json:"options"`
							} `json:"build"`
							Serve struct {
								Options struct {
									ServePath   string `json:"servePath"`
									ProxyConfig string `json:"proxyConfig"`
								} `json:"options"`
								Configurations struct {
									Development struct {
										Port        int    `json:"port"`
										BuildTarget string `json:"buildTarget"`
									} `json:"development"`
								} `json:"configurations"`
							} `json:"serve"`
							Test struct {
								Builder string `json:"builder"`
								Options struct {
									TsConfig string `json:"tsConfig"`
								} `json:"options"`
							} `json:"test"`
						} `json:"architect"`
					} `json:"projects"`
				}
				if err := json.Unmarshal([]byte(read(t, a, "web/angular.json")), &workspace); err != nil {
					t.Fatalf("angular.json after the copy is not JSON: %v", err)
				}
				portal, ok := workspace.Projects["portal"]
				if !ok {
					t.Fatalf("angular.json has no portal project: %v", workspace.Projects)
				}
				got := []string{
					portal.Root, portal.SourceRoot, portal.Architect.Build.Options.BaseHref, portal.Architect.Build.Options.OutputPath.Base,
					portal.Architect.Build.Options.TsConfig, portal.Architect.Serve.Options.ServePath, portal.Architect.Serve.Options.ProxyConfig,
					portal.Architect.Serve.Configurations.Development.BuildTarget,
					portal.Architect.Test.Builder, portal.Architect.Test.Options.TsConfig,
				}
				want := []string{"portal", "portal/src", "/portal/", "dist/portal", "portal/tsconfig.app.json", "/portal", "portal/proxy.conf.js", "portal:build:development", "@angular/build:unit-test", "portal/tsconfig.spec.json"}
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("portal project mismatch (-want +got):\n%s", diff)
				}
				if portal.Architect.Serve.Configurations.Development.Port != 4301 {
					t.Errorf("portal port = %d, want 4301", portal.Architect.Serve.Configurations.Development.Port)
				}
				if console := workspace.Projects["console"]; console.Root != "console" || console.Architect.Serve.Options.ServePath != "/" {
					t.Errorf("console project changed: %+v", console)
				}

				pkg := read(t, a, "web/package.json")
				for _, want := range []string{
					"    \"start:console\": \"ng serve console --no-hmr\",\n    \"start:portal\": \"ng serve portal --no-hmr\",\n",
					`"build": "ng build console && ng build portal"`,
					`"lint": "ng lint console && ng lint portal"`,
					`"test": "ng test console --watch=false && ng test portal --watch=false"`,
				} {
					if !strings.Contains(pkg, want) {
						t.Errorf("package.json lacks %q:\n%s", want, pkg)
					}
				}
				if got := read(t, a, "Procfile"); !strings.Contains(got, "console: bash -c 'cd web && bun install && bun run start:console'\nportal: bash -c 'cd web && bun install && bun run start:portal'\n") {
					t.Errorf("Procfile = %q", got)
				}
			},
		},
		{
			name:        "an API-key outlet",
			outlet:      Outlet{Name: "machines", Prefix: "machines"},
			wantProgram: machinesProgram,
			wantDid: []string{
				`cmd/generate/resourcegenerator/main.go: added WithRouterOutlet("machines", "machines")`,
				"ran go generate ./..., which emitted the machines outlet's routes and handlers",
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				if _, err := os.Stat(a.Abs("web/machines")); !errors.Is(err, os.ErrNotExist) {
					t.Error("an API-key outlet got a browser project")
				}
			},
		},
		{
			name:   "generation prints schema warnings",
			outlet: Outlet{Name: "machines", Prefix: "machines"},
			generateOut: "2026/09/22 10:00:00 Finished Resource generation in 1s\n" +
				"Warning: Reading lists in TenantId, RecordedAt DESC order with no index leading with those columns, so every page sorts the tenant's partition; wanted: CREATE INDEX ReadingsByTenantIdRecordedAt ON Readings(TenantId, RecordedAt DESC)\n" +
				"Warning: Comment resolves its tenant through AnnouncementId, Announcements.TenantId, so its lists scan all of Comments, every tenant, and no index on Comments changes that; a table listed at volume carries the tenant key on the row\n",
			wantProgram: machinesProgram,
			wantDid: []string{
				`cmd/generate/resourcegenerator/main.go: added WithRouterOutlet("machines", "machines")`,
				"ran go generate ./..., which emitted the machines outlet's routes and handlers",
			},
			wantWarnings: []string{
				"Reading lists in TenantId, RecordedAt DESC order with no index leading with those columns, so every page sorts the tenant's partition; wanted: CREATE INDEX ReadingsByTenantIdRecordedAt ON Readings(TenantId, RecordedAt DESC)",
				"Comment resolves its tenant through AnnouncementId, Announcements.TenantId, so its lists scan all of Comments, every tenant, and no index on Comments changes that; a table listed at volume carries the tenant key on the row",
			},
		},
		{
			name:        "generation fails",
			outlet:      Outlet{Name: "machines", Prefix: "machines"},
			generateErr: errors.New("exit status 1"),
			wantProgram: machinesProgram,
			wantDid:     []string{`cmd/generate/resourcegenerator/main.go: added WithRouterOutlet("machines", "machines")`},
			wantSkipped: []string{"go generate ./... failed, so the outlet's routes, handlers, and client are not generated yet; fix the cause and run it:\ngeneration: boom"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := beacon(t, nil)
			exec := &fakeExec{err: tt.generateErr, out: tt.generateOut}
			ch, err := tt.outlet.Apply(t.Context(), a, exec)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if diff := cmp.Diff([]string{"go generate ./..."}, exec.calls); diff != "" {
				t.Errorf("commands mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantWarnings, ch.Warnings); diff != "" {
				t.Errorf("Warnings mismatch (-want +got):\n%s", diff)
			}
			if len(tt.wantWarnings) > 0 && !strings.Contains(ch.Text(), "go generate printed these schema warnings; each is fixed in the schema or accepted by pinning its typed value in the program's warnings test:\n\n- "+tt.wantWarnings[0]+"\n") {
				t.Errorf("Text() does not carry the warnings:\n%s", ch.Text())
			}
			if diff := cmp.Diff(tt.wantDid, ch.Did); diff != "" {
				t.Errorf("Did mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantSkipped, ch.Skipped); diff != "" {
				t.Errorf("Skipped mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantProgram, read(t, a, program)); diff != "" {
				t.Errorf("program mismatch (-want +got):\n%s", diff)
			}
			if tt.check != nil {
				tt.check(t, a)
			}
		})
	}
}

// beaconRouterProgram is beaconProgram with the generated router declared: the console
// is the default outlet on the staff auth with its browser application at /.
var beaconRouterProgram = strings.Replace(beaconProgram,
	"\t\tgeneration.GenerateRoutes(\"pkg/router\", \"api\"),\n",
	"\t\tgeneration.GenerateRouter(),\n\t\tgeneration.GenerateRoutes(\"pkg/router\", \"api\",\n\t\t\tgeneration.Auth(\"example.com/acme/beacon/pkg/auth/staff\", generation.Password),\n\t\t\tgeneration.WebApp(\"/\"),\n\t\t),\n", 1)

// TestOutletApplyGeneratedRouter pins the declaration add outlet writes under the
// generated router: a session outlet binds to the auth --auth names (the console's when
// none is named) and serves its browser application at /<name>, an API-key outlet declares
// APIKey. The fixture has the staff auth (the console's) and a members auth to bind to.
func TestOutletApplyGeneratedRouter(t *testing.T) {
	t.Parallel()

	// movedConsole is the GenerateRoutes a console that left the root reads afterwards.
	const movedConsole = "\t\tgeneration.GenerateRoutes(\"pkg/router\", \"console/api\",\n\t\t\tgeneration.Auth(\"example.com/acme/beacon/pkg/auth/staff\", generation.Password),\n\t\t\tgeneration.WebApp(\"/console\"),\n\t\t),\n"
	// rootConsole is the console's GenerateRoutes as the fixture declares it.
	const rootConsole = "\t\tgeneration.GenerateRoutes(\"pkg/router\", \"api\",\n\t\t\tgeneration.Auth(\"example.com/acme/beacon/pkg/auth/staff\", generation.Password),\n\t\t\tgeneration.WebApp(\"/\"),\n\t\t),\n"
	moveDid := []string{
		`cmd/generate/resourcegenerator/main.go: with two browser applications none is mounted at /, so the default outlet's application moves to /console and its API to /console/api: GenerateRoutes("pkg/router", "console/api", ..., WebApp("/console")) in place of "api" and WebApp("/"); the regenerated router answers the root alone with a redirect to /console/`,
		"web/console: the console project serves under /console/ with its API at /console/api (angular.json baseHref and servePath, the proxy, the environments, the base element)",
	}
	moveSkipped := []string{
		`app/assets.go:9: DeepLink rewrites the console's routes to "/"; it moves to "/console/", and Assets serves the bundle behind http.StripPrefix("/console"), as a second browser application's pair does`,
		`pkg/config/site.go:5: LoginURL "/login" is the console's login page, which is now under /console/: "/console/login"`,
		`test/integration/login_test.go:5, 9: names "/api", the console's old prefix, in a string literal; the console's API is now /console/api`,
		"Procfile:2: names /api, the console's old prefix; the console's API is now /console/api and its pages are under /console/",
		"README.md:3, 5: names /api, the console's old prefix; the console's API is now /console/api and its pages are under /console/",
	}

	tests := []struct {
		name        string
		outlet      Outlet
		wantErr     string
		wantCommand string
		wantDid     string
		// wantMoved pins the console's move: the Did lines after the project copy, the
		// Skipped lines listing what still names the old prefix, and the program.
		wantMoved   bool
		wantProgram []string
		wantAbsent  []string
		wantCookie  string
	}{
		{
			name:        "a session outlet on the console's auth",
			outlet:      Outlet{Name: "portal", Prefix: "portal/api", Sessions: true},
			wantCommand: "impulse add outlet portal --prefix portal/api --auth staff",
			wantDid:     `cmd/generate/resourcegenerator/main.go: added WithRouterOutlet("portal", "portal/api", generation.Auth("example.com/acme/beacon/pkg/auth/staff", generation.Password), generation.WebApp("/portal"))`,
			wantMoved:   true,
			wantProgram: []string{
				"\t\tgeneration.GenerateRouter(),\n",
				movedConsole,
				"\t\tgeneration.WithRouterOutlet(\"portal\", \"portal/api\",\n\t\t\tgeneration.Auth(\"example.com/acme/beacon/pkg/auth/staff\", generation.Password),\n\t\t\tgeneration.WebApp(\"/portal\"),\n\t\t),\n",
			},
			wantAbsent: []string{"ServesSessions", rootConsole},
			wantCookie: "cookieName: 'staff-xsrf'",
		},
		{
			name:        "a session outlet bound to a named auth",
			outlet:      Outlet{Name: "portal", Prefix: "portal/api", Sessions: true, Auth: "members"},
			wantCommand: "impulse add outlet portal --prefix portal/api --auth members",
			wantDid:     `cmd/generate/resourcegenerator/main.go: added WithRouterOutlet("portal", "portal/api", generation.Auth("example.com/acme/beacon/pkg/auth/members", generation.Password), generation.WebApp("/portal"))`,
			wantMoved:   true,
			wantProgram: []string{
				movedConsole,
				"\t\tgeneration.WithRouterOutlet(\"portal\", \"portal/api\",\n\t\t\tgeneration.Auth(\"example.com/acme/beacon/pkg/auth/members\", generation.Password),\n\t\t\tgeneration.WebApp(\"/portal\"),\n\t\t),\n",
			},
			wantAbsent: []string{"ServesSessions", rootConsole},
			wantCookie: "cookieName: 'members-xsrf'",
		},
		{
			name:    "a session outlet bound to an auth the application lacks",
			outlet:  Outlet{Name: "portal", Prefix: "portal/api", Sessions: true, Auth: "partners"},
			wantErr: "no auth package pkg/auth/partners to bind the portal outlet to; the auths are members, staff",
		},
		{
			// No second browser application arrives, so the console stays at the root.
			name:        "an API-key outlet",
			outlet:      Outlet{Name: "machines", Prefix: "machines"},
			wantCommand: "impulse add outlet machines --prefix machines --api-key",
			wantDid:     `cmd/generate/resourcegenerator/main.go: added WithRouterOutlet("machines", "machines", generation.APIKey())`,
			wantProgram: []string{"\t\tgeneration.WithRouterOutlet(\"machines\", \"machines\", generation.APIKey()),\n", rootConsole},
			wantAbsent:  []string{"ServesSessions", "console/api"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := authFiles(t)
			files["cmd/generate/resourcegenerator/main.go"] = beaconRouterProgram
			files["pkg/auth/members/members.go"] = strings.ReplaceAll(strings.ReplaceAll(files["pkg/auth/staff/staff.go"], "staff", "members"), "Staff", "Members")
			files["web/console/src/app/app.config.ts"] = "provideHttpClient(withXsrfConfiguration({ cookieName: 'staff-xsrf' }));\n"
			// What a move leaves for the agent: the App's root deep link, a login page, a
			// test naming the prefix, and the prose; a generated file naming it is not read.
			files["app/assets.go"] = "package app\n\nimport (\n\t\"net/http\"\n\n\t\"github.com/jtwatson/spaassets\"\n)\n\nfunc (a *App) DeepLink(next http.Handler) http.Handler { return spaassets.DeepLink(next, \"/\") }\n"
			files["pkg/config/site.go"] = "package config\n\n// Settings for the console's auth.\nvar settings = struct{ LoginURL string }{\n\tLoginURL: \"/login\",\n}\n"
			files["test/integration/login_test.go"] = "package integration\n\nimport \"testing\"\n\nconst session = \"/api/user/session\"\n\nfunc TestLogin(t *testing.T) {\n\tt.Log(session)\n\tt.Log(\"/api/permission-digest\")\n}\n"
			files["pkg/router/zz_gen_router.go"] = "package router\n\nconst prefix = \"/api/\"\n"
			files["README.md"] = "# beacon\n\nThe console on `/api`.\n\nRoutes under `/api/tenants/{tenantID}/`; the portal on `/portal/api`; the Go API package google.golang.org/api is not it.\n"
			a := beacon(t, files)
			ch, err := tt.outlet.Apply(t.Context(), a, &fakeExec{})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Apply() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if ch.Command != tt.wantCommand {
				t.Errorf("Command = %q, want %q", ch.Command, tt.wantCommand)
			}
			if len(ch.Did) == 0 || ch.Did[0] != tt.wantDid {
				t.Errorf("Did[0] = %q, want %q", ch.Did, tt.wantDid)
			}
			if tt.wantCookie != "" {
				if got := read(t, a, "web/"+tt.outlet.Name+"/src/app/app.config.ts"); !strings.Contains(got, tt.wantCookie) {
					t.Errorf("app.config.ts = %q, want %q", got, tt.wantCookie)
				}
			}
			var wantMoveDid, wantMoveSkipped []string
			if tt.wantMoved {
				wantMoveDid, wantMoveSkipped = moveDid, moveSkipped
			}
			gotMoveDid := []string(nil)
			for _, d := range ch.Did {
				if strings.Contains(d, "moves to /console") || strings.Contains(d, "serves under /console/") {
					gotMoveDid = append(gotMoveDid, d)
				}
			}
			if diff := cmp.Diff(wantMoveDid, gotMoveDid); diff != "" {
				t.Errorf("the move's Did lines mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(wantMoveSkipped, ch.Skipped); diff != "" {
				t.Errorf("Skipped mismatch (-want +got):\n%s", diff)
			}
			consoleFiles := map[string]string{
				"web/console/proxy.conf.js":                   "'/api/': {",
				"web/console/src/index.html":                  `<base href="/" />`,
				"web/console/src/environments/environment.ts": "baseUrl: '',\n  apiUrl: '/api',",
			}
			if tt.wantMoved {
				consoleFiles = map[string]string{
					"web/console/proxy.conf.js":                   "'/console/api/': {",
					"web/console/src/index.html":                  `<base href="/console/" />`,
					"web/console/src/environments/environment.ts": "baseUrl: '/console/',\n  apiUrl: '/console/api',",
				}
			}
			for rel, want := range consoleFiles {
				if got := read(t, a, rel); !strings.Contains(got, want) {
					t.Errorf("%s = %q, want %q", rel, got, want)
				}
			}
			var workspace struct {
				Projects map[string]struct {
					Architect struct {
						Build struct {
							Options struct {
								BaseHref string `json:"baseHref"`
							} `json:"options"`
						} `json:"build"`
						Serve struct {
							Options struct {
								ServePath string `json:"servePath"`
							} `json:"options"`
						} `json:"serve"`
					} `json:"architect"`
				} `json:"projects"`
			}
			if err := json.Unmarshal([]byte(read(t, a, "web/angular.json")), &workspace); err != nil {
				t.Fatalf("angular.json is not JSON: %v", err)
			}
			console := workspace.Projects["console"]
			wantBase, wantServe := "", "/"
			if tt.wantMoved {
				wantBase, wantServe = "/console/", "/console"
			}
			if console.Architect.Build.Options.BaseHref != wantBase || console.Architect.Serve.Options.ServePath != wantServe {
				t.Errorf("console baseHref %q, servePath %q; want %q, %q", console.Architect.Build.Options.BaseHref, console.Architect.Serve.Options.ServePath, wantBase, wantServe)
			}
			if tt.wantMoved {
				// The copy was taken before the move, so the portal reads as its own.
				if got := read(t, a, "web/portal/src/environments/environment.ts"); !strings.Contains(got, "baseUrl: '/portal/',\n  apiUrl: '/portal/api',") {
					t.Errorf("portal environment.ts = %q", got)
				}
			}
			for _, absent := range tt.wantAbsent {
				if got := read(t, a, "cmd/generate/resourcegenerator/main.go"); strings.Contains(got, absent) {
					t.Errorf("program has %q:\n%s", absent, got)
				}
			}
			for _, want := range tt.wantProgram {
				if got := read(t, a, "cmd/generate/resourcegenerator/main.go"); !strings.Contains(got, want) {
					t.Errorf("program lacks %q:\n%s", want, got)
				}
			}
		})
	}
}

func TestChangeText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change Change
		want   string
	}{
		{
			name:   "everything done",
			change: Change{Command: "impulse add outlet portal --prefix portal/api --auth staff", Did: []string{"a", "b"}},
			want:   "`impulse add outlet portal --prefix portal/api --auth staff` made these changes and staged them:\n\n- a\n- b\n",
		},
		{
			name:   "with skips",
			change: Change{Command: "impulse add outlet machines --prefix machines --api-key", Did: []string{"a"}, Skipped: []string{"s"}},
			want:   "`impulse add outlet machines --prefix machines --api-key` made these changes and staged them:\n\n- a\n\nIt could not make these; they are yours:\n\n- s\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, tt.change.Text()); diff != "" {
				t.Errorf("Text() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestOutletApplyConsoleUnderPath pins the copy from a console that already lives under
// its own path, the shape every browser outlet takes once there are two: the new outlet's
// project reads its own prefix and base path in place of the console's, and the console,
// off the root already, does not move.
func TestOutletApplyConsoleUnderPath(t *testing.T) {
	t.Parallel()

	program := strings.Replace(beaconRouterProgram,
		"\t\tgeneration.GenerateRoutes(\"pkg/router\", \"api\",\n\t\t\tgeneration.Auth(\"example.com/acme/beacon/pkg/auth/staff\", generation.Password),\n\t\t\tgeneration.WebApp(\"/\"),\n\t\t),\n",
		"\t\tgeneration.GenerateRoutes(\"pkg/router\", \"console/api\",\n\t\t\tgeneration.Auth(\"example.com/acme/beacon/pkg/auth/staff\", generation.Password),\n\t\t\tgeneration.WebApp(\"/console\"),\n\t\t),\n"+
			"\t\tgeneration.WithRouterOutlet(\"portal\", \"portal/api\",\n\t\t\tgeneration.Auth(\"example.com/acme/beacon/pkg/auth/staff\", generation.Password),\n\t\t\tgeneration.WebApp(\"/portal\"),\n\t\t),\n", 1)
	files := authFiles(t)
	files["cmd/generate/resourcegenerator/main.go"] = program
	files["web/angular.json"] = strings.Replace(strings.Replace(beaconAngular, `"servePath": "/",`, `"servePath": "/console",`, 1), "\"scripts\": []\n", "\"scripts\": [],\n            \"baseHref\": \"/console/\"\n", 1)
	files["web/console/proxy.conf.js"] = "module.exports = {\n  '/console/api/': {\n    target: 'http://127.0.0.1:8090',\n  },\n};\n"
	files["web/console/src/index.html"] = "<html>\n  <head>\n    <base href=\"/console/\" />\n  </head>\n</html>\n"
	files["web/console/src/environments/environment.ts"] = "export const environment = {\n  production: false,\n  baseUrl: '/console/',\n  apiUrl: '/console/api',\n};\n"
	files["app/assets.go"] = "package app\n\nimport (\n\t\"net/http\"\n\n\t\"github.com/jtwatson/spaassets\"\n)\n\nfunc (a *App) DeepLink(next http.Handler) http.Handler { return spaassets.DeepLink(next, \"/console/\") }\n"
	a := beacon(t, files)

	ch, err := Outlet{Name: "kiosk", Prefix: "kiosk/api", Sessions: true}.Apply(t.Context(), a, &fakeExec{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	wantDid := []string{
		`cmd/generate/resourcegenerator/main.go: added WithRouterOutlet("kiosk", "kiosk/api", generation.Auth("example.com/acme/beacon/pkg/auth/staff", generation.Password), generation.WebApp("/kiosk"))`,
		`cmd/generate/resourcegenerator/main.go: added GenerateTypescript("web/kiosk/src/app/core/service", ForOutlet("kiosk"), ...) with the default target's options`,
		"copied the console browser project to web/kiosk, rewriting its API prefix (/console/api to /kiosk/api), base path (/kiosk/), and output paths; its titles still say console",
		"web/angular.json: added the kiosk project as a copy of console, serving under /kiosk on port 4301",
		"web/package.json: added start:kiosk and extended build, lint, and test to the kiosk project",
		"Procfile: added the kiosk process, a copy of console running start:kiosk",
		"ran go generate ./..., which emitted the kiosk outlet's routes and handlers and its browser client",
	}
	if diff := cmp.Diff(wantDid, ch.Did); diff != "" {
		t.Errorf("Did mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string(nil), ch.Skipped); diff != "" {
		t.Errorf("Skipped mismatch (-want +got):\n%s", diff)
	}
	for rel, want := range map[string]string{
		"web/kiosk/proxy.conf.js":                     "'/kiosk/api/': {",
		"web/kiosk/src/index.html":                    `<base href="/kiosk/" />`,
		"web/kiosk/src/environments/environment.ts":   "baseUrl: '/kiosk/',\n  apiUrl: '/kiosk/api',",
		"web/console/src/environments/environment.ts": "baseUrl: '/console/',\n  apiUrl: '/console/api',",
		"cmd/generate/resourcegenerator/main.go":      "\t\tgeneration.GenerateRoutes(\"pkg/router\", \"console/api\",\n",
	} {
		if got := read(t, a, rel); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want %q", rel, got, want)
		}
	}
}
