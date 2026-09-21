package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// TestTestRunnerShapes runs the check over browser workspaces written for the case: the
// fully wired project, the wired project with nothing to run yet, the library that is
// nobody's application, and the script forms that count as running ng test.
func TestTestRunnerShapes(t *testing.T) {
	t.Parallel()

	const wired = `{
  "version": 1,
  "projects": {
    "console": {
      "projectType": "application",
      "root": "console",
      "sourceRoot": "console/src",
      "architect": {
        "test": {
          "builder": "@angular/build:unit-test",
          "options": { "tsConfig": "console/tsconfig.spec.json" }
        }
      }
    }
  }
}
`
	const defaultTsConfig = `{
  "version": 1,
  "projects": {
    "console": {
      "projectType": "application",
      "root": "console",
      "sourceRoot": "console/src",
      "architect": { "test": { "builder": "@angular/build:unit-test" } }
    }
  }
}
`
	const library = `{
  "version": 1,
  "projects": {
    "widgets": {
      "projectType": "library",
      "root": "projects/widgets",
      "sourceRoot": "projects/widgets/src"
    }
  }
}
`
	const spec = "describe('x', () => {});\n"

	tests := []struct {
		name  string
		files map[string]string
		want  Result
	}{
		{
			name: "a wired project with a spec passes",
			files: map[string]string{
				"web/angular.json":                          wired,
				"web/package.json":                          `{ "scripts": { "test": "ng test console --watch=false" } }`,
				"web/console/tsconfig.spec.json":            "{}",
				"web/console/src/app/app.component.spec.ts": spec,
			},
			want: Result{Name: testRunner{}.Name(), Status: Pass, Summary: "1 browser project(s) run their specs on @angular/build:unit-test"},
		},
		{
			name: "the default spec tsconfig in the project root and a runner prefix on the script count",
			files: map[string]string{
				"web/angular.json":                          defaultTsConfig,
				"web/package.json":                          `{ "scripts": { "test": "bun ng test console --watch=false && echo done" } }`,
				"web/console/tsconfig.spec.json":            "{}",
				"web/console/src/app/app.component.spec.ts": spec,
			},
			want: Result{Name: testRunner{}.Name(), Status: Pass, Summary: "1 browser project(s) run their specs on @angular/build:unit-test"},
		},
		{
			name: "a wired project with no spec warns",
			files: map[string]string{
				"web/angular.json":               wired,
				"web/package.json":               `{ "scripts": { "test": "ng test console --watch=false" } }`,
				"web/console/tsconfig.spec.json": "{}",
				"web/console/src/main.ts":        "// main\n",
			},
			want: Result{Name: testRunner{}.Name(), Status: Warn, Summary: "1 browser project(s) run their specs on @angular/build:unit-test; 1 without a spec yet", Details: []string{
				"web: project console has no *.spec.ts under web/console/src yet; the runner is wired and nothing runs on it",
			}},
		},
		{
			name: "a spec under node_modules is not the project's",
			files: map[string]string{
				"web/angular.json":                                   wired,
				"web/package.json":                                   `{ "scripts": { "test": "ng test console --watch=false" } }`,
				"web/console/tsconfig.spec.json":                     "{}",
				"web/console/src/node_modules/left/index.spec.ts":    spec,
				"web/console/src/dist/console/app.component.spec.ts": spec,
			},
			want: Result{Name: testRunner{}.Name(), Status: Warn, Summary: "1 browser project(s) run their specs on @angular/build:unit-test; 1 without a spec yet", Details: []string{
				"web: project console has no *.spec.ts under web/console/src yet; the runner is wired and nothing runs on it",
			}},
		},
		{
			name: "a script naming another project does not count",
			files: map[string]string{
				"web/angular.json":                          wired,
				"web/package.json":                          `{ "scripts": { "test": "ng test consoles --watch=false", "test:portal": "ng test portal" } }`,
				"web/console/tsconfig.spec.json":            "{}",
				"web/console/src/app/app.component.spec.ts": spec,
			},
			want: Result{Name: testRunner{}.Name(), Status: Fail, Summary: "1 test wiring problem(s)", Details: []string{
				"web/package.json: no script runs ng test console (bun run test is the single-run form: ng test console --watch=false)",
			}},
		},
		{
			name: "a library is not an application project",
			files: map[string]string{
				"web/angular.json": library,
				"web/package.json": `{ "scripts": { "test": "ng test widgets" } }`,
			},
			want: Result{Name: testRunner{}.Name(), Status: Skip, Summary: "no browser application project"},
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
			write("go.mod", "module example.com/specs\n\ngo 1.26.6\n")
			for rel, content := range tt.files {
				write(rel, content)
			}

			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := testRunner{}.Run(context.Background(), &Env{App: a})
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
