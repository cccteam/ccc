package config

import (
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
)

// newSession constructs the Auth over tables no migration in this fixture creates, which
// the session-tables check reports, and registers the password method on it.
func newSession(spannerClient any, cookieKey string) (*session.PasswordMethod, error) {
	auth, err := session.NewAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerAuth(spannerClient),
		cookieKey,
		session.WithSessionTableName("LighthouseSessions"),
		session.WithCookieName("lighthouse"),
	)
	if err != nil {
		return nil, err
	}

	return session.PasswordSignIn(auth), nil
}
