package transition

import (
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/authpkg"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

// staffPackage is the base's staff auth package, read from the embedded skeleton: the
// auth the new one is written beside.
func staffPackage(t *testing.T, file string) string {
	t.Helper()

	sub, err := skeleton.FS("solo")
	if err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(sub, "pkg/auth/staff/"+file)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// authConfig is a data level constructing the staff auth.
const authConfig = `package config

import (
	"context"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/go-playground/errors/v5"

	"example.com/acme/beacon/pkg/auth/staff"
)

// DataConfiguration is the second level.
type DataConfiguration struct {
	spannerClient *cloudspanner.Client
	staff         *staff.Auth
}

// NewDataConfiguration opens the clients.
func NewDataConfiguration(ctx context.Context) (*DataConfiguration, error) {
	spannerClient, cookieKey, err := open(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "open()")
	}

	staffAuth, err := staff.New(ctx, spannerClient, &staff.Settings{CookieKey: cookieKey})
	if err != nil {
		return nil, errors.Wrap(err, "staff.New()")
	}

	return &DataConfiguration{
		spannerClient: spannerClient,
		staff:         staffAuth,
	}, nil
}
`

// authConfigEnv is a data level reading its settings from an environment struct, as the
// base does.
const authConfigEnv = `package config

import (
	"context"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/go-playground/errors/v5"

	"example.com/acme/beacon/pkg/auth/staff"
)

// DataConfiguration is the second level.
type DataConfiguration struct {
	spannerClient *cloudspanner.Client
	staff         *staff.Auth
}

// NewDataConfiguration opens the clients.
func NewDataConfiguration(ctx context.Context) (*DataConfiguration, error) {
	env := &dataConfig{}
	spannerClient, cookieKey, err := open(ctx, env)
	if err != nil {
		return nil, errors.Wrap(err, "open()")
	}

	staffAuth, err := staff.New(ctx, spannerClient, &staff.Settings{CookieKey: cookieKey, SessionTimeout: env.SessionTimeout})
	if err != nil {
		return nil, errors.Wrap(err, "staff.New()")
	}

	return &DataConfiguration{
		spannerClient: spannerClient,
		staff:         staffAuth,
	}, nil
}

// dataConfig holds the environment every database-opening process reads.
type dataConfig struct {
	// SessionTimeout is the idle timeout of a browser session.
	SessionTimeout time.Duration ` + "`" + `env:"APP_DEFAULT_SESSION_TIMEOUT,default=10m"` + "`" + `
}
`

// authRouterProgram is a generator program under the generated router: the console on
// the default outlet, a portal outlet, both bound to the staff auth, and an API-key
// outlet.
const authRouterProgram = `// Package main is the beacon resource generator program.
package main

import (
	"context"
	"log"

	"github.com/cccteam/ccc/resource/generation"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	generator, err := generation.NewResourceGenerator(
		ctx,
		"pkg/resources",
		[]string{"file://schema/migrations"},
		generation.GenerateHandlers("app"),
		generation.GenerateRouter(),
		generation.GenerateRoutes("pkg/router", "console/api",
			generation.Auth("example.com/acme/beacon/pkg/auth/staff", generation.PasswordMethod()),
			generation.WebApp("/console"),
		),
		generation.WithRouterOutlet("machines", "machines", generation.APIKey()),
		generation.WithRouterOutlet("portal", "portal/api",
			generation.Auth("example.com/acme/beacon/pkg/auth/staff", generation.PasswordMethod()),
			generation.WebApp("/portal"),
		),
		generation.GenerateHandlerTests("test/authz"),
		generation.WithSpannerEmulatorVersion("1.5.56"),
		generation.GenerateTypescript("web/console/src/app/core/service",
			generation.GenerateMetadata(),
			generation.GeneratePermissions(),
			generation.GenerateEnums(),
		),
	)
	if err != nil {
		return err
	}
	defer generator.Close()

	return generator.Generate()
}
`

// programFile is the generator program's path in the beacon fixture.
const programFile = "cmd/generate/resourcegenerator/main.go"

func authFiles(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{
		"pkg/auth/staff/staff.go":                             staffPackage(t, "staff.go"),
		"pkg/auth/staff/password.go":                          staffPackage(t, "password.go"),
		"pkg/config/data.go":                                  authConfig,
		"pkg/auth/staff/roles.json":                           "{\n  \"roles\": {\n    \"global\": [],\n    \"domain\": []\n  }\n}\n",
		"schema/migrations/000001_DataChangeEvents.up.sql":    "CREATE TABLE DataChangeEvents (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n",
		"schema/migrations/000002_StaffAccess.up.sql":         "CREATE TABLE StaffRoles (\n  Role STRING(128) NOT NULL,\n) PRIMARY KEY (Role);\n\nCREATE TABLE StaffUserRoles (\n  Role STRING(128) NOT NULL,\n) PRIMARY KEY (Role);\n",
		"schema/migrations/000002_StaffAccess.down.sql":       "DROP TABLE StaffUserRoles;\nDROP TABLE StaffRoles;\n",
		"schema/migrations/000003_StaffSessionUsers.up.sql":   "CREATE TABLE StaffSessionUsers (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n",
		"schema/migrations/000003_StaffSessionUsers.down.sql": "DROP TABLE StaffSessionUsers;\n",
		"schema/migrations/000004_StaffSessions.up.sql":       "CREATE TABLE StaffSessions (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\nCREATE TABLE StaffSessionIdentities (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n",
		"schema/migrations/000004_StaffSessions.down.sql":     "DROP TABLE StaffSessionIdentities;\nDROP TABLE StaffSessions;\n",
	}
}

func TestAuthCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		auth Auth
		want string
	}{
		{name: "password", auth: Auth{Name: "partners", Outlet: "default"}, want: "impulse add auth partners --outlet default"},
		{name: "directory methods with their authority and account rule", auth: Auth{Name: "members", Methods: []authpkg.Method{{Kind: app.MethodPassword}, {Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityApplication, Accounts: authpkg.AccountsInvited}}, Outlet: "portal"}, want: "impulse add auth members --method password --method google:workspace --authority application --accounts invited --outlet portal"},
		{name: "the default account rule is not spelled", auth: Auth{Name: "members", Methods: []authpkg.Method{{Kind: app.MethodAzure, Name: "directory", Authority: AuthorityDirectory}}, Outlet: "portal"}, want: "impulse add auth members --method azure:directory --authority directory --outlet portal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.auth.Command(); got != tt.want {
				t.Errorf("Command() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAuthMeaning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		auth  Auth
		wants []string
		never []string
	}{
		{
			name:  "a password auth",
			auth:  Auth{Name: "partners", Outlet: "default"},
			wants: []string{"generation.PasswordMethod()"},
			never: []string{"Invite its people", ".InvitedUsers()"},
		},
		{
			name:  "a directory auth admitting members of the directory",
			auth:  Auth{Name: "partners", Methods: []authpkg.Method{{Kind: app.MethodAzure, Name: "directory", Authority: AuthorityDirectory}}, Outlet: "portal"},
			wants: []string{`generation.AzureMethod("directory"))`, "members of the directory, role membership the directory's"},
			never: []string{"Invite its people", ".InvitedUsers()"},
		},
		{
			name:  "a directory auth admitting invited users only names the Create grant the invitations need",
			auth:  Auth{Name: "partners", Methods: []authpkg.Method{{Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityApplication, Accounts: authpkg.AccountsInvited}}, Outlet: "portal"},
			wants: []string{`generation.GoogleMethod("workspace").InvitedUsers())`, "invited users only, role membership the application's", "Invite its people. The workspace registration(s) admit invited users only (`signin.InvitedUsersOnly`, and `.InvitedUsers()` on the declaration)", "grant Create on `PartnersUsers` to the role that invites in the role file"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			meaning := tt.auth.Meaning()
			for _, want := range tt.wants {
				if !strings.Contains(meaning, want) {
					t.Errorf("Meaning() lacks %q:\n%s", want, meaning)
				}
			}
			for _, unwanted := range tt.never {
				if strings.Contains(meaning, unwanted) {
					t.Errorf("Meaning() carries %q:\n%s", unwanted, meaning)
				}
			}
		})
	}
}

func TestAuthValidate(t *testing.T) {
	t.Parallel()

	routed := map[string]string{programFile: authRouterProgram}
	tests := []struct {
		name    string
		auth    Auth
		bare    bool
		extra   map[string]string
		wantErr string
	}{
		{name: "a password auth", auth: Auth{Name: "partners", Outlet: "default"}},
		{name: "a password auth, said so", auth: Auth{Name: "partners", Methods: []authpkg.Method{{Kind: app.MethodPassword}}, Outlet: "default"}},
		{name: "a bad name", auth: Auth{Name: "Partners", Outlet: "default"}, wantErr: `auth name "Partners"`},
		{name: "a directory auth with the directory as authority", auth: Auth{Name: "partners", Methods: []authpkg.Method{{Kind: app.MethodAzure, Name: "directory", Authority: AuthorityDirectory}}, Outlet: "default"}},
		{name: "a directory auth with the application as authority", auth: Auth{Name: "partners", Methods: []authpkg.Method{{Kind: app.MethodAzure, Name: "directory", Authority: AuthorityApplication}}, Outlet: "default"}},
		{name: "a directory auth without an authority", auth: Auth{Name: "partners", Methods: []authpkg.Method{{Kind: app.MethodAzure, Name: "directory"}}, Outlet: "default"}, wantErr: "the directory method needs --authority"},
		{name: "a directory auth with a made-up authority", auth: Auth{Name: "partners", Methods: []authpkg.Method{{Kind: app.MethodAzure, Name: "directory", Authority: "nobody"}}, Outlet: "default"}, wantErr: `authority "nobody"`},
		{name: "two methods", auth: Auth{Name: "partners", Methods: []authpkg.Method{{Kind: app.MethodPassword}, {Kind: app.MethodGoogle, Name: "google", Authority: AuthorityApplication}}, Outlet: "default"}},
		{name: "a registration given twice", auth: Auth{Name: "partners", Methods: []authpkg.Method{{Kind: app.MethodAzure, Name: "directory", Authority: AuthorityApplication}, {Kind: app.MethodGoogle, Name: "directory", Authority: AuthorityApplication}}, Outlet: "default"}, wantErr: "given twice"},
		{name: "an unknown method", auth: Auth{Name: "partners", Methods: []authpkg.Method{{Kind: "ldap", Name: "ldap", Authority: AuthorityApplication}}, Outlet: "default"}, wantErr: `"ldap" is not a sign-in method`},
		{name: "an account rule on the password method alone", auth: Auth{Name: "partners", Methods: []authpkg.Method{{Kind: app.MethodPassword, Accounts: authpkg.AccountsInvited}}, Outlet: "default"}, wantErr: "the password method takes no slot, account rule or authority"},
		{name: "a made-up account rule", auth: Auth{Name: "partners", Methods: []authpkg.Method{{Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityApplication, Accounts: "anyone"}}, Outlet: "default"}, wantErr: `accounts "anyone": members, invited or open`},
		{name: "open sign-up with the one organization add auth takes", auth: Auth{Name: "partners", Methods: []authpkg.Method{{Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityApplication, Accounts: authpkg.AccountsOpen}}, Outlet: "default"}, wantErr: "open sign-up is for any organization or personal accounts; a registration naming its Workspaces takes members or invited"},
		{name: "the auth exists", auth: Auth{Name: "staff", Outlet: "default"}, wantErr: "the staff auth already exists"},
		{name: "no auth package to write beside", auth: Auth{Name: "partners", Outlet: "default"}, bare: true, wantErr: "no auth package to write beside"},
		{name: "no outlet", auth: Auth{Name: "partners"}, extra: routed, wantErr: "--outlet is required: the outlet the partners auth serves, whose declaration is pointed at the auth; the outlets are default, portal (impulse add outlet adds one)"},
		{name: "an outlet the program does not declare", auth: Auth{Name: "partners", Outlet: "kiosk"}, extra: routed, wantErr: "no kiosk outlet for the partners auth to serve; the outlets are default, portal (impulse add outlet adds one)"},
		{name: "an API-key outlet", auth: Auth{Name: "partners", Outlet: "machines"}, extra: routed, wantErr: "the machines outlet is an API-key outlet (APIKey()), a machine surface with no sign-in; an auth serves a session outlet, and the outlets are default, portal"},
		{name: "a session outlet under the generated router", auth: Auth{Name: "partners", Outlet: "portal"}, extra: routed},
		{name: "the default outlet under the generated router", auth: Auth{Name: "partners", Outlet: "default"}, extra: routed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var files map[string]string
			if !tt.bare {
				files = authFiles(t)
				for rel, content := range tt.extra {
					files[rel] = content
				}
			}
			err := tt.auth.Validate(beacon(t, files))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestAuthApply(t *testing.T) {
	t.Parallel()

	wantConfig := strings.Replace(authConfig, "\t\"example.com/acme/beacon/pkg/auth/staff\"\n", "\t\"example.com/acme/beacon/pkg/auth/partners\"\n\t\"example.com/acme/beacon/pkg/auth/staff\"\n", 1)
	wantConfig = strings.Replace(wantConfig, "\tstaff         *staff.Auth\n}", "\tstaff         *staff.Auth\n\tpartners      *partners.Auth\n}", 1)
	wantConfig = strings.Replace(wantConfig,
		"\treturn &DataConfiguration{\n\t\tspannerClient: spannerClient,\n\t\tstaff:         staffAuth,\n\t}, nil\n",
		"\tpartnersAuth, err := partners.New(ctx, spannerClient, &partners.Settings{CookieKey: cookieKey})\n\tif err != nil {\n\t\treturn nil, errors.Wrap(err, \"partners.New()\")\n\t}\n\n\treturn &DataConfiguration{\n\t\tspannerClient: spannerClient,\n\t\tstaff:         staffAuth,\n\t\tpartners:      partnersAuth,\n\t}, nil\n", 1)

	// The data level after add tenancy: WrapReturn assigned the literal and inserted the
	// roster load, so the constructor no longer ends in "return &DataConfiguration{...}, nil".
	tenantedConfig := strings.Replace(authConfig,
		"\treturn &DataConfiguration{\n\t\tspannerClient: spannerClient,\n\t\tstaff:         staffAuth,\n\t}, nil\n",
		"\tconf := &DataConfiguration{\n\t\tspannerClient: spannerClient,\n\t\tstaff:         staffAuth,\n\t}\n\tif err := conf.loadTenants(ctx); err != nil {\n\t\treturn nil, errors.Wrap(err, \"loadTenants()\")\n\t}\n\n\treturn conf, nil\n", 1)

	// A data level whose constructor builds no literal at all: the auth can be declared
	// and imported, but not constructed.
	builtElsewhereConfig := strings.Replace(authConfig,
		"\treturn &DataConfiguration{\n\t\tspannerClient: spannerClient,\n\t\tstaff:         staffAuth,\n\t}, nil\n",
		"\treturn assemble(spannerClient, staffAuth), nil\n", 1)

	const packageDid = "pkg/auth/partners: the partners auth package (foothold.go, partners.go, password.go), rendered from the skeleton's auth source: the Auth session over tables prefixed Partners, the partners permission store, and the methods password, each in its own file"
	const migrationsDid = "schema/migrations: 000005_PartnersAccess, 000006_PartnersSessionUsers, 000007_PartnersSessions, the partners auth's tables: the access tables copied from the staff auth's under the Partners prefix, and the Auth session tables (sessions, users, identities, auth events, pending sign-ins)"
	const rolesDid = "pkg/auth/partners/roles.json: the partners auth's role file, empty: author its roles when its surfaces are bound"
	const configDid = "pkg/config/data.go: the partners auth constructed on DataConfiguration beside the staff auth (field, construction, import); pkg/config/partners.go: its accessor Partners()"

	routed := map[string]string{programFile: authRouterProgram}
	tests := []struct {
		name        string
		auth        Auth
		extra       map[string]string
		wantDid     []string
		wantSkipped []string
		check       func(t *testing.T, a *app.App)
	}{
		{
			name:    "a password auth written beside staff",
			auth:    Auth{Name: "partners", Outlet: "default"},
			wantDid: []string{packageDid, migrationsDid, rolesDid, configDid, "ran go generate ./..."},
			wantSkipped: []string{
				`pkg/auth/partners/foothold.go: the first administrator the migrate command seeds while the auth's users table holds no rows, a user named administrator with a generated password; author AdministratorRoles with the role file, declare the password in the data level (APP_PARTNERS_ADMINISTRATOR_PASSWORD, secret:"generated") and seed it from pkg/deploy SeedAdministrators`,
				"pkg/config/data.go: Close releases the staff auth only; release the partners auth too",
				programFile + ": the default outlet is served by a hand-written router, so its declaration carries no Auth to point at the partners auth; mount the auth's session group under /api in the router",
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				pkg := read(t, a, "pkg/auth/partners/partners.go")
				for _, want := range []string{"package partners", `Name = "partners"`, `TablePrefix = "Partners"`, "session.NewAuth[session.NoCustomData, session.NoCustomData](", `sessionsTable   = "PartnersSessions"`, "handlers.password = registerPassword(authSession)"} {
					if !strings.Contains(pkg, want) {
						t.Errorf("partners.go lacks %q", want)
					}
				}
				if strings.Contains(pkg, "staff") || strings.Contains(pkg, "Staff") {
					t.Error("partners.go still mentions staff")
				}
				if got := read(t, a, "pkg/auth/partners/password.go"); !strings.Contains(got, "func registerPassword(authSession *Session) *session.PasswordMethod") {
					t.Errorf("password.go = %q", got)
				}
				if diff := cmp.Diff(wantConfig, read(t, a, "pkg/config/data.go")); diff != "" {
					t.Errorf("data.go mismatch (-want +got):\n%s", diff)
				}
				if got := read(t, a, "schema/migrations/000007_PartnersSessions.up.sql"); !strings.Contains(got, "CREATE TABLE PartnersSessions") || !strings.Contains(got, "CREATE TABLE PartnersSessionPendingSignIns") {
					t.Errorf("sessions migration = %q", got)
				}
				if got := read(t, a, "schema/migrations/000006_PartnersSessionUsers.down.sql"); !strings.Contains(got, "DROP TABLE PartnersSessionUsers;") {
					t.Errorf("users down migration = %q", got)
				}
				if got := read(t, a, "schema/migrations/000005_PartnersAccess.down.sql"); !strings.Contains(got, "DROP TABLE PartnersRoles;") {
					t.Errorf("access down migration = %q", got)
				}
				if got := read(t, a, "pkg/config/partners.go"); !strings.Contains(got, "func (c *DataConfiguration) Partners() *partners.Auth") {
					t.Errorf("accessor = %q", got)
				}
			},
		},
		{
			name:        "a password auth added after tenancy is constructed before the assigned literal",
			auth:        Auth{Name: "partners", Outlet: "default"},
			extra:       map[string]string{"pkg/config/data.go": tenantedConfig},
			wantDid:     []string{packageDid, migrationsDid, rolesDid, configDid, "ran go generate ./..."},
			wantSkipped: []string{"pkg/config/data.go: Close releases the staff auth only; release the partners auth too"},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				got := read(t, a, "pkg/config/data.go")
				if !strings.Contains(got, "\tpartnersAuth, err := partners.New(ctx, spannerClient, &partners.Settings{CookieKey: cookieKey})\n\tif err != nil {\n\t\treturn nil, errors.Wrap(err, \"partners.New()\")\n\t}\n\n\tconf := &DataConfiguration{\n\t\tspannerClient: spannerClient,\n\t\tstaff:         staffAuth,\n\t\tpartners:      partnersAuth,\n\t}\n") {
					t.Errorf("data.go = %q", got)
				}
			},
		},
		{
			name:    "a constructor that builds no literal keeps the file and its earlier edits",
			auth:    Auth{Name: "partners", Outlet: "default"},
			extra:   map[string]string{"pkg/config/data.go": builtElsewhereConfig},
			wantDid: []string{packageDid, migrationsDid, rolesDid, configDid, "ran go generate ./..."},
			wantSkipped: []string{
				"pkg/config/data.go: NewDataConfiguration builds no &DataConfiguration{...} literal, so the partners auth is declared but not constructed; construct it beside the staff auth and set the field",
				"pkg/config/data.go: Close releases the staff auth only; release the partners auth too",
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				got := read(t, a, "pkg/config/data.go")
				if !strings.Contains(got, "\tpartners      *partners.Auth\n") || !strings.Contains(got, "return assemble(spannerClient, staffAuth), nil") {
					t.Errorf("data.go = %q", got)
				}
			},
		},
		{
			name:  "a directory auth, the application its authority, read from the environment struct",
			auth:  Auth{Name: "partners", Methods: []authpkg.Method{{Kind: app.MethodAzure, Name: "directory", Authority: AuthorityApplication}}, Outlet: "default"},
			extra: map[string]string{"pkg/config/data.go": authConfigEnv},
			wantDid: []string{
				"pkg/auth/partners: the partners auth package (directory.go, foothold.go, partners.go), rendered from the skeleton's auth source: the Auth session over tables prefixed Partners, the partners permission store, and the methods directory (azure, members of the directory, role membership the application's), each in its own file",
				migrationsDid, rolesDid,
				"pkg/config/data.go: dataConfig reads the partners auth's directory registration from APP_PARTNERS_DIRECTORY_TENANT_ID, APP_PARTNERS_DIRECTORY_CLIENT_ID, APP_PARTNERS_DIRECTORY_CLIENT_SECRET, APP_PARTNERS_DIRECTORY_REDIRECT_URL",
				configDid,
				"Procfile: 2 go run command(s) build with -tags skipAuth, so the partners auth's directory is simulated in development",
				"ran go generate ./...",
			},
			wantSkipped: []string{
				"pkg/auth/partners/foothold.go: the first administrator the migrate command seeds while the auth's users table holds no rows, the administrator role memberships under the sign-in name the placement names; author AdministratorRoles with the role file, declare the name in the data level (APP_PARTNERS_ADMINISTRATOR) and seed it from pkg/deploy SeedAdministrators",
				"pkg/config/data.go: Close releases the staff auth only; release the partners auth too",
				"no environment template (.envrc.template, .env.template, .env.example) to add the directory method's variables to (APP_PARTNERS_DIRECTORY_TENANT_ID, APP_PARTNERS_DIRECTORY_CLIENT_ID, APP_PARTNERS_DIRECTORY_CLIENT_SECRET, APP_PARTNERS_DIRECTORY_REDIRECT_URL)",
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				pkg := read(t, a, "pkg/auth/partners/directory.go")
				for _, want := range []string{`const DirectoryRegistration = "directory"`, "type DirectorySettings struct", "azure.Tenant(settings.Directory.TenantID)", "signin.DisableRoleSync()", "signin.MembersOfDirectory", "func (h *Handlers) DirectoryFrontChannelLogout()"} {
					if !strings.Contains(pkg, want) {
						t.Errorf("directory.go lacks %q", want)
					}
				}
				main := read(t, a, "pkg/auth/partners/partners.go")
				for _, want := range []string{"\tdirectory *azure.Method\n", "handlers.directory = registerDirectory(authSession, accessClient, settings)", "\tLoginURL string\n", "\tDirectory DirectorySettings\n"} {
					if !strings.Contains(main, want) {
						t.Errorf("partners.go lacks %q", want)
					}
				}
				got := read(t, a, "pkg/config/data.go")
				for _, want := range []string{
					"\tPartnersDirectoryTenantID     string `env:\"APP_PARTNERS_DIRECTORY_TENANT_ID\"`\n",
					"\tPartnersDirectoryClientSecret string `env:\"APP_PARTNERS_DIRECTORY_CLIENT_SECRET\" secret:\"true\"`\n",
					"partnersAuth, err := partners.New(ctx, spannerClient, &partners.Settings{\n\t\tCookieKey:      cookieKey,\n\t\tSessionTimeout: env.SessionTimeout,\n\t\tLoginURL:       \"/login\",\n\t\tDirectory: partners.DirectorySettings{\n\t\t\tTenantID:     env.PartnersDirectoryTenantID,\n\t\t\tClientID:     env.PartnersDirectoryClientID,\n\t\t\tClientSecret: env.PartnersDirectoryClientSecret,\n\t\t\tRedirectURL:  env.PartnersDirectoryRedirectURL,\n\t\t},\n\t})",
				} {
					if !strings.Contains(got, want) {
						t.Errorf("data.go lacks %q:\n%s", want, got)
					}
				}
				if procfile := read(t, a, "Procfile"); strings.Count(procfile, "go run -tags skipAuth ") != 2 {
					t.Errorf("Procfile = %q", procfile)
				}
			},
		},
		{
			name:  "a Google directory auth with the directory as authority reads roles from the groups, served by the portal outlet",
			auth:  Auth{Name: "partners", Methods: []authpkg.Method{{Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityDirectory}}, Outlet: "portal"},
			extra: routed,
			wantDid: []string{
				"pkg/auth/partners: the partners auth package (partners.go, workspace.go), rendered from the skeleton's auth source: the Auth session over tables prefixed Partners, the partners permission store, and the methods workspace (google, members of the directory, role membership the directory's), each in its own file",
				programFile + `: the portal outlet's declaration names the partners auth with its methods, generation.Auth("example.com/acme/beacon/pkg/auth/partners", generation.GoogleMethod("workspace")), in place of the staff auth's, so the regenerated router mounts the auth's session group under /portal/api: one login route per method, the methods route its login page renders from, and the pending sign-in routes`,
			},
			wantSkipped: []string{
				"pkg/config/data.go: no environment struct (env := &T{}) to add the partners auth's registration variables to; read them and pass them in partners.Settings",
				`test/integration/harness_test.go and cmd/deployment/migrate/main_test.go: the workspace registration validates its organization slot and its role groups' prefix at registration whether or not the directory is simulated, so give the partners auth's construction in the harness Workspace: partners.WorkspaceSettings{HostedDomain: "example.com", RedirectURL: <the served callback>, GroupPrefix: "partners-"} and set APP_PARTNERS_WORKSPACE_HOSTED_DOMAIN=example.com and APP_PARTNERS_WORKSPACE_GROUP_PREFIX=partners- where the migrate run's environment is`,
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				pkg := read(t, a, "pkg/auth/partners/workspace.go")
				for _, want := range []string{"google.Workspace(WorkspaceRegistration, settings.Workspace.HostedDomain)", "google.RoleSync(accessClient.UserManager(), settings.Workspace.GroupPrefix, lookup)", "google.ParseGroupLookup(settings.Workspace.GroupLookup)", "\tGroupPrefix string\n"} {
					if !strings.Contains(pkg, want) {
						t.Errorf("workspace.go lacks %q", want)
					}
				}
				if got := read(t, a, "pkg/config/data.go"); !strings.Contains(got, "partnersAuth, err := partners.New(ctx, spannerClient, &partners.Settings{\n\t\tCookieKey: cookieKey,\n\t\tLoginURL:  \"/login\",\n\t})") {
					t.Errorf("data.go construction = %q", got)
				}
				program := read(t, a, programFile)
				if want := "\t\tgeneration.WithRouterOutlet(\"portal\", \"portal/api\",\n\t\t\tgeneration.Auth(\"example.com/acme/beacon/pkg/auth/partners\", generation.GoogleMethod(\"workspace\")),\n\t\t\tgeneration.WebApp(\"/portal\"),\n\t\t),"; !strings.Contains(program, want) {
					t.Errorf("program lacks %q:\n%s", want, program)
				}
				if want := "\t\tgeneration.GenerateRoutes(\"pkg/router\", \"console/api\",\n\t\t\tgeneration.Auth(\"example.com/acme/beacon/pkg/auth/staff\", generation.PasswordMethod()),"; !strings.Contains(program, want) {
					t.Errorf("the console's declaration moved:\n%s", program)
				}
			},
		},
		{
			name:  "a Google auth admitting invited users only declares .InvitedUsers() and writes the rule to the method file",
			auth:  Auth{Name: "partners", Methods: []authpkg.Method{{Kind: app.MethodGoogle, Name: "workspace", Authority: AuthorityApplication, Accounts: authpkg.AccountsInvited}}, Outlet: "portal"},
			extra: routed,
			wantDid: []string{
				"pkg/auth/partners: the partners auth package (foothold.go, partners.go, workspace.go), rendered from the skeleton's auth source: the Auth session over tables prefixed Partners, the partners permission store, and the methods workspace (google, invited users only, role membership the application's), each in its own file",
				programFile + `: the portal outlet's declaration names the partners auth with its methods, generation.Auth("example.com/acme/beacon/pkg/auth/partners", generation.GoogleMethod("workspace").InvitedUsers()), in place of the staff auth's, so the regenerated router mounts the auth's session group under /portal/api: one login route per method, the methods route its login page renders from, and the pending sign-in routes`,
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				pkg := read(t, a, "pkg/auth/partners/workspace.go")
				for _, want := range []string{"signin.InvitedUsersOnly", "google.DisableRoleSync()", "google.Workspace(WorkspaceRegistration, settings.Workspace.HostedDomain)"} {
					if !strings.Contains(pkg, want) {
						t.Errorf("workspace.go lacks %q", want)
					}
				}
				if strings.Contains(pkg, "signin.MembersOfDirectory") {
					t.Errorf("workspace.go admits members of the directory:\n%s", pkg)
				}
				program := read(t, a, programFile)
				if want := "\t\tgeneration.WithRouterOutlet(\"portal\", \"portal/api\",\n\t\t\tgeneration.Auth(\"example.com/acme/beacon/pkg/auth/partners\", generation.GoogleMethod(\"workspace\").InvitedUsers()),\n\t\t\tgeneration.WebApp(\"/portal\"),\n\t\t),"; !strings.Contains(program, want) {
					t.Errorf("program lacks %q:\n%s", want, program)
				}
			},
		},
		{
			name:  "a password auth served by the default outlet under the generated router",
			auth:  Auth{Name: "partners", Outlet: "default"},
			extra: routed,
			wantDid: []string{
				programFile + `: the default outlet's declaration names the partners auth with its methods, generation.Auth("example.com/acme/beacon/pkg/auth/partners", generation.PasswordMethod()), in place of the staff auth's, so the regenerated router mounts the auth's session group under /console/api: one login route per method, the methods route its login page renders from, and the pending sign-in routes`,
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				program := read(t, a, programFile)
				if want := "\t\tgeneration.GenerateRoutes(\"pkg/router\", \"console/api\",\n\t\t\tgeneration.Auth(\"example.com/acme/beacon/pkg/auth/partners\", generation.PasswordMethod()),\n\t\t\tgeneration.WebApp(\"/console\"),\n\t\t),"; !strings.Contains(program, want) {
					t.Errorf("program lacks %q:\n%s", want, program)
				}
				if strings.Count(program, "pkg/auth/staff") != 1 {
					t.Errorf("the staff auth is declared %d times, want once (the portal's):\n%s", strings.Count(program, "pkg/auth/staff"), program)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := authFiles(t)
			for rel, content := range tt.extra {
				files[rel] = content
			}
			a := beacon(t, files)
			exec := &fakeExec{}
			ch, err := tt.auth.Apply(t.Context(), a, exec)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			for _, want := range tt.wantDid {
				if !slices.Contains(ch.Did, want) {
					t.Errorf("Did lacks %q; got:\n%s", want, strings.Join(ch.Did, "\n"))
				}
			}
			for _, want := range tt.wantSkipped {
				if !slices.Contains(ch.Skipped, want) {
					t.Errorf("Skipped lacks %q; got:\n%s", want, strings.Join(ch.Skipped, "\n"))
				}
			}
			if len(exec.calls) != 1 || !strings.HasPrefix(exec.calls[0], "go generate") {
				t.Errorf("exec calls = %v, want one go generate", exec.calls)
			}
			if tt.check != nil {
				tt.check(t, a)
			}
		})
	}
}
