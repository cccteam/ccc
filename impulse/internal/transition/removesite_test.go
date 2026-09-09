package transition

import (
	"os"
	"strings"
	"testing"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// threeSites is the beacon application promoted to console, portal, and kiosk.
func threeSites(t *testing.T) *app.App {
	t.Helper()

	a := flatSite(t)
	if _, err := (Site{Name: "portal", First: "console"}).Apply(t.Context(), a, &fakeExec{}); err != nil {
		t.Fatalf("promotion: %v", err)
	}
	if _, err := (Site{Name: "kiosk"}).Apply(t.Context(), mustDiscover(t, a.Root), &fakeExec{}); err != nil {
		t.Fatalf("kiosk: %v", err)
	}

	return mustDiscover(t, a.Root)
}

func TestRemoveSiteValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		app     func(t *testing.T) *app.App
		site    string
		wantErr string
	}{
		{name: "a site of three", app: threeSites, site: "portal"},
		{name: "a site the application lacks", app: threeSites, site: "shop", wantErr: "no site named shop (the sites are console, kiosk, portal)"},
		{name: "a bad name", app: threeSites, site: "Portal", wantErr: `site name "Portal"`},
		{name: "a flat application", app: flatSite, site: "console", wantErr: "the application is flat"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := RemoveSite{Name: tt.site}.Validate(tt.app(t))
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Validate() error = %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestRemoveSiteApply(t *testing.T) {
	t.Parallel()

	a := threeSites(t)
	harness := "package integration\n\nimport _ \"example.com/acme/beacon/apps/portal/app\"\n"
	if err := os.MkdirAll(a.Abs("test/integration"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.Abs("test/integration/harness_test.go"), []byte(harness), 0o644); err != nil {
		t.Fatal(err)
	}
	exec := &fakeExec{}
	ch, err := RemoveSite{Name: "portal"}.Apply(t.Context(), a, exec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	wantDid := []string{
		"deleted apps/portal: the portal site's main package, handlers, router, resources, authorization suite, and browser workspace",
		"deleted cmd/generate/portalgenerator: the portal site's generator",
		"cmd/generate/generate.go: no longer runs ./portalgenerator",
		"cmd/generate/sharedgenerator/main.go: no longer emits the shared TypeScript into the portal site",
		"pkg/deploy/deploy.go: the portal site's router collection is out of the union",
		"Procfile: removed the portal and portal-web process(es); the comments that describe them are still there",
		"ran go generate ./...: every remaining site's generated code at its place, without the portal site's",
	}
	if strings.Join(ch.Did, "\n") != strings.Join(wantDid, "\n") {
		t.Errorf("Did = %q, want %q", ch.Did, wantDid)
	}
	// The harness that imports the site, the integration suite, the deployment, and the
	// resource declarations.
	if len(ch.Skipped) != 4 || !strings.Contains(ch.Skipped[0], "unwired: test/integration/harness_test.go") || !strings.Contains(ch.Skipped[3], "apps/portal/pkg/resources/resources.go") {
		t.Errorf("Skipped = %q", ch.Skipped)
	}
	for _, rel := range []string{"apps/portal", "cmd/generate/portalgenerator"} {
		if _, err := os.Stat(a.Abs(rel)); !os.IsNotExist(err) {
			t.Errorf("%s still exists (%v)", rel, err)
		}
	}
	if got := read(t, a, "cmd/generate/generate.go"); !strings.Contains(got, "//go:generate go run ./consolegenerator\n//go:generate go run ./kioskgenerator\n//go:generate go run ./sharedgenerator\n") {
		t.Errorf("generate.go = %q", got)
	}
	shared := read(t, a, "cmd/generate/sharedgenerator/main.go")
	if strings.Contains(shared, "apps/portal") || !strings.Contains(shared, `GenerateTypescript("apps/console/web/console/src/app/core/service/shared"`) || !strings.Contains(shared, `GenerateTypescript("apps/kiosk/web/kiosk/src/app/core/service/shared"`) {
		t.Errorf("sharedgenerator = %q", shared)
	}
	deploy := read(t, a, "pkg/deploy/deploy.go")
	if strings.Contains(deploy, "portalrouter") || !strings.Contains(deploy, "access.UnionCollection(consolerouter.Collection(), kioskrouter.Collection())") {
		t.Errorf("deploy.go = %q", deploy)
	}
	procfile := read(t, a, "Procfile")
	if strings.Contains(procfile, "apps/portal") || !strings.Contains(procfile, "go run ./apps/console'") || !strings.Contains(procfile, "kiosk-web: ") {
		t.Errorf("Procfile = %q", procfile)
	}
	p := mustDiscover(t, a.Root).Profile()
	if p.Layout != app.LayoutSites || siteNames(p) != "console, kiosk" {
		t.Errorf("profile = %s in the %s layout", siteNames(p), p.Layout)
	}

	// Down to one site: the application stays multi-site, and the last site is kept.
	ch, err = RemoveSite{Name: "kiosk"}.Apply(t.Context(), mustDiscover(t, a.Root), &fakeExec{})
	if err != nil {
		t.Fatalf("Apply() down to one site error = %v", err)
	}
	if last := ch.Did[len(ch.Did)-1]; last != "the console site is the one left; the application stays multi-site, with the site under apps/console and the shared packages at the root" {
		t.Errorf("last Did = %q", last)
	}
	if got := read(t, a, "pkg/deploy/deploy.go"); !strings.Contains(got, "access.UnionCollection(consolerouter.Collection())") {
		t.Errorf("deploy.go = %q", got)
	}
	p = mustDiscover(t, a.Root).Profile()
	if p.Layout != app.LayoutSites || siteNames(p) != "console" || len(p.Shared) != 1 {
		t.Errorf("profile = %s in the %s layout, %d shared", siteNames(p), p.Layout, len(p.Shared))
	}
	for _, rel := range []string{"apps/console/main.go", "cmd/generate/consolegenerator/main.go", "cmd/generate/sharedgenerator/main.go", "pkg/sharedresources/sharedresources.go"} {
		if _, err := os.Stat(a.Abs(rel)); err != nil {
			t.Errorf("%s: missing", rel)
		}
	}
	if err := (RemoveSite{Name: "console"}).Validate(mustDiscover(t, a.Root)); err == nil || !strings.Contains(err.Error(), "the application's only site") {
		t.Errorf("Validate() of the last site error = %v", err)
	}
}
