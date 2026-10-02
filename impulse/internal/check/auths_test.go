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
)

// staffAuth is a minimal auth package: it embeds its role file and hands it to the
// permission engine itself.
const staffAuth = `package staff

import (
	"context"
	_ "embed"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
)

const (
	Name        = "staff"
	TablePrefix = "Staff"
)

//go:embed roles.json
var roleFile access.RoleFile

// Roles is the release's default roles for the staff auth.
func Roles() access.RoleFile { return roleFile }

type Auth struct {
	session *session.PasswordAuth[session.NoCustomData, session.NoCustomData]
}

func New(ctx context.Context, db *cloudspanner.Client, key string, collection access.PermissionCollection) (*Auth, error) {
	if _, err := access.New(nil, access.WithDefaultRoles(collection, Roles())); err != nil {
		return nil, err
	}
	s, err := session.NewPasswordAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerPasswordAuth(db), key,
		session.WithSessionTableName(TablePrefix+"Sessions"), session.WithUserTableName(TablePrefix+"SessionUsers"))
	if err != nil {
		return nil, err
	}

	return &Auth{session: s}, nil
}

func (a *Auth) Session() *session.PasswordAuth[session.NoCustomData, session.NoCustomData] { return a.session }
`

// The staff auth package in other shapes: handing its role file to nobody, exporting no
// Roles(), and reading its role file without embedding it.
var (
	staffAuthUnhanded = strings.Replace(staffAuth, "\tif _, err := access.New(nil, access.WithDefaultRoles(collection, Roles())); err != nil {\n\t\treturn nil, err\n\t}\n", "", 1)
	staffAuthNoRoles  = strings.Replace(staffAuthUnhanded, "//go:embed roles.json\nvar roleFile access.RoleFile\n\n// Roles is the release's default roles for the staff auth.\nfunc Roles() access.RoleFile { return roleFile }\n\n", "", 1)
	staffAuthNoEmbed  = strings.Replace(staffAuth, "//go:embed roles.json\nvar roleFile access.RoleFile\n", "var roleFile access.RoleFile\n", 1)
)

const (
	staffConfig = `package config

import (
	"context"

	"example.com/harbor/pkg/auth/staff"
	"example.com/harbor/pkg/router"
)

type DataConfiguration struct {
	staff *staff.Auth
}

func New(ctx context.Context) (*DataConfiguration, error) {
	s, err := staff.New(ctx, nil, "", router.Collection())
	if err != nil {
		return nil, err
	}

	return &DataConfiguration{staff: s}, nil
}

func (c *DataConfiguration) Staff() *staff.Auth { return c.staff }
`
	// staffConfigHanding is a data level handing the staff auth's role file to the engine
	// itself, for an auth package that does not.
	staffConfigHanding = `package config

import (
	"context"

	"github.com/cccteam/access"

	"example.com/harbor/pkg/auth/staff"
	"example.com/harbor/pkg/router"
)

type DataConfiguration struct {
	staff *staff.Auth
}

func New(ctx context.Context) (*DataConfiguration, error) {
	if _, err := access.New(nil, access.WithDefaultRoles(router.Collection(), staff.Roles())); err != nil {
		return nil, err
	}
	s, err := staff.New(ctx, nil, "", router.Collection())
	if err != nil {
		return nil, err
	}

	return &DataConfiguration{staff: s}, nil
}

func (c *DataConfiguration) Staff() *staff.Auth { return c.staff }
`
	// staffDeploy is the deploy package's policy check over an auth's engine.
	staffDeploy = `package deploy

import (
	"context"
	"fmt"

	"github.com/cccteam/access"
)

func CheckRoles(ctx context.Context, client *access.Client, name string) error {
	warnings, err := client.CheckPolicy(ctx)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Printf("Warning: %s\n", w)
	}

	return nil
}
`
	staffBootstrap = `package main

import (
	"context"

	"github.com/cccteam/access"

	"example.com/harbor/pkg/auth/staff"
	"example.com/harbor/pkg/deploy"
)

func run(ctx context.Context, client *access.Client) error {
	return deploy.CheckRoles(ctx, client, staff.Name)
}
`
	staffApp = `package app

import "example.com/harbor/pkg/auth/staff"

type Configurer interface {
	Staff() *staff.Auth
}
`
	rolesJSON = "{\"roles\": {\"global\": [], \"domain\": []}}\n"
	// rolesValidation is the roles validation test with the staff row: it parses the
	// staff auth's role file and runs access.ValidateRoles over the collection.
	rolesValidation = `package deploy_test

import (
	"testing"

	"github.com/cccteam/access"

	"example.com/harbor/pkg/auth/staff"
)

func TestRoles(t *testing.T) {
	roles, err := staff.Roles().Parse()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := access.ValidateRoles(nil, roles); err != nil {
		t.Fatal(err)
	}
}
`
	// noValidation is a test that calls access.ValidateRoles for some other role file.
	noValidation = `package deploy_test

import (
	"testing"

	"github.com/cccteam/access"
)

func TestRoles(t *testing.T) {
	if _, err := access.ValidateRoles(nil, nil); err != nil {
		t.Fatal(err)
	}
}
`
)

// membersAuth is an OIDC auth package with the role-synchronization slot left to fill in.
const membersAuth = `package members

import (
	"context"
	_ "embed"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/access"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
)

const (
	Name        = "members"
	TablePrefix = "Members"
)

//go:embed roles.json
var roleFile access.RoleFile

func Roles() access.RoleFile { return roleFile }

type Auth struct {
	session *session.OIDCAzure[session.NoCustomData, session.NoCustomData]
}

func New(ctx context.Context, db *cloudspanner.Client, key string, collection access.PermissionCollection) (*Auth, error) {
	if _, err := access.New(nil, access.WithDefaultRoles(collection, Roles())); err != nil {
		return nil, err
	}
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
	"example.com/harbor/pkg/router"
)

type DataConfiguration struct {
	members *members.Auth
}

func New(ctx context.Context) (*DataConfiguration, error) {
	m, err := members.New(ctx, nil, "", router.Collection())
	if err != nil {
		return nil, err
	}

	return &DataConfiguration{members: m}, nil
}

func (c *DataConfiguration) Members() *members.Auth { return c.members }
`
	// membersBootstrap checks the members roles and assigns a development member.
	membersBootstrap = `package main

import (
	"context"

	"example.com/harbor/pkg/auth/members"
	"example.com/harbor/pkg/config"
	"example.com/harbor/pkg/deploy"
)

func run(ctx context.Context, data *config.DataConfiguration) error {
	if err := deploy.CheckRoles(ctx, data.Members().Access(), members.Name); err != nil {
		return err
	}

	return data.Members().Access().UserManager().AddUserRoles(ctx, nil, "client", "Administrator_Domain")
}
`
	membersApp = `package app

import "example.com/harbor/pkg/auth/members"

type Configurer interface {
	Members() *members.Auth
}
`
	// membersValidation is the roles validation test with the members row.
	membersValidation = `package deploy_test

import (
	"testing"

	"github.com/cccteam/access"

	"example.com/harbor/pkg/auth/members"
)

func TestRoles(t *testing.T) {
	roles, err := members.Roles().Parse()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := access.ValidateRoles(nil, roles); err != nil {
		t.Fatal(err)
	}
}
`
)

func TestAuthsWired(t *testing.T) {
	t.Parallel()

	wired := map[string]string{
		"pkg/auth/staff/staff.go":   staffAuth,
		"pkg/auth/staff/roles.json": rolesJSON,
		"pkg/config/data.go":        staffConfig,
		"pkg/deploy/deploy.go":      staffDeploy,
		"cmd/bootstrap/main.go":     staffBootstrap,
		"app/app.go":                staffApp,
		"pkg/deploy/deploy_test.go": rolesValidation,
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
	const noValidationTest = "pkg/auth/staff: no test validates the staff auth's role file (access.ValidateRoles over the collection, parsing staff.Roles()), so a warning the deploy prints is accepted nowhere in code; add the staff row to pkg/deploy/deploy_test.go with its expected warnings empty"

	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "wired", files: wired,
			wantStatus: Pass, wantSummary: "1 auth(s) constructed, roles handed to the engine, and bound: staff",
		},
		{
			name: "never constructed, never bound", files: without("pkg/config/data.go", "app/app.go"),
			wantStatus: Fail, wantSummary: "2 auth wiring problem(s)",
			wantDetails: []string{
				"pkg/auth/staff: nothing outside tests calls staff.New; the staff auth is never constructed",
				`pkg/auth/staff: no outlet declares Auth("example.com/harbor/pkg/auth/staff", ...) and no surface takes *staff.Auth; nothing binds to the staff auth, so its people can sign in nowhere`,
			},
		},
		{
			name: "the role file has no validation test", files: without("pkg/deploy/deploy_test.go"),
			wantStatus: Warn, wantSummary: "1 auth(s) constructed, roles handed to the engine, and bound: staff; 1 role file(s) without a validation test",
			wantDetails: []string{noValidationTest},
		},
		{
			name: "a validation test that parses another role file", files: with(without(), "pkg/deploy/deploy_test.go", noValidation),
			wantStatus: Warn, wantSummary: "1 auth(s) constructed, roles handed to the engine, and bound: staff; 1 role file(s) without a validation test",
			wantDetails: []string{noValidationTest},
		},
		{
			name: "the validation test is named after the package checking the policy", files: with(without("pkg/deploy/deploy.go", "pkg/deploy/deploy_test.go"), "pkg/release/release.go", staffDeploy),
			wantStatus: Warn, wantSummary: "1 auth(s) constructed, roles handed to the engine, and bound: staff; 1 role file(s) without a validation test",
			wantDetails: []string{strings.Replace(noValidationTest, "pkg/deploy/deploy_test.go", "pkg/release/release_test.go", 1)},
		},
		{
			name: "a wiring failure lists the missing validation test beneath it", files: without("pkg/deploy/deploy_test.go", "pkg/auth/staff/roles.json"),
			wantStatus: Fail, wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{
				"pkg/auth/staff: pkg/auth/staff/roles.json does not exist; the staff auth embeds its role file from there",
				noValidationTest,
			},
		},
		{
			name: "the role file never reaches the engine", files: with(without(), "pkg/auth/staff/staff.go", staffAuthUnhanded),
			wantStatus: Fail, wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{"pkg/auth/staff: nothing outside tests hands staff.Roles() to access.WithDefaultRoles; the staff auth's default roles never reach its permission engine"},
		},
		{
			name: "the role file handed over by the data level", files: with(with(without(), "pkg/auth/staff/staff.go", staffAuthUnhanded), "pkg/config/data.go", staffConfigHanding),
			wantStatus: Pass, wantSummary: "1 auth(s) constructed, roles handed to the engine, and bound: staff",
		},
		{
			name: "the package exports no Roles()", files: with(without(), "pkg/auth/staff/staff.go", staffAuthNoRoles),
			wantStatus: Fail, wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{"pkg/auth/staff: the package exports no Roles(); the staff auth's role file reaches the permission engine as staff.Roles() handed to access.WithDefaultRoles"},
		},
		{
			name: "the role file is not embedded", files: with(without(), "pkg/auth/staff/staff.go", staffAuthNoEmbed),
			wantStatus: Fail, wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{"pkg/auth/staff: no //go:embed roles.json directive in the package; the staff auth's role file does not travel with the release"},
		},
		{
			name: "the role file is missing", files: without("pkg/auth/staff/roles.json"),
			wantStatus: Fail, wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{"pkg/auth/staff: pkg/auth/staff/roles.json does not exist; the staff auth embeds its role file from there"},
		},
		{
			name: "an application-run directory auth assigns roles in the bootstrap",
			files: map[string]string{
				"pkg/auth/members/members.go": fmt.Sprintf(membersAuth, "session.DisableRoleSync()"),
				"pkg/auth/members/roles.json": rolesJSON,
				"pkg/config/data.go":          membersConfig,
				"pkg/deploy/deploy.go":        staffDeploy,
				"cmd/bootstrap/main.go":       membersBootstrap,
				"app/app.go":                  membersApp,
				"pkg/deploy/deploy_test.go":   membersValidation,
			},
			wantStatus: Pass, wantSummary: "1 auth(s) constructed, roles handed to the engine, and bound: members",
		},
		{
			name: "a directory-run auth with a role writer in the bootstrap",
			files: map[string]string{
				"pkg/auth/members/members.go": fmt.Sprintf(membersAuth, "session.RoleSync(nil)"),
				"pkg/auth/members/roles.json": rolesJSON,
				"pkg/config/data.go":          membersConfig,
				"pkg/deploy/deploy.go":        staffDeploy,
				"cmd/bootstrap/main.go":       membersBootstrap,
				"app/app.go":                  membersApp,
				"pkg/deploy/deploy_test.go":   membersValidation,
			},
			wantStatus: Fail, wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{"cmd/bootstrap/main.go:16: the members auth hands role membership to the directory (session.RoleSync), but this assigns roles in its store; the directory removes them at the next login. Assign the roles in the directory, or hand membership to the application (session.DisableRoleSync)"},
		},
		{
			name:       "two auths leave their cookies at the library defaults",
			files:      with(with(without(), "pkg/auth/members/members.go", fmt.Sprintf(membersAuth, "session.DisableRoleSync()")), "pkg/auth/members/roles.json", rolesJSON),
			wantStatus: Fail, wantSummary: "4 auth wiring problem(s)",
			wantDetails: []string{
				"pkg/auth/members: nothing outside tests calls members.New; the members auth is never constructed",
				`pkg/auth/members: no outlet declares Auth("example.com/harbor/pkg/auth/members", ...) and no surface takes *members.Auth; nothing binds to the members auth, so its people can sign in nowhere`,
				"pkg/auth/members, pkg/auth/staff: both issue their XSRF token in the cookie XSRF-TOKEN, so a login to one overwrites the other's token in the browser; name each auth's cookie (session.WithXSRFCookieName) and the same name in the web app that binds to it (withXsrfConfiguration)",
				"pkg/auth/members, pkg/auth/staff: both ride their sessions in the cookie auth, so a login to one ends the other's session in the browser; name each auth's cookie (session.WithCookieName)",
				"pkg/auth/members: no test validates the members auth's role file (access.ValidateRoles over the collection, parsing members.Roles()), so a warning the deploy prints is accepted nowhere in code; add the members row to pkg/deploy/deploy_test.go with its expected warnings empty",
			},
		},
		{
			name: "bound by the program's Auth declaration under the generated router",
			files: with(without("app/app.go"), "cmd/generate/main.go", program("pkg/resources",
				`generation.GenerateHandlers("app"),`,
				`generation.GenerateRouter(),`,
				`generation.GenerateRoutes("pkg/router", "api", generation.Auth("example.com/harbor/pkg/auth/staff", generation.Password), generation.WebApp("/")),`,
			)),
			wantStatus: Pass, wantSummary: "1 auth(s) constructed, roles handed to the engine, and bound: staff",
		},
		{
			name: "the program's flavor disagrees with the package",
			files: with(without(), "cmd/generate/main.go", program("pkg/resources",
				`generation.GenerateHandlers("app"),`,
				`generation.GenerateRouter(),`,
				`generation.GenerateRoutes("pkg/router", "api", generation.Auth("example.com/harbor/pkg/auth/staff", generation.OIDCAzure)),`,
			)),
			wantStatus: Fail, wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{"cmd/generate/main.go:15: outlet default binds to the staff auth as OIDCAzure, but pkg/auth/staff constructs a password authenticator; the generated router would mount the wrong login routes"},
		},
		{
			name: "an outlet bound to a package that is no auth",
			files: with(without(), "cmd/generate/main.go", program("pkg/resources",
				`generation.GenerateHandlers("app"),`,
				`generation.GenerateRouter(),`,
				`generation.GenerateRoutes("pkg/router", "api", generation.Auth("example.com/harbor/pkg/auth/staff", generation.Password)),`,
				`generation.WithRouterOutlet("portal", "portal/api", generation.Auth("example.com/harbor/pkg/config", generation.Password)),`,
			)),
			wantStatus: Fail, wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{`cmd/generate/main.go:16: outlet portal binds to Auth("example.com/harbor/pkg/config", ...), which is not one of the application's auth packages (example.com/harbor/pkg/auth/staff)`},
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

func TestCookieCollisions(t *testing.T) {
	t.Parallel()

	auth := func(file, cookie, xsrf string) app.Auth {
		return app.Auth{File: file, CookieName: cookie, XSRFCookieName: xsrf}
	}
	tests := []struct {
		name  string
		auths []app.Auth
		want  []string
	}{
		{name: "one auth", auths: []app.Auth{auth("pkg/auth/staff/staff.go", "", "")}},
		{
			name:  "two auths, every cookie named",
			auths: []app.Auth{auth("pkg/auth/staff/staff.go", "staff", "staff-xsrf"), auth("pkg/auth/members/members.go", "members", "members-xsrf")},
		},
		{
			name:  "two auths sharing both defaults",
			auths: []app.Auth{auth("pkg/auth/staff/staff.go", "", ""), auth("pkg/auth/members/members.go", "", "")},
			want: []string{
				"pkg/auth/members, pkg/auth/staff: both issue their XSRF token in the cookie XSRF-TOKEN, so a login to one overwrites the other's token in the browser; name each auth's cookie (session.WithXSRFCookieName) and the same name in the web app that binds to it (withXsrfConfiguration)",
				"pkg/auth/members, pkg/auth/staff: both ride their sessions in the cookie auth, so a login to one ends the other's session in the browser; name each auth's cookie (session.WithCookieName)",
			},
		},
		{
			name:  "two auths naming the same cookie",
			auths: []app.Auth{auth("pkg/auth/staff/staff.go", "staff", "token"), auth("pkg/auth/members/members.go", "members", "token")},
			want: []string{
				"pkg/auth/members, pkg/auth/staff: both issue their XSRF token in the cookie token, so a login to one overwrites the other's token in the browser; name each auth's cookie (session.WithXSRFCookieName) and the same name in the web app that binds to it (withXsrfConfiguration)",
			},
		},
		{
			name:  "a construction forwarding its options is not judged",
			auths: []app.Auth{auth("pkg/auth/staff/staff.go", "", ""), {File: "pkg/auth/members/members.go", OptionsForwarded: true}},
		},
		{
			name:  "constructions outside auth packages are not judged",
			auths: []app.Auth{auth("pkg/config/session.go", "", ""), auth("pkg/config/other.go", "", "")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, cookieCollisions(tt.auths)); diff != "" {
				t.Errorf("cookieCollisions() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
