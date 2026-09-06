//go:build !skipAuth

package integration

import "context"

// loginPortal needs the simulated directory. The portal's people sign in through OpenID
// Connect, and only the session library's skipAuth build simulates the directory: run
// the suite with `go test -tags skipAuth ./...`, as the Procfile and CI do.
func (b *browser) loginPortal(_ context.Context, _ string) (status int, location string) {
	b.t.Helper()
	b.t.Skip("the portal signs in through a directory: run this suite with -tags skipAuth to simulate it")

	return 0, ""
}
