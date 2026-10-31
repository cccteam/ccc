package persona

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestReadCast(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    string
		want    *Cast
		wantErr string
	}{
		{
			name: "two personas, the name defaulting to the login",
			data: `{"personas": [{"login": "cadet", "name": "Cadet Example", "email": "cadet@example.com", "roles": ["Cadet"]}, {"login": "navigator"}]}`,
			want: &Cast{Personas: []Persona{
				{Login: "cadet", Name: "Cadet Example", Email: "cadet@example.com", Roles: []string{"Cadet"}},
				{Login: "navigator", Name: "navigator"},
			}},
		},
		{name: "no persona", data: `{"personas": []}`, wantErr: "the cast names no persona"},
		{name: "a login with a space", data: `{"personas": [{"login": "cadet one"}]}`, wantErr: `the login "cadet one" is not lowercase letters`},
		{name: "a capitalized login", data: `{"personas": [{"login": "Cadet"}]}`, wantErr: `the login "Cadet" is not`},
		{name: "a login twice", data: `{"personas": [{"login": "cadet"}, {"login": "cadet"}]}`, wantErr: `the login "cadet" names two personas`},
		{name: "an unknown field", data: `{"personas": [{"login": "cadet", "password": "x"}]}`, wantErr: "unknown field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ReadCast([]byte(tt.data))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ReadCast() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("ReadCast() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ReadCast() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestClientIDFor(t *testing.T) {
	t.Parallel()

	if got := ClientIDFor("lanterns", "tst", 12); got != "lanterns-pr12" {
		t.Errorf("ClientIDFor(pull request 12) = %q, want lanterns-pr12", got)
	}
	if got := ClientIDFor("lanterns", "tst", 0); got != "lanterns-tst" {
		t.Errorf("ClientIDFor(the environment) = %q, want lanterns-tst", got)
	}
}

// TestRegister drives the registration client against a stand-in for the provider: the
// request's shape, the bearer token, the answer, and a refusal's message.
func TestRegister(t *testing.T) {
	t.Parallel()

	var got struct {
		method, path, auth string
		body               Registration
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		if r.Method == http.MethodDelete {
			if strings.HasSuffix(r.URL.Path, "/ghost-pr1") {
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, `{"message": "deployer@example.com is not a deploy identity of Ghost"}`)

				return
			}
			w.WriteHeader(http.StatusNoContent)

			return
		}
		if err := json.NewDecoder(r.Body).Decode(&got.body); err != nil {
			t.Errorf("the registration body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"clientId": "lanterns-pr12", "expiresAt": "2026-11-07T00:00:00Z"}`)
	}))
	t.Cleanup(provider.Close)

	reg := &Registration{ApplicationClientID: "lanterns", Environment: "tst", PullRequest: 12, RedirectURIs: []string{"https://lanterns-pr12.example.com/api/user/persona/callback"}, Cast: []Persona{{Login: "cadet", Name: "Cadet Example"}}}
	registered, err := Register(t.Context(), provider.Client(), provider.URL+"/persona/", "lanterns-pr12", "token-1", reg)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if registered.ClientID != "lanterns-pr12" || registered.ExpiresAt.IsZero() {
		t.Errorf("Register() = %+v", registered)
	}
	if got.method != http.MethodPut || got.path != "/persona/registrations/lanterns-pr12" || got.auth != "Bearer token-1" {
		t.Errorf("request = %s %s %s", got.method, got.path, got.auth)
	}
	if diff := cmp.Diff(*reg, got.body); diff != "" {
		t.Errorf("the body sent (-want +got):\n%s", diff)
	}

	if err := Deregister(t.Context(), provider.Client(), provider.URL+"/persona", "lanterns-pr12", "token-1"); err != nil {
		t.Errorf("Deregister() error = %v", err)
	}
	if got.method != http.MethodDelete || got.path != "/persona/registrations/lanterns-pr12" {
		t.Errorf("deregister request = %s %s", got.method, got.path)
	}
	err = Deregister(t.Context(), provider.Client(), provider.URL+"/persona", "ghost-pr1", "token-1")
	if err == nil || !strings.Contains(err.Error(), "answered 403: deployer@example.com is not a deploy identity of Ghost") {
		t.Errorf("Deregister(refused) error = %v, want the provider's message", err)
	}
}
