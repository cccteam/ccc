package generate

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestGeneratedCodeIsCommitted re-runs the full generation pipeline and fails if any
// generated file differs from the committed state. The committed zz_gen files are the
// golden output of the resource generators: any drift here is a generator behavior
// change that must be either intentional (commit the new output) or a regression.
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

	generate := exec.CommandContext(t.Context(), "go", "generate", "./...")
	generate.Dir = moduleRoot
	if out, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("go generate ./...: %v\n%s", err, out)
	}

	status := exec.CommandContext(t.Context(), "git", "status", "--porcelain", "-uall", "--", ".")
	status.Dir = moduleRoot
	out, err := status.Output()
	if err != nil {
		t.Fatalf("git status --porcelain: %v", err)
	}

	var drift []string
	for line := range strings.Lines(string(out)) {
		line = strings.TrimRight(line, "\n")
		if line == "" {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if strings.Contains(path, "zz_gen") || strings.Contains(path, "pkg/mock/") {
			drift = append(drift, line)
		}
	}

	if len(drift) > 0 {
		t.Errorf("generator output differs from the committed state; if the change is intentional, commit the regenerated files:\n%s", strings.Join(drift, "\n"))
	}
}
