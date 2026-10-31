package keepers

import (
	"net/http"

	"github.com/cccteam/access"
	"github.com/cccteam/session/oidc"
	"github.com/cccteam/session/signin"
)

// PersonaRegistration is the name the persona registration goes under on the Auth: the
// segment of its login and callback routes (user/persona/login), and the connection every
// link made through the persona provider carries.
const PersonaRegistration = "persona"

// PersonaSettings is how the application is known to the persona provider for the
// persona registration, read from the environment. The provider is configured in the
// first environment and its pull requests alone: an empty Issuer leaves the method
// unregistered, so a later environment never offers it, and nothing in the build differs
// between them.
type PersonaSettings struct {
	// Issuer is the persona provider's issuer URL; a placement value of the first
	// environment and its pull requests, empty everywhere else.
	Issuer string
	// ClientID is the deployment's client ID at the provider, which bedrock derives.
	ClientID string
	// RedirectURL is the browser-facing callback the provider returns the browser to.
	RedirectURL string
}

// PersonaLogin sends the browser to the persona provider, where the person picks a
// persona of this deployment's cast. Nil where the method is not registered.
func (h *Handlers) PersonaLogin() http.HandlerFunc {
	if h.persona == nil {
		return nil
	}

	return h.persona.Login()
}

// PersonaCallback receives the browser back from the provider and starts the session.
// Nil where the method is not registered.
func (h *Handlers) PersonaCallback() http.HandlerFunc {
	if h.persona == nil {
		return nil
	}

	return h.persona.Callback()
}

// registerPersona registers the persona registration on the Auth where the provider is
// configured, and answers nil where it is not.
func registerPersona(authSession *Session, accessClient *access.Client, settings *Settings) *oidc.Method {
	_ = accessClient
	if settings.Persona.Issuer == "" {
		return nil
	}

	return oidc.SignIn(authSession, PersonaRegistration, nil, &oidc.Config{
		Issuer:      settings.Persona.Issuer,
		ClientID:    settings.Persona.ClientID,
		RedirectURL: settings.Persona.RedirectURL,
		Connection:  PersonaRegistration,
		RoleSync:    signin.DisableRoleSync(),
		Accounts:    signin.InvitedUsersOnly(),
		LoginURL:    settings.LoginURL,
	})
}
