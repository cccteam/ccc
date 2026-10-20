package members

import (
	"net/http"

	"github.com/cccteam/access"
	"github.com/cccteam/session/azure"
	"github.com/cccteam/session/signin"
)

// DirectoryRegistration is the name the directory registration goes under on the Auth: the
// segment of its login, callback and front-channel logout routes (user/directory/login).
// In development the simulated directory signs it in as APP_DIRECTORY_USERNAME, or the
// shared APP_USERNAME when that is unset, with the roles APP_DIRECTORY_ROLES or APP_ROLES
// name.
const DirectoryRegistration = "directory"

// DirectorySettings is how the application is known to Microsoft for the directory
// registration, read from the environment. Under the session library's skipAuth build tag
// the directory is simulated and only RedirectURL is read: it is where the simulated
// sign-in returns.
type DirectorySettings struct {
	// TenantID is the tenant whose accounts the registration admits, by its tenant id (the tid claim, a GUID); the slot is validated at registration, so the simulated directory needs a well-formed placeholder.
	TenantID string
	// ClientID is the app registration's application (client) id.
	ClientID string
	// ClientSecret is the app registration's client secret.
	ClientSecret string
	// RedirectURL is the browser-facing callback the directory returns the browser to: <base>/<outlet>/api/user/directory/callback.
	RedirectURL string
}

// DirectoryLogin sends the browser to Microsoft, to sign in with an account the
// registration admits.
func (h *Handlers) DirectoryLogin() http.HandlerFunc {
	return h.directory.Login()
}

// DirectoryCallback receives the browser back from Microsoft, resolves the identity,
// applies the account rule and starts the session.
func (h *Handlers) DirectoryCallback() http.HandlerFunc {
	return h.directory.Callback()
}

// DirectoryFrontChannelLogout ends the session Microsoft reports signed out: the
// front-channel logout the app registration names.
func (h *Handlers) DirectoryFrontChannelLogout() http.HandlerFunc {
	return h.directory.FrontChannelLogout()
}

// registerDirectory registers the directory registration on the Auth, before the first
// request.
// The application is the authority for role membership (signin.DisableRoleSync): the
// directory proves who someone is and the application decides what they may do, so a
// sign-in neither reads nor changes the person's roles.
// The account rule for an identity Microsoft verifies for the first time is the
// library's standard one, members of the directory: the person becomes a user under their
// user principal name, with no password, linked by the directory's immutable tenant and
// object ids so a renamed account stays the same person.
func registerDirectory(authSession *Session, accessClient *access.Client, settings *Settings) *azure.Method {
	_ = accessClient

	return azure.SignIn(authSession, DirectoryRegistration, nil, &azure.Config{
		Tenants:      azure.Tenant(settings.Directory.TenantID),
		ClientID:     settings.Directory.ClientID,
		ClientSecret: settings.Directory.ClientSecret,
		RedirectURL:  settings.Directory.RedirectURL,
		RoleSync:     signin.DisableRoleSync(),
		Accounts:     signin.MembersOfDirectory,
		LoginURL:     settings.LoginURL,
	})
}
