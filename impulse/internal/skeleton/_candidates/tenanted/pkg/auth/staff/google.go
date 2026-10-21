package staff

import (
	"net/http"

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
func registerGoogle(authSession *Session, accessClient *access.Client, settings *Settings) *google.Method {
	_ = accessClient

	return google.SignIn(authSession, GoogleRegistration, nil, &google.Config{
		ClientID:     settings.Google.ClientID,
		ClientSecret: settings.Google.ClientSecret,
		RedirectURL:  settings.Google.RedirectURL,
		Workspaces:   google.AnyGoogleAccount(),
		RoleSync:     google.DisableRoleSync(),
		Accounts:     signin.OpenSignUp,
		LoginURL:     settings.LoginURL,
	})
}
