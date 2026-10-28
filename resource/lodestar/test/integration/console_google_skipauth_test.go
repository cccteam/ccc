//go:build skipAuth

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"testing"
)

// TestConsoleGoogleLogin signs in on the console through the fleet's Google directory,
// the crew auth's second method beside the password, as a person on no manifest: the
// first sign-in provisions them under the verified email, holding what the sectors
// declaring the directory name as its default seat and nothing else (Cinder's row names
// SectorCrew, Anvil's and Bastion's none), since the crew's roles are the application's
// on every method (a registration whose directory writes the roles stands alone on its
// auth, the members auth's shape); a password persona's session and theirs are sessions
// of the same auth, read at the same route; and the next sign-in finds them as they
// were, nothing written in between. A directory identity whose verified email is a
// password persona's login is not taken at the directory's word: the sign-in waits for
// that persona's password, which links the identity and opens the session as the
// persona, whose seats stay the application's. The fleet administers its Workspace
// (auth.Directory.Administered), so a sign-in through the directory carries the
// Workspace's second factor and is never asked for the sign-in code, the sign-in a
// password confirmed included.
//
// Demonstrates: auth.multi-method, auth.skipauth-directory, tenancy.default-seat, auth.attested-directory.
func TestConsoleGoogleLogin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	b := newBrowser(t, s, consoleAPI)

	if status, body := b.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before login: status %d: %s", status, body)
	}
	if status, location := b.loginConsoleGoogle(ctx, navigatorUser); status != http.StatusFound || location != consolePage {
		t.Fatalf("console Google login: status %d to %q, want %d to %q", status, location, http.StatusFound, consolePage)
	}
	sess := b.session(ctx)
	if sess["authenticated"] != true || sess["username"] != navigatorUser {
		t.Fatalf("session after login = %v, want authenticated as %s", sess, navigatorUser)
	}

	// Provisioned with the default seat alone: the one row naming one seats them in
	// Cinder, and no other sector is theirs until an operator seats them.
	if got, want := b.domains(ctx), []string{cinder}; !slices.Equal(got, want) {
		t.Errorf("user-domains after the first sign-in = %v, want %v: Cinder's row names SectorCrew as its default seat, the others none", got, want)
	}

	// A password persona signs in beside: two methods, one auth, one session route.
	marshal := newBrowser(t, s, consoleAPI)
	marshal.signIn(ctx, "marshal")
	if got := marshal.session(ctx)["username"]; got != "marshal" {
		t.Errorf("the marshal's session reads as %v", got)
	}

	// The next sign-in finds the person as they were: nothing is written by the directory.
	if status, _ := b.loginConsoleGoogle(ctx, navigatorUser); status != http.StatusFound {
		t.Fatalf("second console Google login: status %d", status)
	}
	if got, want := b.domains(ctx), []string{cinder}; !slices.Equal(got, want) {
		t.Errorf("user-domains after the second sign-in = %v, want %v, as after the first", got, want)
	}

	// A directory identity whose verified email is a login a password persona already
	// holds is not taken at the directory's word: the library's standard account rule
	// holds the sign-in for that persona's password. The browser lands on the login page
	// with the pending marker and no session opens; the pending route names the step;
	// the marshal's password links the identity and opens the session as the marshal,
	// whose seats are the application's and stand as they were.
	taken := newBrowser(t, s, consoleAPI)
	if status, body := taken.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before the conflicting login: status %d: %s", status, body)
	}
	status, location := taken.loginConsoleGoogle(ctx, "marshal")
	if want := consoleLogin + "?pending=confirmation&returnUrl=" + url.QueryEscape(consolePage); status != http.StatusFound || location != want {
		t.Fatalf("console Google login as a password persona's login: status %d to %q, want %d to %q", status, location, http.StatusFound, want)
	}
	if got := taken.session(ctx); got["authenticated"] != false {
		t.Errorf("session while the sign-in waits = %v, want unauthenticated", got)
	}
	status, body := taken.do(ctx, http.MethodGet, consoleAPI+"/user/pending", nil)
	var pending struct {
		Reason string `json:"reason"`
		Email  string `json:"email"`
	}
	if err := json.Unmarshal(body, &pending); status != http.StatusOK || err != nil || pending.Reason != "confirmation" || pending.Email != "marshal" {
		t.Fatalf("pending sign-in: status %d: %s (%v), want a confirmation for marshal", status, body, err)
	}
	confirmation, err := json.Marshal(map[string]string{"password": personaPassword})
	if err != nil {
		t.Fatal(err)
	}
	if status, body := taken.do(ctx, http.MethodPost, consoleAPI+"/user/pending/confirm", confirmation); status != http.StatusOK {
		t.Fatalf("confirming the sign-in with the marshal's password: status %d: %s", status, body)
	}
	// The password confirmed the identity and the session opened at once: the sign-in
	// is the directory's, which attests for the marshal, so no code follows the
	// confirmation.
	if sess := taken.session(ctx); sess["authenticated"] != true || sess["username"] != "marshal" {
		t.Fatalf("session after the confirmation = %v, want authenticated as marshal", sess)
	}
	if got, want := taken.domains(ctx), marshal.domains(ctx); !slices.Equal(got, want) {
		t.Errorf("user-domains after the directory signed the marshal in = %v, want the marshal's own %v", got, want)
	}
}

// domains reads the session's sector footholds.
func (b *browser) domains(ctx context.Context) []string {
	b.t.Helper()

	status, body := b.do(ctx, http.MethodGet, b.prefix+"/user-domains", nil)
	if status != http.StatusOK {
		b.t.Fatalf("GET %s/user-domains: status %d: %s", b.prefix, status, body)
	}
	var domains []string
	if err := json.Unmarshal(body, &domains); err != nil {
		b.t.Fatalf("user-domains is not a list: %v: %s", err, body)
	}

	return domains
}
