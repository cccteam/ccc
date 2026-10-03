package resource

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// unreadBody fails a case that reads it: a refused request's body is never read.
type unreadBody struct {
	read bool
}

func (b *unreadBody) Read([]byte) (int, error) {
	b.read = true

	return 0, io.EOF
}

// TestCheckAPIVersion pins the check over a router shaped like a session outlet's
// authenticated group: the checked API routes and a stored-file route inside it, a
// session route outside it.
func TestCheckAPIVersion(t *testing.T) {
	t.Parallel()

	const (
		apiRoute     = "/api/widgets"
		fileRoute    = "/api/widgets/{widgetID}/content"
		fileURL      = "/api/widgets/7/content"
		sessionRoute = "/api/user/session"
	)
	released := APIVersionCheck{ServerVersion: "2.0.0", OldestAnswered: "1.5.0", Exempt: []string{fileRoute}}
	everyRelease := APIVersionCheck{ServerVersion: "2.0.0"}
	thisRelease := APIVersionCheck{ServerVersion: "2.0.0", OldestAnswered: ThisRelease}

	tests := []struct {
		name        string
		check       APIVersionCheck
		app         string
		url         string
		wantCode    int
		wantVary    bool
		wantHandler string
		wantMessage string
	}{
		{name: "a release inside the window is answered", check: released, app: "1.7.0", url: apiRoute, wantCode: http.StatusOK, wantVary: true, wantHandler: "api"},
		{name: "the oldest answered release is answered", check: released, app: "1.5.0", url: apiRoute, wantCode: http.StatusOK, wantVary: true, wantHandler: "api"},
		{name: "the server's release is answered", check: released, app: "2.0.0", url: apiRoute, wantCode: http.StatusOK, wantVary: true, wantHandler: "api"},
		{name: "a leading v is a release", check: released, app: "v1.7.0", url: apiRoute, wantCode: http.StatusOK, wantVary: true, wantHandler: "api"},
		{name: "a prerelease of the server's release is older than it", check: released, app: "2.0.0-rc1", url: apiRoute, wantCode: http.StatusOK, wantVary: true, wantHandler: "api"},
		{name: "a release below the oldest answered is refused", check: released, app: "1.4.9", url: apiRoute, wantCode: http.StatusPreconditionFailed, wantVary: true, wantMessage: "release 1.4.9 of the application is not answered: this server, release 2.0.0, answers releases 1.5.0 through 2.0.0"},
		{name: "a release above the server's is refused", check: released, app: "2.0.1", url: apiRoute, wantCode: http.StatusPreconditionFailed, wantVary: true},
		{name: "no header is answered", check: released, app: "", url: apiRoute, wantCode: http.StatusOK, wantVary: true, wantHandler: "api"},
		{name: "an application that is not a release is answered", check: released, app: "dev", url: apiRoute, wantCode: http.StatusOK, wantVary: true, wantHandler: "api"},
		{name: "a version that is not semantic is answered", check: released, app: "1.x", url: apiRoute, wantCode: http.StatusOK, wantVary: true, wantHandler: "api"},
		{name: "a server that is not a release answers every release", check: APIVersionCheck{ServerVersion: "dev", OldestAnswered: "1.5.0"}, app: "99.0.0", url: apiRoute, wantCode: http.StatusOK, wantVary: true, wantHandler: "api"},
		{name: "a server that is not a release answers this release at any version", check: APIVersionCheck{ServerVersion: "dev", OldestAnswered: ThisRelease}, app: "0.0.1", url: apiRoute, wantCode: http.StatusOK, wantVary: true, wantHandler: "api"},
		{name: "with no oldest answered the first release is answered", check: everyRelease, app: "0.0.1", url: apiRoute, wantCode: http.StatusOK, wantVary: true, wantHandler: "api"},
		{name: "with no oldest answered a release above the server's is refused", check: everyRelease, app: "2.0.1", url: apiRoute, wantCode: http.StatusPreconditionFailed, wantVary: true, wantMessage: "release 2.0.1 of the application is newer than this server, release 2.0.0"},
		{name: "this release answers the server's release", check: thisRelease, app: "2.0.0", url: apiRoute, wantCode: http.StatusOK, wantVary: true, wantHandler: "api"},
		{name: "this release refuses the release before", check: thisRelease, app: "1.9.9", url: apiRoute, wantCode: http.StatusPreconditionFailed, wantVary: true, wantMessage: "answers releases 2.0.0 through 2.0.0"},
		{name: "this release refuses the release after", check: thisRelease, app: "2.0.1", url: apiRoute, wantCode: http.StatusPreconditionFailed, wantVary: true},
		{name: "an exempt route is answered at any release and not varied", check: released, app: "99.0.0", url: fileURL, wantCode: http.StatusOK, wantHandler: "file"},
		{name: "a route outside the check is answered at any release", check: released, app: "99.0.0", url: sessionRoute, wantCode: http.StatusOK, wantHandler: "session"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var handlers []string
			record := func(name string) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					handlers = append(handlers, name)
					w.WriteHeader(http.StatusOK)
				}
			}
			router := chi.NewRouter()
			router.Get(sessionRoute, record("session"))
			router.Group(func(r chi.Router) {
				r.Use(CheckAPIVersion(tt.check))
				r.Get(apiRoute, record("api"))
				r.Get(fileRoute, record("file"))
			})

			body := &unreadBody{}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.url, body)
			if tt.app != "" {
				req.Header.Set(APIVersionHeader, tt.app)
			}
			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			if rr.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d; body %s", rr.Code, tt.wantCode, rr.Body.String())
			}
			if got := slices.Contains(rr.Header().Values("Vary"), APIVersionHeader); got != tt.wantVary {
				t.Errorf("Vary carries %s = %v, want %v", APIVersionHeader, got, tt.wantVary)
			}
			if tt.wantCode == http.StatusPreconditionFailed {
				if got := rr.Header().Get(APIVersionHeader); got != tt.check.ServerVersion {
					t.Errorf("%s = %q, want the server's %q", APIVersionHeader, got, tt.check.ServerVersion)
				}
				if len(handlers) != 0 {
					t.Errorf("handlers ran = %v, want none", handlers)
				}
				if body.read {
					t.Error("the refused request's body was read")
				}
				if tt.wantMessage != "" && !strings.Contains(rr.Body.String(), tt.wantMessage) {
					t.Errorf("body = %s, want it to carry %q", rr.Body.String(), tt.wantMessage)
				}

				return
			}
			if !slices.Equal(handlers, []string{tt.wantHandler}) {
				t.Errorf("handlers ran = %v, want [%s]", handlers, tt.wantHandler)
			}
			if got := rr.Header().Get(APIVersionHeader); got != "" {
				t.Errorf("%s = %q on an answered request, want none", APIVersionHeader, got)
			}
		})
	}
}

// Test_releaseVersion pins what counts as a release: a semantic version, with or
// without the leading v, in canonical form; anything else is not one.
func Test_releaseVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version     string
		want        string
		wantRelease bool
	}{
		{version: "1.5.0", want: "v1.5.0", wantRelease: true},
		{version: "v1.5.0", want: "v1.5.0", wantRelease: true},
		{version: " 1.5 ", want: "v1.5.0", wantRelease: true},
		{version: "2.0.0-rc1", want: "v2.0.0-rc1", wantRelease: true},
		{version: "1.5.0+build.7", want: "v1.5.0", wantRelease: true},
		{version: "dev"},
		{version: ""},
		{version: ThisRelease},
		{version: "1.5.0.1"},
		{version: "abc123"},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			t.Parallel()

			got, release := releaseVersion(tt.version)
			if got != tt.want || release != tt.wantRelease {
				t.Errorf("releaseVersion(%q) = %q, %v; want %q, %v", tt.version, got, release, tt.want, tt.wantRelease)
			}
		})
	}
}
