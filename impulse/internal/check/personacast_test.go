package check

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// The staff auth registering the persona method beside its password method.
var staffPersonaAuth = strings.Replace(strings.Replace(staffAuth,
	"\t\"github.com/cccteam/session/sessionstorage\"\n",
	"\t\"github.com/cccteam/session/oidc\"\n\t\"github.com/cccteam/session/sessionstorage\"\n\t\"github.com/cccteam/session/signin\"\n", 1),
	"\tsession.PasswordSignIn(s)\n",
	"\tsession.PasswordSignIn(s)\n\t_ = oidc.SignIn(s, \"persona\", nil, &oidc.Config{Issuer: \"https://persona.example.com\", RoleSync: signin.DisableRoleSync()})\n", 1)

func TestPersonaCast(t *testing.T) {
	t.Parallel()

	const (
		rolesDefined = `{"roles": {"global": [{"name": "Administrator", "description": "Manages the application's people.", "permissions": {}}], "domain": [{"name": "Auditor", "description": "Reads one tenant's records.", "permissions": {}}]}}`
		castSeated   = `{"personas": [{"login": "admin", "name": "Admin Example", "email": "admin@example.com", "roles": ["Administrator"]}, {"login": "auditor", "roles": ["Auditor"]}]}`
	)
	with := func(auth, roles, cast string) map[string]string {
		return map[string]string{
			"pkg/auth/staff/staff.go":   auth,
			"pkg/auth/staff/roles.json": roles,
			"pkg/auth/staff/cast.json":  cast,
			"pkg/config/data.go":        staffConfig,
			"app/app.go":                staffApp,
		}
	}

	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "a cast whose roles the role file defines", files: with(staffPersonaAuth, rolesDefined, castSeated),
			wantStatus: Pass, wantSummary: "1 cast(s) name their personas and the roles seated: pkg/auth/staff/cast.json (2 persona(s))",
		},
		{
			name: "a cast naming no role reads no role file", files: with(staffPersonaAuth, "", `{"personas": [{"login": "guest"}]}`),
			wantStatus: Pass, wantSummary: "1 cast(s) name their personas and the roles seated: pkg/auth/staff/cast.json (1 persona(s))",
		},
		{
			name: "the cast is missing", files: with(staffPersonaAuth, rolesDefined, ""),
			wantStatus: Fail, wantSummary: "1 finding(s) in the persona cast(s)",
			wantDetails: []string{"pkg/auth/staff/cast.json does not exist; the staff auth registers the persona method, whose sign-in links to the personas the cast names and whose deploy registers them with the provider (impulse add method persona writes an empty cast to fill in)"},
		},
		{
			name: "the empty cast add method writes", files: with(staffPersonaAuth, rolesDefined, "{\n  \"personas\": []\n}\n"),
			wantStatus: Fail, wantSummary: "1 finding(s) in the persona cast(s)",
			wantDetails: []string{`pkg/auth/staff/cast.json: the cast names no persona: list the personas the test environments seed under "personas"`},
		},
		{
			name: "a persona naming a role the file does not define", files: with(staffPersonaAuth, rolesDefined, `{"personas": [{"login": "cadet", "roles": ["Cadet", "Auditor"]}]}`),
			wantStatus: Fail, wantSummary: "1 finding(s) in the persona cast(s)",
			wantDetails: []string{"pkg/auth/staff/cast.json: persona cadet names role Cadet, which pkg/auth/staff/roles.json does not define"},
		},
		{
			name: "a persona naming a role with no role file to define it", files: with(staffPersonaAuth, "", castSeated),
			wantStatus: Fail, wantSummary: "1 finding(s) in the persona cast(s)",
			wantDetails: []string{"pkg/auth/staff/cast.json: the personas' roles cannot be checked against pkg/auth/staff/roles.json: no such file or directory"},
		},
		{
			name: "no persona method", files: with(staffAuth, rolesDefined, ""),
			wantStatus: Skip, wantSummary: "no Auth registers the persona method (oidc.SignIn)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			write := func(rel, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write("go.mod", "module example.com/harbor\n\ngo 1.26.6\n")
			for rel, content := range tt.files {
				if content == "" {
					continue
				}
				write(rel, content)
			}
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			got := personaCast{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: "persona-cast", Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
