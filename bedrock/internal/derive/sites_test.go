package derive

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/impulse/app"
)

// siteProcesses is the lantern Procfile's two site lines with the given inline
// assignments before each site's program, as impulse writes them.
func siteProcesses(console, portal string) string {
	return fmt.Sprintf("# the sites\nconsole: bash -c 'go run ./cmd/bootstrap && APP_SERVICE_NAME=console PORT=8094 %sgo run ./apps/console'\nportal: bash -c 'APP_SERVICE_NAME=portal PORT=8095 %sgo run ./apps/portal'\n", console, portal)
}

// angularWorkspace is a console workspace's angular.json: one application project with
// the builder and the raw outputPath given, no outputPath when empty, under the key the
// targets are declared by (architect, or targets).
func angularWorkspace(targetsKey, builder, outputPath string) string {
	options := `"index": "src/index.html"`
	if outputPath != "" {
		options = `"outputPath": ` + outputPath + `, ` + options
	}

	return fmt.Sprintf(`{"version": 1, "projects": {"console": {"projectType": "application", "root": "", %q: {"build": {"builder": %q, "options": {%s}}}}}}`, targetsKey, builder, options)
}

// bundleLine spells where a site's bundle is: the workspace, the served directory, the
// Procfile's and the build's.
func bundleLine(s *Site) string {
	return fmt.Sprintf("%s: workspace %s, serves %s, Procfile %q, build %q", s.Name, s.Workspace, s.BundleDir, s.ProcfileBundleDir, s.OutputDir)
}

// TestSiteBundleDir settles where each site of the sites layout serves its bundle from,
// over copies of the lantern fixture whose Procfile and console angular.json each case
// writes: the Procfile's directory for the site's process, else the directory the
// workspace's build writes, read from angular.json's outputPath in each form Angular
// takes (an object with base and browser, a string, none at all) under the application
// builder and the browser builder of old; a Procfile directory the build does not write
// is taken as written, with both recorded for the check; a directory outside the
// workspace, and a site neither names a directory for, are refused.
func TestSiteBundleDir(t *testing.T) {
	t.Parallel()

	const (
		application = "@angular/build:application"
		browser     = "@angular-devkit/build-angular:browser"
		consoleDist = "APP_DIST=apps/console/web/dist/console "
		portalDist  = "APP_DIST=apps/portal/web/dist/portal "
	)
	tests := []struct {
		name string
		// procfile is the Procfile's content; empty removes it; fixture keeps the fixture's.
		procfile string
		fixture  bool
		// angular is the console workspace's angular.json; empty keeps the fixture's.
		angular string
		want    []string
		wantErr string
	}{
		{
			name:    "the Procfile names both sites' bundles, the directories the builds write",
			fixture: true,
			want: []string{
				`console: workspace apps/console/web, serves apps/console/web/dist/console, Procfile "apps/console/web/dist/console", build "apps/console/web/dist/console"`,
				`portal: workspace apps/portal/web, serves apps/portal/web/dist/portal, Procfile "apps/portal/web/dist/portal", build "apps/portal/web/dist/portal"`,
			},
		},
		{
			name:     "a Procfile naming one site's bundle: the other serves the directory its build writes",
			procfile: siteProcesses(consoleDist, ""),
			want: []string{
				`console: workspace apps/console/web, serves apps/console/web/dist/console, Procfile "apps/console/web/dist/console", build "apps/console/web/dist/console"`,
				`portal: workspace apps/portal/web, serves apps/portal/web/dist/portal, Procfile "", build "apps/portal/web/dist/portal"`,
			},
		},
		{
			name:     "a Procfile directory the build does not write is served as written, both recorded",
			procfile: siteProcesses("APP_DIST=apps/console/web/dist ", portalDist),
			want: []string{
				`console: workspace apps/console/web, serves apps/console/web/dist, Procfile "apps/console/web/dist", build "apps/console/web/dist/console"`,
				`portal: workspace apps/portal/web, serves apps/portal/web/dist/portal, Procfile "apps/portal/web/dist/portal", build "apps/portal/web/dist/portal"`,
			},
		},
		{
			name:     "a Procfile directory in double quotes, cleaned",
			procfile: siteProcesses(`APP_DIST="apps/console/web/./dist/console" `, portalDist),
			want: []string{
				`console: workspace apps/console/web, serves apps/console/web/dist/console, Procfile "apps/console/web/./dist/console", build "apps/console/web/dist/console"`,
				`portal: workspace apps/portal/web, serves apps/portal/web/dist/portal, Procfile "apps/portal/web/dist/portal", build "apps/portal/web/dist/portal"`,
			},
		},
		{
			name:    "no Procfile: an object outputPath with browser empty is its base",
			angular: angularWorkspace("architect", application, `{"base": "dist/console", "browser": ""}`),
			want: []string{
				`console: workspace apps/console/web, serves apps/console/web/dist/console, Procfile "", build "apps/console/web/dist/console"`,
				`portal: workspace apps/portal/web, serves apps/portal/web/dist/portal, Procfile "", build "apps/portal/web/dist/portal"`,
			},
		},
		{
			name:    "no Procfile: an object outputPath with a browser subdirectory",
			angular: angularWorkspace("architect", application, `{"base": "dist/console", "browser": "www"}`),
			want: []string{
				`console: workspace apps/console/web, serves apps/console/web/dist/console/www, Procfile "", build "apps/console/web/dist/console/www"`,
				`portal: workspace apps/portal/web, serves apps/portal/web/dist/portal, Procfile "", build "apps/portal/web/dist/portal"`,
			},
		},
		{
			name:    "no Procfile: an object outputPath without browser takes Angular's browser subdirectory",
			angular: angularWorkspace("architect", application, `{"base": "dist/console"}`),
			want: []string{
				`console: workspace apps/console/web, serves apps/console/web/dist/console/browser, Procfile "", build "apps/console/web/dist/console/browser"`,
				`portal: workspace apps/portal/web, serves apps/portal/web/dist/portal, Procfile "", build "apps/portal/web/dist/portal"`,
			},
		},
		{
			name:    "no Procfile: a string outputPath under the application builder, which writes browser/ under it",
			angular: angularWorkspace("architect", application, `"dist/console"`),
			want: []string{
				`console: workspace apps/console/web, serves apps/console/web/dist/console/browser, Procfile "", build "apps/console/web/dist/console/browser"`,
				`portal: workspace apps/portal/web, serves apps/portal/web/dist/portal, Procfile "", build "apps/portal/web/dist/portal"`,
			},
		},
		{
			name:    "no Procfile: a string outputPath under the browser builder of old is the directory itself",
			angular: angularWorkspace("architect", browser, `"dist/console"`),
			want: []string{
				`console: workspace apps/console/web, serves apps/console/web/dist/console, Procfile "", build "apps/console/web/dist/console"`,
				`portal: workspace apps/portal/web, serves apps/portal/web/dist/portal, Procfile "", build "apps/portal/web/dist/portal"`,
			},
		},
		{
			name:    "no Procfile and no outputPath: Angular's default, dist under the project's name",
			angular: angularWorkspace("targets", application, ""),
			want: []string{
				`console: workspace apps/console/web, serves apps/console/web/dist/console/browser, Procfile "", build "apps/console/web/dist/console/browser"`,
				`portal: workspace apps/portal/web, serves apps/portal/web/dist/portal, Procfile "", build "apps/portal/web/dist/portal"`,
			},
		},
		{
			name:     "a Procfile directory outside the workspace is refused",
			procfile: siteProcesses("APP_DIST=dist/console ", portalDist),
			wantErr:  "Procfile: the console process sets APP_DIST=dist/console, which is not under the console site's browser workspace apps/console/web, where the image builds the bundle: the stack would point the site's service at a directory the image does not hold; set it to the directory under apps/console/web the build writes (apps/console/web/dist/console)",
		},
		{
			name:     "a site neither the Procfile nor angular.json names a directory for is refused",
			procfile: siteProcesses("", portalDist),
			angular:  `{"version": 1, "projects": {"shared": {"projectType": "library", "root": "projects/shared"}}}`,
			wantErr:  "the console site (apps/console) names no bundle directory: its console process line in the Procfile sets no APP_DIST, and apps/console/web/angular.json has no application project to read the build's output directory from; set APP_DIST=<directory> on the process line, the directory under apps/console/web the build writes",
		},
		{
			name:     "an angular.json with two application projects names no one bundle, so the Procfile must",
			procfile: siteProcesses("", portalDist),
			angular:  `{"version": 1, "projects": {"console": {"projectType": "application"}, "kiosk": {"projectType": "application"}}}`,
			wantErr:  "the console site (apps/console) names no bundle directory",
		},
		{
			name:    "an outputPath object without a base is refused",
			angular: angularWorkspace("architect", application, `{"browser": ""}`),
			wantErr: "apps/console/web/angular.json: project console: outputPath sets no base",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "app")
			if err := os.CopyFS(dir, os.DirFS(filepath.Join("testdata", "lantern"))); err != nil {
				t.Fatalf("os.CopyFS() error = %v", err)
			}
			if !tt.fixture {
				writeOrRemove(t, filepath.Join(dir, ProcfileName), tt.procfile)
			}
			if tt.angular != "" {
				writeOrRemove(t, filepath.Join(dir, "apps", "console", "web", angularJSONName), tt.angular)
			}
			a, err := app.Discover(dir)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			m, err := Derive(a, fixturePlacement(t, "placement-lantern.json"))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Derive() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Derive() error = %v", err)
			}
			var got []string
			for i := range m.Sites {
				got = append(got, bundleLine(&m.Sites[i]))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Sites =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

// writeOrRemove writes the file with the content, or removes it for empty content.
func writeOrRemove(t *testing.T, file, content string) {
	t.Helper()

	if content == "" {
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}

		return
	}
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestProcfileEnv reads the inline assignment of a variable off a process's command, as
// a shell would before the program: unquoted or within the quotes of a bash -c, the
// variable's own name and not a longer one's, and nothing from a process the file lacks
// or a command that sets none.
func TestProcfileEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		procfile string
		process  string
		want     string
	}{
		{name: "an assignment within a bash -c", procfile: "console: bash -c 'cd x && PORT=1 APP_DIST=apps/console/web/dist/console go run ./apps/console'\n", process: "console", want: "apps/console/web/dist/console"},
		{name: "an unquoted assignment", procfile: "console: APP_DIST=web/dist/console go run .\n", process: "console", want: "web/dist/console"},
		{name: "a double-quoted value within a bash -c in single quotes", procfile: `console: bash -c 'APP_DIST="apps/console/web/dist" go run ./apps/console'` + "\n", process: "console", want: "apps/console/web/dist"},
		{name: "a single-quoted value within a bash -c in double quotes", procfile: `console: bash -c "APP_DIST='apps/console/web/dist' go run ./apps/console"` + "\n", process: "console", want: "apps/console/web/dist"},
		{name: "a quoted empty value sets nothing", procfile: `console: APP_DIST="" go run .` + "\n", process: "console", want: ""},
		{name: "a longer variable's assignment is not this one's", procfile: "console: XAPP_DIST=other APP_DIST=mine go run .\n", process: "console", want: "mine"},
		{name: "a longer name alone sets nothing", procfile: "console: XAPP_DIST=other go run .\n", process: "console", want: ""},
		{name: "a process that sets none", procfile: "console: go run ./apps/console\n", process: "console", want: ""},
		{name: "a process the file lacks", procfile: "console: APP_DIST=x go run .\n", process: "portal", want: ""},
		{name: "a comment naming the assignment is not a process line", procfile: "# console: APP_DIST=x go run .\nconsole: go run .\n", process: "console", want: ""},
		{name: "a blank and a malformed line are skipped", procfile: "\nnot a process line\nconsole: APP_DIST=x go run .\n", process: "console", want: "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := parseProcfile(tt.procfile).env(tt.process, SiteBundleVariable); got != tt.want {
				t.Errorf("env(%q, APP_DIST) = %q, want %q", tt.process, got, tt.want)
			}
		})
	}
}
