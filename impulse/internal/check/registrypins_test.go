package check

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/impulse/app"
)

func TestRegistryPins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		packageJSON string
		lock        string
		noWebApps   bool
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name:        "registry pins pass",
			packageJSON: `{"dependencies": {"@cccteam/resource": "0.0.2", "@cccteam/resource-angular": "0.0.2"}, "overrides": {"@cccteam/resource": "0.0.2"}}`,
			lock:        `{"lockfileVersion": 1, "packages": {"@cccteam/resource": ["@cccteam/resource@0.0.2", "", {}, "sha512-x"]}}`,
			wantStatus:  Pass,
			wantSummary: "1 browser app(s) install from the registry",
		},
		{
			name:        "a yalc attachment in the dependencies and the overrides is refused",
			packageJSON: `{"dependencies": {"@cccteam/resource-angular": "file:.yalc/@cccteam/resource-angular", "@cccteam/resource": "file:.yalc/@cccteam/resource"}, "overrides": {"@cccteam/resource": "file:.yalc/@cccteam/resource"}}`,
			lock:        `{"lockfileVersion": 1, "packages": {"@cccteam/resource": ["@cccteam/resource@file:.yalc/@cccteam/resource", {}]}}`,
			wantStatus:  Fail,
			wantSummary: "4 yalc attachment(s) committed; a clean checkout cannot install them (ccclib.sh restore puts the registry pins back)",
			wantDetails: []string{
				"web/package.json dependencies: @cccteam/resource is file:.yalc/@cccteam/resource",
				"web/package.json dependencies: @cccteam/resource-angular is file:.yalc/@cccteam/resource-angular",
				"web/package.json overrides: @cccteam/resource is file:.yalc/@cccteam/resource",
				"web/bun.lock: records a yalc attachment; run ccclib.sh restore and commit the lockfile it leaves",
			},
		},
		{
			name:        "no browser apps",
			noWebApps:   true,
			wantStatus:  Skip,
			wantSummary: "no browser apps",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			a := &app.App{Root: dir}
			if !tt.noWebApps {
				a.WebApps = []app.WebApp{{Dir: "web"}}
				if err := os.MkdirAll(filepath.Join(dir, "web"), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "web", "package.json"), []byte(tt.packageJSON), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "web", "bun.lock"), []byte(tt.lock), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got := registryPins{}.Run(context.Background(), &Env{App: a})
			if got.Status != tt.wantStatus || got.Summary != tt.wantSummary {
				t.Fatalf("Run() = %s %q, want %s %q (details %v)", got.Status, got.Summary, tt.wantStatus, tt.wantSummary, got.Details)
			}
			if strings.Join(got.Details, "\n") != strings.Join(tt.wantDetails, "\n") {
				t.Errorf("Details = %v, want %v", got.Details, tt.wantDetails)
			}
		})
	}
}
