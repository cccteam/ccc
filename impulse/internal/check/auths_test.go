package check

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// staffAuth is a minimal auth package.
const staffAuth = `package staff

import (
	"context"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
)

const (
	Name        = "staff"
	TablePrefix = "Staff"
	RolesPath   = "schema/roles/" + Name + ".json"
)

type Auth struct {
	session *session.PasswordAuth[session.NoCustomData, session.NoCustomData]
}

func New(ctx context.Context, db *cloudspanner.Client, key string) (*Auth, error) {
	s, err := session.NewPasswordAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerPasswordAuth(db), key,
		session.WithSessionTableName(TablePrefix+"Sessions"), session.WithUserTableName(TablePrefix+"SessionUsers"))
	if err != nil {
		return nil, err
	}

	return &Auth{session: s}, nil
}

func (a *Auth) Session() *session.PasswordAuth[session.NoCustomData, session.NoCustomData] { return a.session }
`

const (
	staffConfig = `package config

import (
	"context"

	"example.com/harbor/pkg/auth/staff"
)

type DataConfiguration struct {
	staff *staff.Auth
}

func New(ctx context.Context) (*DataConfiguration, error) {
	s, err := staff.New(ctx, nil, "")
	if err != nil {
		return nil, err
	}

	return &DataConfiguration{staff: s}, nil
}

func (c *DataConfiguration) Staff() *staff.Auth { return c.staff }
`
	staffBootstrap = `package main

import (
	"context"

	"github.com/cccteam/access"

	"example.com/harbor/pkg/auth/staff"
	"example.com/harbor/pkg/deploy"
)

func run(ctx context.Context, manager access.UserManager, roles *access.RoleConfig) error {
	return deploy.MigrateRoles(ctx, manager, roles, staff.RolesPath)
}
`
	staffPrinter = `package main

import (
	"fmt"

	"example.com/harbor/pkg/auth/staff"
)

func main() { fmt.Println(staff.RolesPath) }
`
	staffApp = `package app

import "example.com/harbor/pkg/auth/staff"

type Configurer interface {
	Staff() *staff.Auth
}
`
	rolesWrapper = `package deploy

import (
	"context"

	"github.com/cccteam/access"
	"github.com/cccteam/ccc/accesstypes"
)

func MigrateRoles(ctx context.Context, manager access.UserManager, roles *access.RoleConfig, rolesPath string, domains ...accesstypes.Domain) error {
	return access.MigrateRoles(ctx, manager, nil, roles, domains...)
}
`
	rolesJSON = "{\"roles\": {\"global\": [], \"domain\": []}}\n"
)

// membersAuth is an OIDC auth package with the role-synchronization slot left to fill in.
const membersAuth = `package members

import (
	"context"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
)

const (
	Name        = "members"
	TablePrefix = "Members"
	RolesPath   = "schema/roles/" + Name + ".json"
)

type Auth struct {
	session *session.OIDCAzure[session.NoCustomData, session.NoCustomData]
}

func New(ctx context.Context, db *cloudspanner.Client, key string) (*Auth, error) {
	s, err := session.NewOIDCAzure[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerOIDC(db, sessionstorage.WithOIDCUsers()), %s, key, "", "", "", "",
		session.WithSessionTableName(TablePrefix+"Sessions"), session.WithOIDCUserTableName(TablePrefix+"OIDCUsers"))
	if err != nil {
		return nil, err
	}

	return &Auth{session: s}, nil
}

func (a *Auth) Session() *session.OIDCAzure[session.NoCustomData, session.NoCustomData] { return a.session }
`

const (
	membersConfig = `package config

import (
	"context"

	"example.com/harbor/pkg/auth/members"
)

type DataConfiguration struct {
	members *members.Auth
}

func New(ctx context.Context) (*DataConfiguration, error) {
	m, err := members.New(ctx, nil, "")
	if err != nil {
		return nil, err
	}

	return &DataConfiguration{members: m}, nil
}

func (c *DataConfiguration) Members() *members.Auth { return c.members }
`
	// membersBootstrap provisions the members roles and assigns a development member.
	membersBootstrap = `package main

import (
	"context"

	"github.com/cccteam/access"

	"example.com/harbor/pkg/auth/members"
	"example.com/harbor/pkg/config"
	"example.com/harbor/pkg/deploy"
)

func run(ctx context.Context, data *config.DataConfiguration, roles *access.RoleConfig) error {
	if err := deploy.MigrateRoles(ctx, data.Members().Access().UserManager(), roles, members.RolesPath); err != nil {
		return err
	}

	return data.Members().Access().UserManager().AddUserRoles(ctx, nil, "client", "Administrator")
}
`
	membersApp = `package app

import "example.com/harbor/pkg/auth/members"

type Configurer interface {
	Members() *members.Auth
}
`
)

func TestAuthsWired(t *testing.T) {
	t.Parallel()

	wired := map[string]string{
		"pkg/auth/staff/staff.go": staffAuth,
		"pkg/config/data.go":      staffConfig,
		"pkg/deploy/deploy.go":    rolesWrapper,
		"cmd/bootstrap/main.go":   staffBootstrap,
		"app/app.go":              staffApp,
		"schema/roles/staff.json": rolesJSON,
	}
	without := func(keys ...string) map[string]string {
		files := map[string]string{}
		for k, v := range wired {
			files[k] = v
		}
		for _, k := range keys {
			delete(files, k)
		}

		return files
	}
	with := func(files map[string]string, k, v string) map[string]string {
		files[k] = v

		return files
	}

	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "wired", files: wired,
			wantStatus: Pass, wantSummary: "1 auth(s) constructed, provisioned, and bound: staff",
		},
		{
			name: "never constructed, never bound", files: without("pkg/config/data.go", "app/app.go"),
			wantStatus: Fail, wantSummary: "2 auth wiring problem(s)",
			wantDetails: []string{
				"pkg/auth/staff: nothing outside tests calls staff.New; the staff auth is never constructed",
				"pkg/auth/staff: no surface takes *staff.Auth; nothing binds to the staff auth, so its people can sign in nowhere",
			},
		},
		{
			name: "roles never provisioned", files: without("cmd/bootstrap/main.go"),
			wantStatus: Fail, wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{"pkg/auth/staff: nothing outside tests reads staff.RolesPath; the staff auth's roles are never provisioned"},
		},
		{
			name: "roles path read where no roles are migrated", files: with(without("cmd/bootstrap/main.go"), "cmd/print/main.go", staffPrinter),
			wantStatus: Fail, wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{"pkg/auth/staff: staff.RolesPath is read (cmd/print/main.go) but not by a file that migrates roles; the staff auth's roles are never provisioned"},
		},
		{
			name: "the roles file is missing", files: without("schema/roles/staff.json"),
			wantStatus: Fail, wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{"pkg/auth/staff: staff.RolesPath names schema/roles/staff.json, which does not exist"},
		},
		{
			name: "an application-run directory auth assigns roles in the bootstrap",
			files: map[string]string{
				"pkg/auth/members/members.go": fmt.Sprintf(membersAuth, "session.DisableRoleSync()"),
				"pkg/config/data.go":          membersConfig,
				"pkg/deploy/deploy.go":        rolesWrapper,
				"cmd/bootstrap/main.go":       membersBootstrap,
				"app/app.go":                  membersApp,
				"schema/roles/members.json":   rolesJSON,
			},
			wantStatus: Pass, wantSummary: "1 auth(s) constructed, provisioned, and bound: members",
		},
		{
			name: "a directory-run auth with a role writer in the bootstrap",
			files: map[string]string{
				"pkg/auth/members/members.go": fmt.Sprintf(membersAuth, "session.RoleSync(nil, nil)"),
				"pkg/config/data.go":          membersConfig,
				"pkg/deploy/deploy.go":        rolesWrapper,
				"cmd/bootstrap/main.go":       membersBootstrap,
				"app/app.go":                  membersApp,
				"schema/roles/members.json":   rolesJSON,
			},
			wantStatus: Fail, wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{"cmd/bootstrap/main.go:18: the members auth hands role membership to the directory (session.RoleSync), but this assigns roles in its store; the directory removes them at the next login. Assign the roles in the directory, or hand membership to the application (session.DisableRoleSync)"},
		},
		{
			name: "an authenticator outside an auth package", files: map[string]string{"pkg/config/session.go": authFile(passwordDefault)},
			wantStatus: Warn, wantSummary: "no auth package: the authenticators are constructed outside pkg/auth/<name> packages, so the auths cannot be told apart",
			wantDetails: []string{"pkg/config/session.go:9: password"},
		},
		{
			name: "no authenticator", files: map[string]string{"pkg/config/doc.go": "package config\n"},
			wantStatus: Skip, wantSummary: "no session authenticator is constructed",
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
			got := authsWired{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: "auths-wired", Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
