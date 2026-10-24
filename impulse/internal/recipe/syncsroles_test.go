package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// membersGoogle is an auth package registering one Google method, its RoleSync spliced in.
const membersGoogle = `package members

import (
	"context"

	"github.com/cccteam/access"
	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/session"
	"github.com/cccteam/session/google"
	"github.com/cccteam/session/sessionstorage"
	"github.com/cccteam/session/signin"
)

type Session = session.Auth[session.NoCustomData, session.NoCustomData]

type Auth struct {
	session *Session
}

func New(ctx context.Context, db *cloudspanner.Client, key string, collection access.PermissionCollection) (*Auth, error) {
	s, err := session.NewAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerAuth(db), key)
	if err != nil {
		return nil, err
	}
	if _, err := google.SignIn(s, "google", signin.MembersOfDirectory, &google.Config{RoleSync: %s}); err != nil {
		return nil, err
	}

	return &Auth{session: s}, nil
}
`

// portalProgram is a generator program binding the portal outlet to the members auth,
// the method's declaration spliced in.
const portalProgram = `package main

import (
	"context"

	"github.com/cccteam/ccc/resource/generation"
)

func newGenerator(ctx context.Context) (generation.Generator, error) {
	return generation.NewResourceGenerator(ctx, "pkg/resources", []string{"file://schema/migrations"},
		generation.GenerateHandlers("app"),
		generation.GenerateRouter(),
		generation.GenerateRoutes("pkg/router", "api"),
		generation.WithRouterOutlet("portal", "portal/api", generation.Auth("example.com/harbor/pkg/auth/members", %s), generation.WebApp("/portal")),
	)
}
`

func TestSyncsRoles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		files       map[string]string
		wantDetect  []string
		wantDid     []string
		wantProgram string
	}{
		{
			name: "a declaration without the flag gains it where the method file syncs roles",
			files: map[string]string{
				"pkg/auth/members/members.go": fmt.Sprintf(membersGoogle, "google.RoleSync(nil, \"members-\", 0)"),
				"cmd/generate/main.go":        fmt.Sprintf(portalProgram, `generation.GoogleMethod("google").Labeled("Company directory")`),
			},
			wantDetect:  []string{"cmd/generate/main.go:14: outlet portal declares the google method google on the members auth without .SyncsRoles(), and pkg/auth/members/members.go:25 registers it with RoleSync"},
			wantDid:     []string{`cmd/generate/main.go: .SyncsRoles() chained on generation.GoogleMethod("google") on 1 outlet Auth option(s) bound to the members auth, since pkg/auth/members/members.go:25 registers the method with RoleSync, so the regenerated users surface shows these people's roles as the directory's and never writes them`},
			wantProgram: `generation.Auth("example.com/harbor/pkg/auth/members", generation.GoogleMethod("google").Labeled("Company directory").SyncsRoles())`,
		},
		{
			name: "a declaration chaining the flag has nothing to do",
			files: map[string]string{
				"pkg/auth/members/members.go": fmt.Sprintf(membersGoogle, "google.RoleSync(nil, \"members-\", 0)"),
				"cmd/generate/main.go":        fmt.Sprintf(portalProgram, `generation.GoogleMethod("google").SyncsRoles()`),
			},
			wantProgram: `generation.Auth("example.com/harbor/pkg/auth/members", generation.GoogleMethod("google").SyncsRoles())`,
		},
		{
			name: "a method whose roles the application assigns has nothing to do",
			files: map[string]string{
				"pkg/auth/members/members.go": fmt.Sprintf(membersGoogle, "google.DisableRoleSync()"),
				"cmd/generate/main.go":        fmt.Sprintf(portalProgram, `generation.GoogleMethod("google")`),
			},
			wantProgram: `generation.Auth("example.com/harbor/pkg/auth/members", generation.GoogleMethod("google"))`,
		},
		{
			name:  "an application without an auth package has nothing to do",
			files: map[string]string{"cmd/generate/main.go": readerDeclaration},
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
				write(rel, content)
			}
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			r := SyncsRoles{}
			found, err := r.Detect(t.Context(), a)
			if err != nil {
				t.Fatalf("Detect() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDetect, found); diff != "" {
				t.Errorf("Detect() mismatch (-want +got):\n%s", diff)
			}
			ch, err := r.Apply(t.Context(), a, nil)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantDid, ch.Did); diff != "" {
				t.Errorf("Apply() Did mismatch (-want +got):\n%s", diff)
			}
			if tt.wantProgram != "" {
				program, err := os.ReadFile(filepath.Join(root, "cmd", "generate", "main.go"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(program), tt.wantProgram) {
					t.Errorf("the program lacks %q:\n%s", tt.wantProgram, program)
				}
			}
			// A second run reads the edited application and finds nothing.
			a, err = app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() again error = %v", err)
			}
			found, err = r.Detect(t.Context(), a)
			if err != nil {
				t.Fatalf("Detect() again error = %v", err)
			}
			if len(found) != 0 {
				t.Errorf("Detect() again = %v, want nothing", found)
			}
			ch, err = r.Apply(t.Context(), a, nil)
			if err != nil {
				t.Fatalf("Apply() again error = %v", err)
			}
			if len(ch.Did) != 0 {
				t.Errorf("Apply() again did %v, want nothing", ch.Did)
			}
		})
	}
}
