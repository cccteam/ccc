//go:build !skipAuth

package integration

import "context"

// The portal's people sign in through Google, and the members auth reads their groups
// through the Admin SDK, which needs credentials a test process has none of. Only the
// session library's skipAuth build simulates the directory, so the served suites run
// under `go test -tags skipAuth ./...`, as the Procfile and the CI stub do; without the tag
// they skip with the reason.

// simulatedDirectory reports whether this build simulates the directory: it does not, so
// the served suites skip.
func simulatedDirectory() bool { return false }

// loginPortal needs the simulated directory; see requireSimulatedDirectory.
func (b *browser) loginPortal(_ context.Context, _ string, _ ...string) (status int, location string) {
	b.t.Helper()
	b.t.Skip("the portal signs in through a directory: run this suite with -tags skipAuth to simulate it")

	return 0, ""
}
