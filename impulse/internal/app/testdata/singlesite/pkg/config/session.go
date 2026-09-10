package config

import (
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
)

// newSession constructs the password authenticator over tables no migration in this
// fixture creates, which the auth-wired check reports.
func newSession(spannerClient any, cookieKey string) (*session.PasswordAuth[session.NoCustomData, session.NoCustomData], error) {
	return session.NewPasswordAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerPasswordAuth(spannerClient),
		cookieKey,
		session.WithSessionTableName("LighthouseSessions"),
		session.WithCookieName("lighthouse"),
	)
}
