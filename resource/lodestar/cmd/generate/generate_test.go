package generate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestGeneratedCodeIsIdempotent re-runs the full generation pipeline and fails if any
// generated file differs from the state before the run. The zz_gen files (Go,
// TypeScript, and the workflow DOT graphs) are the golden output of the resource
// generators: any drift here is a generator behavior change that must be either
// intentional (keep the new output) or a regression.
//
// The comparison is a content snapshot rather than git status because Lodestar's tree
// is untracked between build rounds (design plan §12): a git-based drift check would
// report every generated file as new. Requires the Spanner emulator (podman/docker),
// like the rest of this module's tests.
func TestGeneratedCodeIsIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("generation requires the Spanner emulator")
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	moduleRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	before := snapshotGenerated(t, moduleRoot)

	generate := exec.CommandContext(t.Context(), "go", "generate", "./...")
	generate.Dir = moduleRoot
	if out, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("go generate ./...: %v\n%s", err, out)
	}

	after := snapshotGenerated(t, moduleRoot)

	var drift []string
	for path, hash := range after {
		if before[path] != hash {
			drift = append(drift, path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			drift = append(drift, path+" (removed)")
		}
	}
	slices.Sort(drift)
	if len(drift) > 0 {
		t.Errorf("generator output differs from the committed state; if the change is intentional, keep the regenerated files:\n%s", strings.Join(drift, "\n"))
	}
}

// snapshotGenerated hashes every zz_gen file under the module root (node_modules and
// build outputs excluded), keyed by its module-relative path.
func snapshotGenerated(t *testing.T, moduleRoot string) map[string]string {
	t.Helper()

	var paths []string
	err := filepath.WalkDir(moduleRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walking %s: %w", path, err)
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "dist", ".angular", ".ccc-cache", ".yalc":
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
		t.Fatalf("walking %s: %v", moduleRoot, err)
	}

	hashes := make(map[string]string, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		sum := sha256.Sum256(data)
		rel, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			t.Fatalf("filepath.Rel(%s): %v", path, err)
		}
		hashes[rel] = hex.EncodeToString(sum[:])
	}

	return hashes
}
