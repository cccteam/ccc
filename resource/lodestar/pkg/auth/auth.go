// Package auth holds what the auths share: the request-scoped record of which auth a
// request came through, and the policy signal both permission engines take (PolicySignal,
// over the live service). Each auth is its own package beneath this one (crew, members),
// owning its session manager, its permission store, and its roles file. A surface binds a
// request to an auth in the session group that authenticated it, and everything that
// answers for that request (permission checks, tenant visibility) answers from that
// auth's store.
//
// Demonstrates: auth.two-populations.
package auth

import "context"

type ctxKey struct{}

// Bind records the auth a request came through: the name of the auth package whose
// session group authenticated it.
func Bind(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, ctxKey{}, name)
}

// Name returns the auth the request came through, or "" when no session group bound it,
// which every surface treats as its default auth.
func Name(ctx context.Context) string {
	name, _ := ctx.Value(ctxKey{}).(string)

	return name
}

// Directory is an auth's registration with Google: the OAuth client's credentials, the
// callback the directory returns the browser to, the Workspace domain (hosted domain)
// sign-in is restricted to, and the groups lookup that carries role membership. Under
// the session library's skipAuth build tag the directory is simulated: the sign-in is
// the person the environment names for the registration (APP_<NAME>_USERNAME, else
// APP_USERNAME) in the groups it names (APP_<NAME>_ROLES, else APP_ROLES), and only
// RedirectURL, HostedDomain and GroupPrefix are read.
type Directory struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	HostedDomain string
	// GroupPrefix is the local-part prefix of the Google Groups that carry roles: a group
	// <GroupPrefix><role>@<domain> assigns <role>. It is never empty, since it is the only
	// filter between role groups and the rest of the directory.
	GroupPrefix string
	// GroupLookup is how far the groups lookup reaches: "direct" (the default when empty)
	// reads the groups the person is a direct member of, and "nested" climbs from those to
	// the groups they are in, level by level, for a directory that nests its role groups.
	// It has no effect under the skipAuth build tag, where the lookup is simulated.
	GroupLookup string
}
