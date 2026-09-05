package skeleton

import (
	"bytes"
	"context"
	"go/format"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

const targetModule = "example.com/acme/beacon"

// write puts content at a path under root, creating directories as needed.
func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		candidate string
		// script is a shell script that must come out executable.
		script string
		// static lists the checks that must not fail on the rendered tree. pins is left
		// out: the templates pin pseudo-versions on purpose while the framework is
		// mid-branch, and that check warns rather than fails anyway.
		static []string
	}{
		{name: "solo", candidate: "solo", script: "web/ccclib.sh"},
		{name: "tenanted", candidate: "tenanted", script: "web/ccclib.sh"},
		{name: "outlets", candidate: "outlets", script: "web/ccclib.sh"},
		{name: "sites", candidate: "sites", script: "apps/console/web/ccclib.sh"},
	}
	static := []string{"generator-program", "options", "emulator-version", "prettier-ignore", "eslint-ignore", "package-manager", "multi-site", "env-template"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "beacon")
			got, err := Render(Options{Candidate: tt.candidate, Dir: dir, ModulePath: targetModule})
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if got.Files == 0 || got.Workspace != "" {
				t.Errorf("Render() = %+v, want files written and no workspace", got)
			}

			// go.mod is back under the target module path, and the template name is gone.
			goMod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(goMod), "module "+targetModule+"\n") {
				t.Errorf("go.mod starts %q, want module %s", firstLine(goMod), targetModule)
			}
			placeholder := modulePrefix + tt.candidate
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return errors.Wrap(err, "fs.WalkDir()")
				}
				if d.IsDir() {
					return nil
				}
				if d.Name() == ModFile {
					t.Errorf("%s: %s left in the rendered tree", tt.name, p)
				}
				data, err := fs.ReadFile(root.FS(), p)
				if err != nil {
					return errors.Wrap(err, "fs.ReadFile()")
				}
				if bytes.Contains(data, []byte(placeholder)) {
					t.Errorf("%s: placeholder module path survives in %s", tt.name, p)
				}
				if path.Ext(p) == ".go" {
					formatted, err := format.Source(data)
					if err != nil {
						return errors.Wrap(err, "format.Source()")
					}
					if !bytes.Equal(formatted, data) {
						t.Errorf("%s: %s is not gofmt-clean after the rewrite", tt.name, p)
					}
				}

				return nil
			})
			if err != nil {
				t.Fatal(err)
			}

			info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(tt.script)))
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode()&0o100 == 0 {
				t.Errorf("%s: %s is not executable (%v)", tt.name, tt.script, info.Mode())
			}

			// The rendered tree is a complete application the static checks accept.
			a, err := app.Discover(dir)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			checks, err := check.Select(static)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range check.Run(context.Background(), &check.Env{App: a}, checks) {
				if r.Status == check.Fail {
					t.Errorf("%s: check %s failed: %s\n%s", tt.name, r.Name, r.Summary, strings.Join(r.Details, "\n"))
				}
			}
		})
	}
}

func TestRenderRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    func(t *testing.T) Options
		wantErr string
	}{
		{
			name: "unknown candidate",
			opts: func(t *testing.T) Options {
				t.Helper()
				return Options{Candidate: "ghost", Dir: t.TempDir(), ModulePath: targetModule}
			},
			wantErr: `unknown candidate "ghost"`,
		},
		{
			name: "invalid module path",
			opts: func(t *testing.T) Options {
				t.Helper()
				return Options{Candidate: "solo", Dir: t.TempDir(), ModulePath: "not a module"}
			},
			wantErr: "module path",
		},
		{
			name: "non-empty directory",
			opts: func(t *testing.T) Options {
				t.Helper()
				dir := t.TempDir()
				write(t, dir, "README.md", "an application already lives here\n")

				return Options{Candidate: "solo", Dir: dir, ModulePath: targetModule}
			},
			wantErr: "is not empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Render(tt.opts(t))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Render() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRenderDevWorkspace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// checkouts are the framework repositories present under the dev root.
		checkouts []string
		// inside renders the application under the dev root, where use paths are relative.
		inside bool
		// wantUses are the go.work use lines; DEV stands for the absolute dev root.
		wantUses    []string
		wantUsed    []string
		wantMissing []string
	}{
		{
			name:      "beside the dev root: absolute paths to the checkouts",
			checkouts: []string{"ccc/resource", "session", "spxscan"},
			wantUses:  []string{".", "DEV/ccc/resource", "DEV/session"},
			wantUsed:  []string{"github.com/cccteam/ccc/resource", "github.com/cccteam/session"},
			wantMissing: []string{
				"github.com/cccteam/access", "github.com/cccteam/ccc/accesstypes", "github.com/cccteam/db-initiator",
				"github.com/cccteam/httpio", "github.com/cccteam/logger",
			},
		},
		{
			name:      "inside the dev root: relative paths",
			checkouts: []string{"ccc/resource", "session"},
			inside:    true,
			wantUses:  []string{".", "../ccc/resource", "../session"},
			wantUsed:  []string{"github.com/cccteam/ccc/resource", "github.com/cccteam/session"},
			wantMissing: []string{
				"github.com/cccteam/access", "github.com/cccteam/ccc/accesstypes", "github.com/cccteam/db-initiator",
				"github.com/cccteam/httpio", "github.com/cccteam/logger",
			},
		},
		{
			name:     "nothing checked out: the pins stay in force",
			wantUses: []string{"."},
			wantMissing: []string{
				"github.com/cccteam/access", "github.com/cccteam/ccc/accesstypes", "github.com/cccteam/ccc/resource",
				"github.com/cccteam/db-initiator", "github.com/cccteam/httpio", "github.com/cccteam/logger", "github.com/cccteam/session",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			base := t.TempDir()
			devRoot := filepath.Join(base, "dev")
			if err := os.Mkdir(devRoot, 0o750); err != nil {
				t.Fatal(err)
			}
			for _, c := range tt.checkouts {
				write(t, devRoot, filepath.Join(c, "go.mod"), "module github.com/cccteam/"+c+"\n\ngo 1.26\n")
			}
			dir := filepath.Join(base, "beacon")
			if tt.inside {
				dir = filepath.Join(devRoot, "beacon")
			}
			wantUses := make([]string, len(tt.wantUses))
			for i, u := range tt.wantUses {
				wantUses[i] = strings.Replace(u, "DEV", devRoot, 1)
			}

			got, err := Render(Options{Candidate: "solo", Dir: dir, ModulePath: targetModule, DevRoot: devRoot})
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if got.Workspace != filepath.Join(dir, "go.work") {
				t.Errorf("Workspace = %q, want go.work in the rendered directory", got.Workspace)
			}
			if diff := cmp.Diff(tt.wantUsed, got.DevUsed); diff != "" {
				t.Errorf("DevUsed mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantMissing, got.DevMissing); diff != "" {
				t.Errorf("DevMissing mismatch (-want +got):\n%s", diff)
			}

			work, err := os.ReadFile(got.Workspace)
			if err != nil {
				t.Fatal(err)
			}
			var uses []string
			for line := range strings.Lines(string(work)) {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, ".") || (strings.HasPrefix(line, "/") && !strings.HasPrefix(line, "//")) {
					uses = append(uses, line)
				}
			}
			if diff := cmp.Diff(wantUses, uses); diff != "" {
				t.Errorf("go.work use lines mismatch (-want +got):\n%s", diff)
			}
			if !strings.Contains(string(work), "go 1.26.6\n") {
				t.Errorf("go.work carries no go directive from go.mod:\n%s", work)
			}
		})
	}
}

func firstLine(b []byte) string {
	line, _, _ := strings.Cut(string(b), "\n")

	return line
}
