package staff

import (
	"net/http"

	"github.com/cccteam/access"
	"github.com/cccteam/session/google"
	"github.com/cccteam/session/signin"
	"github.com/go-playground/errors/v5"
)

// DirectoryRegistration is the name the directory registration goes under on the Auth:
// the segment of its login and callback routes (user/directory/login). In development
// the simulated directory signs it in as APP_DIRECTORY_USERNAME, or the shared
// APP_USERNAME when that is unset, with the roles APP_DIRECTORY_ROLES or APP_ROLES name.
const DirectoryRegistration = "directory"

// DirectorySettings is how the application is known to Google for the directory
// registration, read from the environment. Under the session library's skipAuth build
// tag the directory is simulated and only RedirectURL, HostedDomain and GroupPrefix are
// read.
type DirectorySettings struct {
	// ClientID is the OAuth client's id.
	ClientID string
	// ClientSecret is the OAuth client's secret.
	ClientSecret string
	// RedirectURL is the browser-facing callback Google returns the browser to:
	// <base>/api/user/directory/callback.
	RedirectURL string
	// HostedDomain is the Workspace's domain, checked against the verified hd claim.
	HostedDomain string
	// GroupPrefix is the local-part prefix of the Google Groups that carry roles: a group
	// <GroupPrefix><role>@<domain> assigns <role>. It is never empty, since it is the only
	// filter between role groups and the rest of the directory.
	GroupPrefix string
	// GroupLookup is how far the groups lookup reaches: "direct" (the default when empty)
	// reads the groups the person is a direct member of, and "nested" climbs from those to
	// the groups they are in, level by level, for a directory that nests its role groups.
	GroupLookup string
}

// DirectoryLogin sends the browser to Google, to sign in with an account of the
// Workspace.
func (h *Handlers) DirectoryLogin() http.HandlerFunc {
	return h.directory.Login()
}

// DirectoryCallback receives the browser back from Google, resolves the identity,
// reconciles the person's roles to their groups and starts the session.
func (h *Handlers) DirectoryCallback() http.HandlerFunc {
	return h.directory.Callback()
}

// registerDirectory registers the directory registration on the Auth, before the first
// request. The directory is the authority for role membership (google.RoleSync): every
// login reconciles the person's roles to the Google Groups the directory places them in,
// those named by the group prefix, and a login naming no known role is refused.
func registerDirectory(authSession *Session, accessClient *access.Client, settings *Settings) (*google.Method, error) {
	lookup, err := google.ParseGroupLookup(settings.Directory.GroupLookup)
	if err != nil {
		return nil, errors.Wrap(err, "google.ParseGroupLookup()")
	}

	return google.SignIn(authSession, DirectoryRegistration, nil, &google.Config{
		Workspaces:   google.Workspace(Name, settings.Directory.HostedDomain),
		ClientID:     settings.Directory.ClientID,
		ClientSecret: settings.Directory.ClientSecret,
		RedirectURL:  settings.Directory.RedirectURL,
		RoleSync:     google.RoleSync(accessClient.UserManager(), settings.Directory.GroupPrefix, lookup),
		Accounts:     signin.MembersOfDirectory,
		LoginURL:     settings.LoginURL,
	}), nil
}
