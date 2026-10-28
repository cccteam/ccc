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

import (
	"context"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/session/google"
	"github.com/cccteam/session/mail"
	"github.com/cccteam/session/sessioninfo"
	"github.com/cccteam/session/signin"
)

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

// Mail is how an auth sends mail: the invitations its administrators send, the link
// that verifies an address a person adds, and the link a password sign-up starts from.
// The sender is the configuration's one driver, shared by both auths (the Google
// Workspace relay as the configured mailbox, or the library's logging stand-in in
// development); LoginURL is the absolute URL of the login page of the surface that
// binds to the auth, where every mailed link leads with its token in the query.
type Mail struct {
	Sender   mail.Sender
	LoginURL string
}

// Directory is an auth's registration with Google: the OAuth client's credentials, the
// callback the directory returns the browser to, the Workspace domain (hosted domain)
// sign-in is restricted to, whether the fleet administers the Workspace, and the groups
// lookup that carries role membership. Under the session library's skipAuth build tag
// the directory is simulated: the sign-in is the person the environment names for the
// registration (APP_<NAME>_USERNAME, else APP_USERNAME) in the groups it names
// (APP_<NAME>_ROLES, else APP_ROLES), and only RedirectURL, HostedDomain, Administered
// and GroupPrefix are read.
type Directory struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	// Connection is the application's own name for the Google Workspace organization the
	// directory is: it keys every Google identity link, so it is set once and kept.
	Connection   string
	HostedDomain string
	// Administered says the fleet administers the Workspace and knows it enforces a
	// second factor at every sign-in (2-Step Verification enforcement), so the
	// registration declares the Workspace attesting (google.WorkspaceSet.Attesting): a
	// sign-in through it carries the Workspace's second factor as the session's
	// attested-by-declaration event and is never asked for the sign-in code. Google
	// puts no evidence of a second factor in its tokens, so the declaration is the
	// only way a Google sign-in attests; a Workspace the fleet does not administer
	// leaves it unset, and its people enter the code.
	Administered bool
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

// Workspace is the directory as the Google method's organization slot: the one
// Workspace, named by the connection and checked against the hosted domain, declared
// attesting when the fleet administers it.
//
// Demonstrates: auth.attested-directory.
func (d *Directory) Workspace() google.WorkspaceSet {
	workspace := google.Workspace(d.Connection, d.HostedDomain)
	if d.Administered {
		return workspace.Attesting()
	}

	return workspace
}

// RequireSecondFactor is the last word of both auths' sign-in rules, chained after the
// tenant policy (signin.Policy.Then): every session carries a second factor, with the
// sign-in code the library mails as the factor the login page runs
// (signin.RequireMFA(sessioninfo.MethodEmailCode)). The library satisfies it before it
// holds the sign-in: a directory the fleet administers attests for its people
// (Directory.Administered), a browser the person asked to be remembered after a code
// stands in for the next thirty days, and otherwise the sign-in waits for the code the
// person reads from their mail. The requirement is the rule's, not a tenant's, so no
// sector turns it off.
//
// Demonstrates: auth.second-factor.
func RequireSecondFactor(context.Context, *cloudspanner.ReadWriteTransaction, *sessioninfo.NewSessionRequest) (*signin.Decision, error) {
	return signin.RequireMFA(sessioninfo.MethodEmailCode), nil
}
