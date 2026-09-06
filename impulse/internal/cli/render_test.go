package cli

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

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
				candidate: "solo", dir: "../beacon", modulePath: "example.com/acme/beacon",
				rendered: &skeleton.Rendered{Files: 90}, port: "8090", emulator: "9014",
				goProcs: []string{"spanner", "server"},
				web:     []webWorkspace{{Dir: "web", Projects: []webProject{{Name: "console", Port: 4300}}}},
			},
			want: `Rendered solo into ../beacon as example.com/acme/beacon (90 files).

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
				candidate: "sites", dir: "/w/harbor", modulePath: "github.com/cccteam/harbor", devRoot: "/w",
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
				candidate: "solo", dir: "../beacon", modulePath: "example.com/acme/beacon",
				rendered: &skeleton.Rendered{Files: 90}, port: "8090", emulator: "9014",
				goProcs: []string{"spanner", "server"},
				web:     []webWorkspace{{Dir: "web", Projects: []webProject{{Name: "console", Port: 4300}}}},
			},
			want: `Created example.com/acme/beacon at ../beacon with the members auth (90 files).
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
  6. impulse add tenancy | impulse add outlet <name> --prefix <p> --sessions | impulse add auth <name>
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
