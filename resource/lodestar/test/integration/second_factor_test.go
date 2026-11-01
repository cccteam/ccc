//go:build skipAuth

package integration

import (
	"context"
	stderrors "errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/session/sessioninfo"
	"google.golang.org/api/iterator"
)

// TestSecondFactor proves the second factor every session carries, on the served stack
// and the way the login page runs it. The cadet's password is admitted and her sign-in
// waits for the sign-in code: she holds no address, gives one, reads the code from the
// mail the auth sent, and the right code opens her session, the address now hers and
// verified. The browser she asked to be remembered is not asked again; another browser
// of hers is; headquarters resets her second factor from the user-management surface,
// and the remembered browser is asked once more. The navigator, who signs in through
// the fleet's directory, is never asked: the fleet administers its Workspace, which
// attests for him.
//
// Demonstrates: auth.second-factor, auth.trusted-device, auth.attested-directory.
func TestSecondFactor(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)

	// The password admits the cadet, and the sign-in waits for the code: no session
	// yet, the pending route says why, and a code entered while none was sent asks
	// for one.
	cass := newBrowser(t, s, consoleAPI)
	if status, body := cass.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before login: status %d: %s", status, body)
	}
	if status, body := cass.login(ctx, "cadet", personaPassword); status != http.StatusOK || !heldForFactor(t, body) {
		t.Fatalf("the cadet's password sign-in: status %d: %s, want 200 holding the sign-in for the second factor", status, body)
	}
	if sess := cass.session(ctx); sess["authenticated"] != false {
		t.Fatalf("session while the sign-in waits = %v, want none", sess)
	}
	pending := cass.pending(ctx)
	if pending.Reason != string(sessioninfo.PendingMFA) || !slices.Equal(pending.Factors, []string{string(sessioninfo.MethodEmailCode)}) || pending.EmailCode == nil || !pending.EmailCode.EnrollEmail || pending.EmailCode.CodeSent {
		t.Fatalf("the pending sign-in = %+v, want the second factor's wait, the email code as the factor, an address to give and no code sent", pending)
	}
	if status, body := cass.do(ctx, http.MethodPost, consoleAPI+"/user/pending/code/verify", jsonBody(t, map[string]any{"code": "000000"})); status != http.StatusUnauthorized || !strings.Contains(string(body), string(sessioninfo.RefusedCodeExpired)) {
		t.Fatalf("a code entered while none was sent: status %d: %s, want 401 naming %s", status, body, sessioninfo.RefusedCodeExpired)
	}

	// She gives an address and the code goes there: a wrong code is refused and the
	// one in the mail, with the space the mail shows it with, opens the session and
	// remembers the browser. The address is hers now, verified by the code.
	code := cass.sentCode(ctx, true, factorAddress("cadet"))
	if pending := cass.pending(ctx); pending.EmailCode == nil || !pending.EmailCode.CodeSent || pending.EmailCode.EnrollEmail || !strings.HasSuffix(pending.EmailCode.CodeEmail, "@mail.example") || strings.Contains(pending.EmailCode.CodeEmail, "cadet") {
		t.Fatalf("the pending sign-in once the code was sent = %+v, want the code standing and the address masked", pending)
	}
	wrong := code[1:] + code[:1]
	if status, body := cass.do(ctx, http.MethodPost, consoleAPI+"/user/pending/code/verify", jsonBody(t, map[string]any{"code": wrong})); status != http.StatusUnauthorized || !strings.Contains(string(body), string(sessioninfo.RefusedInvalidCode)) {
		t.Fatalf("a wrong code: status %d: %s, want 401 naming %s", status, body, sessioninfo.RefusedInvalidCode)
	}
	spaced := code[:3] + " " + code[3:]
	if status, body := cass.do(ctx, http.MethodPost, consoleAPI+"/user/pending/code/verify", jsonBody(t, map[string]any{"code": spaced, "rememberDevice": true})); status != http.StatusOK {
		t.Fatalf("the code from the mail: status %d: %s", status, body)
	}
	sess := cass.session(ctx)
	if sess["authenticated"] != true || sess["username"] != "cadet" || sess["email"] != factorAddress("cadet") || sess["emailVerified"] != true {
		t.Fatalf("session after the code = %v, want the cadet signed in holding her address verified", sess)
	}
	if got, want := authEvents(ctx, t, s, "cadet"), []string{string(sessioninfo.MethodPassword), string(sessioninfo.MethodEmailCode)}; !slices.Equal(got, want) {
		t.Errorf("the cadet's auth events = %v, want %v", got, want)
	}

	// The remembered browser signs in without the code; another browser of hers is
	// asked, and the code goes to the address she holds now.
	cass.logout(ctx)
	if status, body := cass.login(ctx, "cadet", personaPassword); status != http.StatusOK || heldForFactor(t, body) {
		t.Fatalf("the remembered browser's sign-in: status %d: %s, want 200 with no second factor asked", status, body)
	}
	if sess := cass.session(ctx); sess["authenticated"] != true {
		t.Fatalf("session from the remembered browser = %v, want the cadet signed in", sess)
	}
	if got, want := authEvents(ctx, t, s, "cadet"), []string{string(sessioninfo.MethodPassword), string(sessioninfo.MethodTrustedDevice)}; !slices.Equal(got, want) {
		t.Errorf("the remembered browser's auth events = %v, want %v", got, want)
	}
	other := newBrowser(t, s, consoleAPI)
	if status, body := other.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before the other browser's login: status %d: %s", status, body)
	}
	if status, body := other.login(ctx, "cadet", personaPassword); status != http.StatusOK || !heldForFactor(t, body) {
		t.Fatalf("another browser's sign-in: status %d: %s, want 200 holding the sign-in for the second factor", status, body)
	}
	if pending := other.pending(ctx); pending.EmailCode == nil || pending.EmailCode.EnrollEmail {
		t.Fatalf("the other browser's pending sign-in = %+v, want the code offered to the address she holds", pending)
	}
	other.secondFactor(ctx, "cadet")
	if sess := other.session(ctx); sess["authenticated"] != true {
		t.Fatalf("session from the other browser after the code = %v, want the cadet signed in", sess)
	}

	// Headquarters resets her second factor: every browser she asked to be
	// remembered is forgotten, and the remembered one is asked again.
	governor := newBrowser(t, s, consoleAPI)
	governor.signIn(ctx, "governor")
	reset := jsonBody(t, map[string]any{"user": crewUserID(ctx, t, governor, "cadet")})
	if status, body := governor.do(ctx, http.MethodPost, consoleAPI+"/"+crewPartitions().Global.ResetFactorsRoute, reset); status != http.StatusOK {
		t.Fatalf("resetting the cadet's second factor: status %d: %s", status, body)
	}
	cass.logout(ctx)
	if status, body := cass.login(ctx, "cadet", personaPassword); status != http.StatusOK || !heldForFactor(t, body) {
		t.Fatalf("the once-remembered browser's sign-in after the reset: status %d: %s, want 200 holding the sign-in for the second factor", status, body)
	}
	cass.secondFactor(ctx, "cadet")
	if sess := cass.session(ctx); sess["authenticated"] != true {
		t.Fatalf("session after the reset and the code = %v, want the cadet signed in", sess)
	}

	// The fleet administers its Workspace, so the directory attests for the
	// navigator: his sign-in is never asked for the code.
	nav := newBrowser(t, s, consoleAPI)
	if status, body := nav.do(ctx, http.MethodGet, consoleAPI+"/user/session", nil); status != http.StatusOK {
		t.Fatalf("GET session before the navigator's login: status %d: %s", status, body)
	}
	if status, location := nav.loginConsoleGoogle(ctx, navigatorUser); status != http.StatusFound || location != consolePage {
		t.Fatalf("the navigator's Google login: status %d to %q, want %d to %q with no wait", status, location, http.StatusFound, consolePage)
	}
	if sess := nav.session(ctx); sess["authenticated"] != true || sess["username"] != navigatorUser {
		t.Fatalf("the navigator's session = %v, want signed in", sess)
	}
	if got, want := authEvents(ctx, t, s, navigatorUser), []string{string(sessioninfo.MethodGoogle), string(sessioninfo.MethodAttestedByDeclaration)}; !slices.Equal(got, want) {
		t.Errorf("the navigator's auth events = %v, want %v", got, want)
	}
}

// logout ends this browser's session.
func (b *browser) logout(ctx context.Context) {
	b.t.Helper()

	if status, body := b.do(ctx, http.MethodDelete, b.prefix+"/user/session", nil); status != http.StatusOK {
		b.t.Fatalf("DELETE %s/user/session: status %d: %s", b.prefix, status, body)
	}
}

// authEvents reads how the crew member's newest session was authenticated: the
// methods of its auth events in order, from the crew's tables.
func authEvents(ctx context.Context, t *testing.T, s *served, username string) []string {
	t.Helper()

	iter := s.db.Client.Single().Query(ctx, cloudspanner.Statement{
		SQL: `SELECT e.Method FROM CrewSessionAuthEvents e
			WHERE e.SessionId = (SELECT Id FROM CrewSessions WHERE Username = @username ORDER BY CreatedAt DESC LIMIT 1)
			ORDER BY e.Seq`,
		Params: map[string]any{"username": username},
	})
	defer iter.Stop()
	var methods []string
	for {
		row, err := iter.Next()
		if stderrors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			t.Fatalf("reading the auth events of %s: %v", username, err)
		}
		var method string
		if err := row.Columns(&method); err != nil {
			t.Fatalf("reading an auth event of %s: %v", username, err)
		}
		methods = append(methods, method)
	}

	return methods
}
