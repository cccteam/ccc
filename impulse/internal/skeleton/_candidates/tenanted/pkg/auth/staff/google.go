package staff

import (
	"net/http"
	"strings"

	"github.com/cccteam/access"
	"github.com/cccteam/session/google"
	"github.com/cccteam/session/signin"
)

// GoogleRegistration is the name the google registration goes under on the Auth: the
// segment of its login and callback routes (user/google/login), and the connection every
// link made through its Workspace carries. In development the simulated directory signs
// it in as APP_GOOGLE_USERNAME, or the shared APP_USERNAME when that is unset, with the
// groups APP_GOOGLE_ROLES or APP_ROLES name.
const GoogleRegistration = "google"

// GoogleSettings is how the application is known to Google for the google
// registration, read from the environment. Under the session library's skipAuth build tag
// Google is simulated and only the fields the simulated sign-in presents are read.
type GoogleSettings struct {
	// AdministeredWorkspaces is the Workspaces the deployment administers, by domain, comma-separated, each one the registration admits: a sign-in from one carries the Workspace's own second factor, which the deployment vouches for (2-Step Verification enforced for everyone), and is not asked for the sign-in code; empty when the deployment administers none.
	AdministeredWorkspaces string
	// ClientID is the OAuth client's id.
	ClientID string
	// ClientSecret is the OAuth client's secret.
	ClientSecret string
	// RedirectURL is the browser-facing callback Google returns the browser to: <base>/<outlet>/api/user/google/callback.
	RedirectURL string
}

// GoogleLogin sends the browser to Google, to sign in with an account the
// registration admits.
func (h *Handlers) GoogleLogin() http.HandlerFunc {
	return h.google.Login()
}

// GoogleCallback receives the browser back from Google, resolves the identity,
// applies the account rule and starts the session. Google has no directory-initiated
// logout, so there is no front-channel route: the session's own logout route ends it.
func (h *Handlers) GoogleCallback() http.HandlerFunc {
	return h.google.Callback()
}

// registerGoogle registers the google registration on the Auth, before the first
// request.
// The application is the authority for role membership (google.DisableRoleSync): Google
// proves who someone is and the application decides what they may do, so a sign-in
// neither reads nor changes the person's roles.
// The account rule for an identity Google verifies for the first time is the library's
// standard one, open sign-up: anyone the slot admits becomes a user under their
// verified email.
// The slot admits any Google account, with any Workspace named beside it: a Workspace
// account's identity then carries its Workspace's domain as its connection, which is
// what a tenant's declaration names a Google directory by, and an account outside a
// Workspace carries the provider itself (google.AnyAccountConnection).
func registerGoogle(authSession *Session, accessClient *access.Client, settings *Settings) *google.Method {
	workspaces := googleSlot(settings)
	_ = accessClient

	return google.SignIn(authSession, GoogleRegistration, nil, &google.Config{
		ClientID:     settings.Google.ClientID,
		ClientSecret: settings.Google.ClientSecret,
		RedirectURL:  settings.Google.RedirectURL,
		Workspaces:   workspaces,
		RoleSync:     google.DisableRoleSync(),
		Accounts:     signin.OpenSignUp,
		LoginURL:     settings.LoginURL,
	})
}

// googleSlot is the registration's organization slot, any Google account, with the
// Workspaces the deployment administers declared as attesting (AdministeredWorkspaces):
// a sign-in from one carries the Workspace's own second factor, which the deployment
// vouches for, and is not asked for the sign-in code.
func googleSlot(settings *Settings) google.WorkspaceSet {
	administered := parseGoogleAdministered(settings.Google.AdministeredWorkspaces)
	sets := make([]google.WorkspaceSet, 0, len(administered)+2)
	sets = append(sets, google.AnyWorkspace(), google.AnyGoogleAccount())
	for _, domain := range administered {
		// Named by its domain: the connection a Workspace account's link carries under the
		// open entry, so the links made before the Workspace was declared keep theirs.
		sets = append(sets, google.Workspace(domain, domain).Attesting())
	}

	return google.Workspaces(sets...)
}

// parseGoogleAdministered reads the administered Workspaces from their setting: the
// domains, comma-separated, lower-cased; none for an empty setting.
func parseGoogleAdministered(value string) []string {
	var domains []string
	for _, domain := range strings.Split(value, ",") {
		if domain = strings.ToLower(strings.TrimSpace(domain)); domain != "" {
			domains = append(domains, domain)
		}
	}

	return domains
}
