//go:build !dev

package resource

import (
	"os/exec"
	"testing"
)

// TestStarportApp runs the starport module's full test suite (generated-code drift check,
// generated router tests, and the permission-enforcement integration suites against the
// Spanner emulator).
//
// The starport app is deliberately NOT registered in release-please-config.json: the shared
// golang-ci workflow derives its CI matrix from that file's packages, and registering
// starport there would also publish releases for it. This stub is what runs the starport
// suite in CI, as part of the resource module's own tests.
//
// Note: `go test` cannot see the starport files this subprocess reads, so a cached pass
// of this test is not invalidated by starport-only changes; run with -count=1 (or run
// `go test ./...` inside starport directly) when iterating on the starport app.
func TestStarportApp(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("starport tests require the Spanner emulator")
	}

	cmd := exec.CommandContext(t.Context(), "go", "test", "./...")
	cmd.Dir = "starport"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go test ./... (starport): %v\n%s", err, out)
	}
}
