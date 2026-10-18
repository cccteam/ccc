package skeleton

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/authpkg"
	"github.com/cccteam/ccc/impulse/internal/names"
)

// candidateAuths are the auth packages each candidate carries, as authpkg renders them.
// The candidates are the rendering's source of truth for the transitions that copy them,
// and the rendering is the candidates' source of truth, so this test holds the two
// together: run with IMPULSE_UPDATE_AUTHPKG=1 to rewrite the candidates' auth packages
// from the templates after changing either.
var candidateAuths = map[string][]authpkg.Spec{
	"solo":     {staffSpec("solo")},
	"tenanted": {staffSpec("tenanted")},
	"sites":    {staffSpec("sites")},
	"outlets": {staffSpec("outlets"), {
		Module: candidateModule("outlets"), AuthDir: "pkg/auth", Name: "members",
		Doc:     "the people who sign in through the organization's directory (OpenID Connect against Microsoft Entra), the portal's people",
		Methods: []authpkg.Method{{Kind: app.MethodAzure, Name: "directory", Authority: app.AuthorityApplication}},
		// The development member's domain role (cmd/bootstrap/users.json), in every domain.
		AdministratorRoles: authpkg.AdministratorRoles{EveryDomain: []string{"Administrator_Domain"}},
	}},
}

// staffSpec is the staff auth of each candidate: a password auth whose first administrator
// holds what the development administrator holds (cmd/bootstrap/users.json): the global
// roles, and the domain role in every domain where the candidate has tenants.
func staffSpec(candidate string) authpkg.Spec {
	roles := authpkg.AdministratorRoles{Global: []string{"Administrator_Global", "FeatureAdministrator"}, EveryDomain: []string{"Administrator_Domain"}}
	if candidate == Base {
		roles.EveryDomain = nil
	}

	return authpkg.Spec{
		Module: candidateModule(candidate), AuthDir: "pkg/auth", Name: PlaceholderAuth,
		Doc:                "the people who sign in to the console with a password",
		Methods:            []authpkg.Method{{Kind: app.MethodPassword}},
		AdministratorRoles: roles,
	}
}

func candidateModule(candidate string) string {
	return "github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/" + candidate
}

// candidateMigrations are the sequence numbers each candidate's auth session migrations
// carry, by auth: the users migration and the sessions migration, in that order.
var candidateMigrations = map[string]map[string][2]int{
	"solo":     {PlaceholderAuth: {3, 4}},
	"tenanted": {PlaceholderAuth: {3, 4}},
	"sites":    {PlaceholderAuth: {3, 4}},
	"outlets":  {PlaceholderAuth: {3, 4}, "members": {9, 10}},
}

// TestAuthPackages holds each candidate's auth packages and their session migrations to
// the authpkg rendering.
func TestAuthPackages(t *testing.T) {
	t.Parallel()
	update := os.Getenv("IMPULSE_UPDATE_AUTHPKG") != ""
	for candidate, specs := range candidateAuths {
		for i := range specs {
			spec := specs[i]
			files, err := authpkg.Files(&spec)
			if err != nil {
				t.Fatalf("%s: authpkg.Files(%s) error = %v", candidate, spec.Name, err)
			}
			dir := filepath.Join("_candidates", candidate, filepath.FromSlash(spec.AuthDir), spec.Name)
			expected := map[string][]byte{}
			for name, data := range files {
				expected[filepath.Join(dir, name)] = data
			}
			numbers := candidateMigrations[candidate][spec.Name]
			for j, m := range authpkg.Migrations(names.Pascal(spec.Name), spec.Name) {
				base := filepath.Join("_candidates", candidate, "schema", "migrations", fmt.Sprintf("%06d_%s", numbers[j], m.Stem))
				expected[base+".up.sql"] = []byte(m.Up)
				expected[base+".down.sql"] = []byte(m.Down)
			}
			for file, want := range expected {
				if update {
					if err := os.WriteFile(file, want, 0o600); err != nil {
						t.Fatal(err)
					}

					continue
				}
				got, err := os.ReadFile(file)
				if err != nil {
					t.Errorf("%s: %v (run with IMPULSE_UPDATE_AUTHPKG=1 to write the auth packages)", candidate, err)

					continue
				}
				if !bytes.Equal(got, want) {
					t.Errorf("%s: %s differs from the authpkg rendering (run with IMPULSE_UPDATE_AUTHPKG=1 to rewrite it)", candidate, file)
				}
			}
		}
	}
}
