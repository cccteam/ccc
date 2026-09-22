package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// siteProgram renders one site's generator program under apps/<site>.
func siteProgram(site string) string {
	return program("apps/"+site+"/pkg/resources",
		`generation.GenerateHandlers("apps/`+site+`/app"),`,
		`generation.GenerateRoutes("apps/`+site+`/pkg/router", "api"),`,
	)
}

// unionDeploy renders a deployment package migrating roles against the routers it imports.
func unionDeploy(routers ...string) string {
	imports := ""
	for _, r := range routers {
		imports += "\t_ \"example.com/harbor/apps/" + r + "/pkg/router\"\n"
	}

	return `package deploy

import (
	"context"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
` + imports + `)

func MigrateRoles(ctx context.Context, manager access.UserManager, roles *access.RoleConfig, domains ...accesstypes.Domain) error {
	return access.MigrateRoles(ctx, manager, nil, roles, domains...)
}
`
}

const (
	mainPackage = "package main\n\nfunc main() {}\n"
	twoPorts    = `spanner: podman run --rm gcr.io/cloud-spanner-emulator/emulator:1.5.56
console: bash -c 'go run ./cmd/bootstrap && PORT=8094 APP_DIST=apps/console/web/dist go run ./apps/console'
portal: bash -c 'PORT=8095 APP_DIST=apps/portal/web/dist go run ./apps/portal'
`
	onePort = `console: bash -c 'PORT=8094 go run --tags=dev ./apps/console/'
portal: bash -c 'PORT=8094 go run ./apps/portal/main.go'
`
)

func TestSitesWired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "two sites wired",
			files: map[string]string{
				"cmd/generate/console/main.go": siteProgram("console"),
				"cmd/generate/portal/main.go":  siteProgram("portal"),
				"apps/console/main.go":         mainPackage,
				"apps/portal/main.go":          mainPackage,
				"apps/console/pkg/router/r.go": "package router\n",
				"apps/portal/pkg/router/r.go":  "package router\n",
				"Procfile":                     twoPorts,
				"pkg/deploy/deploy.go":         unionDeploy("console", "portal"),
			},
			wantStatus:  Pass,
			wantSummary: "2 site(s) wired: console (:8094), portal (:8095); every site's router is covered by a role migration",
		},
		{
			name: "unserved site, shared port, and a role migration missing a router",
			files: map[string]string{
				"cmd/generate/console/main.go": siteProgram("console"),
				"cmd/generate/portal/main.go":  siteProgram("portal"),
				"apps/console/main.go":         mainPackage,
				"apps/console/pkg/router/r.go": "package router\n",
				"apps/portal/pkg/router/r.go":  "package router\n",
				"Procfile":                     onePort,
				"pkg/deploy/deploy.go":         unionDeploy("console"),
			},
			wantStatus:  Fail,
			wantSummary: "3 site wiring problem(s)",
			wantDetails: []string{
				"site portal has no main package in apps/portal; nothing serves it",
				"sites console and portal all listen on PORT=8094 in Procfile",
				"no role migration covers site portal: example.com/harbor/apps/portal/pkg/router is not imported by any package calling access.MigrateRoles (pkg/deploy)",
			},
		},
		{
			name: "no process files",
			files: map[string]string{
				"cmd/generate/console/main.go": siteProgram("console"),
				"cmd/generate/portal/main.go":  siteProgram("portal"),
				"apps/console/main.go":         mainPackage,
				"apps/portal/main.go":          mainPackage,
			},
			wantStatus:  Fail,
			wantSummary: "2 site wiring problem(s)",
			wantDetails: []string{
				"no process file runs site console (expected a process running go run ./apps/console)",
				"no process file runs site portal (expected a process running go run ./apps/portal)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			write := func(rel, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write("go.mod", "module example.com/harbor\n\ngo 1.26.6\n")
			for rel, content := range tt.files {
				write(rel, content)
			}

			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := sitesWired{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: sitesWired{}.Name(), Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
