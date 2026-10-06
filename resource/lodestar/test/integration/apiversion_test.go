package integration

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestAPIVersion_refusal drives the served router's release check the way a browser
// application built from a release does. The server is built from release 1.4.0
// (newServedAt), its console outlet answers from 0.1.0 (the oldest answered release the
// generator program declares) and its portal outlet from the first release that sends
// the header. A request whose X-Api-Version lies in that range is answered; one below
// the oldest answered or above the server's is refused with 412 carrying the server's
// release, before any handler runs (an unknown row answers 412, not its 404); a request
// with no header is answered; the session routes and a stored-file route are answered at
// any release, the file route exempt from the check and so untouched by it; and every
// checked response carries Vary: X-Api-Version. The development stack's dev server is
// TestAPIVersion_devServerChecksNothing.
//
// Demonstrates: api.version-refusal.
func TestAPIVersion_refusal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServedAt(ctx, t, "1.4.0")

	// A refit that does not exist, well formed: its read answers 404 when the handler runs.
	const unknownRefit = "a0000000-0000-4000-8000-0000000000de"

	tests := []versionCase{
		{name: "no header: answered", user: "governor", path: consoleAPI + "/user-domains", wantStatus: http.StatusOK, wantVary: true},
		{name: "a release in range is answered", user: "governor", version: "1.2.0", path: consoleAPI + "/user-domains", wantStatus: http.StatusOK, wantVary: true},
		{name: "the oldest answered release itself is answered", user: "governor", version: "0.1.0", path: consoleAPI + "/user-domains", wantStatus: http.StatusOK, wantVary: true},
		{name: "the server's own release is answered", user: "governor", version: "1.4.0", path: consoleAPI + "/user-domains", wantStatus: http.StatusOK, wantVary: true},
		{name: "a release below the oldest answered is refused with the server's release", user: "governor", version: "0.0.1", path: consoleAPI + "/user-domains", wantStatus: http.StatusPreconditionFailed, wantServer: "1.4.0", wantVary: true},
		{name: "a release above the server's is refused the same way", user: "governor", version: "9.9.9", path: consoleAPI + "/user-domains", wantStatus: http.StatusPreconditionFailed, wantServer: "1.4.0", wantVary: true},
		{name: "the refusal lands before the handler: an unknown row is 412, not 404", user: "governor", version: "0.0.1", path: sectorPath(anvil, "refits/"+unknownRefit), wantStatus: http.StatusPreconditionFailed, wantServer: "1.4.0", wantVary: true},
		{name: "the same unknown row is 404 with no header: the handler ran", user: "governor", path: sectorPath(anvil, "refits/"+unknownRefit), wantStatus: http.StatusNotFound, wantVary: true},
		{name: "the session route is answered at any release", version: "9.9.9", path: consoleAPI + "/user/session", wantStatus: http.StatusOK},
		{name: "the portal's session route is answered at any release", version: "0.0.1", path: portalAPI + "/user/session", wantStatus: http.StatusOK},
		{name: "a stored-file route is answered at any release, untouched by the check", user: "purser", version: "9.9.9", path: sectorPath(anvil, "expense-manifests/"+missionPodID+"/content"), wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tt.run(ctx, t, s)
		})
	}
}

// TestAPIVersion_devServerChecksNothing is the development stack's side of the check: a
// server whose version is dev (newServed) answers every release, below the oldest
// answered and above its own alike, as a dev build sends no header at all.
//
// Demonstrates: api.version-refusal.
func TestAPIVersion_devServerChecksNothing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)

	tests := []versionCase{
		{name: "a release below the oldest answered is answered", user: "governor", version: "0.0.1", path: consoleAPI + "/user-domains", wantStatus: http.StatusOK, wantVary: true},
		{name: "a release above the server's is answered", user: "governor", version: "9.9.9", path: consoleAPI + "/user-domains", wantStatus: http.StatusOK, wantVary: true},
		{name: "no header: answered", user: "governor", path: consoleAPI + "/user-domains", wantStatus: http.StatusOK, wantVary: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tt.run(ctx, t, s)
		})
	}
}

// versionCase is one request as a browser application built from a release: who is
// signed in (empty for no session), the X-Api-Version it sends (empty for no header),
// the path, and the answer: its status, the X-Api-Version it carries (the server's
// release on a refusal, nothing otherwise), and whether the route is a checked one,
// whose answers vary by the header.
type versionCase struct {
	name       string
	user       string
	version    string
	path       string
	wantStatus int
	wantServer string
	wantVary   bool
}

func (tt *versionCase) run(ctx context.Context, t *testing.T, s *served) {
	t.Helper()

	prefix := consoleAPI
	if strings.HasPrefix(tt.path, portalAPI) {
		prefix = portalAPI
	}
	b := newBrowser(t, s, prefix)
	if tt.user != "" {
		b.signIn(ctx, tt.user)
	}
	headers := map[string]string{}
	if tt.version != "" {
		headers["X-Api-Version"] = tt.version
	}

	resp := b.doWith(ctx, http.MethodGet, tt.path, nil, headers)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != tt.wantStatus {
		t.Errorf("GET %s with X-Api-Version %q: status = %d, want %d: %s", tt.path, tt.version, resp.StatusCode, tt.wantStatus, body)
	}
	if got := resp.Header.Get("X-Api-Version"); got != tt.wantServer {
		t.Errorf("GET %s with X-Api-Version %q: answer's X-Api-Version = %q, want %q", tt.path, tt.version, got, tt.wantServer)
	}
	if got := strings.Contains(resp.Header.Get("Vary"), "X-Api-Version"); got != tt.wantVary {
		t.Errorf("GET %s: Vary = %q, want X-Api-Version named: %v", tt.path, resp.Header.Get("Vary"), tt.wantVary)
	}
}
