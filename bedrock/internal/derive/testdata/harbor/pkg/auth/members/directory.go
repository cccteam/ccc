package members

import (
	"net/http"
	"strings"

	"github.com/cccteam/access"
	"github.com/cccteam/session/azure"
	"github.com/cccteam/session/signin"
	"github.com/go-playground/errors/v5"
)

// DirectoryRegistration is the name the directory registration goes under on the Auth:
// the segment of its login, callback and front-channel logout routes
// (user/directory/login).
const DirectoryRegistration = "directory"

// DirectorySettings is how the application is known to Microsoft for the directory
// registration, read from the environment. Under the session library's skipAuth build
// tag the directory is simulated and only TenantID and RedirectURL are read.
type DirectorySettings struct {
	// TenantID is the tenant whose accounts the registration admits, by its tenant id
	// (the tid claim, a GUID); the slot is validated at registration.
	TenantID string
	// AdministeredTenants is the tenants the deployment administers, by tenant id,
	// comma-separated, each one the registration admits: a sign-in from one carries the
	// tenant's own second factor, which the deployment vouches for (a Conditional Access
	// policy requiring multifactor authentication of everyone), and is not asked for the
	// sign-in code; empty when the deployment administers none.
	AdministeredTenants string
	// ClientID is the app registration's application (client) id.
	ClientID string
	// ClientSecret is the app registration's client secret.
	ClientSecret string
	// RedirectURL is the browser-facing callback the directory returns the browser to:
	// <base>/portal/api/user/directory/callback.
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
// request. The application is the authority for role membership
// (signin.DisableRoleSync): the directory proves who someone is and the application
// decides what they may do.
func registerDirectory(authSession *Session, accessClient *access.Client, settings *Settings) (*azure.Method, error) {
	tenants, err := directorySlot(settings)
	if err != nil {
		return nil, err
	}
	_ = accessClient

	return azure.SignIn(authSession, DirectoryRegistration, nil, &azure.Config{
		Tenants:      tenants,
		ClientID:     settings.Directory.ClientID,
		ClientSecret: settings.Directory.ClientSecret,
		RedirectURL:  settings.Directory.RedirectURL,
		RoleSync:     signin.DisableRoleSync(),
		Accounts:     signin.MembersOfDirectory,
		LoginURL:     settings.LoginURL,
	}), nil
}

// directorySlot is the registration's organization slot, one tenant (TenantID), with the
// tenants the deployment administers declared as attesting (AdministeredTenants): a
// sign-in from one carries the tenant's own second factor, which the deployment vouches
// for, and is not asked for the sign-in code.
func directorySlot(settings *Settings) (azure.TenantSet, error) {
	tenant := azure.Tenant(settings.Directory.TenantID)
	for _, id := range parseDirectoryAdministered(settings.Directory.AdministeredTenants) {
		if !strings.EqualFold(id, settings.Directory.TenantID) {
			return azure.TenantSet{}, errors.Newf("DirectoryRegistration: the administered tenant %q is not the tenant admitted, %s", id, settings.Directory.TenantID)
		}
		tenant = tenant.Attesting()
	}

	return tenant, nil
}

// parseDirectoryAdministered reads the administered tenants from their setting: the
// tenant ids, comma-separated, lower-cased; none for an empty setting.
func parseDirectoryAdministered(value string) []string {
	var ids []string
	for _, id := range strings.Split(value, ",") {
		if id = strings.ToLower(strings.TrimSpace(id)); id != "" {
			ids = append(ids, id)
		}
	}

	return ids
}
