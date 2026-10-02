package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/ci"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

func TestRenderReport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		report renderReport
		want   string
	}{
		{
			name: "one workspace, plain",
			report: renderReport{
				candidate: "solo", dir: "../beacon", modulePath: "example.com/acme/beacon", name: "beacon",
				rendered: &skeleton.Rendered{Files: 90}, port: "8090", emulator: "9014",
				goProcs: []string{"spanner", "server"},
				web:     []webWorkspace{{Dir: "web", Projects: []webProject{{Name: "console", Port: 4300}}}},
			},
			want: `Rendered solo into ../beacon as example.com/acme/beacon (90 files).
Named beacon: the web package, APP_SERVICE_NAME, and the development database carry it.

Ports: the server listens on :8090 and the Spanner emulator on :9014.
       Both are set in .envrc.template; change them there if either is taken.

Next steps
  1. cd ../beacon
  2. cp .envrc.template .envrc && direnv allow
  3. overmind start -l spanner,server
     The Go side alone. The first run compiles and bootstraps before it listens; wait for
     "Starting Server", then sign in against the API as admin with the password "password".
  4. (cd web && ./ccclib.sh local)
     Once, for the browser apps: publishes ccc-lib to the local yalc store, links it, and runs
     bun install. ccclib.sh expects the ccc-lib checkout beside this application; set CCC_LIB otherwise.
  5. overmind start
     Everything, with ng serve for the console at http://127.0.0.1:4300.
`,
		},
		{
			name: "two sites, dev workspace with a missing checkout, styled",
			report: renderReport{
				candidate: "sites", dir: "/w/harbor", modulePath: "github.com/cccteam/harbor", name: "harbor", devRoot: "/w",
				rendered: &skeleton.Rendered{
					Files: 200, Workspace: "/w/harbor/go.work",
					DevUsed:    []string{"github.com/cccteam/ccc/resource", "github.com/cccteam/session"},
					DevMissing: []string{"github.com/cccteam/db-initiator"},
				},
				port: "8094", emulator: "9017", styled: true,
				goProcs: []string{"spanner", "console", "portal"},
				web: []webWorkspace{
					{Dir: "apps/console/web", Projects: []webProject{{Name: "console", Port: 4304}}},
					{Dir: "apps/portal/web", Projects: []webProject{{Name: "portal", Port: 4305}}},
				},
			},
			want: "Rendered sites into /w/harbor as github.com/cccteam/harbor (200 files).\n" +
				"Named harbor: the web package, APP_SERVICE_NAME, and the development database carry it.\n" +
				"Wrote go.work using 2 local framework checkout(s) under /w.\n" +
				"No checkout for github.com/cccteam/db-initiator; those pins stay in force.\n" +
				"\n\x1b[1mPorts:\x1b[0m the server listens on :8094 and the Spanner emulator on :9017.\n" +
				"       Both are set in .envrc.template; change them there if either is taken.\n" +
				"\n\x1b[1mNext steps\x1b[0m\n" +
				"  \x1b[1m1.\x1b[0m cd /w/harbor\n" +
				"  \x1b[1m2.\x1b[0m cp .envrc.template .envrc && direnv allow\n" +
				"  \x1b[1m3.\x1b[0m overmind start -l spanner,console,portal\n" +
				"     The Go side alone. The first run compiles and bootstraps before it listens; wait for\n" +
				"     \"Starting Server\", then sign in against the API as admin with the password \"password\".\n" +
				"  \x1b[1m4.\x1b[0m (cd apps/console/web && ./ccclib.sh local)\n" +
				"  \x1b[1m5.\x1b[0m (cd apps/portal/web && ./ccclib.sh local)\n" +
				"     Once, for the browser apps: publishes ccc-lib to the local yalc store, links it, and runs\n" +
				"     bun install. ccclib.sh expects the ccc-lib checkout beside this application; set CCC_LIB otherwise.\n" +
				"  \x1b[1m6.\x1b[0m overmind start\n" +
				"     Everything, with ng serve for the console at http://127.0.0.1:4304 and the portal at http://127.0.0.1:4305.\n",
		},
		{
			name: "a new application: headline, first commit, and the options step",
			report: renderReport{
				headline: "Created example.com/acme/beacon at ../beacon with the members auth (90 files).",
				gitNote:  "Committed the tree as the application's first commit.", options: true,
				candidate: "solo", dir: "../beacon", modulePath: "example.com/acme/beacon", name: "beacon",
				rendered: &skeleton.Rendered{Files: 90}, port: "8090", emulator: "9014",
				goProcs: []string{"spanner", "server"},
				web:     []webWorkspace{{Dir: "web", Projects: []webProject{{Name: "console", Port: 4300}}}},
			},
			want: `Created example.com/acme/beacon at ../beacon with the members auth (90 files).
Named beacon: the web package, APP_SERVICE_NAME, and the development database carry it.
Committed the tree as the application's first commit.

Ports: the server listens on :8090 and the Spanner emulator on :9014.
       Both are set in .envrc.template; change them there if either is taken.

Next steps
  1. cd ../beacon
  2. cp .envrc.template .envrc && direnv allow
  3. overmind start -l spanner,server
     The Go side alone. The first run compiles and bootstraps before it listens; wait for
     "Starting Server", then sign in against the API as admin with the password "password".
  4. (cd web && ./ccclib.sh local)
     Once, for the browser apps: publishes ccc-lib to the local yalc store, links it, and runs
     bun install. ccclib.sh expects the ccc-lib checkout beside this application; set CCC_LIB otherwise.
  5. overmind start
     Everything, with ng serve for the console at http://127.0.0.1:4300.
  6. impulse add tenancy | impulse add outlet <name> --prefix <p> --auth <auth> | impulse add auth <name>
     Options are added one at a time from a clean tree; each ends in a handoff brief for the
     wiring the tool cannot do and a check that says when it is done.
`,
		},
		{
			name: "no browser apps and no env template",
			report: renderReport{
				candidate: "solo", dir: "x", modulePath: "example.com/x",
				rendered: &skeleton.Rendered{Files: 1}, goProcs: []string{"spanner", "server"},
			},
			want: `Rendered solo into x as example.com/x (1 files).

Next steps
  1. cd x
  2. cp .envrc.template .envrc && direnv allow
  3. overmind start
     The first run compiles and bootstraps before it listens. Wait for "Starting Server",
     then sign in as admin with the password "password".
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var b strings.Builder
			tt.report.write(&b)
			if diff := cmp.Diff(tt.want, b.String()); diff != "" {
				t.Errorf("write() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAppName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		flag       string
		modulePath string
		want       string
		wantErr    string
	}{
		{name: "the module path's last segment", modulePath: "example.com/acme/beacon", want: "beacon"},
		{name: "before a major version suffix", modulePath: "example.com/acme/beacon/v2", want: "beacon"},
		{name: "the flag", flag: "harbor", modulePath: "example.com/acme/beacon", want: "harbor"},
		{name: "a last segment that is not a name asks for the flag", modulePath: "example.com/acme/beacon.service", wantErr: "pass --name"},
		{name: "a flag that is not a name", flag: "Beacon", modulePath: "example.com/acme/beacon", wantErr: `application name "Beacon"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := appName(tt.flag, tt.modulePath)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("appName() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("appName() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("appName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRenderOwned runs the form without arguments: inside an application it writes the
// owned files from the code and says which it wrote; outside one it refuses.
func TestRenderOwned(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// dir builds the directory the command runs in.
		dir     func(t *testing.T) string
		want    string
		wantErr string
	}{
		{
			name: "a rendered application missing its workflow gets it written",
			dir: func(t *testing.T) string {
				t.Helper()
				dir := renderSolo(t)
				if err := os.Remove(filepath.Join(dir, ci.File)); err != nil {
					t.Fatal(err)
				}

				return dir
			},
			want: "Rendered the owned files from the code: .github/workflows/ci.yml written.\n",
		},
		{
			name: "a rendered application whose workflow is current is left unchanged",
			dir:  renderSolo,
			want: "Rendered the owned files from the code: .github/workflows/ci.yml unchanged.\n",
		},
		{
			name:    "a directory without go.mod is no application",
			dir:     func(t *testing.T) string { t.Helper(); return t.TempDir() },
			wantErr: "no go.mod at",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := tt.dir(t)
			var b strings.Builder
			err := renderOwned(&b, dir)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("renderOwned() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("renderOwned() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, b.String()); diff != "" {
				t.Errorf("output mismatch (-want +got):\n%s", diff)
			}
			if d, err := ci.Compare(mustDiscover(t, dir)); err != nil || d != nil {
				t.Errorf("Compare() after renderOwned() = %v, %v; want nil, nil", d, err)
			}
		})
	}
}

// renderSolo renders the base skeleton into a temporary directory.
func renderSolo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if _, err := skeleton.Render(&skeleton.Options{Candidate: skeleton.Base, Dir: dir, ModulePath: "example.com/acme/beacon"}); err != nil {
		t.Fatalf("skeleton.Render() error = %v", err)
	}

	return dir
}

func mustDiscover(t *testing.T, dir string) *app.App {
	t.Helper()

	a, err := app.Discover(dir)
	if err != nil {
		t.Fatalf("app.Discover() error = %v", err)
	}

	return a
}

// TestRenderArguments pins the two forms' argument rules: none inside an application, a
// candidate and a directory with --module, and nothing in between.
func TestRenderArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "one argument is neither form", args: []string{"solo"}, wantErr: `"solo" alone is neither`},
		{name: "three arguments are too many", args: []string{"solo", "dir", "extra"}, wantErr: "accepts between 0 and 2 arg(s)"},
		{name: "a candidate without --module", args: []string{"solo", "dir"}, wantErr: "--module is required to render a candidate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := newRender()
			cmd.SetArgs(tt.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Execute(%q) error = %v, want containing %q", tt.args, err, tt.wantErr)
			}
		})
	}
}
