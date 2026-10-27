package check

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

// The skeletons' construction of a user surface, as a flat application's: the staff
// auth's surface built with the generated constructor on line 14, handed the auth's
// administrator roles.
const (
	surfaceApp = `package app

import (
	"example.com/harbor/pkg/auth/staff"
	"example.com/harbor/pkg/router"
	"github.com/cccteam/ccc/resource"
)

type App struct {
	staffUsers *resource.UserSurface[router.StaffAuth]
}

func New(accounts resource.UserAccounts, seats resource.UserSeats) *App {
	return &App{staffUsers: router.NewStaffUserSurface(accounts, seats, resource.WithAdministratorRoles(staff.AdministratorRoles()))}
}
`
	surfaceRoles = ", resource.WithAdministratorRoles(staff.AdministratorRoles())"
	// directorySurfaceApp builds the members auth's surface on line 13 without the
	// option, as an auth whose directory names its roles does.
	directorySurfaceApp = `package app

import (
	"example.com/harbor/pkg/router"
	"github.com/cccteam/ccc/resource"
)

type App struct {
	membersUsers *resource.UserSurface[router.MembersAuth]
}

func New(accounts resource.UserAccounts, seats resource.UserSeats) *App {
	return &App{membersUsers: router.NewMembersUserSurface(accounts, seats)}
}
`
)

func TestAdministratorRoles(t *testing.T) {
	t.Parallel()

	const (
		taken   = "app/app.go:14: NewStaffUserSurface takes resource.WithAdministratorRoles"
		without = "NewStaffUserSurface is built without resource.WithAdministratorRoles; the user surface takes resource.WithAdministratorRoles(staff.AdministratorRoles()) so a self-unseat is refused only for the last holder of a partition's administrator role"
	)

	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name:        "the skeletons' surface takes the roles",
			files:       map[string]string{"app/app.go": surfaceApp},
			wantStatus:  Pass,
			wantSummary: "1 user surface(s) take resource.WithAdministratorRoles: app/app.go",
			wantDetails: []string{taken},
		},
		{
			name:        "a surface built without the roles",
			files:       map[string]string{"app/app.go": strings.Replace(surfaceApp, surfaceRoles, "", 1)},
			wantStatus:  Fail,
			wantSummary: "1 user surface(s) built without resource.WithAdministratorRoles",
			wantDetails: []string{"app/app.go:14: " + without},
		},
		{
			name:        "a surface forwarding its caller's options is not read",
			files:       map[string]string{"app/app.go": strings.Replace(surfaceApp, surfaceRoles, ", opts...", 1)},
			wantStatus:  Warn,
			wantSummary: "1 user surface(s) forward their caller's options, so whether they take resource.WithAdministratorRoles was not read",
			wantDetails: []string{"app/app.go:14: NewStaffUserSurface forwards its caller's options (opts...), so whether resource.WithAdministratorRoles is among them was not read"},
		},
		{
			name: "two sites' surfaces take the roles",
			files: map[string]string{
				"apps/console/app/app.go": surfaceApp,
				"apps/portal/app/app.go":  surfaceApp,
			},
			wantStatus:  Pass,
			wantSummary: "2 user surface(s) take resource.WithAdministratorRoles: apps/console/app/app.go, apps/portal/app/app.go",
			wantDetails: []string{
				"apps/console/app/app.go:14: NewStaffUserSurface takes resource.WithAdministratorRoles",
				"apps/portal/app/app.go:14: NewStaffUserSurface takes resource.WithAdministratorRoles",
			},
		},
		{
			name: "one site's surface built without the roles names the site",
			files: map[string]string{
				"apps/console/app/app.go": surfaceApp,
				"apps/portal/app/app.go":  strings.Replace(surfaceApp, surfaceRoles, "", 1),
			},
			wantStatus:  Fail,
			wantSummary: "1 user surface(s) built without resource.WithAdministratorRoles",
			wantDetails: []string{"apps/portal/app/app.go:14: " + without},
		},
		{
			name: "an auth whose directory names its roles builds its surface without the option",
			files: map[string]string{
				"app/app.go":                  directorySurfaceApp,
				"pkg/auth/members/members.go": fmt.Sprintf(membersAuth, "signin.RoleSync()"),
			},
			wantStatus:  Pass,
			wantSummary: "1 user surface(s) of an auth whose directory names its roles are built without resource.WithAdministratorRoles: app/app.go",
			wantDetails: []string{"app/app.go:13: NewMembersUserSurface names no administrator roles: the members auth's directory names its roles (every method is registered with RoleSync)"},
		},
		{
			name: "an auth with a method of its own beside the directory's is held",
			files: map[string]string{
				"app/app.go":                  directorySurfaceApp,
				"pkg/auth/members/members.go": fmt.Sprintf(membersAuthBeside, "signin.RoleSync()"),
			},
			wantStatus:  Fail,
			wantSummary: "1 user surface(s) built without resource.WithAdministratorRoles",
			wantDetails: []string{"app/app.go:13: NewMembersUserSurface is built without resource.WithAdministratorRoles; the user surface takes resource.WithAdministratorRoles(members.AdministratorRoles()) so a self-unseat is refused only for the last holder of a partition's administrator role"},
		},
		{
			name:        "a generated file's call is not the application's",
			files:       map[string]string{"app/zz_gen_app.go": surfaceApp},
			wantStatus:  Skip,
			wantSummary: "no user surface is built (no file outside generated files calls a New<Auth>UserSurface constructor)",
		},
		{
			name:        "no user surface",
			files:       map[string]string{"app/app.go": "package app\n"},
			wantStatus:  Skip,
			wantSummary: "no user surface is built (no file outside generated files calls a New<Auth>UserSurface constructor)",
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
			got := administratorRoles{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: administratorRoles{}.Name(), Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// candidateEdit is one change to a rendered candidate: the text of a file replaced
// once, which the file must carry.
type candidateEdit struct {
	file, old, new string
}

// renderCandidate renders a skeleton candidate into a temporary directory, applies the
// edits, and discovers the application.
func renderCandidate(t *testing.T, candidate string, edits ...candidateEdit) *app.App {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "beacon")
	if _, err := skeleton.Render(&skeleton.Options{Candidate: candidate, Dir: dir, ModulePath: "example.com/acme/beacon"}); err != nil {
		t.Fatalf("skeleton.Render() error = %v", err)
	}
	for _, e := range edits {
		p := filepath.Join(dir, filepath.FromSlash(e.file))
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), e.old) {
			t.Fatalf("%s of the %s candidate does not carry %q", e.file, candidate, e.old)
		}
		if err := os.WriteFile(p, []byte(strings.Replace(string(data), e.old, e.new, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := app.Discover(dir)
	if err != nil {
		t.Fatalf("app.Discover() error = %v", err)
	}

	return a
}

// TestAdministratorRolesCandidates holds the skeleton candidates to the check: each
// builds its surfaces with the roles, and a candidate with the wiring removed fails
// naming the site.
func TestAdministratorRolesCandidates(t *testing.T) {
	t.Parallel()

	const without = "NewStaffUserSurface is built without resource.WithAdministratorRoles; the user surface takes resource.WithAdministratorRoles(staff.AdministratorRoles()) so a self-unseat is refused only for the last holder of a partition's administrator role"

	tests := []struct {
		name        string
		candidate   string
		edits       []candidateEdit
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "solo", candidate: "solo",
			wantStatus: Pass, wantSummary: "1 user surface(s) take resource.WithAdministratorRoles: app/app.go",
			wantDetails: []string{"app/app.go:116: NewStaffUserSurface takes resource.WithAdministratorRoles"},
		},
		{
			name: "tenanted", candidate: "tenanted",
			wantStatus: Pass, wantSummary: "1 user surface(s) take resource.WithAdministratorRoles: app/app.go",
			wantDetails: []string{"app/app.go:137: NewStaffUserSurface takes resource.WithAdministratorRoles"},
		},
		{
			name: "outlets", candidate: "outlets",
			wantStatus: Pass, wantSummary: "2 user surface(s) take resource.WithAdministratorRoles: app/app.go",
			wantDetails: []string{
				"app/app.go:160: NewStaffUserSurface takes resource.WithAdministratorRoles",
				"app/app.go:161: NewMembersUserSurface takes resource.WithAdministratorRoles",
			},
		},
		{
			name: "sites", candidate: "sites",
			wantStatus: Pass, wantSummary: "2 user surface(s) take resource.WithAdministratorRoles: apps/console/app/app.go, apps/portal/app/app.go",
			wantDetails: []string{
				"apps/console/app/app.go:140: NewStaffUserSurface takes resource.WithAdministratorRoles",
				"apps/portal/app/app.go:140: NewStaffUserSurface takes resource.WithAdministratorRoles",
			},
		},
		{
			name: "tenanted with the roles removed", candidate: "tenanted",
			edits:      []candidateEdit{{file: "app/app.go", old: surfaceRoles}},
			wantStatus: Fail, wantSummary: "1 user surface(s) built without resource.WithAdministratorRoles",
			wantDetails: []string{"app/app.go:137: " + without},
		},
		{
			name: "sites with the portal's roles removed names the portal", candidate: "sites",
			edits:      []candidateEdit{{file: "apps/portal/app/app.go", old: surfaceRoles}},
			wantStatus: Fail, wantSummary: "1 user surface(s) built without resource.WithAdministratorRoles",
			wantDetails: []string{"apps/portal/app/app.go:140: " + without},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := renderCandidate(t, tt.candidate, tt.edits...)
			got := administratorRoles{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: administratorRoles{}.Name(), Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAdministratorRolesMeaning(t *testing.T) {
	t.Parallel()

	meaning := administratorRoles{}.Meaning()
	for _, want := range []string{"`New<Auth>UserSurface`", "`resource.WithAdministratorRoles(<auth>.AdministratorRoles())`", "last holder", "every method registered with `RoleSync`", "`app/app.go`"} {
		if !strings.Contains(meaning, want) {
			t.Errorf("Meaning() lacks %q", want)
		}
	}
}
