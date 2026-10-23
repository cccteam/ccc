package staff

import (
	"net/http"

	"github.com/cccteam/session"
)

// PasswordRegistration is the name the password method is registered under on the Auth,
// always password: the segment of its login route (user/password/login). A password
// sign-in is checked against the user's hash in the users table; the bootstrap creates
// the development logins with their passwords, and a deployed environment starts from the
// administrator the migrate command seeds (foothold.go), the rest coming from its
// administration surface and from the invitations it mails.
const PasswordRegistration = "password"

// PasswordLogin checks a username and password and starts the session.
func (h *Handlers) PasswordLogin() http.HandlerFunc {
	return h.password.Login()
}

// PasswordChange changes the signed-in user's password, given the old one.
func (h *Handlers) PasswordChange() http.HandlerFunc {
	return h.password.ChangeUserPassword()
}

// InvitationSignUp accepts an invitation as a stranger with a new password: the invitee
// becomes a user under the sign-in name they give, with the
// address verified by the link, seated in the roles the invitation names.
func (h *Handlers) InvitationSignUp() http.HandlerFunc {
	return h.Invitation().SignUp()
}

// registerPassword registers the password method on the Auth, before the first request.
func registerPassword(authSession *Session) *session.PasswordMethod {
	return session.PasswordSignIn(authSession)
}
