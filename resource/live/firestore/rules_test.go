package firestore_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/live"
)

// unsignedToken mints the emulator's unsigned identity token for uid: a JWT with
// alg none, which the emulator evaluates the rules against as a signed-in user.
func unsignedToken(uid string) string {
	encode := func(v any) string {
		b, _ := json.Marshal(v)

		return base64.RawURLEncoding.EncodeToString(b)
	}
	now := time.Now().Unix()
	header := encode(map[string]string{"alg": "none", "typ": "JWT"})
	payload := encode(map[string]any{
		"iss":       "https://securetoken.google.com/" + testProject,
		"aud":       testProject,
		"sub":       uid,
		"user_id":   uid,
		"auth_time": now,
		"iat":       now,
		"exp":       now + 3600,
	})

	return header + "." + payload + "."
}

// restCall calls the emulator's REST API as the bearer of token (none when empty) and
// returns the status and body.
func restCall(t *testing.T, method, path, token, body string) (status int, response string) {
	t.Helper()

	url := fmt.Sprintf("http://%s/v1/projects/%s/databases/%s/documents/%s", firestoreEmulator(t), testProject, testDatabase, path)
	req, err := http.NewRequestWithContext(context.Background(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.Client.Do() error = %v", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}

	return resp.StatusCode, string(out)
}

// TestRules proves the rules file the emulator was started with: a signed-in user reads
// their own change set and nothing else, nobody writes a change set from a client, and
// the subscription record is unreachable.
func TestRules(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	svc := newService(t, fixedClock(now))
	res := resourceFor(t)
	alice, bob := principalFor(t, "alice"), principalFor(t, "bob")
	register(t, svc, now,
		live.Subscription{Principal: alice, Tab: "t1", Resource: res, Key: "k1"},
		live.Subscription{Principal: bob, Tab: "t2", Resource: res, Key: "k1"},
	)
	if err := svc.Publish(t.Context(), "anvil", map[accesstypes.Resource][]resource.RowChange{res: {{Key: "k1"}}}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	ownDoc := "users/" + alice + "/changes/" + live.ChangeDocument{Kind: live.RowChange, Resource: res, Key: "k1"}.ID(now.Unix())

	tests := []struct {
		name       string
		method     string
		path       string
		token      string
		body       string
		wantStatus int
		wantBody   string
	}{
		{name: "a user lists their own change set", method: http.MethodGet, path: "users/" + alice + "/changes", token: unsignedToken(alice), wantStatus: http.StatusOK, wantBody: `"kind"`},
		{name: "a user reads one of their own documents", method: http.MethodGet, path: ownDoc, token: unsignedToken(alice), wantStatus: http.StatusOK, wantBody: `"resource"`},
		{name: "a user cannot list another user's change set", method: http.MethodGet, path: "users/" + bob + "/changes", token: unsignedToken(alice), wantStatus: http.StatusForbidden},
		{name: "a user cannot read another user's document", method: http.MethodGet, path: "users/" + bob + "/changes/" + live.ChangeDocument{Kind: live.RowChange, Resource: res, Key: "k1"}.ID(now.Unix()), token: unsignedToken(alice), wantStatus: http.StatusForbidden},
		{name: "nobody reads a change set without signing in", method: http.MethodGet, path: "users/" + alice + "/changes", wantStatus: http.StatusForbidden},
		{name: "a user cannot write into their own change set", method: http.MethodPatch, path: ownDoc, token: unsignedToken(alice), body: `{"fields":{"kind":{"stringValue":"forged"}}}`, wantStatus: http.StatusForbidden},
		{name: "a user cannot add to another user's change set", method: http.MethodPatch, path: "users/" + bob + "/changes/forged", token: unsignedToken(alice), body: `{"fields":{"kind":{"stringValue":"forged"}}}`, wantStatus: http.StatusForbidden},
		{name: "the subscription record is unreachable", method: http.MethodGet, path: "subscriptions", token: unsignedToken(alice), wantStatus: http.StatusForbidden},
		{name: "a user's own document is unreachable too", method: http.MethodGet, path: "users/" + alice, token: unsignedToken(alice), wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status, body := restCall(t, tt.method, tt.path, tt.token, tt.body)
			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d: %s", status, tt.wantStatus, body)
			}
			if tt.wantBody != "" && !strings.Contains(body, tt.wantBody) {
				t.Errorf("body = %s, want it to contain %q", body, tt.wantBody)
			}
		})
	}
}
