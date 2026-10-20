package members

import (
	"net/http"

	"github.com/cccteam/session"
)

// PasswordRegistration is the name the password method is registered under on the Auth,
// always password: the segment of its login route (user/password/login).
const PasswordRegistration = "password"

// PasswordLogin checks a username and password and starts the session.
func (h *Handlers) PasswordLogin() http.HandlerFunc {
	return h.password.Login()
}

// PasswordChange changes the signed-in user's password, given the old one.
func (h *Handlers) PasswordChange() http.HandlerFunc {
	return h.password.ChangeUserPassword()
}

// registerPassword registers the password method on the Auth, before the first request.
func registerPassword(authSession *Session) *session.PasswordMethod {
	return session.PasswordSignIn(authSession)
}
