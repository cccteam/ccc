// Demonstrates: regen-idempotent, workflow.dot.
package generate

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestGeneratedCodeIsCommitted re-runs the full generation pipeline and fails if any
// generated file differs from the state on disk. The zz_gen files (Go, TypeScript, DOT)
// are the golden output of the resource generators: any drift here is a generator
// behavior change that must be either intentional (commit the new output) or a
// regression. The comparison hashes the files before and after, so it holds whether the
// tree is committed or still untracked.
//
// Requires the Spanner emulator (podman/docker), like the rest of this module's tests.
func TestGeneratedCodeIsCommitted(t *testing.T) {
	if testing.Short() {
		t.Skip("generation requires the Spanner emulator")
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	moduleRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	before := hashGenerated(t, moduleRoot)

	generate := exec.CommandContext(t.Context(), "go", "generate", "./...")
	generate.Dir = moduleRoot
	if out, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("go generate ./...: %v\n%s", err, out)
	}

	after := hashGenerated(t, moduleRoot)

	var drift []string
	for path, sum := range after {
		if before[path] != sum {
			drift = append(drift, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			drift = append(drift, path+" (removed)")
		}
	}
	if len(drift) > 0 {
		t.Errorf("generator output differs from the state on disk; if the change is intentional, commit the regenerated files:\n%s", strings.Join(drift, "\n"))
	}
}

// hashGenerated maps every zz_gen file under the module (the Angular workspace's
// node_modules excluded) to its content hash.
func hashGenerated(t *testing.T, root string) map[string][32]byte {
	t.Helper()

	sums := make(map[string][32]byte)
	var paths []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == ".angular" || d.Name() == "dist" {
				return filepath.SkipDir
			}

			return nil
		}
		if strings.Contains(d.Name(), "zz_gen") {
			paths = append(paths, path)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		sums[rel] = sha256.Sum256(raw)
	}

	return sums
}
