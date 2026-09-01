//go:build !dev

package resource

import (
	"os/exec"
	"testing"
)

// TestWaystationApp runs the waystation module's full test suite (generated-code drift
// check, generated router tests, the integration suites — including the bootstrap-parity
// conditions suite over the real permission engine — and the generated authorization
// matrix) against the Spanner emulator.
//
// Like starport, waystation is deliberately NOT registered in release-please-config.json:
// the shared golang-ci workflow derives its CI matrix from that file's packages, and
// registering it there would also publish releases for it. This stub is what runs the
// waystation suite in CI, as part of the resource module's own tests.
//
// Note: `go test` cannot see the waystation files this subprocess reads, so a cached
// pass of this test is not invalidated by waystation-only changes; run with -count=1
// (or run `go test ./...` inside waystation directly) when iterating on the app.
func TestWaystationApp(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("waystation tests require the Spanner emulator")
	}

	cmd := exec.CommandContext(t.Context(), "go", "test", "./...")
	cmd.Dir = "waystation"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go test ./... (waystation): %v\n%s", err, out)
	}
}
