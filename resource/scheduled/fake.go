package scheduled

import (
	"context"
	"strconv"
	"sync"

	"github.com/go-playground/errors/v5"
	"google.golang.org/api/idtoken"
)

// Fake is a Verifier for tests, standing in for Google's signing keys: it accepts exactly
// the tokens it issued, each for the audience it was issued for, answering the claims it
// was issued with, and refuses every other token as Google's keys refuse a token they did
// not sign.
type Fake struct {
	mu     sync.Mutex
	tokens map[string]*idtoken.Payload
}

// NewFake is a fake that has issued no token yet.
func NewFake() *Fake {
	return &Fake{tokens: map[string]*idtoken.Payload{}}
}

// Mint issues a token as Cloud Scheduler presents one: issued by Google for the audience,
// carrying email as its verified email.
func (f *Fake) Mint(audience, email string) string {
	return f.Issue(&idtoken.Payload{
		Issuer:   googleIssuers[0],
		Audience: audience,
		Claims:   map[string]any{emailClaim: email, emailVerifiedClaim: true},
	})
}

// Issue issues a token carrying the payload as given, for a test of a token that verifies
// and is still refused: another issuer, an email that is not verified.
func (f *Fake) Issue(payload *idtoken.Payload) string {
	f.mu.Lock()
	defer f.mu.Unlock()

	token := "fake-token-" + strconv.Itoa(len(f.tokens)+1)
	f.tokens[token] = payload

	return token
}

// Validate answers the claims of a token the fake issued for the audience, and refuses a
// token it did not issue or one issued for another audience.
func (f *Fake) Validate(_ context.Context, token, audience string) (*idtoken.Payload, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	payload, ok := f.tokens[token]
	if !ok {
		return nil, errors.New("the token is not signed by Google's keys")
	}
	if payload.Audience != audience {
		return nil, errors.Newf("the token's audience %q is not %q", payload.Audience, audience)
	}

	return payload, nil
}
