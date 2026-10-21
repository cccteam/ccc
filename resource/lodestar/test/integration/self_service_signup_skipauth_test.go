//go:build skipAuth

package integration

import (
	"context"
	"net/http"
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
// Demonstrates: tenancy.self-service, auth.open-sign-up, auth.skipauth-directory.
func TestSelfServiceSignUp_signInFirstThenChart(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	b := newBrowser(t, s, consoleAPI)

	if status, body := b.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before the sign-up: status %d: %s", status, body)
	}
	if status, location := b.loginConsoleSignUp(ctx, string(pioneerUser)); status != http.StatusFound || location != consolePage {
		t.Fatalf("console sign-up: status %d to %q, want %d to %q", status, location, http.StatusFound, consolePage)
	}
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
