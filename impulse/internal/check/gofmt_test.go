package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

func TestGofmt(t *testing.T) {
	t.Parallel()

	// gofmt aligns a struct's field types in a column; the unformatted form has them unaligned.
	const formatted = "package x\n\ntype S struct {\n\tA    int\n\tLong string\n}\n"
	const unformatted = "package x\n\ntype S struct {\n\tA int\n\tLong string\n}\n"
	tests := []struct {
		name string
		// files are the tree, root-relative; fix runs the check with --fix.
		files       map[string]string
		fix         bool
		wantStatus  Status
		wantSummary string
		wantDetails []string
		// wantAfter is a file's content after the run, for the fix.
		wantAfter map[string]string
	}{
		{
			name:        "formatted files pass, test files counted",
			files:       map[string]string{"main.go": formatted, "pkg/x/x_test.go": formatted},
			wantStatus:  Pass,
			wantSummary: "2 Go file(s) gofmt-formatted",
		},
		{
			name:        "an unformatted test file fails, named",
			files:       map[string]string{"main.go": formatted, "test/integration/harness_test.go": unformatted},
			wantStatus:  Fail,
			wantSummary: "1 Go file(s) not gofmt-formatted (--fix formats them)",
			wantDetails: []string{"test/integration/harness_test.go: not gofmt-formatted"},
		},
		{
			name:        "generated files, vendor, testdata, node_modules and underscore directories are left out",
			files:       map[string]string{"app/zz_gen_x.go": unformatted, "vendor/v/v.go": unformatted, "pkg/testdata/t.go": unformatted, "web/node_modules/m/m.go": unformatted, "_example/e.go": unformatted, "main.go": formatted},
			wantStatus:  Pass,
			wantSummary: "1 Go file(s) gofmt-formatted",
		},
		{
			name:        "a file that does not parse is left to the compiler",
			files:       map[string]string{"main.go": "package x\n\ntype\n"},
			wantStatus:  Pass,
			wantSummary: "1 Go file(s) gofmt-formatted",
		},
		{
			name:        "--fix formats the files and says which",
			files:       map[string]string{"main.go": unformatted, "pkg/x/x_test.go": unformatted, "ok.go": formatted},
			fix:         true,
			wantStatus:  Pass,
			wantSummary: "2 Go file(s) formatted; 3 checked",
			wantDetails: []string{"main.go", "pkg/x/x_test.go"},
			wantAfter:   map[string]string{"main.go": formatted, "pkg/x/x_test.go": formatted},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			for rel, content := range tt.files {
				abs := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got := gofmtCheck{}.Run(context.Background(), &Env{App: &app.App{Root: root}, Fix: tt.fix})
			if got.Status != tt.wantStatus {
				t.Fatalf("Status = %s, want %s (%s: %v)", got.Status, tt.wantStatus, got.Summary, got.Details)
			}
			if got.Summary != tt.wantSummary {
				t.Errorf("Summary = %q, want %q", got.Summary, tt.wantSummary)
			}
			if diff := cmp.Diff(tt.wantDetails, got.Details); diff != "" {
				t.Errorf("Details mismatch (-want +got):\n%s", diff)
			}
			for rel, want := range tt.wantAfter {
				data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != want {
					t.Errorf("%s after --fix = %q, want %q", rel, data, want)
				}
			}
		})
	}
}
