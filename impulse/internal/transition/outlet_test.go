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

	"github.com/cccteam/ccc/impulse/internal/app"
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
		[]string{
			"example.com/acme/beacon/pkg/resources",
			"example.com/acme/beacon/pkg/router",
		},
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

// beacon writes a flat application with one console project and discovers it.
func beacon(t *testing.T) *app.App {
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
		"web/console/src/index.html":                     "<html>\n  <head>\n    <title>Beacon</title>\n    <base href=\"/\" />\n  </head>\n</html>\n",
		"web/console/src/environments/environment.ts":    "export const environment = {\n  production: false,\n  baseUrl: '',\n  apiUrl: '/api',\n};\n",
		"web/console/src/app/core/api/api.ts":            "import { Api } from '@app/service/zz_gen_api';\nexport const api = new Api();\n",
		"web/console/src/app/core/service/zz_gen_api.ts": "// generated\n",
		"web/console/node_modules/left/index.js":         "// installed\n",
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

// fakeExec answers go generate.
type fakeExec struct {
	calls []string
	err   error
}

func (f *fakeExec) Run(_ context.Context, _ string, _ []string, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	if f.err != nil {
		return []byte("generation: boom\n"), f.err
	}

	return nil, nil
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

			err := tt.outlet.Validate(beacon(t))
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
		name        string
		outlet      Outlet
		generateErr error
		wantProgram string
		wantDid     []string
		wantSkipped []string
		check       func(t *testing.T, a *app.App)
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
				"web/package.json: added start:portal and extended build and lint to the portal project",
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
				}
				want := []string{"portal", "portal/src", "/portal/", "dist/portal", "portal/tsconfig.app.json", "/portal", "portal/proxy.conf.js", "portal:build:development"}
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

			a := beacon(t)
			exec := &fakeExec{err: tt.generateErr}
			ch, err := tt.outlet.Apply(t.Context(), a, exec)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if diff := cmp.Diff([]string{"go generate ./..."}, exec.calls); diff != "" {
				t.Errorf("commands mismatch (-want +got):\n%s", diff)
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

func TestChangeText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change Change
		want   string
	}{
		{
			name:   "everything done",
			change: Change{Command: "impulse add outlet portal --prefix portal/api --sessions", Did: []string{"a", "b"}},
			want:   "`impulse add outlet portal --prefix portal/api --sessions` made these changes and staged them:\n\n- a\n- b\n",
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
