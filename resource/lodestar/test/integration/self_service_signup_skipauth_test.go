//go:build skipAuth

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"slices"
	"testing"

	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
)

// TestSelfServiceSignUp_signInFirstThenChart proves the order the Sector record's
// creation shape settles, on the served stack: the pioneer signs in first, through the
// crew auth's sign-up registration (any Google account, open sign-up: a name the auth has
// never seen becomes a crew login holding no role and no sector; simulated under the
// skipAuth tag as APP_SIGN_UP_USERNAME), and then names her own sector through the
// generated create-sector route, standing in it at once.
//
// Ember's first directory is the one she came through, written by the route in the
// row's transaction, so Ember admits her Google session at once.
//
// Any Google account is a directory the fleet does not administer, so her sign-up
// waits for the second factor: the browser lands on the login page with the pending
// marker, she gives an address for the sign-in code (a sign-up holds none), and the
// code opens the session.
//
// Demonstrates: tenancy.self-service, tenancy.creator-directory, auth.open-sign-up, auth.skipauth-directory, auth.second-factor.
func TestSelfServiceSignUp_signInFirstThenChart(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	b := newBrowser(t, s, consoleAPI)

	if status, body := b.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before the sign-up: status %d: %s", status, body)
	}
	held := consoleLogin + "?pending=mfa&returnUrl=" + url.QueryEscape(consolePage)
	if status, location := b.loginConsoleSignUp(ctx, string(pioneerUser)); status != http.StatusFound || location != held {
		t.Fatalf("console sign-up: status %d to %q, want %d to %q, the login page with the second factor's wait", status, location, http.StatusFound, held)
	}
	b.secondFactor(ctx, string(pioneerUser), false)
	sess := b.session(ctx)
	if sess["authenticated"] != true || sess["username"] != string(pioneerUser) {
		t.Fatalf("session after the sign-up = %v, want authenticated as %s", sess, pioneerUser)
	}

	// Signed in and holding nothing: her chart is dark.
	if got := b.domains(ctx); len(got) != 0 {
		t.Fatalf("user-domains after the sign-up = %v, want none", got)
	}

	// She charts Ember and stands in it: the row, her marshal's seat and the roster.
	if status, body := b.do(ctx, http.MethodPost, createSectorRoute, []byte(chartEmber)); status != http.StatusOK {
		t.Fatalf("POST %s: status %d: %s", createSectorRoute, status, body)
	}
	if got, want := b.domains(ctx), []string{ember}; !slices.Equal(got, want) {
		t.Errorf("user-domains after charting = %v, want %v", got, want)
	}
	if status, body := b.do(ctx, http.MethodGet, consoleAPI+"/sectors/"+ember+"/missions", nil); status != http.StatusOK {
		t.Errorf("GET Ember's flight deck: status %d, want 200: %s", status, body)
	}
	// Ember declares the directory she came through, and nothing else: her Google
	// session is admitted by it, which is why her chart lights Ember at all.
	status, body := b.do(ctx, http.MethodGet, consoleAPI+"/sectors/"+ember+"/sector-directories", nil)
	if status != http.StatusOK {
		t.Fatalf("GET Ember's directories: status %d: %s", status, body)
	}
	var declared []struct {
		Method     string `json:"method"`
		Connection string `json:"connection"`
	}
	if err := json.Unmarshal(body, &declared); err != nil {
		t.Fatalf("Ember's directories are not a list: %v: %s", err, body)
	}
	if len(declared) != 1 || declared[0].Method != "google" || declared[0].Connection == "" {
		t.Errorf("Ember's directories = %+v, want the one Google directory the pioneer came through", declared)
	}
	// One sector per person: a second is refused.
	if status, body := b.do(ctx, http.MethodPost, createSectorRoute, []byte(chartEmberAgain)); status != http.StatusForbidden {
		t.Errorf("POST %s again: status %d, want 403: %s", createSectorRoute, status, body)
	}
}

// loginConsoleSignUp signs the browser in through the crew auth's sign-up registration
// as the named person, simulated by the skipAuth build from APP_SIGN_UP_USERNAME, and
// returns the callback's status and where it sent the browser.
func (b *browser) loginConsoleSignUp(ctx context.Context, user string) (status int, location string) {
	b.t.Helper()

	simulatedLogin.Lock()
	defer simulatedLogin.Unlock()
	if err := os.Setenv("APP_SIGN_UP_USERNAME", user); err != nil {
		b.t.Fatal(err)
	}

	return b.loginThrough(ctx, "/user/"+crew.SignUpRegistration+"/login", consolePage)
}
