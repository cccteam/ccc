package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/cccteam/ccc/impulse/persona"
)

// personaProvider is a stand-in for the persona provider: it records the requests and
// answers a registration, a removal, or the refusal a case asks for.
type personaProvider struct {
	mu       sync.Mutex
	requests []personaRequest
	// refuse is the status and message the provider answers every request with, when set.
	refuse  int
	message string
}

type personaRequest struct {
	Method, Path, Authorization string
	Body                        persona.Registration
}

func (p *personaProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := personaRequest{Method: r.Method, Path: r.URL.Path, Authorization: r.Header.Get("Authorization")}
	if r.Method == http.MethodPut {
		_ = json.NewDecoder(r.Body).Decode(&req.Body)
	}
	p.mu.Lock()
	p.requests = append(p.requests, req)
	p.mu.Unlock()
	if p.refuse != 0 {
		w.WriteHeader(p.refuse)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": p.message})

		return
	}
	switch r.Method {
	case http.MethodPut:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"clientId": strings.TrimPrefix(r.URL.Path, "/persona/registrations/"), "expiresAt": "2026-11-07T12:00:00Z"})
	case http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func TestPersona(t *testing.T) {
	t.Parallel()

	const cast = `{"personas": [{"login": "admin", "name": "Admin Example", "roles": ["Administrator"]}, {"login": "keeper"}]}`
	registrations := func(issuer string) string {
		data, err := json.Marshal([]personaRegistration{{Auth: "keepers", Registration: "persona", Issuer: issuer + "/persona", ClientIDRoot: "beacon", Site: "console", CallbackPath: "/api/user/persona/callback", CastFile: "pkg/auth/keepers/cast.json"}})
		if err != nil {
			t.Fatal(err)
		}

		return string(data)
	}
	build := func(subs map[string]string) string {
		data, err := json.Marshal(Build{ID: "b1", Substitutions: subs})
		if err != nil {
			t.Fatal(err)
		}

		return string(data)
	}
	tests := []struct {
		name   string
		action PersonaAction
		// env is the environment file beyond the release; subs the build's substitutions
		// (_PERSONA filled in from the provider when persona is true); noCast leaves the
		// cast out of the checkout.
		env     string
		subs    map[string]string
		persona bool
		noCast  bool
		refuse  int
		message string
		// wantRequests are the provider's requests as METHOD path; wantBody the first
		// registration's body when one is sent.
		wantRequests []string
		wantBody     *persona.Registration
		wantOut      []string
		wantErr      string
	}{
		{
			name: "a pull-request build registers its deployment", action: PersonaRegister,
			env:  "export PR_HOSTNAME=\"beacon-pr12.impulseframework.dev\"\n",
			subs: map[string]string{"_ENV": "tst", "_PR_NUMBER": "12", "_HOSTNAME": "beacon-tst.impulseframework.dev"}, persona: true,
			wantRequests: []string{"PUT /persona/registrations/beacon-pr12"},
			wantBody:     &persona.Registration{ApplicationClientID: "beacon", Environment: "tst", PullRequest: 12, RedirectURIs: []string{"https://beacon-pr12.impulseframework.dev/api/user/persona/callback"}, Cast: []persona.Persona{{Login: "admin", Name: "Admin Example", Roles: []string{"Administrator"}}, {Login: "keeper", Name: "keeper"}}},
			wantOut:      []string{"Registered beacon-pr12 with ", "/persona for the keepers auth's persona registration: 2 persona(s), callback https://beacon-pr12.impulseframework.dev/api/user/persona/callback, kept until 2026-11-07 12:00 UTC."},
		},
		{
			name: "a release build in the first environment registers the environment", action: PersonaRegister,
			env:  "export CANONICAL_HOSTNAME=\"beacon-tst.impulseframework.dev\"\n",
			subs: map[string]string{"_ENV": "tst", "_PR_NUMBER": "", "_HOSTNAME": "beacon-tst.impulseframework.dev"}, persona: true,
			wantRequests: []string{"PUT /persona/registrations/beacon-tst"},
			wantBody:     &persona.Registration{ApplicationClientID: "beacon", Environment: "tst", RedirectURIs: []string{"https://beacon-tst.impulseframework.dev/api/user/persona/callback"}, Cast: []persona.Persona{{Login: "admin", Name: "Admin Example", Roles: []string{"Administrator"}}, {Login: "keeper", Name: "keeper"}}},
			wantOut:      []string{"Registered beacon-tst with "},
		},
		{
			name: "two sites: the callback is on the method's site", action: PersonaRegister,
			env:  "export CANONICAL_HOSTNAMES=\"console=beacon-console-tst.impulseframework.dev,portal=beacon-portal-tst.impulseframework.dev\"\n",
			subs: map[string]string{"_ENV": "tst", "_PR_NUMBER": ""}, persona: true,
			wantRequests: []string{"PUT /persona/registrations/beacon-tst"},
			wantBody:     &persona.Registration{ApplicationClientID: "beacon", Environment: "tst", RedirectURIs: []string{"https://beacon-console-tst.impulseframework.dev/api/user/persona/callback"}, Cast: []persona.Persona{{Login: "admin", Name: "Admin Example", Roles: []string{"Administrator"}}, {Login: "keeper", Name: "keeper"}}},
		},
		{
			name: "no persona provider in the environment", action: PersonaRegister,
			subs:    map[string]string{"_ENV": "stg", "_PERSONA": ""},
			wantOut: []string{"No persona provider in stg: nothing to register."},
		},
		{
			name: "a torn-down environment registers nothing", action: PersonaRegister,
			env: "export SKIP_DEPLOY=\"true\"\n", subs: map[string]string{"_ENV": "tst", "_PR_NUMBER": "12"}, persona: true,
			wantOut: []string{tornDown},
		},
		{
			name: "a teardown registers nothing", action: PersonaRegister,
			env: "export DOWN=\"true\"\nexport SKIP_DEPLOY=\"true\"\n", subs: map[string]string{"_ENV": "tst", "_PR_NUMBER": "12"}, persona: true,
			wantOut: []string{"A teardown: nothing to register with the persona provider."},
		},
		{
			name: "a teardown removes the registration", action: PersonaRemove,
			env: "export DOWN=\"true\"\nexport SKIP_DEPLOY=\"true\"\n", subs: map[string]string{"_ENV": "tst", "_PR_NUMBER": "12"}, persona: true,
			wantRequests: []string{"DELETE /persona/registrations/beacon-pr12"},
			wantOut:      []string{"Removed beacon-pr12 from "},
		},
		{
			name: "a removal of a registration the provider does not hold", action: PersonaRemove,
			env: "export DOWN=\"true\"\nexport SKIP_DEPLOY=\"true\"\n", subs: map[string]string{"_ENV": "tst", "_PR_NUMBER": "12"}, persona: true,
			refuse: http.StatusNotFound, message: "no registration beacon-pr12",
			wantRequests: []string{"DELETE /persona/registrations/beacon-pr12"},
			wantOut:      []string{"beacon-pr12 was not registered with ", ": nothing to remove."},
		},
		{
			name: "outside a teardown nothing is removed", action: PersonaRemove,
			subs: map[string]string{"_ENV": "tst", "_PR_NUMBER": "12"}, persona: true,
			wantOut: []string{"Not a teardown: nothing to remove from the persona provider."},
		},
		{
			name: "the provider refuses the deploy identity", action: PersonaRegister,
			env:  "export PR_HOSTNAME=\"beacon-pr12.impulseframework.dev\"\n",
			subs: map[string]string{"_ENV": "tst", "_PR_NUMBER": "12"}, persona: true,
			refuse: http.StatusForbidden, message: "deployer@example.com is not a deploy identity of beacon",
			wantRequests: []string{"PUT /persona/registrations/beacon-pr12"},
			wantErr:      "the provider answered 403: deployer@example.com is not a deploy identity of beacon",
		},
		{
			name: "a missing cast", action: PersonaRegister,
			env:  "export PR_HOSTNAME=\"beacon-pr12.impulseframework.dev\"\n",
			subs: map[string]string{"_ENV": "tst", "_PR_NUMBER": "12"}, persona: true, noCast: true,
			wantErr: "the keepers auth's persona registration has no cast at pkg/auth/keepers/cast.json",
		},
		{
			name: "no canonical hostname", action: PersonaRegister,
			subs: map[string]string{"_ENV": "tst", "_PR_NUMBER": "12"}, persona: true,
			wantErr: "the stack names no hostname for the deployment",
		},
		{
			name: "an action that is not one", action: "renew",
			subs:    map[string]string{"_ENV": "tst"},
			wantErr: `"renew" is not a persona action`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			provider := &personaProvider{refuse: tt.refuse, message: tt.message}
			server := httptest.NewServer(provider)
			t.Cleanup(server.Close)
			subs := map[string]string{}
			for k, v := range tt.subs {
				subs[k] = v
			}
			if tt.persona {
				subs["_PERSONA"] = registrations(server.URL)
			}
			files := map[string]string{
				EnvironmentFile: "export RELEASE=\"v1.2.3\"\n" + tt.env,
				BuildFile:       build(subs),
			}
			if !tt.noCast {
				files["pkg/auth/keepers/cast.json"] = cast
			}
			w := workspaceFiles(t, files)
			clients := &Clients{HTTP: server.Client(), IDToken: func(_ context.Context, audience string) (string, error) {
				return "token-for-" + audience, nil
			}}
			var out strings.Builder
			err := Persona(t.Context(), clients, w, tt.action, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Persona() error = %v, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("Persona() error = %v\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			var got []string
			for _, r := range provider.requests {
				got = append(got, r.Method+" "+r.Path)
				if want := "Bearer token-for-" + server.URL + "/persona"; r.Authorization != want {
					t.Errorf("Authorization = %q, want %q", r.Authorization, want)
				}
			}
			if strings.Join(got, ";") != strings.Join(tt.wantRequests, ";") {
				t.Errorf("requests = %v, want %v", got, tt.wantRequests)
			}
			if tt.wantBody != nil {
				if len(provider.requests) == 0 {
					t.Fatal("no request to read the body of")
				}
				gotBody, wantBody := provider.requests[0].Body, *tt.wantBody
				g, _ := json.Marshal(gotBody)
				w, _ := json.Marshal(wantBody)
				if !bytes.Equal(g, w) {
					t.Errorf("body = %s\nwant   %s", g, w)
				}
			}
		})
	}
}
