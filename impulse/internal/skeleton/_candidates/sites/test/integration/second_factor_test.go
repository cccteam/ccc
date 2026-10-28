package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"testing"

	"github.com/cccteam/session/sessioninfo"

	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/pkg/auth/staff"
)

// The second factor every session carries, as the login page completes it: the sign-in
// code the auth mails to the person's address, which a browser the person asked to be
// remembered stands in for. The helpers here sign a login in through it.

// signIn primes the XSRF handshake and signs a login in the way a person does, failing
// the test on refusal: the password, then the second factor. The login's first sign-in
// on the stack gives an address for the code (the development logins hold none), enters
// the code read from the mail the auth sent and asks for the browser to be remembered;
// every later sign-in of theirs carries that browser's token (served.trusted), so it is
// the remembered browser and is not asked. One sign-in at a time runs its factor on the
// stack, so the code a browser reads from the recorder is its own, and a login is mailed
// a code once (an address is sent three codes within fifteen minutes, no more).
func (b *browser) signIn(ctx context.Context, user string) {
	b.t.Helper()

	s := b.served
	s.codeMu.Lock()
	defer s.codeMu.Unlock()
	if cookie := s.trusted[user]; cookie != nil {
		b.remember(cookie)
	}
	if status, body := b.do(ctx, http.MethodGet, "/api/user/session", nil); status != http.StatusOK {
		b.t.Fatalf("GET user/session before login as %s: status %d: %s", user, status, body)
	}
	status, body := b.login(ctx, user, adminPassword)
	if status != http.StatusOK {
		b.t.Fatalf("login as %s: status %d: %s", user, status, body)
	}
	if heldForFactor(b.t, body) {
		b.secondFactorLocked(ctx, user, true)
		s.trusted[user] = b.trustedCookie()
	}
}

// heldForFactor reads a password login's answer: whether the sign-in waits for the
// second factor (mfaIsRequired).
func heldForFactor(t *testing.T, body []byte) bool {
	t.Helper()

	var answer struct {
		MFAIsRequired bool `json:"mfaIsRequired"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decoding the login's answer: %v: %s", err, body)
	}

	return answer.MFAIsRequired
}

// remember puts a remembered browser's trusted-device cookie, as its jar holds it, into
// this browser's jar: the person signs in again from the browser they asked to be
// remembered.
func (b *browser) remember(cookie *http.Cookie) {
	b.t.Helper()

	site, err := url.Parse(b.base)
	if err != nil {
		b.t.Fatal(err)
	}
	b.client.Jar.SetCookies(site, []*http.Cookie{cookie})
}

// trustedCookie is the trusted-device cookie this browser's jar holds, or nil when the
// person never asked for the browser to be remembered.
func (b *browser) trustedCookie() *http.Cookie {
	b.t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, b.base+"/api/user/session", http.NoBody)
	if err != nil {
		b.t.Fatal(err)
	}
	for _, cookie := range b.client.Jar.Cookies(req.URL) {
		if cookie.Name == staff.TrustedDeviceCookie {
			return cookie
		}
	}

	return nil
}

// pendingSignIn is the pending route's answer as the login page reads it: why the sign-in
// waits, and for the second factor, the state of the sign-in code.
type pendingSignIn struct {
	Reason    string   `json:"reason"`
	Email     string   `json:"email"`
	Factors   []string `json:"factors"`
	EmailCode *struct {
		CodeEmail   string `json:"codeEmail"`
		EnrollEmail bool   `json:"enrollEmail"`
		CodeSent    bool   `json:"codeSent"`
	} `json:"emailCode"`
}

// pending reads the sign-in this browser waits on.
func (b *browser) pending(ctx context.Context) *pendingSignIn {
	b.t.Helper()

	status, body := b.do(ctx, http.MethodGet, "/api/user/pending", nil)
	if status != http.StatusOK {
		b.t.Fatalf("GET user/pending: status %d: %s", status, body)
	}
	pending := &pendingSignIn{}
	if err := json.Unmarshal(body, pending); err != nil {
		b.t.Fatalf("decoding the pending sign-in: %v: %s", err, body)
	}

	return pending
}

// codeSubject finds the sign-in code in a code message's subject ("Your <application>
// sign-in code is 483 920").
var codeSubject = regexp.MustCompile(`\b(\d{3}) (\d{3})\b`)

// factorAddress is the address a login gives for the sign-in code when it holds none: the
// development logins carry no address until they give one.
func factorAddress(user string) string {
	return user + "@mail.example"
}

// secondFactor completes the second factor this browser's sign-in waits for, the way the
// login page does once the password admitted the person (or a directory the deployment
// does not administer): the address for the code where the person holds none, the code
// asked for and read from the mail the auth sent, and the code verified, asking for the
// browser to be remembered when remember is set. One sign-in at a time runs its factor on
// the stack.
func (b *browser) secondFactor(ctx context.Context, user string, remember bool) {
	b.t.Helper()

	b.served.codeMu.Lock()
	defer b.served.codeMu.Unlock()
	b.secondFactorLocked(ctx, user, remember)
}

// secondFactorLocked is secondFactor for a caller holding the stack's factor lock.
func (b *browser) secondFactorLocked(ctx context.Context, user string, remember bool) {
	b.t.Helper()

	pending := b.pending(ctx)
	if pending.Reason != string(sessioninfo.PendingMFA) || pending.EmailCode == nil {
		b.t.Fatalf("the sign-in of %s waits for %+v, want the second factor with the sign-in code offered", user, pending)
	}
	code := b.askCodeLocked(ctx, pending.EmailCode.EnrollEmail, factorAddress(user))
	verify, err := json.Marshal(map[string]any{"code": code, "rememberDevice": remember})
	if err != nil {
		b.t.Fatal(err)
	}
	if status, body := b.do(ctx, http.MethodPost, "/api/user/pending/code/verify", verify); status != http.StatusOK {
		b.t.Fatalf("verifying the sign-in code of %s: status %d: %s", user, status, body)
	}
}

// askCodeLocked asks for the sign-in code, giving the address first where the person holds
// none, and reads the code from the message the auth sent: the first new message whose
// subject carries one. The caller holds the stack's factor lock, so the message is this
// sign-in's.
func (b *browser) askCodeLocked(ctx context.Context, enroll bool, address string) string {
	b.t.Helper()

	s := b.served
	before := len(s.mail.Messages())
	if enroll {
		give, err := json.Marshal(map[string]string{"email": address})
		if err != nil {
			b.t.Fatal(err)
		}
		if status, body := b.do(ctx, http.MethodPost, "/api/user/pending/email", give); status != http.StatusOK {
			b.t.Fatalf("giving %s for the sign-in code: status %d: %s", address, status, body)
		}
	} else if status, body := b.do(ctx, http.MethodPost, "/api/user/pending/code", nil); status != http.StatusOK {
		b.t.Fatalf("asking for the sign-in code: status %d: %s", status, body)
	}
	for _, m := range s.mail.Messages()[before:] {
		if match := codeSubject.FindStringSubmatch(m.Subject); match != nil {
			return match[1] + match[2]
		}
	}
	b.t.Fatalf("no sign-in code was mailed after the request (%d messages before)", before)

	return ""
}
