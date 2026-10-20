package check

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// The report's lines for the lantern sites: the two passing, and each refusal in full.
const (
	consolePassed = "  bundle   the console site's service serves its bundle from /home/nonroot/apps/console/web/dist/console (APP_DIST), the directory its process line in the Procfile sets and its workspace's build writes\n"
	portalPassed  = "  bundle   the portal site's service serves its bundle from /home/nonroot/apps/portal/web/dist/portal (APP_DIST), the directory its process line in the Procfile sets and its workspace's build writes\n"
	consoleUnset  = "  refused  the console site's process line in the Procfile sets no APP_DIST, the directory the site serves its bundle from: the stack sets its service's to /home/nonroot/apps/console/web/dist/console, the directory the workspace's build writes, and in development the site starts without one (the variable is required); set APP_DIST=apps/console/web/dist/console on the console process line\n"
	consoleDist   = "  refused  the console site's process line in the Procfile sets APP_DIST=apps/console/web/dist, and its workspace's build writes the bundle to apps/console/web/dist/console (angular.json's outputPath): the stack sets its service's APP_DIST to /home/nonroot/apps/console/web/dist, a directory without index.html, so in production every browser path of the site answers 404 while its API answers; set APP_DIST=apps/console/web/dist/console on the console process line, or make the build write to apps/console/web/dist\n"
)

func TestScanSiteBundles(t *testing.T) {
	t.Parallel()

	dist := &derive.Variable{Name: "APP_DIST", Level: derive.LevelSite}
	portal := derive.Site{Name: "portal", Dir: "apps/portal", Bundle: dist, Workspace: "apps/portal/web", BundleDir: "apps/portal/web/dist/portal", ProcfileBundleDir: "apps/portal/web/dist/portal", OutputDir: "apps/portal/web/dist/portal"}
	portalWant := SiteBundle{Site: "portal", Variable: "APP_DIST", Procfile: "apps/portal/web/dist/portal", Output: "apps/portal/web/dist/portal", Served: "/home/nonroot/apps/portal/web/dist/portal"}
	tests := []struct {
		name  string
		model derive.Model
		want  []SiteBundle
		// wantOutput are the report's lines for the bundles, in full; wantNoOutput the
		// words the report must not carry.
		wantOutput   []string
		wantNoOutput []string
	}{
		{
			name: "both sites serve the directory the Procfile sets and the build writes",
			model: derive.Model{Sites: []derive.Site{
				{Name: "console", Dir: "apps/console", Bundle: dist, Workspace: "apps/console/web", BundleDir: "apps/console/web/dist/console", ProcfileBundleDir: "apps/console/web/dist/console", OutputDir: "apps/console/web/dist/console"},
				portal,
			}},
			want: []SiteBundle{
				{Site: "console", Variable: "APP_DIST", Procfile: "apps/console/web/dist/console", Output: "apps/console/web/dist/console", Served: "/home/nonroot/apps/console/web/dist/console"},
				portalWant,
			},
			wantOutput:   []string{consolePassed, portalPassed},
			wantNoOutput: []string{"refused"},
		},
		{
			name: "a Procfile directory written with a dot segment is the build's once cleaned",
			model: derive.Model{Sites: []derive.Site{
				{Name: "console", Dir: "apps/console", Bundle: dist, Workspace: "apps/console/web", BundleDir: "apps/console/web/dist/console", ProcfileBundleDir: "apps/console/web/./dist/console", OutputDir: "apps/console/web/dist/console"},
				portal,
			}},
			want: []SiteBundle{
				{Site: "console", Variable: "APP_DIST", Procfile: "apps/console/web/./dist/console", Output: "apps/console/web/dist/console", Served: "/home/nonroot/apps/console/web/dist/console"},
				portalWant,
			},
			wantNoOutput: []string{"refused"},
		},
		{
			name: "a site whose Procfile line sets no directory is refused",
			model: derive.Model{Sites: []derive.Site{
				{Name: "console", Dir: "apps/console", Bundle: dist, Workspace: "apps/console/web", BundleDir: "apps/console/web/dist/console", OutputDir: "apps/console/web/dist/console"},
				portal,
			}},
			want: []SiteBundle{
				{Site: "console", Variable: "APP_DIST", Output: "apps/console/web/dist/console", Served: "/home/nonroot/apps/console/web/dist/console", Problem: "the console site's process line in the Procfile sets no APP_DIST, the directory the site serves its bundle from: the stack sets its service's to /home/nonroot/apps/console/web/dist/console, the directory the workspace's build writes, and in development the site starts without one (the variable is required); set APP_DIST=apps/console/web/dist/console on the console process line"},
				portalWant,
			},
			wantOutput:   []string{consoleUnset, portalPassed},
			wantNoOutput: []string{"bundle   the console site"},
		},
		{
			name: "a Procfile naming the workspace's dist while the build writes dist/console is refused, with both directories",
			model: derive.Model{Sites: []derive.Site{
				{Name: "console", Dir: "apps/console", Bundle: dist, Workspace: "apps/console/web", BundleDir: "apps/console/web/dist", ProcfileBundleDir: "apps/console/web/dist", OutputDir: "apps/console/web/dist/console"},
				portal,
			}},
			want: []SiteBundle{
				{Site: "console", Variable: "APP_DIST", Procfile: "apps/console/web/dist", Output: "apps/console/web/dist/console", Served: "/home/nonroot/apps/console/web/dist", Problem: "the console site's process line in the Procfile sets APP_DIST=apps/console/web/dist, and its workspace's build writes the bundle to apps/console/web/dist/console (angular.json's outputPath): the stack sets its service's APP_DIST to /home/nonroot/apps/console/web/dist, a directory without index.html, so in production every browser path of the site answers 404 while its API answers; set APP_DIST=apps/console/web/dist/console on the console process line, or make the build write to apps/console/web/dist"},
				portalWant,
			},
			wantOutput: []string{consoleDist, portalPassed},
		},
		{
			name: "a build whose output angular.json does not say passes on the Procfile alone",
			model: derive.Model{Sites: []derive.Site{
				{Name: "console", Dir: "apps/console", Bundle: dist, Workspace: "apps/console/web", BundleDir: "apps/console/web/dist/console", ProcfileBundleDir: "apps/console/web/dist/console"},
				portal,
			}},
			want: []SiteBundle{
				{Site: "console", Variable: "APP_DIST", Procfile: "apps/console/web/dist/console", Served: "/home/nonroot/apps/console/web/dist/console"},
				portalWant,
			},
			wantOutput: []string{"  bundle   the console site's service serves its bundle from /home/nonroot/apps/console/web/dist/console (APP_DIST), the directory its process line in the Procfile sets\n", portalPassed},
		},
		{
			name: "a site without a browser workspace has no bundle to hold",
			model: derive.Model{Sites: []derive.Site{
				{Name: "console", Dir: "apps/console"},
				portal,
			}},
			want:         []SiteBundle{portalWant},
			wantOutput:   []string{portalPassed},
			wantNoOutput: []string{"console"},
		},
		{
			name: "the flat layout's image sets the bundle variables itself: nothing to hold",
			model: derive.Model{Sites: []derive.Site{
				{Name: "harbor", Dir: ".", Bundle: dist, Workspace: "web", BundleDir: "web/dist"},
			}},
			wantNoOutput: []string{"bundle", "refused"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := scanSiteBundles(&tt.model)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("scanSiteBundles() mismatch (-want +got):\n%s", diff)
			}
			r := &Report{SiteBundles: got}
			wantClean := true
			for _, b := range tt.want {
				wantClean = wantClean && b.Problem == ""
			}
			if r.Clean() != wantClean {
				t.Errorf("Clean() = %v, want %v", r.Clean(), wantClean)
			}
			var out bytes.Buffer
			r.Write(&out)
			for _, want := range tt.wantOutput {
				if !strings.Contains(out.String(), want) {
					t.Errorf("Write() output lacks %q:\n%s", want, out.String())
				}
			}
			for _, unwanted := range tt.wantNoOutput {
				if strings.Contains(out.String(), unwanted) {
					t.Errorf("Write() output carries %q:\n%s", unwanted, out.String())
				}
			}
		})
	}
}

// TestLanternSiteBundles runs the scan over the lantern fixture as derived: its Procfile
// sets each site's directory to the one its workspace's build writes, so both sites pass
// and the report says where each serves from.
func TestLanternSiteBundles(t *testing.T) {
	t.Parallel()

	m := fixtureModel(t, "lantern", "placement-lantern.json")
	got := scanSiteBundles(m)
	want := []SiteBundle{
		{Site: "console", Variable: "APP_DIST", Procfile: "apps/console/web/dist/console", Output: "apps/console/web/dist/console", Served: "/home/nonroot/apps/console/web/dist/console"},
		{Site: "portal", Variable: "APP_DIST", Procfile: "apps/portal/web/dist/portal", Output: "apps/portal/web/dist/portal", Served: "/home/nonroot/apps/portal/web/dist/portal"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("scanSiteBundles() mismatch (-want +got):\n%s", diff)
	}
	var out bytes.Buffer
	(&Report{SiteBundles: got}).Write(&out)
	for _, line := range []string{consolePassed, portalPassed} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("Write() output lacks %q:\n%s", line, out.String())
		}
	}
}
