package integration

// This suite covers the headers the stored file route answers under in the served
// application: the marshal attaches an HTML page that would call the session endpoint,
// an image, and a plain-text brief, and reads each back through the file route behind
// the application's own security headers. Every answer carries nosniff and the frame's
// sandbox policy beside the application's policy, which keeps its frame-ancestors; the
// page downloads, typed as it was declared; the image and the brief display inline; and
// a kept copy revalidates under the same headers.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
)

// frameSandboxPolicy is the policy the frame adds to every served file.
const frameSandboxPolicy = "sandbox; default-src 'none'"

// securedDocumentWorld is a fresh demo world whose App writes documents into memory
// stores the test owns, served through the generated routes behind the application's
// SecurityHeaders middleware, where production's router mounts them: the response
// carries the application's policy for the frame to add its own beside.
func securedDocumentWorld(t *testing.T) http.Handler {
	t.Helper()

	db, err := prepareDatabase(t.Context(), t, migrationsSource, demoSeedSource)
	if err != nil {
		t.Fatal(err)
	}
	a := newAppWithStores(db, demoAccessClient(t), newTestStores())

	return a.SecurityHeaders(router.NewTestRouter(a))
}

// TestMissionDocument_fileRouteHeaders pins what the file route answers under: the
// marshal, allowed to upload, attaches a page, an image and a brief, and each comes back
// under nosniff and both policies, the page as a download and the others inline.
func TestMissionDocument_fileRouteHeaders(t *testing.T) {
	t.Parallel()

	h := securedDocumentWorld(t)

	page := uploadFile{name: "page.html", contentType: "text/html", content: []byte(`<!doctype html><script>fetch("/console/api/user/session")</script>`)}
	chart := uploadFile{name: "chart.png", contentType: "image/png", content: []byte("PNG chart")}
	brief := uploadFile{name: "brief.txt", contentType: "text/plain", content: []byte(haulerBrief)}
	body, contentType := multipartBody(t, fmt.Sprintf(`{"missionId":%q,"title":"Hauler files"}`, missionHaulerID), page, chart, brief)
	status, respBody := doUploadAs(t, h, "marshal", sectorPath(anvil, "attach-mission-document"), body, contentType, false)
	assertStatus(t, status, http.StatusOK, respBody)

	// Each row's file route, by the file's name.
	targets := map[string]string{}
	for _, row := range readRows(t, h, "marshal", sectorPath(anvil, "mission-documents")) {
		targets[cell[string](t, row, "fileName")] = sectorPath(anvil, "mission-documents/"+cell[string](t, row, "id")+"/content")
	}
	if len(targets) != 3 {
		t.Fatalf("documents = %v, want the three files", targets)
	}
	pageTag := fileRequestAs(t, h, "marshal", targets[page.name], nil).Header().Get("ETag")
	if pageTag == "" {
		t.Fatal("the page's answer carries no ETag")
	}

	tests := []struct {
		name            string
		file            string
		headers         map[string]string
		wantStatus      int
		wantType        string
		wantDisposition string
		wantBody        string
	}{
		{
			name:            "the page downloads, typed as declared",
			file:            page.name,
			wantStatus:      http.StatusOK,
			wantType:        "text/html",
			wantDisposition: `attachment; filename=page.html`,
			wantBody:        string(page.content),
		},
		{
			name:            "the image displays inline",
			file:            chart.name,
			wantStatus:      http.StatusOK,
			wantType:        "image/png",
			wantDisposition: `inline; filename=chart.png`,
			wantBody:        string(chart.content),
		},
		{
			name:            "the brief displays inline",
			file:            brief.name,
			wantStatus:      http.StatusOK,
			wantType:        "text/plain",
			wantDisposition: `inline; filename=brief.txt`,
			wantBody:        haulerBrief,
		},
		{
			name:       "a kept copy of the page revalidates under the same headers",
			file:       page.name,
			headers:    map[string]string{"If-None-Match": pageTag},
			wantStatus: http.StatusNotModified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rr := fileRequestAs(t, h, "marshal", targets[tt.file], tt.headers)
			assertStatus(t, rr.Code, tt.wantStatus, rr.Body.Bytes())
			if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
			policies := rr.Header().Values("Content-Security-Policy")
			if len(policies) != 2 || policies[1] != frameSandboxPolicy {
				t.Fatalf("Content-Security-Policy = %q, want the application's policy and then %q", policies, frameSandboxPolicy)
			}
			if !strings.Contains(policies[0], "frame-ancestors 'none'") {
				t.Errorf("the application's policy %q lost its frame-ancestors", policies[0])
			}
			if got := rr.Header().Get("Content-Type"); got != tt.wantType {
				t.Errorf("Content-Type = %q, want %q", got, tt.wantType)
			}
			if got := rr.Header().Get("Content-Disposition"); got != tt.wantDisposition {
				t.Errorf("Content-Disposition = %q, want %q", got, tt.wantDisposition)
			}
			if rr.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rr.Body.String(), tt.wantBody)
			}
		})
	}
}
