package live

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
	"github.com/cccteam/logger"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-playground/errors/v5"
)

// The refusals a subscribing request can meet before any handler runs.
const (
	notSessionOutletMsg    = SubscribeHeader + " is not served on this outlet: it serves no browser sessions"
	malformedTabMessageFmt = "invalid " + SubscribeHeader + " value: a tab id is 1 to 64 characters of [A-Za-z0-9_-]"
)

// Tab reads the subscribe header: the tab id when the request subscribes, the empty
// string when it does not, and a 400 error when the header's value is not a tab id.
func Tab(r *http.Request) (string, error) {
	tab := r.Header.Get(SubscribeHeader)
	if tab == "" {
		return "", nil
	}
	if !ValidTab(tab) {
		return "", httpio.NewBadRequestMessage(malformedTabMessageFmt)
	}

	return tab, nil
}

// authKey is the request context key Subscribing binds the auth's name under.
type authKey struct{}

// Subscribing is the middleware a session-serving outlet's generated routes run under.
// auth is the name of the auth whose sessions the outlet serves (the auth package's
// Name); every request has it bound, so the live layer keys the session principal by
// it (PrincipalID). A request carrying the subscribe header has the tab noted on its
// request log line and is refused when the tab is malformed; a request without the
// header passes on with nothing else done. An auth name that is empty, or that carries
// AuthSeparator or a "/", panics as the routes are registered, so a server never
// starts with a name that cannot begin a principal id.
func Subscribing(auth string) func(http.Handler) http.Handler {
	if err := checkAuthName(auth); err != nil {
		panic(err.Error())
	}

	return func(next http.Handler) http.Handler {
		return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
			r = r.WithContext(context.WithValue(r.Context(), authKey{}, auth))
			if r.Header.Get(SubscribeHeader) == "" {
				next.ServeHTTP(w, r)

				return nil
			}
			tab, err := Tab(r)
			if err != nil {
				return httpio.NewEncoder(w).ClientMessage(r.Context(), err)
			}
			logger.FromReq(r).AddRequestAttribute(LogAttribute, tab)
			next.ServeHTTP(w, r)

			return nil
		})
	}
}

// checkAuthName refuses an auth name that cannot begin a principal id: an empty one, one
// carrying AuthSeparator, which would make the boundary between the auth and the user
// name ambiguous, and one carrying "/", which no Firestore document id holds.
func checkAuthName(auth string) error {
	switch {
	case auth == "":
		return errors.New("live.Subscribing(): the auth's name is empty; pass the name of the auth whose sessions the outlet serves, the auth package's Name")
	case strings.Contains(auth, AuthSeparator):
		return errors.Newf("live.Subscribing(%q): an auth's name cannot carry %q, which separates it from the user name in a principal id", auth, AuthSeparator)
	case strings.Contains(auth, "/"):
		return errors.Newf("live.Subscribing(%q): an auth's name cannot carry \"/\", which no Firestore document id holds", auth)
	}

	return nil
}

// Refusing is the middleware an API-key outlet's generated routes run under: the
// outlet serves no browser sessions, so a request carrying the subscribe header is
// refused naming the header, after the tab is noted on its request log line. A machine
// client wanting change notification is a different consumer on a topic.
func Refusing() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
			if r.Header.Get(SubscribeHeader) == "" {
				next.ServeHTTP(w, r)

				return nil
			}
			logger.FromReq(r).AddRequestAttribute(LogAttribute, r.Header.Get(SubscribeHeader))

			return httpio.NewEncoder(w).ClientMessage(r.Context(), httpio.NewBadRequestMessage(notSessionOutletMsg))
		})
	}
}

// Gate is the endpoint gate of a decoded request: whether the required permission is
// granted, or granted under a condition, on the resource in the request's scope. A
// decoded QuerySet is one.
type Gate interface {
	Permitted(ctx context.Context) (bool, error)
}

// Subscribe registers the request's subscription when the request carries the
// subscribe header and its gate admits it, before the handler runs the query: a commit
// that lands during the query then finds the subscription. A refused request registers
// nothing. A record that fails to take the subscription is logged and the request is
// still answered: the live layer is best effort end to end, and a data request never
// fails over it.
func Subscribe(ctx context.Context, r *http.Request, svc Service, gate Gate, sub *Subscription) {
	tab, err := Tab(r)
	if err != nil || tab == "" {
		// A malformed tab was refused by the outlet's middleware before the handler ran.
		return
	}
	permitted, err := gate.Permitted(ctx)
	if err != nil {
		logger.FromCtx(ctx).Errorf("live: the gate check for the %s subscription of tab %s failed; the subscription is not registered: %v", sub.Resource, tab, err)

		return
	}
	if !permitted {
		return
	}
	principal, err := PrincipalID(ctx)
	if err != nil {
		logger.FromCtx(ctx).Errorf("live: the %s subscription of tab %s is not registered; the page is served without it: %v", sub.Resource, tab, err)

		return
	}
	registered := sub.Normalized()
	registered.Principal = principal
	registered.Tab = tab
	registered.Expiry = time.Now().Add(SubscriptionTTL)
	ctx, cancel := context.WithTimeout(ctx, RecordTimeout)
	defer cancel()
	if err := svc.Register(ctx, []Subscription{registered}); err != nil {
		logger.FromCtx(ctx).Errorf("live: registering the %s subscription of tab %s failed; the page is served without it: %v", sub.Resource, tab, err)
	}
}

// SetCacheControl marks a list or read response cacheable by the browser when its
// request carried the version parameter: Cache-Control private with the subscription's
// window, and the no-cache Pragma and Expires the outlet's NoCaching middleware set are
// dropped so nothing contradicts it. Every other response is left uncached.
func SetCacheControl(w http.ResponseWriter, r *http.Request) {
	if !r.URL.Query().Has(VersionParam) {
		return
	}
	header := w.Header()
	header.Set("Cache-Control", CacheControl)
	header.Del("Pragma")
	header.Del("Expires")
}

// Publish writes a committed request's changes into its subscribers' change sets, after
// the commit and before the response: the rows the request's transactions wrote, each
// group under the domain its patch was decoded in (domain is the request's route
// domain, which rows written by hand take). It is bounded by PublishTimeout, a failure
// is logged, and the request answers either way. Nothing happens for a request that
// committed nothing.
func Publish(ctx context.Context, svc Service, domain accesstypes.Domain, touched *resource.TouchedRows) {
	if touched.Empty() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, PublishTimeout)
	defer cancel()
	for changeDomain, rows := range touched.Rows(domain) {
		if err := svc.Publish(ctx, changeDomain, rows); err != nil {
			logger.FromCtx(ctx).Errorf("live: publishing the changes of domain %q failed; the pages refetch at their next change: %v", changeDomain, err)
		}
	}
}

// PrincipalID is the session principal's id as the subscription record, the change set
// and the browser's identity carry it: the name of the auth the request came through
// (bound by Subscribing), AuthSeparator, and the user name for a user principal (an
// ordinary session, or an impersonated user) or the role prefixed with "role:" for a
// session established as a role. A password user alice of the crew auth is crew|alice,
// a directory user of the members auth is members|alice@example.com, and a session
// established as the Auditor role through the crew auth is crew|role:Auditor, so two
// auths over one database never share an id.
//
// Nothing is truncated or hashed. An id longer than MaxPrincipalIDLength, Firebase's
// uid limit, fails with a 403 client message whose cause names the id, its length and
// the limit, so the person uses the application while live pages refuse; a request no
// auth was bound to fails as a server error, since its principal could not be told from
// another auth's.
func PrincipalID(ctx context.Context) (string, error) {
	auth, ok := ctx.Value(authKey{}).(string)
	if !ok {
		return "", errors.New("live.PrincipalID(): no auth is bound to the request; the outlet's generated routes bind it (live.Subscribing)")
	}
	id := auth + AuthSeparator + principalName(ctx)
	if len(id) > MaxPrincipalIDLength {
		cause := errors.Newf("live.PrincipalID(): the principal id %q is %d bytes, over Firebase's uid limit of %d", id, len(id), MaxPrincipalIDLength)

		return "", httpio.NewForbiddenMessageWithErrorf(cause, "live pages are not served to this sign-in: its principal id is %d bytes, over the limit of %d", len(id), MaxPrincipalIDLength)
	}

	return id, nil
}

// principalName is the session principal as its id names it after the auth: the user
// name for a user principal, the role marked "role:" for a session established as a role.
func principalName(ctx context.Context) string {
	principal := sessioninfo.PrincipalFromCtx(ctx)
	if role, ok := principal.Role(); ok {
		return rolePrefix + string(role)
	}
	user, _ := principal.User()

	return string(user)
}
