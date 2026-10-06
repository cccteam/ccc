package resource

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/cccteam/httpio"
	"github.com/go-chi/chi/v5"
	"golang.org/x/mod/semver"
)

// APIVersionHeader carries a browser application's release on every request it sends,
// and the server's release on a refusal: a request the server will not answer is
// refused with 412 and this header naming the release the server was built from, so
// the application can pick up the current build.
const APIVersionHeader = "X-Api-Version"

// ThisRelease names the server's own release as an outlet's oldest answered: only an
// application built from the same release is answered. A generator program writes it
// for a release that changes the API inside a maintenance window.
const ThisRelease = "this release"

// APIVersionCheck is what a session outlet's version check compares a request's
// APIVersionHeader against. The generated router builds one per session outlet from
// the server version the application reports and the oldest answered release the
// generator program declared.
type APIVersionCheck struct {
	// ServerVersion is the release the server was built from, the configuration's
	// APP_VERSION. A value that is not a release, dev for one, checks nothing.
	ServerVersion string
	// OldestAnswered is the oldest release still answered: a release such as 1.5.0,
	// ThisRelease for the server's own, or empty for every release that sends the
	// header.
	OldestAnswered string
	// Exempt are the route patterns answered whatever the header says: the
	// stored-file routes, which a link or an image reaches.
	Exempt []string
}

// CheckAPIVersion is the middleware a session outlet's API routes run under. A request
// carrying APIVersionHeader is answered when its release is between the oldest answered
// release and the server's, inclusive, and refused with 412 otherwise, before any
// handler runs and before its body is read; the refusal carries the server's release
// in APIVersionHeader. A request without the header is answered, and so is any request
// when either side's version is not a release (dev, or anything that is not a semantic
// version). Every response on a checked route carries Vary: APIVersionHeader, so a
// browser never serves one application's cached answer to another. Routes named in
// Exempt pass untouched. Versions compare as semantic versions, with or without a
// leading v.
func CheckAPIVersion(check APIVersionCheck) func(http.Handler) http.Handler {
	window := newReleaseWindow(check)
	exempt := slices.Clone(check.Exempt)

	return func(next http.Handler) http.Handler {
		return httpio.Log(func(w http.ResponseWriter, r *http.Request) error {
			if rctx := chi.RouteContext(r.Context()); rctx != nil && slices.Contains(exempt, rctx.RoutePattern()) {
				next.ServeHTTP(w, r)

				return nil
			}

			w.Header().Add("Vary", APIVersionHeader)
			app := r.Header.Get(APIVersionHeader)
			if window.answers(app) {
				next.ServeHTTP(w, r)

				return nil
			}

			w.Header().Set(APIVersionHeader, check.ServerVersion)

			return httpio.NewEncoder(w).StatusCodeWithBody(http.StatusPreconditionFailed, &httpio.MessageResponse{Message: window.refusal(app)})
		})
	}
}

// releaseWindow is the resolved range of releases a check answers: the canonical
// versions, and whether each bound is a release at all.
type releaseWindow struct {
	server, oldest             string
	serverText, oldestText     string
	serverRelease, oldestBound bool
}

// newReleaseWindow resolves the check's bounds: ThisRelease to the server's version,
// and each bound to its canonical semantic version where it is one.
func newReleaseWindow(check APIVersionCheck) releaseWindow {
	oldestText := check.OldestAnswered
	if oldestText == ThisRelease {
		oldestText = check.ServerVersion
	}
	window := releaseWindow{serverText: check.ServerVersion, oldestText: oldestText}
	window.server, window.serverRelease = releaseVersion(check.ServerVersion)
	window.oldest, window.oldestBound = releaseVersion(oldestText)

	return window
}

// answers reports whether a request carrying app as its version is answered: always
// when the header is absent or either side is not a release, otherwise when app is
// between the oldest answered release and the server's.
func (w releaseWindow) answers(app string) bool {
	appVersion, appRelease := releaseVersion(app)
	if !w.serverRelease || !appRelease {
		return true
	}
	if w.oldestBound && semver.Compare(appVersion, w.oldest) < 0 {
		return false
	}

	return semver.Compare(appVersion, w.server) <= 0
}

// refusal is the message a refused request carries.
func (w releaseWindow) refusal(app string) string {
	if w.oldestBound {
		return fmt.Sprintf("release %s of the application is not answered: this server, release %s, answers releases %s through %s", app, w.serverText, w.oldestText, w.serverText)
	}

	return fmt.Sprintf("release %s of the application is newer than this server, release %s", app, w.serverText)
}

// releaseVersion reads a version as a release: a semantic version, with or without a
// leading v, returned in canonical form; anything else is not a release.
func releaseVersion(version string) (string, bool) {
	version = strings.TrimSpace(version)
	if version == "" {
		return "", false
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	if !semver.IsValid(version) {
		return "", false
	}

	return semver.Canonical(version), true
}
