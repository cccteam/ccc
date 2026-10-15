//go:build skipAuth

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// TestConsoleGoogleLogin signs in on the console through the fleet's Google directory,
// the crew auth's second method beside the password, as a person on no manifest: the
// first sign-in provisions them under the verified email and seats them in the roles
// the directory's groups name, spelled the role file's way (the groups name pilot, the
// crew's role is Pilot); a password persona's session and theirs are sessions of the
// same auth, read at the same route; and the next sign-in reconciles the roles to the
// groups again, holding what the directory still names and dropping what it no longer
// does, with no churn in between.
//
// Demonstrates: auth.multi-method, auth.directory-roles, auth.skipauth-directory.
func TestConsoleGoogleLogin(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)
	b := newBrowser(t, s, consoleAPI)

	if status, body := b.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before login: status %d: %s", status, body)
	}
	if status, location := b.loginConsoleGoogle(ctx, navigatorUser, "pilot", "crewcommon"); status != http.StatusFound || location != consolePage {
		t.Fatalf("console Google login: status %d to %q, want %d to %q", status, location, http.StatusFound, consolePage)
	}
	sess := b.session(ctx)
	if sess["authenticated"] != true || sess["username"] != navigatorUser {
		t.Fatalf("session after login = %v, want authenticated as %s", sess, navigatorUser)
	}

	// Pilot is a domain role held in every sector, so the navigator has a foothold in
	// all three; CrewCommon is global.
	if got, want := b.domains(ctx), []string{anvil, bastion, cinder}; !slices.Equal(got, want) {
		t.Errorf("user-domains after the first sign-in = %v, want %v", got, want)
	}

	// A password persona signs in beside: two methods, one auth, one session route.
	marshal := newBrowser(t, s, consoleAPI)
	marshal.signIn(ctx, "marshal")
	if got := marshal.session(ctx)["username"]; got != "marshal" {
		t.Errorf("the marshal's session reads as %v", got)
	}

	// The next sign-in names the same groups: nothing is added or removed, and the
	// foothold stands.
	if status, _ := b.loginConsoleGoogle(ctx, navigatorUser, "pilot", "crewcommon"); status != http.StatusFound {
		t.Fatalf("second console Google login: status %d", status)
	}
	if got, want := b.domains(ctx), []string{anvil, bastion, cinder}; !slices.Equal(got, want) {
		t.Errorf("user-domains after the second sign-in = %v, want %v", got, want)
	}

	// The directory drops the pilot group: the sector foothold goes with it, and the
	// global role alone remains.
	if status, _ := b.loginConsoleGoogle(ctx, navigatorUser, "crewcommon"); status != http.StatusFound {
		t.Fatalf("third console Google login: status %d", status)
	}
	if got := b.domains(ctx); len(got) != 0 {
		t.Errorf("user-domains after the pilot group is dropped = %v, want none", got)
	}

	// A directory that names no crew role refuses the sign-in: a browser arriving with no
	// session lands on the console's login page with the refusal's code, and none opens.
	fresh := newBrowser(t, s, consoleAPI)
	if status, body := fresh.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before the refused login: status %d: %s", status, body)
	}
	status, location := fresh.loginConsoleGoogle(ctx, navigatorUser, "not-a-role")
	if status != http.StatusFound || location != consoleLogin+"?code=no_roles" {
		t.Fatalf("console Google login naming no role: status %d to %q, want %d to %q", status, location, http.StatusFound, consoleLogin+"?code=no_roles")
	}
	if got := fresh.session(ctx)["authenticated"]; got != false {
		t.Errorf("session after a refused sign-in = %v, want unauthenticated", got)
	}

	// A directory identity whose verified email is a login a password persona already
	// holds is not that persona: the provisioning refuses the taken username, the
	// browser returns to the login page with a code, and no session opens as the marshal.
	taken := newBrowser(t, s, consoleAPI)
	if status, body := taken.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before the conflicting login: status %d: %s", status, body)
	}
	status, location = taken.loginConsoleGoogle(ctx, "marshal", "pilot")
	if status != http.StatusFound || !strings.HasPrefix(location, consoleLogin+"?code=") {
		t.Fatalf("console Google login as a password persona's login: status %d to %q, want %d to the login page with a code", status, location, http.StatusFound)
	}
	if got := taken.session(ctx); got["authenticated"] != false {
		t.Errorf("session after the conflicting sign-in = %v, want unauthenticated", got)
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
