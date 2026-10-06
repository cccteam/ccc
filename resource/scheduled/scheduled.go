// Package scheduled is the framework's side of a scheduled route: a method the
// application declares with @schedule, which Cloud Scheduler calls on its schedule. The
// generated router mounts every scheduled route under Prefix, one POST per method at its
// name in kebab case (/_scheduled/prune-droid-reports), behind the application's
// SchedulerAuth, which delegates to a Guard.
//
// The service is open to the load balancer, so Cloud Run's own permission check lets
// every caller through, and the check is the application's. Cloud Scheduler calls with
// an OpenID Connect token (an OIDC token: a JSON web token Google signs) minted for the
// invoker identity, a service account the application's stack creates for its
// schedules, with the endpoint's URL as the token's audience. A Guard accepts a call
// whose token is signed by Google's keys, has not expired, is issued by Google, names
// the endpoint's URL as its audience, and carries the invoker identity as its verified
// email. Every other call answers 401 Unauthorized, and the reason is logged.
//
// The stack hands the service the invoker identity's email in InvokerVariable
// (APP_SCHEDULER_INVOKER). FromEnvironment reads it when the application starts; with
// the variable empty the scheduled routes are off, which the start logs, and every
// scheduled call is refused. A pull-request stack, which schedules nothing, sets no
// invoker.
package scheduled

import (
	"context"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/idtoken"
)

const (
	// Prefix is the path the generated router mounts every scheduled route under. The
	// router reserves it: no outlet's prefix and no browser application's mount path may
	// sit there.
	Prefix = "/_scheduled"
	// InvokerVariable is the environment variable holding the email of the invoker
	// identity, the service account whose tokens Cloud Scheduler presents. The stack sets
	// it on the service in every environment that schedules a route.
	InvokerVariable = "APP_SCHEDULER_INVOKER"
)

// The claims of a Google-signed ID token the guard reads beside the verified ones.
const (
	emailClaim         = "email"
	emailVerifiedClaim = "email_verified"
	bearerPrefix       = "Bearer "
)

// googleIssuers are the issuers a token Google signs carries: Google's accounts service,
// with and without its scheme.
var googleIssuers = []string{"https://accounts.google.com", "accounts.google.com"}

// Verifier checks an ID token against Google's signing keys: its signature, its expiry,
// and its audience, answering the token's claims. google.golang.org/api/idtoken's
// Validator is the production one; Fake stands in for it in tests.
type Verifier interface {
	Validate(ctx context.Context, token, audience string) (*idtoken.Payload, error)
}

// Guard admits Cloud Scheduler's calls to the scheduled routes: a call carrying a token
// the verifier accepts for the endpoint's URL, issued by Google, whose verified email is
// the invoker identity. A guard with no invoker refuses every call, and so does a nil
// guard.
type Guard struct {
	invoker  string
	verifier Verifier
}

// NewGuard is a guard admitting the calls whose token carries invoker as its verified
// email, checked by verifier. An empty invoker turns the scheduled routes off.
func NewGuard(invoker string, verifier Verifier) *Guard {
	return &Guard{invoker: invoker, verifier: verifier}
}

// FromEnvironment is the guard the process's environment configures: the invoker named
// in InvokerVariable, its tokens checked against Google's signing keys. With the variable
// empty the scheduled routes are off: the guard refuses every call, and this logs so.
func FromEnvironment(ctx context.Context) (*Guard, error) {
	invoker := os.Getenv(InvokerVariable)
	if invoker == "" {
		logger.FromCtx(ctx).Infof("scheduled: %s is empty, so the scheduled routes are off: every call under %s is refused", InvokerVariable, Prefix)

		return NewGuard("", nil), nil
	}
	validator, err := idtoken.NewValidator(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "idtoken.NewValidator()")
	}
	logger.FromCtx(ctx).Infof("scheduled: the scheduled routes under %s answer the calls carrying a Google-signed token of %s", Prefix, invoker)

	return NewGuard(invoker, validator), nil
}

// Enabled reports whether the guard admits any call: it names an invoker and has a
// verifier to check the invoker's tokens with.
func (g *Guard) Enabled() bool {
	return g != nil && g.invoker != "" && g.verifier != nil
}

// Middleware admits a call the guard accepts to next and answers any other 401
// Unauthorized, logging why. The answer never says why: a caller probing the route
// learns nothing from it.
func (g *Guard) Middleware(next http.Handler) http.Handler {
	return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
		if err := g.Check(r); err != nil {
			return httpio.NewEncoder(w).ClientMessage(r.Context(), httpio.NewUnauthorizedMessageWithError(err, "a scheduled route answers Cloud Scheduler's calls alone"))
		}
		next.ServeHTTP(w, r)

		return nil
	})
}

// Check answers why the guard refuses the call, or nil when it admits it.
func (g *Guard) Check(r *http.Request) error {
	if !g.Enabled() {
		return errors.Newf("scheduled: %s %s refused: the scheduled routes are off (%s is empty)", r.Method, r.URL.Path, InvokerVariable)
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), bearerPrefix)
	if !ok || token == "" {
		return errors.Newf("scheduled: %s %s refused: the call carries no bearer token", r.Method, r.URL.Path)
	}
	audience := Audience(r)
	payload, err := g.verifier.Validate(r.Context(), token, audience)
	if err != nil {
		return errors.Wrapf(err, "scheduled: %s %s refused: the token does not verify for the audience %s", r.Method, r.URL.Path, audience)
	}
	if !slices.Contains(googleIssuers, payload.Issuer) {
		return errors.Newf("scheduled: %s %s refused: the token's issuer %q is not Google's", r.Method, r.URL.Path, payload.Issuer)
	}
	if verified, _ := payload.Claims[emailVerifiedClaim].(bool); !verified {
		return errors.Newf("scheduled: %s %s refused: the token carries no verified email", r.Method, r.URL.Path)
	}
	if email, _ := payload.Claims[emailClaim].(string); email != g.invoker {
		return errors.Newf("scheduled: %s %s refused: the token's email %q is not the invoker %q", r.Method, r.URL.Path, email, g.invoker)
	}

	return nil
}

// Audience is the audience a scheduled call's token carries: the endpoint's URL as Cloud
// Scheduler calls it, https, the host the call names and the route's path, with no query.
// The stack mints the token for that same URL.
func Audience(r *http.Request) string {
	return "https://" + r.Host + r.URL.Path
}
