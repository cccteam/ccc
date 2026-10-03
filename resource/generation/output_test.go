package generation

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_writeFileAtomic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// existing is the content already at the path, "" for no file.
		existing string
		// subdir places the file in a directory that does not exist yet.
		subdir string
		data   string
	}{
		{
			name: "a new file in an existing directory",
			data: "package a\n",
		},
		{
			name:     "an existing file is replaced whole",
			existing: "package a\n\nfunc Old() {}\n",
			data:     "package a\n",
		},
		{
			name:   "a missing directory is created",
			subdir: "deeper/still",
			data:   "export const a = 1;\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			dir := filepath.Join(root, tt.subdir)
			path := filepath.Join(dir, genPrefix+"_target.go")
			if tt.existing != "" {
				if err := os.WriteFile(path, []byte(tt.existing), 0o600); err != nil {
					t.Fatalf("os.WriteFile() error = %v", err)
				}
			}

			if err := writeFileAtomic(path, []byte(tt.data)); err != nil {
				t.Fatalf("writeFileAtomic() error = %v", err)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("os.ReadFile() error = %v", err)
			}
			if string(got) != tt.data {
				t.Errorf("writeFileAtomic() content = %q, want %q", got, tt.data)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("os.Stat() error = %v", err)
			}
			if info.Mode().Perm() != generatedFileMode {
				t.Errorf("writeFileAtomic() mode = %o, want %o", info.Mode().Perm(), generatedFileMode)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("os.ReadDir() error = %v", err)
			}
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			if want := []string{filepath.Base(path)}; !slices.Equal(names, want) {
				t.Errorf("writeFileAtomic() left %v in the directory, want %v (no temporary file)", names, want)
			}
		})
	}
}

// Test_generatedOutput_removeStaleOutput proves the run-level contract: what the run
// wrote survives, what an earlier run left goes, hand-written files are never touched,
// and the sweep forgets the record so a second sweep removes nothing.
func Test_generatedOutput_removeStaleOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// targets registers each directory (relative to the temp root) by method.
		targets map[string]generatedFileDeleteMethod
		// stale is the previous run's output, by directory: name → content.
		stale map[string]map[string]string
		// writes is this run's output through writeGeneratedFile, by directory.
		writes map[string]map[string]string
		// wantKept is what remains, by directory, sorted.
		wantKept map[string][]string
	}{
		{
			name:    "a Go package: written files stay, stale generated files go, hand-written stay",
			targets: map[string]generatedFileDeleteMethod{"app": prefix},
			stale: map[string]map[string]string{"app": {
				genPrefix + "_ships.go":   "old",
				genPrefix + "_hangars.go": "old",
				"app.go":                  "hand-written",
			}},
			writes:   map[string]map[string]string{"app": {genPrefix + "_ships.go": "rewritten"}},
			wantKept: map[string][]string{"app": {"app.go", genPrefix + "_ships.go"}},
		},
		{
			name:    "a router package: the release file goes with the router it was written beside, a hand-written JSON file stays",
			targets: map[string]generatedFileDeleteMethod{"router": prefix},
			stale: map[string]map[string]string{"router": {
				genPrefix + "_router.go":    "old",
				genPrefix + "_release.json": "{}",
				genPrefix + "_routes.go":    "old",
				"fixture.json":              "hand-written",
			}},
			writes:   map[string]map[string]string{"router": {genPrefix + "_routes.go": "rewritten"}},
			wantKept: map[string][]string{"router": {"fixture.json", genPrefix + "_routes.go"}},
		},
		{
			name:    "a TypeScript target by header, and a types package by method file names",
			targets: map[string]generatedFileDeleteMethod{"web": headerComment, "types": methodFiles},
			stale: map[string]map[string]string{
				"web":   {genPrefix + "_api.ts": generationHeader + "\nold", genPrefix + "_gone.ts": generationHeader + "\nold", "index.ts": "hand-written", "package.json": "{}"},
				"types": {generatedGoFileName(jsonOutputName): "old", generatedGoFileName(storageOutputName): "old", genPrefix + "_notmine.go": "someone else's", "payload.go": "hand-written"},
			},
			writes: map[string]map[string]string{
				"web":   {genPrefix + "_api.ts": generationHeader + "\nrewritten"},
				"types": {generatedGoFileName(jsonOutputName): "rewritten"},
			},
			wantKept: map[string][]string{
				"web":   {"index.ts", "package.json", genPrefix + "_api.ts"},
				"types": {"payload.go", genPrefix + "_notmine.go", generatedGoFileName(jsonOutputName)},
			},
		},
		{
			name:     "a registered directory that never existed is left missing",
			targets:  map[string]generatedFileDeleteMethod{"absent": prefix},
			wantKept: map[string][]string{"absent": nil},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			for dir, files := range tt.stale {
				if err := os.MkdirAll(filepath.Join(root, dir), 0o750); err != nil {
					t.Fatalf("os.MkdirAll() error = %v", err)
				}
				for name, content := range files {
					if err := os.WriteFile(filepath.Join(root, dir, name), []byte(content), 0o600); err != nil {
						t.Fatalf("os.WriteFile() error = %v", err)
					}
				}
			}

			var output generatedOutput
			for dir, method := range tt.targets {
				output.registerOutput(filepath.Join(root, dir), method)
				output.registerOutput(filepath.Join(root, dir), method) // twice is harmless
			}
			for dir, files := range tt.writes {
				for name, content := range files {
					if err := output.writeGeneratedFile(filepath.Join(root, dir, name), []byte(content)); err != nil {
						t.Fatalf("writeGeneratedFile() error = %v", err)
					}
				}
			}

			if err := output.removeStaleOutput(); err != nil {
				t.Fatalf("removeStaleOutput() error = %v", err)
			}

			for dir, want := range tt.wantKept {
				got := listDir(t, filepath.Join(root, dir))
				slices.Sort(want)
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("removeStaleOutput() kept files in %s mismatch (-want +got):\n%s", dir, diff)
				}
				for _, name := range got {
					if content, ok := tt.writes[dir][name]; ok {
						raw, err := os.ReadFile(filepath.Join(root, dir, name))
						if err != nil {
							t.Fatalf("os.ReadFile() error = %v", err)
						}
						if string(raw) != content {
							t.Errorf("written file %s/%s = %q, want %q", dir, name, raw, content)
						}
					}
				}
			}

			// The record is forgotten: a second sweep has nothing registered and removes
			// nothing, even though the written set is gone too.
			if err := output.removeStaleOutput(); err != nil {
				t.Fatalf("second removeStaleOutput() error = %v", err)
			}
			for dir, want := range tt.wantKept {
				slices.Sort(want)
				if diff := cmp.Diff(want, listDir(t, filepath.Join(root, dir))); diff != "" {
					t.Errorf("second removeStaleOutput() changed %s (-want +got):\n%s", dir, diff)
				}
			}
		})
	}
}

// listDir lists a directory's entry names sorted, nil when the directory is missing.
func listDir(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("os.ReadDir() error = %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)

	return names
}
