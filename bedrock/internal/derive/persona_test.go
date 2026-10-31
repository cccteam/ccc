package derive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/impulse/app"
)

// TestPersona derives the beacon fixture, whose keepers auth registers the persona method
// beside its password method and whose placement names the provider: the method carries
// the three settings under their roles, the callback on the outlet, and the cast; the
// placement and the method are held to each other; a cast that is missing or malformed
// is refused naming the registration.
func TestPersona(t *testing.T) {
	t.Parallel()

	placement := func(t *testing.T, persona *PersonaPlacement) *Placement {
		t.Helper()
		p, err := ReadPlacement(filepath.Join("testdata", "placement-beacon.json"))
		if err != nil {
			t.Fatal(err)
		}
		p.Persona = persona

		return p
	}
	// copyFixture copies the beacon fixture to a directory of the test's own, where a
	// case may change the cast.
	copyFixture := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.CopyFS(dir, os.DirFS(filepath.Join("testdata", "beacon"))); err != nil {
			t.Fatal(err)
		}

		return dir
	}

	t.Run("the persona method, its settings, its callback and its cast", func(t *testing.T) {
		t.Parallel()

		a, err := app.Discover(filepath.Join("testdata", "beacon"))
		if err != nil {
			t.Fatal(err)
		}
		m, err := Derive(a, placement(t, &PersonaPlacement{Issuer: "https://persona.example.com/persona", ClientID: "beacon"}))
		if err != nil {
			t.Fatalf("Derive() error = %v", err)
		}
		personas := m.Personas()
		if len(personas) != 1 {
			t.Fatalf("Personas() = %d, want the keepers auth's", len(personas))
		}
		p := personas[0]
		if p.Auth != "keepers" || p.Name != "persona" || !p.Persona() || p.Directory() {
			t.Errorf("method = %s %s (persona %t, directory %t)", p.Auth, p.Name, p.Persona(), p.Directory())
		}
		for field, role := range map[string]Role{FieldIssuer: RoleIssuer, FieldClientID: RolePersonaClientID, FieldRedirectURL: RoleRedirectURL} {
			v := p.Variable(field)
			if v == nil || v.Role != role {
				t.Errorf("%s = %+v, want role %s", field, v, role)
			}
		}
		if p.Callback.Path != "/api/user/persona/callback" || p.Outlet == "" {
			t.Errorf("callback = %+v on outlet %q", p.Callback, p.Outlet)
		}
		if p.CastFile != "pkg/auth/keepers/cast.json" {
			t.Errorf("CastFile = %q", p.CastFile)
		}
		if len(m.DirectoryMethods()) != 0 {
			t.Errorf("DirectoryMethods() = %d, want none: the persona method is not a directory method", len(m.DirectoryMethods()))
		}
	})

	t.Run("the placement names no provider", func(t *testing.T) {
		t.Parallel()

		a, err := app.Discover(filepath.Join("testdata", "beacon"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = Derive(a, placement(t, nil))
		want := "the keepers auth's persona registration is the persona method and placement.json names no persona provider"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Derive() error = %v, want %q", err, want)
		}
	})

	t.Run("the placement names a provider nothing registers", func(t *testing.T) {
		t.Parallel()

		a, err := app.Discover(filepath.Join("testdata", "harbor"))
		if err != nil {
			t.Fatal(err)
		}
		p, err := ReadPlacement(filepath.Join("testdata", "placement.json"))
		if err != nil {
			t.Fatal(err)
		}
		p.Persona = &PersonaPlacement{Issuer: "https://persona.example.com", ClientID: "harbor"}
		_, err = Derive(a, p)
		want := "placement.json names a persona provider (persona) and no auth registers the persona method"
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Derive() error = %v, want %q", err, want)
		}
	})

	casts := []struct {
		name string
		cast string
		want string
	}{
		{name: "a missing cast", cast: "", want: "the keepers auth's persona registration (persona) has no cast at pkg/auth/keepers/cast.json"},
		{name: "the empty cast add method writes", cast: "{\n  \"personas\": []\n}\n", want: "pkg/auth/keepers/cast.json: the cast names no persona"},
		{name: "a login twice", cast: `{"personas": [{"login": "admin"}, {"login": "admin"}]}`, want: `pkg/auth/keepers/cast.json: the login "admin" names two personas`},
	}
	for _, tt := range casts {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := copyFixture(t)
			cast := filepath.Join(dir, "pkg", "auth", "keepers", "cast.json")
			if tt.cast == "" {
				if err := os.Remove(cast); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(cast, []byte(tt.cast), 0o600); err != nil {
				t.Fatal(err)
			}
			a, err := app.Discover(dir)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Derive(a, placement(t, &PersonaPlacement{Issuer: "https://persona.example.com", ClientID: "beacon"}))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Derive() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPersonaPlacementValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		persona *PersonaPlacement
		wantErr string
	}{
		{name: "no provider"},
		{name: "an issuer with a path and a client id root", persona: &PersonaPlacement{Issuer: "https://redgrade.example.com/persona", ClientID: "lanterns"}},
		{name: "a plain http issuer", persona: &PersonaPlacement{Issuer: "http://persona.example.com", ClientID: "lanterns"}, wantErr: "persona.issuer"},
		{name: "an issuer with a query", persona: &PersonaPlacement{Issuer: "https://persona.example.com/?x=1", ClientID: "lanterns"}, wantErr: "persona.issuer"},
		{name: "a client id root starting with a digit", persona: &PersonaPlacement{Issuer: "https://persona.example.com", ClientID: "1st"}, wantErr: "persona.clientId"},
		{name: "a client id root with a capital", persona: &PersonaPlacement{Issuer: "https://persona.example.com", ClientID: "Lanterns"}, wantErr: "persona.clientId"},
		{name: "an empty client id root", persona: &PersonaPlacement{Issuer: "https://persona.example.com"}, wantErr: "persona.clientId"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.persona.Validate()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("Validate() error = %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
