// signin.go lists the sign-in routes of the generated router: the routes under each
// session outlet that take a credential by POST, which the stack's Cloud Armor policy
// throttles by client address (cloud-armor.tf). They are read from the outlets' Auth
// declarations in the generator programs, as the directory methods' callbacks are
// (auths.go): the password method's login, /<prefix>/user/password/login, where the
// outlet's auth registers one, and the pending sign-in's completion routes,
// /<prefix>/user/pending/confirm (which takes the password) and /cancel, which every
// session outlet mounts. A directory method's login and callback are not among them:
// the directory holds the credential, and its callback carries a code the application
// exchanges, not something a client guesses.

package derive

import (
	"net/http"
	"path"
	"slices"

	"github.com/cccteam/ccc/impulse/app"
)

// SignInRoute is one route of the generated router that takes a sign-in credential by
// POST: a session outlet's password login, or one of its pending sign-in's completion
// routes. Cloud Armor, where an environment turns it on, throttles each by client
// address.
type SignInRoute struct {
	// Outlet is the outlet whose route it is, and Site the site serving that outlet.
	Outlet string
	Site   string
	// Method is the HTTP method the route answers, POST for every sign-in route.
	Method string
	// Path is the route as the router mounts it, under the outlet's prefix
	// (/api/user/password/login).
	Path string
	// Kind is SignInPassword, SignInConfirm or SignInCancel.
	Kind string
}

// The kinds of sign-in route: the password method's login, the pending sign-in's
// confirmation, and its cancel.
const (
	SignInPassword = "password"
	SignInConfirm  = "confirm"
	SignInCancel   = "cancel"
)

// The segments of the generated router's session routes under an outlet's prefix.
const (
	userSegment    = "user"
	loginSegment   = "login"
	pendingSegment = "pending"
	confirmSegment = "confirm"
	cancelSegment  = "cancel"
)

// Declaration says what the route is, for a rule's description: the outlet's password
// login, or its pending sign-in's confirmation or cancel.
func (r *SignInRoute) Declaration() string {
	switch r.Kind {
	case SignInPassword:
		return "the " + r.Outlet + " outlet's password login"
	case SignInConfirm:
		return "the " + r.Outlet + " outlet's pending sign-in confirmation"
	default:
		return "the " + r.Outlet + " outlet's pending sign-in cancel"
	}
}

// signInRoutes lists one outlet's sign-in routes from its declaration: the password
// login for each password method its auth registers, then the pending sign-in's
// confirmation and cancel. None for an outlet that binds no auth: an API-key outlet,
// or one outside the generated router, whose routes bedrock does not know.
func signInRoutes(site string, o *app.Outlet) []SignInRoute {
	if o.Auth == nil {
		return nil
	}
	route := func(kind string, segments ...string) SignInRoute {
		return SignInRoute{Outlet: o.Name, Site: site, Method: http.MethodPost, Path: path.Join(append([]string{"/", o.Prefix, userSegment}, segments...)...), Kind: kind}
	}
	var routes []SignInRoute
	for i := range o.Auth.Methods {
		if m := &o.Auth.Methods[i]; m.Kind == app.MethodPassword {
			routes = append(routes, route(SignInPassword, m.Name, loginSegment))
		}
	}

	return append(routes,
		route(SignInConfirm, pendingSegment, confirmSegment),
		route(SignInCancel, pendingSegment, cancelSegment),
	)
}

// allSignInRoutes is the union of the sites' sign-in routes, the first site's first, a
// route listed once: what Cloud Armor throttles, since a rule is matched by path on
// every host alike.
func (m *Model) allSignInRoutes() []SignInRoute {
	var routes []SignInRoute
	for i := range m.Sites {
		for j := range m.Sites[i].SignInRoutes {
			r := m.Sites[i].SignInRoutes[j]
			if !slices.ContainsFunc(routes, func(have SignInRoute) bool { return have.Method == r.Method && have.Path == r.Path }) {
				routes = append(routes, r)
			}
		}
	}

	return routes
}
