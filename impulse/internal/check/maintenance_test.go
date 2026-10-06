package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/google/go-cmp/cmp"
)

// Mains for the maintenance switch check: parsed, never compiled.
const (
	mainWithSwitch = `package main

import (
	"context"

	"example.com/harbor/pkg/config"
	"github.com/cccteam/ccc/resource/maintenance"
)

func main() {}

func Main() error {
	ctx := context.Background()
	if maintenance.Requested() {
		return maintenance.Serve(ctx)
	}
	conf, err := config.NewSiteConfiguration(ctx)
	if err != nil {
		return err
	}
	_ = conf

	return nil
}
`
	mainWithoutSwitch = `package main

import (
	"context"

	"example.com/harbor/pkg/config"
)

func main() {}

func Main() error {
	conf, err := config.NewSiteConfiguration(context.Background())
	if err != nil {
		return err
	}
	_ = conf

	return nil
}
`
	mainWithLateSwitch = `package main

import (
	"context"

	"example.com/harbor/pkg/config"
	"github.com/cccteam/ccc/resource/maintenance"
)

func main() {}

func Main() error {
	ctx := context.Background()
	conf, err := config.NewSiteConfiguration(ctx)
	if err != nil {
		return err
	}
	_ = conf
	if maintenance.Requested() {
		return maintenance.Serve(ctx)
	}

	return nil
}
`
	mainWithoutConfiguration = `package main

func main() {}
`
	mainAliased = `package main

import (
	"context"

	down "github.com/cccteam/ccc/resource/maintenance"
	"example.com/harbor/pkg/config"
)

func main() {}

func Main() error {
	ctx := context.Background()
	if down.Requested() {
		return down.Serve(ctx)
	}
	_, err := config.NewSiteConfiguration(ctx)

	return err
}
`
)

func TestMaintenanceSwitch(t *testing.T) {
	t.Parallel()

	flat := program("pkg/resources", `generation.GenerateHandlers("app"),`, `generation.GenerateRoutes("pkg/router", "api"),`)
	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name:        "a flat application's main checks the switch first",
			files:       map[string]string{"cmd/generate/main.go": flat, "main.go": mainWithSwitch},
			wantStatus:  Pass,
			wantSummary: "1 main(s) check maintenance.Requested() before building the site configuration",
		},
		{
			name:        "an aliased import counts",
			files:       map[string]string{"cmd/generate/main.go": flat, "main.go": mainAliased},
			wantStatus:  Pass,
			wantSummary: "1 main(s) check maintenance.Requested() before building the site configuration",
		},
		{
			name:        "a main without the switch is refused",
			files:       map[string]string{"cmd/generate/main.go": flat, "main.go": mainWithoutSwitch},
			wantStatus:  Fail,
			wantSummary: "1 main(s) without the maintenance switch before the site configuration",
			wantDetails: []string{"harbor/main.go: the main builds the site configuration without checking maintenance.Requested() first; add, before config.NewSiteConfiguration(ctx): if maintenance.Requested() { return maintenance.Serve(ctx) } (import github.com/cccteam/ccc/resource/maintenance)"},
		},
		{
			name:        "a switch after the configuration is refused",
			files:       map[string]string{"cmd/generate/main.go": flat, "main.go": mainWithLateSwitch},
			wantStatus:  Fail,
			wantSummary: "1 main(s) without the maintenance switch before the site configuration",
			wantDetails: []string{"harbor/main.go: maintenance.Requested() is checked after config.NewSiteConfiguration() builds the configuration, so a maintenance revision would open the database; move the check before it"},
		},
		{
			name:        "a main that builds no site configuration is a warning",
			files:       map[string]string{"cmd/generate/main.go": flat, "main.go": mainWithoutConfiguration},
			wantStatus:  Warn,
			wantSummary: "1 main(s) check maintenance.Requested() before building the site configuration; 1 could not be read for it",
			wantDetails: []string{"harbor/main.go: no call to config.NewSiteConfiguration found in the main package; the maintenance switch goes before the site configuration is built, wherever that is"},
		},
		{
			name: "two sites, one without the switch",
			files: map[string]string{
				"cmd/generate/console/main.go": siteProgram("console"),
				"cmd/generate/portal/main.go":  siteProgram("portal"),
				"apps/console/main.go":         mainWithSwitch,
				"apps/portal/main.go":          mainWithoutSwitch,
			},
			wantStatus:  Fail,
			wantSummary: "1 main(s) without the maintenance switch before the site configuration",
			wantDetails: []string{"portal/main.go: the main builds the site configuration without checking maintenance.Requested() first; add, before config.NewSiteConfiguration(ctx): if maintenance.Requested() { return maintenance.Serve(ctx) } (import github.com/cccteam/ccc/resource/maintenance)"},
		},
		{
			name:        "a site with no main package is left to the sites check",
			files:       map[string]string{"cmd/generate/console/main.go": siteProgram("console"), "cmd/generate/portal/main.go": siteProgram("portal"), "apps/console/main.go": mainWithSwitch},
			wantStatus:  Pass,
			wantSummary: "1 main(s) check maintenance.Requested() before building the site configuration",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), "harbor")
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
			got := maintenanceSwitch{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: maintenanceSwitchName, Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
