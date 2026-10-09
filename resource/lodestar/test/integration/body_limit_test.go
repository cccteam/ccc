package integration

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
)

// TestBodyLimit proves the request body limits land where the generator put them. The
// application's limit is 2 MiB, set once in the generator program: a route the router
// wraps (here the consolidated patch route) refuses a body over it with 413 naming it,
// and an RPC method that declares no maximum (HailShip) takes the same limit through
// its handler. A method that declares its own (IssueBulletin, 64 KB) answers at that
// maximum instead: a body under the application's limit but over its own is refused,
// one under its own is served. An upload is bounded by its @upload(max: 5MB) alone, so
// a 3 MiB photo, over the application's limit, lands.
//
// Demonstrates: generation.body-limit, @rpc.max.
func TestBodyLimit(t *testing.T) {
	t.Parallel()

	_, h, _ := sharedWorld(t)

	const (
		appLimit      = 2 << 20
		bulletinLimit = 64 << 10
	)
	// Every body is valid JSON padded with spaces to the size the case needs, so the
	// only thing a refusal can be about is the size.
	tests := []struct {
		name        string
		user        accesstypes.User
		method      string
		target      string
		body        string
		wantStatus  int
		wantMessage string
	}{
		{
			name: "a route the router wraps refuses a body over the application's limit, naming it",
			user: "governor", method: http.MethodPatch, target: "/console/api/resources",
			body:        "[" + strings.Repeat(" ", appLimit) + "]",
			wantStatus:  http.StatusRequestEntityTooLarge,
			wantMessage: "the request body exceeds the maximum of 2MB",
		},
		{
			name: "an RPC method that declares no maximum takes the application's limit",
			user: "pilot", method: http.MethodPost, target: sectorPath(anvil, "hail-ship"),
			body:        `{"shipId":` + strconv.Quote(shipKingfisherID) + strings.Repeat(" ", appLimit) + `}`,
			wantStatus:  http.StatusRequestEntityTooLarge,
			wantMessage: "the request body exceeds the maximum of 2MB",
		},
		{
			name: "an RPC method that declares its own maximum refuses a body over it, though under the application's limit",
			user: "marshal", method: http.MethodPost, target: "/console/api/issue-bulletin",
			body:        `{"announcement":"All hands: drill at 0600"` + strings.Repeat(" ", bulletinLimit) + `}`,
			wantStatus:  http.StatusRequestEntityTooLarge,
			wantMessage: "the request body exceeds the maximum of 64KB",
		},
		{
			name: "a body under the method's own maximum is served",
			user: "marshal", method: http.MethodPost, target: "/console/api/issue-bulletin",
			body:       `{"announcement":"All hands: drill at 0600"` + strings.Repeat(" ", bulletinLimit-128) + `}`,
			wantStatus: http.StatusOK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, respBody := doRequestAs(t, h, tt.user, tt.method, tt.target, tt.body)
			assertStatus(t, status, tt.wantStatus, respBody)
			if tt.wantMessage != "" && !strings.Contains(string(respBody), tt.wantMessage) {
				t.Errorf("body = %s, want it to contain %q", respBody, tt.wantMessage)
			}
		})
	}

	t.Run("an upload is bounded by its own maximum alone: a photo over the application's limit lands", func(t *testing.T) {
		t.Parallel()

		h, stores := documentWorld(t)
		hull := uploadFile{name: "hull.png", contentType: "image/png", content: bytes.Repeat([]byte("P"), 3<<20)}
		body, contentType := multipartBody(t, photoTarget, hull)
		status, respBody := doUploadAs(t, h, "photographer", sectorPath(anvil, "attach-refit-photo"), body, contentType, false)
		assertStatus(t, status, http.StatusOK, respBody)
		if files := stores.files.Keys(); len(files) != 1 {
			t.Errorf("default store = %v, want the photo alone", files)
		}
	})
}
