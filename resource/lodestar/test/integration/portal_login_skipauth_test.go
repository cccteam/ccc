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

// The portal's people sign in through a directory, and so may the crew. Under the session
// library's skipAuth build tag the directory is simulated: the login route sends the
// browser straight to the callback, and the callback takes the username and the person's
// groups from the environment on every login (APP_<REGISTRATION>_USERNAME and _ROLES for
// the registration, else APP_USERNAME and APP_ROLES), so one test process signs in as
// many people as it likes, one at a time.
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

	return b.loginThrough(ctx, "/user/directory/login", portalPage)
}

// loginConsoleGoogle signs the browser in on the console as user through the fleet's
// simulated Google directory and returns the callback's status and where it sent the
// browser. The console's Google registration reads its own setting, APP_GOOGLE_USERNAME,
// so the portal's client is untouched; it names no groups, since the crew's roles are the
// application's on every method.
//
// Demonstrates: auth.multi-method.
func (b *browser) loginConsoleGoogle(ctx context.Context, user string) (status int, location string) {
	b.t.Helper()

	simulatedLogin.Lock()
	defer simulatedLogin.Unlock()
	if err := os.Setenv("APP_GOOGLE_USERNAME", user); err != nil {
		b.t.Fatal(err)
	}

	return b.loginThrough(ctx, "/user/google/login", consolePage)
}

// loginThrough follows a redirect sign-in: the login route sends the browser to the
// directory (here straight to the callback), and the callback starts the session and
// sends the browser on to the page the login named.
func (b *browser) loginThrough(ctx context.Context, loginPath, page string) (status int, location string) {
	b.t.Helper()

	status, location = b.redirect(ctx, b.prefix+loginPath+"?returnUrl="+url.QueryEscape(page))
	if status != http.StatusFound {
		b.t.Fatalf("GET %s%s: status %d, want %d to the directory", b.prefix, loginPath, status, http.StatusFound)
	}

	return b.redirect(ctx, location)
}
