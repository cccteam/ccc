package transition

import (
	"go/format"
	"os"
	"path"
	"slices"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/names"
)

// The flat site's hand-written files in the base's shape, minimal. The main package
// imports pkg/config beside pkg/router: the promotion moves the router under
// apps/console/pkg/router, which sorts before pkg/config, so the import block's order
// flips under the rewrite.
const (
	siteMain = `// main serves beacon.
package main

import (
	"example.com/acme/beacon/app"
	"example.com/acme/beacon/pkg/config"
	"example.com/acme/beacon/pkg/router"
)

func main() {
	conf, _ := config.NewSiteConfiguration(nil)
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
	siteLevel = `package config

// SiteConfiguration is the third level: the served site.
type SiteConfiguration struct {
	env *siteConfig
}

func NewSiteConfiguration(ctx any) (*SiteConfiguration, error) {
	return &SiteConfiguration{}, nil
}

func (c *SiteConfiguration) ConsoleDist() string {
	return c.env.ConsoleDist
}

type siteConfig struct {
	Port        string ` + "`" + `env:"PORT,default=8080"` + "`" + `
	ConsoleDist string ` + "`" + `env:"APP_CONSOLE_DIST,default=web/dist/console"` + "`" + `
}
`
	siteDeploy = `package deploy

import (
	"context"

	"github.com/cccteam/access"
)

func CheckRoles(ctx context.Context, client *access.Client, name string) error {
	_, err := client.CheckPolicy(ctx)

	return err
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
	siteEnv = `# --- site: the served site ---
export PORT=8090
# export APP_CONSOLE_DIST=web/dist/console
`
	siteProcfile = `spanner: podman run --rm -p 127.0.0.1:${SPANNER_EMULATOR_PORT}:9010 gcr.io/cloud-spanner-emulator/emulator:1.5.56
server: bash -c 'for i in {1..300}; do (echo > /dev/tcp/127.0.0.1/${SPANNER_EMULATOR_PORT}) >/dev/null 2>&1 && break || sleep 0.1; done; go run ./cmd/bootstrap && go run .'
console: bash -c 'for i in {1..600}; do (echo > /dev/tcp/127.0.0.1/${PORT}) >/dev/null 2>&1 && break || sleep 0.5; done; cd web && bun install && bun run start:console'
`
)

// siteData is the flat data level handing the staff auth the one router's collection.
func siteData() string {
	text := strings.Replace(authConfig, "\t\"example.com/acme/beacon/pkg/auth/staff\"\n", "\t\"example.com/acme/beacon/pkg/auth/staff\"\n\t\"example.com/acme/beacon/pkg/router\"\n", 1)

	return strings.Replace(text, "staff.Settings{CookieKey: cookieKey}", "staff.Settings{Collection: router.Collection(), CookieKey: cookieKey}", 1)
}

// flatSite is the beacon application with a flat site's hand-written files.
func flatSite(t *testing.T) *app.App {
	t.Helper()

	return beacon(t, flatSiteFiles(t))
}

// generatedRouterSite is the beacon application with a flat site whose router package is
// generated code alone, as a skeleton's pkg/router is: no hand-written file for add site
// to copy, while the main package and the authorization harness import the package.
func generatedRouterSite(t *testing.T) *app.App {
	t.Helper()

	a := beacon(t, flatSiteFiles(t))
	if err := os.Remove(a.Abs("pkg/router/router.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.Abs("pkg/router/zz_gen_router.go"), []byte("package router\n\nimport \"example.com/acme/beacon/app\"\n\nfunc New(a *app.App) any { return generatedRoutes(a) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	return mustDiscover(t, a.Root)
}

// flatSiteFiles is the flat site's file set over the auth's.
func flatSiteFiles(t *testing.T) map[string]string {
	t.Helper()

	files := authFiles(t)
	files["main.go"] = siteMain
	files["app/app.go"] = siteApp
	files["pkg/router/router.go"] = siteRouter
	files["pkg/router/zz_gen_routes.go"] = "package router\n\nfunc generatedRoutes(any) any { return nil }\n"
	files["pkg/config/site.go"] = siteLevel
	files["pkg/config/data.go"] = siteData()
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

	return files
}

// siteRegenerated is the command sequence add site runs: the new site's generate program
// alone, so its router package exists, then the module is made whole, then everything is
// regenerated.
func siteRegenerated(site string) []string {
	return append([]string{"go run ./" + site + "generator"}, regenerated...)
}

func TestSiteValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		site    Site
		wantErr string
	}{
		{name: "promotion", site: Site{Name: "portal", Existing: "console"}},
		{name: "the longest name allowed", site: Site{Name: strings.Repeat("s", 25), Existing: "console"}},
		{name: "promotion without --existing", site: Site{Name: "portal"}, wantErr: "--existing is required"},
		{name: "a bad name", site: Site{Name: "Portal", Existing: "console"}, wantErr: `site name "Portal": ` + names.SiteGuidance},
		{name: "a bad name is refused before --existing is missed", site: Site{Name: "Portal"}, wantErr: `site name "Portal": ` + names.SiteGuidance},
		{name: "a hyphenated name", site: Site{Name: "my-site", Existing: "console"}, wantErr: `site name "my-site": ` + names.SiteGuidance},
		{name: "a name over the cap", site: Site{Name: strings.Repeat("s", 26), Existing: "console"}, wantErr: "is 26 characters, and 25 is the most a site name may take"},
		{name: "a bad existing name", site: Site{Name: "portal", Existing: "Console"}, wantErr: `site name "Console": ` + names.SiteGuidance},
		{name: "an existing name over the cap", site: Site{Name: "portal", Existing: strings.Repeat("c", 26)}, wantErr: "is 26 characters, and 25 is the most a site name may take"},
		{name: "the same name twice", site: Site{Name: "portal", Existing: "portal"}, wantErr: "names the new site too"},
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
	ch, err := Site{Name: "portal", Existing: "console"}.Apply(t.Context(), a, exec)
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
	for _, rel := range []string{"main.go", "app", "pkg/router", "pkg/resources", "test/authz", "web", "cmd/generate/resourcegenerator"} {
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
	// Every Go file the promotion rewrote is written as gofmt leaves it: the moved
	// packages' imports now sort elsewhere in their blocks.
	if want := "import (\n\t\"example.com/acme/beacon/apps/console/app\"\n\t\"example.com/acme/beacon/apps/console/pkg/router\"\n\t\"example.com/acme/beacon/pkg/config\"\n)\n"; !strings.Contains(main, want) {
		t.Errorf("apps/console/main.go's import block is not sorted:\n%s", main)
	}
	for _, rel := range []string{
		"apps/console/main.go", "apps/console/app/app.go", "apps/console/pkg/router/router.go", "apps/console/test/authz/harness_test.go",
		"apps/portal/main.go", "apps/portal/app/app.go", "apps/portal/pkg/router/router.go", "apps/portal/test/authz/harness_test.go",
		"cmd/generate/consolegenerator/main.go", "pkg/config/data.go", "pkg/deploy/deploy.go",
	} {
		assertFormatted(t, a, rel)
	}
	if got := read(t, a, "apps/console/web/go.mod"); !strings.Contains(got, "module example.com/acme/beacon/apps/console/web\n") {
		t.Errorf("web boundary module = %q", got)
	}

	// The generator program is the console site's.
	gen := read(t, a, "cmd/generate/consolegenerator/main.go")
	for _, want := range []string{`"apps/console/pkg/resources"`, `GenerateHandlers("apps/console/app")`, `GenerateRoutes("apps/console/pkg/router", "api")`, `GenerateHandlerTests("apps/console/test/authz")`, `GenerateTypescript("apps/console/web/console/src/app/core/service"`} {
		if !strings.Contains(gen, want) {
			t.Errorf("consolegenerator lacks %q:\n%s", want, gen)
		}
	}
	if got := read(t, a, "cmd/generate/generate.go"); !strings.Contains(got, "//go:generate go run ./consolegenerator\n//go:generate go run ./portalgenerator\n//go:generate go run ./sharedgenerator\n") {
		t.Errorf("generate.go = %q", got)
	}

	// The site level.
	site := read(t, a, "pkg/config/site.go")
	for _, want := range []string{"type SiteConfiguration struct", "func NewSiteConfiguration(", "func (c *SiteConfiguration) Dist() string {\n\treturn c.env.Dist\n}", `env:"APP_DIST,required"`} {
		if !strings.Contains(site, want) {
			t.Errorf("site.go lacks %q:\n%s", want, site)
		}
	}
	if appGo := read(t, a, "apps/console/app/app.go"); !strings.Contains(appGo, "Dist() string") || strings.Contains(appGo, "consoleDist") {
		t.Errorf("app.go = %q", appGo)
	}

	// The union and the shared generator cover both sites.
	data := read(t, a, "pkg/config/data.go")
	for _, want := range []string{`consolerouter "example.com/acme/beacon/apps/console/pkg/router"`, `portalrouter "example.com/acme/beacon/apps/portal/pkg/router"`, "\tcollection, err := Collection()\n\tif err != nil {\n\t\treturn nil, err\n\t}\n\n\tstaffAuth, err := staff.New(ctx, spannerClient, &staff.Settings{Collection: collection, CookieKey: cookieKey})", `"github.com/cccteam/access"`, `"github.com/go-playground/errors/v5"`, "access.UnionCollection(consolerouter.Collection(), portalrouter.Collection())"} {
		if !strings.Contains(data, want) {
			t.Errorf("data.go lacks %q:\n%s", want, data)
		}
	}
	shared := read(t, a, "cmd/generate/sharedgenerator/generator.go")
	for _, want := range []string{`"pkg/sharedresources"`, `GenerateTypescript("apps/console/web/console/src/app/core/service/shared"`, `GenerateTypescript("apps/portal/web/portal/src/app/core/service/shared"`, `WithSpannerEmulatorVersion("1.5.56")`} {
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
	for _, want := range []string{`"apps/portal/pkg/resources"`, `GenerateHandlers("apps/portal/app")`, `GenerateTypescript("apps/portal/web/portal/src/app/core/service"`} {
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
	// The scripts are renamed for the project, the test script with its flags kept, and the
	// spec tsconfig came along with the project copy.
	pkg := read(t, a, "apps/portal/web/package.json")
	for _, want := range []string{`"start:portal": "ng serve portal --no-hmr"`, `"test": "ng test portal --watch=false"`} {
		if !strings.Contains(pkg, want) {
			t.Errorf("portal package.json lacks %s:\n%s", want, pkg)
		}
	}
	if _, err := os.Stat(a.Abs("apps/portal/web/portal/tsconfig.spec.json")); err != nil {
		t.Errorf("the portal site's spec tsconfig: %v", err)
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
		"console-web: bash -c 'for i in {1..600}; do (echo > /dev/tcp/127.0.0.1/8090) >/dev/null 2>&1 && break || sleep 0.5; done; cd apps/console/web && bun install && PORT=8090 bun run start:console'\n",
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

	// The CI workflow is rewritten from the code with a browser job per site.
	workflow := read(t, a, ".github/workflows/ci.yml")
	for _, want := range []string{"\n  angular-console:\n", "\n  angular-portal:\n", "working-directory: apps/console/web\n", "working-directory: apps/portal/web\n"} {
		if !strings.Contains(workflow, want) {
			t.Errorf("ci.yml lacks %q", want)
		}
	}
	if strings.Contains(workflow, "angular-web:") {
		t.Errorf("ci.yml still carries the flat workspace's job:\n%s", workflow)
	}
	if !slices.Contains(ch.Did, ".github/workflows/ci.yml: rewritten from the code (an angular-portal job for apps/portal/web)") {
		t.Errorf("Did lacks the workflow rewrite: %q", ch.Did)
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
	if want := siteRegenerated("portal"); !slices.Equal(want, exec.calls) {
		t.Errorf("exec calls = %q, want %q", exec.calls, want)
	}
}

// assertFormatted fails when gofmt would change the file.
func assertFormatted(t *testing.T, a *app.App, rel string) {
	t.Helper()

	text := read(t, a, rel)
	formatted, err := format.Source([]byte(text))
	if err != nil {
		t.Fatalf("%s: format.Source() error = %v", rel, err)
	}
	if string(formatted) != text {
		t.Errorf("%s: gofmt would change it after the promotion:\n%s", rel, text)
	}
}

func TestSiteApplyAddsToSites(t *testing.T) {
	t.Parallel()

	a := flatSite(t)
	if _, err := (Site{Name: "portal", Existing: "console"}).Apply(t.Context(), a, &fakeExec{}); err != nil {
		t.Fatalf("promotion: %v", err)
	}
	promoted, err := app.Discover(a.Root)
	if err != nil {
		t.Fatal(err)
	}
	if err := (Site{Name: "kiosk", Existing: "console"}).Validate(promoted); err == nil || !strings.Contains(err.Error(), "--existing is for promotion") {
		t.Errorf("Validate() with --existing on an application in the sites layout error = %v", err)
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
	if got := read(t, promoted, "pkg/config/data.go"); !strings.Contains(got, "access.UnionCollection(consolerouter.Collection(), portalrouter.Collection(), kioskrouter.Collection())") || !strings.Contains(got, `kioskrouter "example.com/acme/beacon/apps/kiosk/pkg/router"`) {
		t.Errorf("data.go = %q", got)
	}
	if got := read(t, promoted, "cmd/generate/sharedgenerator/generator.go"); !strings.Contains(got, `GenerateTypescript("apps/kiosk/web/kiosk/src/app/core/service/shared"`) {
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
	if got := read(t, promoted, ".github/workflows/ci.yml"); !strings.Contains(got, "\n  angular-kiosk:\n") || !strings.Contains(got, "working-directory: apps/kiosk/web\n") {
		t.Errorf("ci.yml lacks the kiosk site's job:\n%s", got)
	}
	if !slices.Contains(ch.Did, ".github/workflows/ci.yml: rewritten from the code (an angular-kiosk job for apps/kiosk/web)") {
		t.Errorf("Did lacks the workflow rewrite: %q", ch.Did)
	}
	p := mustDiscover(t, a.Root).Profile()
	sites := siteNames(p)
	if len(p.Sites) != 3 || !strings.Contains(sites, "kiosk") {
		t.Errorf("profile sites = %s", sites)
	}
}

// TestSiteApplyGeneratesRouterFirst: the new site's router package is generated code
// alone, so nothing is copied into it and the site's generate program runs before go mod
// tidy, which otherwise refuses the module for the site's imports of a package that does
// not exist.
func TestSiteApplyGeneratesRouterFirst(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// promoted adds the site to an application already in the sites layout: the
		// flat application is promoted first, with portal beside console.
		promoted bool
		site     Site
		// router is the new site's router package, which the program lays in.
		router string
		// fails makes the program fail.
		fails   bool
		wantErr string
	}{
		{
			name:   "a flat application whose router is generated gains a second site",
			site:   Site{Name: "portal", Existing: "console"},
			router: "apps/portal/pkg/router",
		},
		{
			name:     "an application in the sites layout gains a third site",
			promoted: true,
			site:     Site{Name: "kiosk"},
			router:   "apps/kiosk/pkg/router",
		},
		{
			name:    "the program fails before the tidy",
			site:    Site{Name: "portal", Existing: "console"},
			router:  "apps/portal/pkg/router",
			fails:   true,
			wantErr: "go run ./portalgenerator failed in cmd/generate, so the portal site's router package is not generated and go mod tidy cannot resolve the site's imports; fix the cause and run it there, then go mod tidy and go generate ./...:\ngeneration: boom",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := generatedRouterSite(t)
			if tt.promoted {
				first := &fakeExec{program: layRouter(a, "apps/portal/pkg/router")}
				if _, err := (Site{Name: "portal", Existing: "console"}).Apply(t.Context(), a, first); err != nil {
					t.Fatalf("promotion: %v", err)
				}
				a = mustDiscover(t, a.Root)
			}
			// What the program and the tidy see of the router package when each runs.
			routerBeforeProgram, routerAtTidy := true, false
			exec := &fakeExec{
				program: func() error {
					_, err := os.Stat(a.Abs(tt.router))
					routerBeforeProgram = err == nil
					if tt.fails {
						return errors.New("boom")
					}

					return layRouter(a, tt.router)()
				},
				tidy: func() error {
					_, err := os.Stat(a.Abs(path.Join(tt.router, "zz_gen_router.go")))
					routerAtTidy = err == nil

					return nil
				},
			}
			ch, err := tt.site.Apply(t.Context(), a, exec)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Apply() error = %v, want %q", err, tt.wantErr)
				}
				if want := []string{"go run ./" + tt.site.Name + "generator"}; !slices.Equal(want, exec.calls) {
					t.Errorf("exec calls = %q, want %q: a failed program refuses the transition before the tidy", exec.calls, want)
				}

				return
			}
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if want := siteRegenerated(tt.site.Name); !slices.Equal(want, exec.calls) {
				t.Errorf("exec calls = %q, want %q", exec.calls, want)
			}
			if routerBeforeProgram {
				t.Errorf("%s: exists before the program ran; the copy lays no generated file in", tt.router)
			}
			if !routerAtTidy {
				t.Errorf("%s: missing when go mod tidy ran", tt.router)
			}
			// The site imports the package the program laid in, and nothing else of the
			// first site's router came along.
			if got := read(t, a, path.Join(sitesDir, tt.site.Name, "main.go")); !strings.Contains(got, `"example.com/acme/beacon/`+tt.router+`"`) {
				t.Errorf("main.go lacks the router import:\n%s", got)
			}
			entries, err := os.ReadDir(a.Abs(tt.router))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 || entries[0].Name() != "zz_gen_router.go" {
				t.Errorf("%s holds %d entries, want the program's zz_gen_router.go alone", tt.router, len(entries))
			}
			if want := "cmd/generate: ran ./" + tt.site.Name + "generator before go mod tidy, so the " + tt.site.Name + " site's router package, generated code alone, exists when tidy reads the site's imports"; !slices.Contains(ch.Did, want) {
				t.Errorf("Did lacks %q:\n%s", want, strings.Join(ch.Did, "\n"))
			}
		})
	}
}

// layRouter is a generate program's script: it writes the site's router package, as the
// generator does on a run.
func layRouter(a *app.App, router string) func() error {
	return func() error {
		if err := os.MkdirAll(a.Abs(router), 0o755); err != nil {
			return errors.Wrap(err, "os.MkdirAll()")
		}
		if err := os.WriteFile(a.Abs(path.Join(router, "zz_gen_router.go")), []byte("package router\n"), 0o644); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}

		return nil
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
