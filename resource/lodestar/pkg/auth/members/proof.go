package members

import (
	"net/http"
	"strings"

	"github.com/cccteam/access"
	"github.com/cccteam/session/azure"
	"github.com/cccteam/session/google"
	"github.com/cccteam/session/signin"
	"github.com/go-playground/errors/v5"
)

// The proof registrations: one per organization-slot shape of the session library's
// sign-in rules, registered on the members Auth beside the directory, so the portal's
// login page offers each as a button and a deployment against the real identity
// providers proves every shape: a positive sign-in and its key refusals, the identity
// keyed as the slot names it, roles synchronized where the shape allows it, and a logout
// started at Microsoft ending the right session. They are a proof harness on a proof
// branch, not part of the members' story: the directory stays the members' way in.
const (
	// EntraRegistration admits the lab tenant alone, the application that belongs to one
	// organization, served at the tenant's own endpoint, with role synchronization from
	// the token's application roles.
	EntraRegistration = "entra"
	// EntraListedRegistration admits the lab tenant and the second tenant, each under the
	// name every link made through it carries as its tenant key.
	EntraListedRegistration = "entra-listed"
	// EntraAnyRegistration admits the work or school account of any tenant, for the
	// people pre-created under their user principal names (invited users only).
	EntraAnyRegistration = "entra-any"
	// EntraPersonalRegistration admits the lab tenant and Microsoft personal accounts,
	// for the people pre-created under their sign-in names (invited users only).
	EntraPersonalRegistration = "entra-personal"
	// GoogleAnyInvitedRegistration admits any Google account, for the people pre-created
	// under their verified email (invited users only).
	GoogleAnyInvitedRegistration = "google-any-invited"
	// GoogleAnyOpenRegistration admits any Google account and signs anyone up.
	GoogleAnyOpenRegistration = "google-any-open"
	// GoogleListedRegistration admits the lab Workspace and the second one, each under
	// the name every link made through it carries.
	GoogleListedRegistration = "google-listed"
)

// The names the listed shapes give their organizations, which every link made through
// them carries as its tenant key.
const (
	labTenantName       = "lab-a"
	secondTenantName    = "lab-b"
	labWorkspaceName    = "lab"
	secondWorkspaceName = "second"
)

// placeholderSecondDomain stands in for the second Workspace's domain when the
// environment names none: the listed shape still registers and refuses every account
// outside the lab Workspace, and the checks that need the second Workspace cannot run.
const placeholderSecondDomain = "second.invalid"

// directoryCallbackSuffix is the tail of the directory's callback URL, which every proof
// registration's callback replaces with its own name.
const directoryCallbackSuffix = "/user/" + DirectoryRegistration + "/callback"

// ProofSettings are the proof registrations' settings: the two tenants, the two Entra
// application registrations (one for the lab tenant alone, one admitting any
// organization and personal accounts) and the second Workspace's domain. The Google
// proof registrations share the directory's OAuth client, and every proof registration's
// callback is the directory's with its own name in place of the directory's.
type ProofSettings struct {
	// TenantID is the lab tenant; SecondTenantID the second test tenant.
	TenantID       string
	SecondTenantID string
	// ClientID and ClientSecret are the single-tenant application registration's.
	ClientID     string
	ClientSecret string
	// MultiClientID and MultiClientSecret are the application registration's that admits
	// any organization and personal accounts.
	MultiClientID     string
	MultiClientSecret string
	// GoogleSecondDomain is the second Workspace's domain, empty when there is none.
	GoogleSecondDomain string
}

// proofMethods are the registered proof methods.
type proofMethods struct {
	entra            *azure.Method
	entraListed      *azure.Method
	entraAny         *azure.Method
	entraPersonal    *azure.Method
	googleAnyInvited *google.Method
	googleAnyOpen    *google.Method
	googleListed     *google.Method
}

// registerProof registers the proof methods on the Auth, before the first request.
func registerProof(auth *Session, accessClient *access.Client, settings *Settings) (*proofMethods, error) {
	callback, err := proofCallback(settings.Directory.RedirectURL)
	if err != nil {
		return nil, err
	}
	p := &settings.Proof

	// The lab tenant alone, at its own endpoint: the one shape role synchronization
	// follows, since a token's application roles are asserted by the tenant that issued it.
	oneTenant := &azure.Config{
		Tenants:      azure.Tenant(p.TenantID),
		ClientID:     p.ClientID,
		ClientSecret: p.ClientSecret,
		RedirectURL:  callback(EntraRegistration),
		RoleSync:     signin.RoleSync(accessClient.UserManager()),
		Accounts:     signin.MembersOfDirectory,
		LoginURL:     settings.LoginURL,
	}
	listedTenants := multiTenant(p, callback(EntraListedRegistration), settings.LoginURL,
		azure.Tenants(azure.NamedTenant(p.TenantID, labTenantName), azure.NamedTenant(p.SecondTenantID, secondTenantName)),
		signin.MembersOfDirectory)
	anyTenant := multiTenant(p, callback(EntraAnyRegistration), settings.LoginURL, azure.AnyTenant(), signin.InvitedUsersOnly)
	personal := multiTenant(p, callback(EntraPersonalRegistration), settings.LoginURL,
		azure.Tenants(azure.Tenant(p.TenantID), azure.PersonalAccounts()), signin.InvitedUsersOnly)

	secondDomain := p.GoogleSecondDomain
	if secondDomain == "" {
		secondDomain = placeholderSecondDomain
	}
	listedWorkspaces := &google.Config{
		ClientID:     settings.Directory.ClientID,
		ClientSecret: settings.Directory.ClientSecret,
		RedirectURL:  callback(GoogleListedRegistration),
		Workspaces: google.Workspaces(
			google.Workspace(labWorkspaceName, settings.Directory.HostedDomain),
			google.Workspace(secondWorkspaceName, secondDomain),
		),
		RoleSync: google.DisableRoleSync(),
		Accounts: signin.MembersOfDirectory,
		LoginURL: settings.LoginURL,
	}

	return &proofMethods{
		entra:            azure.SignIn(auth, EntraRegistration, nil, oneTenant),
		entraListed:      azure.SignIn(auth, EntraListedRegistration, nil, listedTenants),
		entraAny:         azure.SignIn(auth, EntraAnyRegistration, nil, anyTenant),
		entraPersonal:    azure.SignIn(auth, EntraPersonalRegistration, nil, personal),
		googleAnyInvited: google.SignIn(auth, GoogleAnyInvitedRegistration, nil, anyGoogle(settings, callback(GoogleAnyInvitedRegistration), signin.InvitedUsersOnly)),
		googleAnyOpen:    google.SignIn(auth, GoogleAnyOpenRegistration, nil, anyGoogle(settings, callback(GoogleAnyOpenRegistration), signin.OpenSignUp)),
		googleListed:     google.SignIn(auth, GoogleListedRegistration, nil, listedWorkspaces),
	}, nil
}

// multiTenant is an Entra proof registration served through Microsoft's shared endpoints
// under the application registration that admits any organization and personal accounts,
// with role synchronization off, as the library requires beyond one tenant.
func multiTenant(p *ProofSettings, redirectURL, loginURL string, tenants azure.TenantSet, accounts signin.StandardAccountRule) *azure.Config {
	return &azure.Config{
		Tenants:      tenants,
		ClientID:     p.MultiClientID,
		ClientSecret: p.MultiClientSecret,
		RedirectURL:  redirectURL,
		RoleSync:     signin.DisableRoleSync(),
		Accounts:     accounts,
		LoginURL:     loginURL,
	}
}

// anyGoogle is a Google proof registration admitting any Google account on the directory's
// OAuth client, with role synchronization off, as the library requires beyond one Workspace.
func anyGoogle(settings *Settings, redirectURL string, accounts signin.StandardAccountRule) *google.Config {
	return &google.Config{
		ClientID:     settings.Directory.ClientID,
		ClientSecret: settings.Directory.ClientSecret,
		RedirectURL:  redirectURL,
		Workspaces:   google.AnyGoogleAccount(),
		RoleSync:     google.DisableRoleSync(),
		Accounts:     accounts,
		LoginURL:     settings.LoginURL,
	}
}

// proofCallback derives each proof registration's callback from the directory's: the same
// origin and prefix with the registration's own name in place of the directory's. A
// process that opens the auth without serving it, such as the migrate step, names no
// callback for the directory, and then none for the proof registrations either.
func proofCallback(directoryRedirectURL string) (func(name string) string, error) {
	if directoryRedirectURL == "" {
		return func(string) string {
			return ""
		}, nil
	}
	base, ok := strings.CutSuffix(directoryRedirectURL, directoryCallbackSuffix)
	if !ok {
		return nil, errors.Newf("members.Settings.Directory.RedirectURL %q does not end in %s, which the proof registrations' callbacks are derived from",
			directoryRedirectURL, directoryCallbackSuffix)
	}

	return func(name string) string {
		return base + "/user/" + name + "/callback"
	}, nil
}

// The proof registrations' handlers, under the accessor names the generated router
// expects for each registration.

// EntraLogin sends the browser to Microsoft for the lab tenant.
func (h *Handlers) EntraLogin() http.HandlerFunc {
	return h.proof.entra.Login()
}

// EntraCallback receives the browser back from Microsoft for the lab tenant.
func (h *Handlers) EntraCallback() http.HandlerFunc {
	return h.proof.entra.Callback()
}

// EntraFrontChannelLogout ends the sessions a logout started at Microsoft names, for the
// lab tenant.
func (h *Handlers) EntraFrontChannelLogout() http.HandlerFunc {
	return h.proof.entra.FrontChannelLogout()
}

// EntraListedLogin sends the browser to Microsoft for the listed tenants.
func (h *Handlers) EntraListedLogin() http.HandlerFunc {
	return h.proof.entraListed.Login()
}

// EntraListedCallback receives the browser back from Microsoft for the listed tenants.
func (h *Handlers) EntraListedCallback() http.HandlerFunc {
	return h.proof.entraListed.Callback()
}

// EntraListedFrontChannelLogout ends the sessions a logout started at Microsoft names,
// for the listed tenants.
func (h *Handlers) EntraListedFrontChannelLogout() http.HandlerFunc {
	return h.proof.entraListed.FrontChannelLogout()
}

// EntraAnyLogin sends the browser to Microsoft for any tenant.
func (h *Handlers) EntraAnyLogin() http.HandlerFunc {
	return h.proof.entraAny.Login()
}

// EntraAnyCallback receives the browser back from Microsoft for any tenant.
func (h *Handlers) EntraAnyCallback() http.HandlerFunc {
	return h.proof.entraAny.Callback()
}

// EntraAnyFrontChannelLogout ends the sessions a logout started at Microsoft names, for
// any tenant.
func (h *Handlers) EntraAnyFrontChannelLogout() http.HandlerFunc {
	return h.proof.entraAny.FrontChannelLogout()
}

// EntraPersonalLogin sends the browser to Microsoft for the lab tenant and personal
// accounts.
func (h *Handlers) EntraPersonalLogin() http.HandlerFunc {
	return h.proof.entraPersonal.Login()
}

// EntraPersonalCallback receives the browser back from Microsoft for the lab tenant and
// personal accounts.
func (h *Handlers) EntraPersonalCallback() http.HandlerFunc {
	return h.proof.entraPersonal.Callback()
}

// EntraPersonalFrontChannelLogout ends the sessions a logout started at Microsoft names,
// for the lab tenant and personal accounts.
func (h *Handlers) EntraPersonalFrontChannelLogout() http.HandlerFunc {
	return h.proof.entraPersonal.FrontChannelLogout()
}

// GoogleAnyInvitedLogin sends the browser to Google for any account, invited users only.
func (h *Handlers) GoogleAnyInvitedLogin() http.HandlerFunc {
	return h.proof.googleAnyInvited.Login()
}

// GoogleAnyInvitedCallback receives the browser back from Google for any account, invited
// users only.
func (h *Handlers) GoogleAnyInvitedCallback() http.HandlerFunc {
	return h.proof.googleAnyInvited.Callback()
}

// GoogleAnyOpenLogin sends the browser to Google for any account, open sign-up.
func (h *Handlers) GoogleAnyOpenLogin() http.HandlerFunc {
	return h.proof.googleAnyOpen.Login()
}

// GoogleAnyOpenCallback receives the browser back from Google for any account, open
// sign-up.
func (h *Handlers) GoogleAnyOpenCallback() http.HandlerFunc {
	return h.proof.googleAnyOpen.Callback()
}

// GoogleListedLogin sends the browser to Google for the listed Workspaces.
func (h *Handlers) GoogleListedLogin() http.HandlerFunc {
	return h.proof.googleListed.Login()
}

// GoogleListedCallback receives the browser back from Google for the listed Workspaces.
func (h *Handlers) GoogleListedCallback() http.HandlerFunc {
	return h.proof.googleListed.Callback()
}
