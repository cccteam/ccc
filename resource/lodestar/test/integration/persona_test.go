package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
)

// TestPersonaMethod holds the persona method to its two states. With no provider
// configured, as in every environment but the test environments, the console's methods
// route lists no persona method and the router mounts no persona route: the handlers are
// nil, and the generated router mounts and offers the method only where they are not.
// With the provider configured (the stack sets the issuer in a pull-request environment
// and the first environment), the method is offered last, under its label and at its
// route, and its login is mounted: under the skipAuth build the provider is simulated and
// the login returns the browser straight to the console's own callback, as the simulated
// directories do; against the provider itself the login sends the browser to its
// authorization endpoint as a public client with PKCE, which the session library's own
// tests hold and the lab proves.
//
// Demonstrates: auth.persona.
func TestPersonaMethod(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	t.Run("no provider configured: the method is neither offered nor mounted", func(t *testing.T) {
		t.Parallel()

		s := newServed(ctx, t)
		b := newBrowser(t, s, consoleAPI)
		status, body := b.do(ctx, http.MethodGet, consoleAPI+"/user/methods", nil)
		if status != http.StatusOK {
			t.Fatalf("GET /user/methods: status %d: %s", status, body)
		}
		var methods loginMethods
		if err := json.Unmarshal(body, &methods); err != nil {
			t.Fatal(err)
		}
		for _, m := range methods.Methods {
			if m.Name == crew.PersonaRegistration {
				t.Errorf("the methods route offers %s with no provider configured: %s", m.Name, body)
			}
		}
		if status, _ := b.do(ctx, http.MethodGet, consoleAPI+"/user/"+crew.PersonaRegistration+"/login", nil); status != http.StatusNotFound {
			t.Errorf("GET /user/persona/login with no provider configured: status %d, want 404 (the route is not mounted)", status)
		}
	})

	t.Run("the provider configured: offered last, and the login is mounted", func(t *testing.T) {
		t.Parallel()

		s := newServedWith(ctx, t, servedOptions{persona: &auth.Persona{Issuer: "https://persona.example.com", ClientID: "lodestar-pr12"}})
		b := newBrowser(t, s, consoleAPI)
		status, body := b.do(ctx, http.MethodGet, consoleAPI+"/user/methods", nil)
		if status != http.StatusOK {
			t.Fatalf("GET /user/methods: status %d: %s", status, body)
		}
		var methods loginMethods
		if err := json.Unmarshal(body, &methods); err != nil {
			t.Fatal(err)
		}
		last := methods.Methods[len(methods.Methods)-1]
		if last.Name != crew.PersonaRegistration || last.Method != "oidc" || last.Kind != "redirect" || last.Label != "Persona" || last.Path != consoleAPI+"/user/persona/login" {
			t.Errorf("the last method = %+v, want the persona method under its label at its route", last)
		}

		status, location := b.redirect(ctx, consoleAPI+"/user/"+crew.PersonaRegistration+"/login")
		if status != http.StatusFound && status != http.StatusSeeOther && status != http.StatusTemporaryRedirect {
			t.Fatalf("GET /user/persona/login: status %d, want a redirect (the route is mounted where the provider is configured)", status)
		}
		if !strings.HasSuffix(location, consoleAPI+"/user/"+crew.PersonaRegistration+"/callback") {
			t.Errorf("the simulated provider returns the browser to %s, want the console's persona callback", location)
		}
	})
}

// TestPersonaCast seeds the cast: a password-less crew login per persona, seated in the
// roles the cast names where the role file holds them (Headquarters in the global
// partition, SectorMarshal and Cadet in every sector), with the persona's address; a
// second run finds every login and creates none, so every start seeds the cast again
// harmlessly.
//
// Demonstrates: auth.persona.
func TestPersonaCast(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	s := newServed(ctx, t)

	created, err := s.crew.SeedCast(ctx)
	if err != nil {
		t.Fatalf("SeedCast() error = %v", err)
	}
	if created != 3 {
		t.Errorf("SeedCast() created %d logins, want the cast's 3", created)
	}
	api := s.crew.Session().API()
	manager := s.access.UserManager()
	for _, tt := range []struct {
		login string
		scope accesstypes.PolicyScope
		role  string
	}{
		{login: "hq", scope: accesstypes.GlobalPolicyScope(), role: "Headquarters"},
		{login: "warden", scope: accesstypes.EveryDomainPolicyScope(), role: "SectorMarshal"},
		{login: "rookie", scope: accesstypes.EveryDomainPolicyScope(), role: "Cadet"},
	} {
		user, err := api.UserByUsername(ctx, tt.login)
		if err != nil {
			t.Fatalf("UserByUsername(%s) error = %v: the seed made no login", tt.login, err)
		}
		if user.Email == nil || *user.Email != tt.login+"@example.com" {
			t.Errorf("%s's address = %v, want the cast's", tt.login, user.Email)
		}
		roles, err := manager.UserRoles(ctx, accesstypes.User(tt.login), tt.scope)
		if err != nil {
			t.Fatalf("UserRoles(%s) error = %v", tt.login, err)
		}
		if !strings.Contains(strings.Join(roleNames(roles), ","), tt.role) {
			t.Errorf("%s holds %v, want %s", tt.login, roles, tt.role)
		}
	}

	again, err := s.crew.SeedCast(ctx)
	if err != nil {
		t.Fatalf("SeedCast() again error = %v", err)
	}
	if again != 0 {
		t.Errorf("SeedCast() again created %d logins, want none", again)
	}
}

// roleNames flattens a role collection to its role names.
func roleNames(roles accesstypes.RoleCollection) []string {
	var names []string
	for _, held := range roles {
		for _, role := range held {
			names = append(names, string(role))
		}
	}

	return names
}
