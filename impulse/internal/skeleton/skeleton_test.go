package skeleton

import (
	"io/fs"
	"path"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/mod/modfile"
)

const modulePrefix = "github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/"

func TestCandidates(t *testing.T) {
	t.Parallel()

	got, err := Candidates()
	if err != nil {
		t.Fatalf("Candidates() error = %v", err)
	}

	want := []Candidate{
		{Name: "outlets", ModulePath: modulePrefix + "outlets"},
		{Name: "sites", ModulePath: modulePrefix + "sites"},
		{Name: "solo", ModulePath: modulePrefix + "solo"},
		{Name: "tenanted", ModulePath: modulePrefix + "tenanted"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Candidates() mismatch (-want +got):\n%s", diff)
	}
}

func TestFS(t *testing.T) {
	t.Parallel()

	// A template is a complete application minus its build products: these files must
	// be present in every candidate, and nothing from these directories may be embedded.
	required := []string{ModFile, "go.sum", ".gitignore", ".golangci.yml", ".envrc.template", "Procfile", "README.md", "pkg/auth/staff/roles.json", "pkg/auth/staff/staff.go", ".github/workflows/ci.yml", "schema/firestore/firestore.rules", "schema/firestore/firestore.indexes.json"}
	forbiddenDirs := []string{"node_modules", ".angular", "dist", ".ccc-cache", ".yalc", ".git"}
	// codeql-config.yml left with the CodeQL job: nothing reads it.
	forbiddenFiles := []string{"go.mod", "go.work", "go.work.sum", ".envrc", ".overmind.sock", "yalc.lock", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "codeql-config.yml"}

	tests := []struct {
		name    string
		wantErr bool
		// entry is the file that proves the layout: main.go at the root for a
		// flat template, the apps directory for the sites layout. lockfile is the bun
		// lockfile of the (first) browser workspace; bunfig.toml sits beside it.
		entry, lockfile string
	}{
		{name: "solo", entry: "main.go", lockfile: "web/bun.lock"},
		{name: "tenanted", entry: "main.go", lockfile: "web/bun.lock"},
		{name: "outlets", entry: "main.go", lockfile: "web/bun.lock"},
		{name: "sites", entry: "apps/console/main.go", lockfile: "apps/console/web/bun.lock"},
		{name: "unknown", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sub, err := FS(tt.name)
			if (err != nil) != tt.wantErr {
				t.Fatalf("FS() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			for _, rel := range append(required, tt.entry, tt.lockfile, path.Join(path.Dir(tt.lockfile), "bunfig.toml")) {
				if _, err := fs.Stat(sub, rel); err != nil {
					t.Errorf("%s: missing %s: %v", tt.name, rel, err)
				}
			}

			err = fs.WalkDir(sub, ".", func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				for _, dir := range forbiddenDirs {
					if d.IsDir() && d.Name() == dir {
						t.Errorf("%s: build product embedded at %s", tt.name, p)

						return fs.SkipDir
					}
				}
				for _, file := range forbiddenFiles {
					if !d.IsDir() && d.Name() == file {
						t.Errorf("%s: %s embedded at %s", tt.name, file, p)
					}
				}
				if !d.IsDir() && path.Ext(p) == ".go" {
					if err := checkImports(sub, p, tt.name); err != nil {
						t.Error(err)
					}
				}

				return nil
			})
			if err != nil {
				t.Fatalf("fs.WalkDir() error = %v", err)
			}
		})
	}
}

// TestModFileCarriesTheImpulseTool pins the impulse pin's home: every candidate's go.mod
// carries the tool directive for impulse and a require of it, so a rendered application
// runs go tool impulse check at a version go.mod names and Go's checksum database verifies.
func TestModFileCarriesTheImpulseTool(t *testing.T) {
	t.Parallel()

	const impulseModule = "github.com/cccteam/ccc/impulse"
	tests := []struct {
		name string
	}{
		{name: "solo"}, {name: "tenanted"}, {name: "outlets"}, {name: "sites"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sub, err := FS(tt.name)
			if err != nil {
				t.Fatalf("FS() error = %v", err)
			}
			data, err := fs.ReadFile(sub, ModFile)
			if err != nil {
				t.Fatalf("fs.ReadFile(%s) error = %v", ModFile, err)
			}
			mod, err := modfile.Parse(ModFile, data, nil)
			if err != nil {
				t.Fatalf("modfile.Parse() error = %v", err)
			}
			held := false
			for _, tool := range mod.Tool {
				if tool.Path == impulseModule {
					held = true
				}
			}
			if !held {
				t.Errorf("%s: no tool directive for %s", tt.name, impulseModule)
			}
			pinned := ""
			for _, r := range mod.Require {
				if r.Mod.Path == impulseModule {
					pinned = r.Mod.Version
				}
			}
			if pinned == "" {
				t.Errorf("%s: no require of %s", tt.name, impulseModule)
			}
		})
	}
}

// checkImports fails when a Go file still imports the template by any path other than
// its own placeholder module path, which rendering would then fail to rewrite.
func checkImports(sub fs.FS, p, name string) error {
	src, err := fs.ReadFile(sub, p)
	if err != nil {
		return errors.Wrap(err, "fs.ReadFile()")
	}
	for _, line := range strings.Split(string(src), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "cccteam/impulse") && !strings.Contains(line, "cccteam/ccc/impulse") {
			continue
		}
		if !strings.Contains(line, modulePrefix+name) {
			return &importError{candidate: name, file: p, line: line}
		}
	}

	return nil
}

type importError struct {
	candidate, file, line string
}

func (e *importError) Error() string {
	return e.candidate + ": " + e.file + " imports the template by a stale path: " + e.line
}
