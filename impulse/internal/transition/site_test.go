package transition

import (
	"os"
	"strings"
	"testing"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// The flat site's hand-written files in the base's shape, minimal.
const (
	siteMain = `// main serves beacon.
package main

import (
	"example.com/acme/beacon/app"
	"example.com/acme/beacon/pkg/config"
	"example.com/acme/beacon/pkg/router"
)

func main() {
	conf, _ := config.NewServerConfiguration(nil)
	_ = router.New(app.New(conf))
}
`
	siteApp = `// Package app contains the http handlers for beacon.
package app

import "example.com/acme/beacon/pkg/auth/staff"

type Configurer interface {
	Staff() *staff.Auth
	ConsoleDist() string
}

type App struct {
	consoleDist string
}

func New(cfg Configurer) *App { return &App{consoleDist: cfg.ConsoleDist()} }
`
	siteRouter = `package router

import "example.com/acme/beacon/app"

func New(a *app.App) any { return generatedRoutes(a) }
`
	siteServer = `package config

// ServerConfiguration is the third level: the served application.
type ServerConfiguration struct {
	env *serverConfig
}

func NewServerConfiguration(ctx any) (*ServerConfiguration, error) { return &ServerConfiguration{}, nil }

func (c *ServerConfiguration) ConsoleDist() string { return c.env.ConsoleDist }

type serverConfig struct {
	Port        string ` + "`" + `env:"PORT,default=8080"` + "`" + `
	ConsoleDist string ` + "`" + `env:"APP_CONSOLE_DIST,default=web/dist/console"` + "`" + `
}
`
	siteDeploy = `package deploy

import (
	"context"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"

	"example.com/acme/beacon/pkg/router"
)

func MigrateRoles(ctx context.Context, manager access.UserManager, rolesPath string, domains ...accesstypes.Domain) error {
	return access.MigrateRoles(ctx, manager, router.Collection(), nil, domains...)
}
`
	siteAuthz = `package authz

import (
	"example.com/acme/beacon/app"
	"example.com/acme/beacon/pkg/router"
)

var _ = router.New
var _ = app.New
`
	siteEnv = `# --- server: the served application ---
export PORT=8090
# export APP_CONSOLE_DIST=web/dist/console
`
	siteProcfile = `spanner: podman run --rm -p 127.0.0.1:${SPANNER_EMULATOR_PORT}:9010 gcr.io/cloud-spanner-emulator/emulator:1.5.56
server: bash -c 'for i in {1..300}; do (echo > /dev/tcp/127.0.0.1/${SPANNER_EMULATOR_PORT}) >/dev/null 2>&1 && break || sleep 0.1; done; go run ./cmd/bootstrap && go run .'
console: bash -c 'for i in {1..600}; do (echo > /dev/tcp/127.0.0.1/${PORT}) >/dev/null 2>&1 && break || sleep 0.5; done; cd web && bun install && bun run start:console'
`
)

// flatSite is the beacon application with a flat site's hand-written files.
func flatSite(t *testing.T) *app.App {
	t.Helper()

	files := authFiles(t)
	files["main.go"] = siteMain
	files["app/app.go"] = siteApp
	files["pkg/router/router.go"] = siteRouter
	files["pkg/router/zz_gen_routes.go"] = "package router\n\nfunc generatedRoutes(any) any { return nil }\n"
	files["pkg/config/server.go"] = siteServer
	files["pkg/deploy/deploy.go"] = siteDeploy
	files["test/authz/harness_test.go"] = siteAuthz
	files["test/authz/zz_gen_authz_test.go"] = "package authz\n"
	files[".envrc.template"] = siteEnv
	files["Procfile"] = siteProcfile
	files["web/go.mod"] = "module example.com/acme/beacon/web\n\ngo 1.26\n"
	files["web/console/src/app/core/service/zz_gen_resources.ts"] = "// generated\n"
	files["web/console/src/main.ts"] = "// main\n"
	files["web/.yalc/@cccteam/resource/package.json"] = "{}\n"
	files["web/bun.lock"] = "{\n  \"workspaces\": {\n    \"\": {\n      \"name\": \"beacon-web\",\n    },\n  },\n}\n"

	return beacon(t, files)
}

func TestSiteValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		site    Site
		wantErr string
	}{
		{name: "promotion", site: Site{Name: "portal", First: "console"}},
		{name: "promotion without --first", site: Site{Name: "portal"}, wantErr: "--first is required"},
		{name: "a bad name", site: Site{Name: "Portal", First: "console"}, wantErr: `site name "Portal"`},
		{name: "a bad first name", site: Site{Name: "portal", First: "Console"}, wantErr: `first site name "Console"`},
		{name: "the same name twice", site: Site{Name: "portal", First: "portal"}, wantErr: "names the new site too"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.site.Validate(flatSite(t))
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Validate() error = %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestSiteApplyPromotes(t *testing.T) {
	t.Parallel()

	a := flatSite(t)
	exec := &fakeExec{}
	ch, err := Site{Name: "portal", First: "console"}.Apply(t.Context(), a, exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(ch.Skipped) != 2 {
		t.Errorf("Skipped = %q, want the integration suite and the deployment notes only", ch.Skipped)
	}

	// The flat site moved under apps/console and its imports followed.
	for _, rel := range []string{
		"apps/console/main.go", "apps/console/app/app.go", "apps/console/pkg/router/router.go", "apps/console/pkg/router/zz_gen_routes.go",
		"apps/console/pkg/resources/doc.go", "apps/console/test/authz/harness_test.go", "apps/console/web/angular.json", "apps/console/web/console/src/main.ts",
		"cmd/generate/consolegenerator/main.go", "pkg/config/site.go", "pkg/sharedresources/sharedresources.go", "cmd/generate/sharedgenerator/main.go",
	} {
		if _, err := os.Stat(a.Abs(rel)); err != nil {
			t.Errorf("%s: missing after the promotion", rel)
		}
	}
	for _, rel := range []string{"main.go", "app", "pkg/router", "pkg/resources", "test/authz", "web", "cmd/generate/resourcegenerator", "pkg/config/server.go"} {
		if _, err := os.Stat(a.Abs(rel)); err == nil {
			t.Errorf("%s: still at the root after the promotion", rel)
		}
	}
	main := read(t, a, "apps/console/main.go")
	for _, want := range []string{`"example.com/acme/beacon/apps/console/app"`, `"example.com/acme/beacon/apps/console/pkg/router"`, `"example.com/acme/beacon/pkg/config"`, "config.NewSiteConfiguration("} {
		if !strings.Contains(main, want) {
			t.Errorf("apps/console/main.go lacks %q:\n%s", want, main)
		}
	}
	if got := read(t, a, "apps/console/web/go.mod"); !strings.Contains(got, "module example.com/acme/beacon/apps/console/web\n") {
		t.Errorf("web boundary module = %q", got)
	}

	// The generator program is the console site's.
	gen := read(t, a, "cmd/generate/consolegenerator/main.go")
	for _, want := range []string{`"apps/console/pkg/resources"`, `"example.com/acme/beacon/apps/console/pkg/resources"`, `GenerateHandlers("apps/console/app")`, `GenerateRoutes("apps/console/pkg/router", "api")`, `GenerateHandlerTests("apps/console/test/authz")`, `GenerateTypescript("apps/console/web/console/src/app/core/service"`} {
		if !strings.Contains(gen, want) {
			t.Errorf("consolegenerator lacks %q:\n%s", want, gen)
		}
	}
	if got := read(t, a, "cmd/generate/generate.go"); !strings.Contains(got, "//go:generate go run ./consolegenerator\n//go:generate go run ./portalgenerator\n//go:generate go run ./sharedgenerator\n") {
		t.Errorf("generate.go = %q", got)
	}

	// The site level.
	site := read(t, a, "pkg/config/site.go")
	for _, want := range []string{"type SiteConfiguration struct", "func NewSiteConfiguration(", "func (c *SiteConfiguration) Dist() string { return c.env.Dist }", `env:"APP_DIST,required"`} {
		if !strings.Contains(site, want) {
			t.Errorf("site.go lacks %q:\n%s", want, site)
		}
	}
	if appGo := read(t, a, "apps/console/app/app.go"); !strings.Contains(appGo, "Dist() string") || strings.Contains(appGo, "consoleDist") {
		t.Errorf("app.go = %q", appGo)
	}

	// The union and the shared generator cover both sites.
	deploy := read(t, a, "pkg/deploy/deploy.go")
	for _, want := range []string{`consolerouter "example.com/acme/beacon/apps/console/pkg/router"`, `portalrouter "example.com/acme/beacon/apps/portal/pkg/router"`, "collection, err := Collection()", "access.MigrateRoles(ctx, manager, collection, nil, domains...)", `"github.com/go-playground/errors/v5"`, "access.UnionCollection(consolerouter.Collection(), portalrouter.Collection())"} {
		if !strings.Contains(deploy, want) {
			t.Errorf("deploy.go lacks %q:\n%s", want, deploy)
		}
	}
	shared := read(t, a, "cmd/generate/sharedgenerator/main.go")
	for _, want := range []string{`"pkg/sharedresources"`, `"example.com/acme/beacon/pkg/sharedresources"`, `GenerateTypescript("apps/console/web/console/src/app/core/service/shared"`, `GenerateTypescript("apps/portal/web/portal/src/app/core/service/shared"`, `WithSpannerEmulatorVersion("1.5.56")`} {
		if !strings.Contains(shared, want) {
			t.Errorf("sharedgenerator lacks %q:\n%s", want, shared)
		}
	}

	// The portal site.
	for _, rel := range []string{"apps/portal/main.go", "apps/portal/app/app.go", "apps/portal/pkg/router/router.go", "apps/portal/pkg/resources/resources.go", "apps/portal/test/authz/harness_test.go", "apps/portal/web/angular.json", "apps/portal/web/portal/src/main.ts", "cmd/generate/portalgenerator/main.go", "apps/portal/web/portal/src/app/core/service/shared"} {
		if _, err := os.Stat(a.Abs(rel)); err != nil {
			t.Errorf("%s: missing", rel)
		}
	}
	for _, rel := range []string{"apps/portal/pkg/router/zz_gen_routes.go", "apps/portal/test/authz/zz_gen_authz_test.go", "apps/portal/web/.yalc", "apps/portal/web/portal/src/app/core/service/zz_gen_resources.ts"} {
		if _, err := os.Stat(a.Abs(rel)); err == nil {
			t.Errorf("%s: copied, but generated files and attachments are not copied", rel)
		}
	}
	portalMain := read(t, a, "apps/portal/main.go")
	if !strings.Contains(portalMain, `"example.com/acme/beacon/apps/portal/app"`) || strings.Contains(portalMain, "apps/console") {
		t.Errorf("apps/portal/main.go = %q", portalMain)
	}
	if got := read(t, a, "apps/portal/pkg/resources/resources.go"); !strings.Contains(got, "the portal site") || !strings.Contains(got, "func defaultConfig() resource.Config") {
		t.Errorf("portal resources.go = %q", got)
	}
	portalGen := read(t, a, "cmd/generate/portalgenerator/main.go")
	for _, want := range []string{`"apps/portal/pkg/resources"`, `"example.com/acme/beacon/apps/portal/pkg/router"`, `GenerateHandlers("apps/portal/app")`, `GenerateTypescript("apps/portal/web/portal/src/app/core/service"`} {
		if !strings.Contains(portalGen, want) {
			t.Errorf("portalgenerator lacks %q:\n%s", want, portalGen)
		}
	}
	angular := read(t, a, "apps/portal/web/angular.json")
	for _, want := range []string{`"portal": {`, `"root": "portal"`, `"sourceRoot": "portal/src"`, `"port": 4301`, `"buildTarget": "portal:build:development"`} {
		if !strings.Contains(angular, want) {
			t.Errorf("portal angular.json lacks %q", want)
		}
	}
	if pkg := read(t, a, "apps/portal/web/package.json"); !strings.Contains(pkg, `"start:portal": "ng serve portal --no-hmr"`) {
		t.Errorf("portal package.json = %q", pkg)
	}
	// Each site's workspace is named for it, in the manifest and the lockfile alike.
	for rel, want := range map[string]string{
		"apps/console/web/package.json": `"name": "beacon-console-web"`,
		"apps/console/web/bun.lock":     `"name": "beacon-console-web"`,
		"apps/portal/web/package.json":  `"name": "beacon-portal-web"`,
		"apps/portal/web/bun.lock":      `"name": "beacon-portal-web"`,
	} {
		if got := read(t, a, rel); !strings.Contains(got, want) {
			t.Errorf("%s lacks %s:\n%s", rel, want, got)
		}
	}

	// The processes and the environment.
	procfile := read(t, a, "Procfile")
	for _, want := range []string{
		"console: bash -c 'for i in {1..300}; do (echo > /dev/tcp/127.0.0.1/${SPANNER_EMULATOR_PORT}) >/dev/null 2>&1 && break || sleep 0.1; done; go run ./cmd/bootstrap && APP_SERVICE_NAME=console PORT=8090 APP_DIST=apps/console/web/dist/console go run ./apps/console'\n",
		"console: bash -c 'for i in {1..600}; do (echo > /dev/tcp/127.0.0.1/8090) >/dev/null 2>&1 && break || sleep 0.5; done; cd apps/console/web && bun install && PORT=8090 bun run start:console'\n",
		"portal: bash -c 'for i in {1..300}; do (echo > /dev/tcp/127.0.0.1/8090) >/dev/null 2>&1 && break || sleep 0.1; done; APP_SERVICE_NAME=portal PORT=8091 APP_DIST=apps/portal/web/dist/portal go run ./apps/portal'\n",
		"portal-web: bash -c 'for i in {1..600}; do (echo > /dev/tcp/127.0.0.1/8091) >/dev/null 2>&1 && break || sleep 0.5; done; cd apps/portal/web && bun install && PORT=8091 bun run start:portal'\n",
	} {
		if !strings.Contains(procfile, want) {
			t.Errorf("Procfile lacks %q:\n%s", want, procfile)
		}
	}
	env := read(t, a, ".envrc.template")
	if strings.Contains(env, "export PORT=8090") || !strings.Contains(env, "# export PORT=\n") || !strings.Contains(env, "# export APP_DIST=\n") || !strings.Contains(env, "# --- site: one served site") {
		t.Errorf(".envrc.template = %q", env)
	}

	// The profile reads two sites.
	after, err := app.Discover(a.Root)
	if err != nil {
		t.Fatal(err)
	}
	p := after.Profile()
	if p.Layout != app.LayoutSites || len(p.Sites) != 2 || p.Sites[0].Name != "console" || p.Sites[1].Name != "portal" || len(p.Shared) != 1 {
		t.Errorf("profile = %+v", p)
	}
	if len(exec.calls) != 1 || exec.calls[0] != "go generate ./..." {
		t.Errorf("exec calls = %q", exec.calls)
	}
}

func TestSiteApplyAddsToSites(t *testing.T) {
	t.Parallel()

	a := flatSite(t)
	if _, err := (Site{Name: "portal", First: "console"}).Apply(t.Context(), a, &fakeExec{}); err != nil {
		t.Fatalf("promotion: %v", err)
	}
	promoted, err := app.Discover(a.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Site{Name: "kiosk", First: "console"}).Validate(promoted); err == nil || !strings.Contains(err.Error(), "--first is for promotion") {
		t.Errorf("Validate() with --first on a multi-site application error = %v", err)
	}
	ch, err := Site{Name: "kiosk"}.Apply(t.Context(), promoted, &fakeExec{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(ch.Skipped) != 2 {
		t.Errorf("Skipped = %q", ch.Skipped)
	}
	for _, rel := range []string{"apps/kiosk/main.go", "apps/kiosk/web/kiosk/src/main.ts", "cmd/generate/kioskgenerator/main.go"} {
		if _, err := os.Stat(promoted.Abs(rel)); err != nil {
			t.Errorf("%s: missing", rel)
		}
	}
	if got := read(t, promoted, "cmd/generate/generate.go"); !strings.Contains(got, "./portalgenerator\n//go:generate go run ./kioskgenerator\n//go:generate go run ./sharedgenerator\n") {
		t.Errorf("generate.go = %q", got)
	}
	if got := read(t, promoted, "pkg/deploy/deploy.go"); !strings.Contains(got, "access.UnionCollection(consolerouter.Collection(), portalrouter.Collection(), kioskrouter.Collection())") || !strings.Contains(got, `kioskrouter "example.com/acme/beacon/apps/kiosk/pkg/router"`) {
		t.Errorf("deploy.go = %q", got)
	}
	if got := read(t, promoted, "cmd/generate/sharedgenerator/main.go"); !strings.Contains(got, `GenerateTypescript("apps/kiosk/web/kiosk/src/app/core/service/shared"`) {
		t.Errorf("sharedgenerator = %q", got)
	}
	if got := read(t, promoted, "Procfile"); !strings.Contains(got, "APP_SERVICE_NAME=kiosk PORT=8092 APP_DIST=apps/kiosk/web/dist/kiosk go run ./apps/kiosk'") || !strings.Contains(got, "kiosk-web: ") {
		t.Errorf("Procfile = %q", got)
	}
	if got := read(t, promoted, "apps/kiosk/web/angular.json"); !strings.Contains(got, `"port": 4302`) {
		t.Errorf("kiosk angular.json lacks the next port")
	}
	if got := read(t, promoted, "apps/kiosk/web/package.json"); !strings.Contains(got, `"name": "beacon-kiosk-web"`) {
		t.Errorf("kiosk package.json is not named for the site:\n%s", got)
	}
	if got := read(t, promoted, "apps/kiosk/web/bun.lock"); !strings.Contains(got, `"name": "beacon-kiosk-web"`) {
		t.Errorf("kiosk bun.lock is not named for the site:\n%s", got)
	}
	p := (mustDiscover(t, a.Root)).Profile()
	names := siteNames(p)
	if len(p.Sites) != 3 || !strings.Contains(names, "kiosk") {
		t.Errorf("profile sites = %s", names)
	}
}

func mustDiscover(t *testing.T, root string) *app.App {
	t.Helper()

	a, err := app.Discover(root)
	if err != nil {
		t.Fatal(err)
	}

	return a
}
