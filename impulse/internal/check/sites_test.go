package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// siteProgram renders one site's generator program under apps/<site>.
func siteProgram(site string) string {
	return program("apps/"+site+"/pkg/resources",
		`generation.GenerateHandlers("apps/`+site+`/app"),`,
		`generation.GenerateRoutes("apps/`+site+`/pkg/router", "api"),`,
	)
}

// unionConfig renders a data level handing the permission engine a collection over the
// routers it imports.
func unionConfig(routers ...string) string {
	return `package config

import (
	"github.com/cccteam/access"
` + routerImports(routers) + `)

func engine(store access.Store, collection access.PermissionCollection, roles access.RoleFile) (*access.Client, error) {
	return access.New(store, access.WithDefaultRoles(collection, roles))
}
`
}

// authCallers renders a data level constructing the staff auth, which hands the engine
// its role file itself, over the routers the data level imports.
func authCallers(routers ...string) string {
	return `package config

import (
	"context"

	"example.com/harbor/pkg/auth/staff"
` + routerImports(routers) + `)

func New(ctx context.Context) error {
	_, err := staff.New(ctx, nil, "", nil)

	return err
}
`
}

func routerImports(routers []string) string {
	imports := ""
	for _, r := range routers {
		imports += "\t_ \"example.com/harbor/apps/" + r + "/pkg/router\"\n"
	}

	return imports
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
				"pkg/config/data.go":           unionConfig("console", "portal"),
			},
			wantStatus:  Pass,
			wantSummary: "2 site(s) wired: console (:8094), portal (:8095); every site's router is in a collection handed to the permission engine",
		},
		{
			name: "the collection chosen by the auth package's callers",
			files: map[string]string{
				"cmd/generate/console/main.go": siteProgram("console"),
				"cmd/generate/portal/main.go":  siteProgram("portal"),
				"apps/console/main.go":         mainPackage,
				"apps/portal/main.go":          mainPackage,
				"apps/console/pkg/router/r.go": "package router\n",
				"apps/portal/pkg/router/r.go":  "package router\n",
				"Procfile":                     twoPorts,
				"pkg/auth/staff/staff.go":      staffAuth,
				"pkg/config/data.go":           authCallers("console", "portal"),
			},
			wantStatus:  Pass,
			wantSummary: "2 site(s) wired: console (:8094), portal (:8095); every site's router is in a collection handed to the permission engine",
		},
		{
			name: "the auth package's callers miss a router",
			files: map[string]string{
				"cmd/generate/console/main.go": siteProgram("console"),
				"cmd/generate/portal/main.go":  siteProgram("portal"),
				"apps/console/main.go":         mainPackage,
				"apps/portal/main.go":          mainPackage,
				"apps/console/pkg/router/r.go": "package router\n",
				"apps/portal/pkg/router/r.go":  "package router\n",
				"Procfile":                     twoPorts,
				"pkg/auth/staff/staff.go":      staffAuth,
				"pkg/config/data.go":           authCallers("console"),
			},
			wantStatus:  Fail,
			wantSummary: "1 site wiring problem(s)",
			wantDetails: []string{
				"no permission collection covers site portal: example.com/harbor/apps/portal/pkg/router is not imported by any package passing a collection to access.WithDefaultRoles (pkg/config)",
			},
		},
		{
			name: "unserved site, shared port, and a collection missing a router",
			files: map[string]string{
				"cmd/generate/console/main.go": siteProgram("console"),
				"cmd/generate/portal/main.go":  siteProgram("portal"),
				"apps/console/main.go":         mainPackage,
				"apps/console/pkg/router/r.go": "package router\n",
				"apps/portal/pkg/router/r.go":  "package router\n",
				"Procfile":                     onePort,
				"pkg/config/data.go":           unionConfig("console"),
			},
			wantStatus:  Fail,
			wantSummary: "3 site wiring problem(s)",
			wantDetails: []string{
				"site portal has no main package in apps/portal; nothing serves it",
				"sites console and portal all listen on PORT=8094 in Procfile",
				"no permission collection covers site portal: example.com/harbor/apps/portal/pkg/router is not imported by any package passing a collection to access.WithDefaultRoles (pkg/config)",
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
