//go:build skipAuth

package integration

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

// simulatedDirectory reports whether this build simulates the directory: it does.
func simulatedDirectory() bool { return true }

// The portal's people sign in through a directory. Under the session library's skipAuth
// build tag the directory is simulated: the login route sends the browser straight to the
// callback, and the callback takes the username from APP_USERNAME and the person's groups
// from APP_ROLES on every login, so one test process signs in as many clients as it
// likes, one at a time.
var simulatedLogin sync.Mutex

// loginPortal signs the browser in as user through the simulated directory, in the named
// role groups, and returns the callback's status and where it sent the browser: the page
// the login named.
//
// Demonstrates: auth.directory-roles, auth.skipauth-directory.
func (b *browser) loginPortal(ctx context.Context, user string, roles ...string) (status int, location string) {
	b.t.Helper()

	simulatedLogin.Lock()
	defer simulatedLogin.Unlock()
	if err := os.Setenv("APP_USERNAME", user); err != nil {
		b.t.Fatal(err)
	}
	if err := os.Setenv("APP_ROLES", strings.Join(roles, ",")); err != nil {
		b.t.Fatal(err)
	}

	status, location = b.redirect(ctx, b.prefix+"/user/login?returnUrl="+url.QueryEscape(portalPage))
	if status != http.StatusFound {
		b.t.Fatalf("GET %s/user/login: status %d, want %d to the directory", b.prefix, status, http.StatusFound)
	}

	return b.redirect(ctx, location)
}
