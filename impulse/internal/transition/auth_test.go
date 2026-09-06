package transition

import (
	"errors"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

// staffPackage is the base's staff auth package, read from the embedded skeleton so the
// test copies what add auth copies.
func staffPackage(t *testing.T) string {
	t.Helper()

	sub, err := skeleton.FS("solo")
	if err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(sub, "pkg/auth/staff/staff.go")
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

	staffAuth, err := staff.New(ctx, spannerClient, staff.Settings{CookieKey: cookieKey})
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

	staffAuth, err := staff.New(ctx, spannerClient, staff.Settings{CookieKey: cookieKey, SessionTimeout: env.SessionTimeout})
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

// wantConfigEnv is authConfigEnv with the partners OIDC auth constructed beside staff.
const wantConfigEnv = `package config

import (
	"context"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/go-playground/errors/v5"

	"example.com/acme/beacon/pkg/auth/partners"
	"example.com/acme/beacon/pkg/auth/staff"
)

// DataConfiguration is the second level.
type DataConfiguration struct {
	spannerClient *cloudspanner.Client
	staff         *staff.Auth
	partners      *partners.Auth
}

// NewDataConfiguration opens the clients.
func NewDataConfiguration(ctx context.Context) (*DataConfiguration, error) {
	env := &dataConfig{}
	spannerClient, cookieKey, err := open(ctx, env)
	if err != nil {
		return nil, errors.Wrap(err, "open()")
	}

	staffAuth, err := staff.New(ctx, spannerClient, staff.Settings{CookieKey: cookieKey, SessionTimeout: env.SessionTimeout})
	if err != nil {
		return nil, errors.Wrap(err, "staff.New()")
	}

	partnersAuth, err := partners.New(ctx, spannerClient, &partners.Settings{
		CookieKey:      cookieKey,
		SessionTimeout: env.SessionTimeout,
		LoginURL:       "/login",
		Directory: partners.Directory{
			IssuerURL:    env.PartnersIssuerURL,
			ClientID:     env.PartnersClientID,
			ClientSecret: env.PartnersClientSecret,
			RedirectURL:  env.PartnersRedirectURL,
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "partners.New()")
	}

	return &DataConfiguration{
		spannerClient: spannerClient,
		staff:         staffAuth,
		partners:      partnersAuth,
	}, nil
}

// dataConfig holds the environment every database-opening process reads.
type dataConfig struct {
	// SessionTimeout is the idle timeout of a browser session.
	SessionTimeout time.Duration ` + "`" + `env:"APP_DEFAULT_SESSION_TIMEOUT,default=10m"` + "`" + `
	// The partners auth's directory registration (pkg/auth/partners): the OpenID Connect issuer, the
	// application's client credentials, and the callback the directory returns the browser to.
	// Under the session library's skipAuth build tag only the redirect URL is read.
	PartnersIssuerURL    string ` + "`" + `env:"APP_PARTNERS_OIDC_ISSUER_URL"` + "`" + `
	PartnersClientID     string ` + "`" + `env:"APP_PARTNERS_OIDC_CLIENT_ID"` + "`" + `
	PartnersClientSecret string ` + "`" + `env:"APP_PARTNERS_OIDC_CLIENT_SECRET"` + "`" + `
	PartnersRedirectURL  string ` + "`" + `env:"APP_PARTNERS_OIDC_REDIRECT_URL"` + "`" + `
}
`

func authFiles(t *testing.T) map[string]string {
	t.Helper()

	return map[string]string{
		"pkg/auth/staff/staff.go":                             staffPackage(t),
		"pkg/config/data.go":                                  authConfig,
		"schema/roles/staff.json":                             "{\n  \"roles\": {\n    \"global\": [],\n    \"domain\": []\n  }\n}\n",
		"schema/migrations/000001_DataChangeEvents.up.sql":    "CREATE TABLE DataChangeEvents (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n",
		"schema/migrations/000002_StaffAccess.up.sql":         "CREATE TABLE StaffRoles (\n  Role STRING(128) NOT NULL,\n) PRIMARY KEY (Role);\n\nCREATE TABLE StaffUserRoles (\n  Role STRING(128) NOT NULL,\n) PRIMARY KEY (Role);\n",
		"schema/migrations/000002_StaffAccess.down.sql":       "DROP TABLE StaffUserRoles;\nDROP TABLE StaffRoles;\n",
		"schema/migrations/000003_StaffSessions.up.sql":       "CREATE TABLE StaffSessions (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\nCREATE INDEX StaffSessions_Expired_idx ON StaffSessions (Id);\n",
		"schema/migrations/000003_StaffSessions.down.sql":     "DROP INDEX StaffSessions_Expired_idx;\nDROP TABLE StaffSessions;\n",
		"schema/migrations/000004_StaffSessionUsers.up.sql":   "CREATE TABLE StaffSessionUsers (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n",
		"schema/migrations/000004_StaffSessionUsers.down.sql": "DROP TABLE StaffSessionUsers;\n",
	}
}

func TestAuthValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		auth    Auth
		bare    bool
		wantErr string
	}{
		{name: "a password auth", auth: Auth{Name: "partners", Flavor: FlavorPassword}},
		{name: "a preauth auth", auth: Auth{Name: "devices", Flavor: FlavorPreauth}},
		{name: "a bad name", auth: Auth{Name: "Partners", Flavor: FlavorPassword}, wantErr: `auth name "Partners"`},
		{name: "an OIDC auth with the directory as authority", auth: Auth{Name: "partners", Flavor: FlavorOIDCAzure, Authority: AuthorityDirectory}},
		{name: "an OIDC auth with the application as authority", auth: Auth{Name: "partners", Flavor: FlavorOIDCAzure, Authority: AuthorityApplication}},
		{name: "an OIDC auth without an authority", auth: Auth{Name: "partners", Flavor: FlavorOIDCAzure}, wantErr: "an OIDC auth needs --authority"},
		{name: "an OIDC auth with a made-up authority", auth: Auth{Name: "partners", Flavor: FlavorOIDCAzure, Authority: "nobody"}, wantErr: "an OIDC auth needs --authority"},
		{name: "a password auth cannot hand membership to a directory", auth: Auth{Name: "partners", Flavor: FlavorPassword, Authority: AuthorityDirectory}, wantErr: "only an auth that signs in through a directory"},
		{name: "the Google flavor follows", auth: Auth{Name: "partners", Flavor: app.FlavorOIDCGoogle, Authority: AuthorityDirectory}, wantErr: "the Google flavor follows"},
		{name: "an unknown flavor", auth: Auth{Name: "partners", Flavor: "ldap"}, wantErr: `flavor "ldap"`},
		{name: "the auth exists", auth: Auth{Name: "staff", Flavor: FlavorPassword}, wantErr: "the staff auth already exists"},
		{name: "no auth package to copy", auth: Auth{Name: "partners", Flavor: FlavorPassword}, bare: true, wantErr: "no auth package to copy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var files map[string]string
			if !tt.bare {
				files = authFiles(t)
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
		"\tpartnersAuth, err := partners.New(ctx, spannerClient, partners.Settings{CookieKey: cookieKey})\n\tif err != nil {\n\t\treturn nil, errors.Wrap(err, \"partners.New()\")\n\t}\n\n\treturn &DataConfiguration{\n\t\tspannerClient: spannerClient,\n\t\tstaff:         staffAuth,\n\t\tpartners:      partnersAuth,\n\t}, nil\n", 1)

	tests := []struct {
		name        string
		auth        Auth
		extra       map[string]string
		wantDid     []string
		wantSkipped []string
		check       func(t *testing.T, a *app.App)
	}{
		{
			name: "an OIDC auth copied from the reference, the application its authority",
			auth: Auth{Name: "partners", Flavor: FlavorOIDCAzure, Authority: AuthorityApplication},
			extra: map[string]string{
				"pkg/config/data.go": authConfigEnv,
				".envrc.template":    "export PORT=8090\n",
			},
			wantDid: []string{
				"pkg/auth/partners: the partners auth package, a copy of the reference skeleton's members auth (Azure OpenID Connect) with its names substituted, role membership the application's (session.DisableRoleSync) (tables PartnersSessions and PartnersOIDCUsers, cookie partners, store prefix Partners)",
				"schema/migrations: 000005_PartnersAccess, 000006_PartnersSessions, 000007_PartnersOIDCUsers, the partners auth's tables copied from the members auth's under the Partners prefix",
				"schema/roles/partners.json: the partners auth's role configuration, empty (the Administrator role at each scope is implicit)",
				"pkg/config/data.go: dataConfig reads the partners auth's directory registration from APP_PARTNERS_OIDC_ISSUER_URL, _CLIENT_ID, _CLIENT_SECRET, and _REDIRECT_URL",
				"pkg/config/data.go: the partners auth constructed on DataConfiguration beside the staff auth (field, construction, import); pkg/config/partners.go: its accessor Partners()",
				"Procfile: 2 go run command(s) build with -tags skipAuth, so the partners auth's directory is simulated in development and every partners login is APP_USERNAME",
				".envrc.template: APP_USERNAME and APP_ROLES for the simulated directory, and the partners auth's APP_PARTNERS_OIDC_* registration, to fill in",
				"ran go generate ./...",
			},
			wantSkipped: []string{"pkg/config/data.go: Close releases the staff auth only; release the partners auth too"},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				pkg := read(t, a, "pkg/auth/partners/partners.go")
				for _, want := range []string{"package partners", `Name = "partners"`, `TablePrefix = "Partners"`, "session.NewOIDCAzure[", "session.DisableRoleSync(),", `usersTable    = TablePrefix + "OIDCUsers"`, "Role\n// membership is the application's"} {
					if !strings.Contains(pkg, want) {
						t.Errorf("partners.go lacks %q", want)
					}
				}
				for _, absent := range []string{"RoleSync(accessClient", "Domains session.DomainsProvider"} {
					if strings.Contains(pkg, absent) {
						t.Errorf("partners.go still has %q", absent)
					}
				}
				// The reference's name is gone as a name ("members", "membersAuth", "MembersSessions"),
				// while the English word "membership" stays.
				if leftover := regexp.MustCompile(`(^|[^A-Za-z])members([^a-z]|$)|Members([^a-z]|$)`).FindString(pkg); leftover != "" {
					t.Errorf("partners.go still names the reference auth: %q", leftover)
				}
				if !strings.Contains(pkg, "membership") {
					t.Error("partners.go lost the word membership to the rename")
				}
				if diff := cmp.Diff(wantConfigEnv, read(t, a, "pkg/config/data.go")); diff != "" {
					t.Errorf("data.go mismatch (-want +got):\n%s", diff)
				}
				if got := read(t, a, "schema/migrations/000006_PartnersSessions.up.sql"); !strings.Contains(got, "CREATE TABLE PartnersSessions") || !strings.Contains(got, "OidcSid") || !strings.Contains(got, "PartnersSessionsByOidcSid") {
					t.Errorf("sessions migration = %q", got)
				}
				if got := read(t, a, "schema/migrations/000007_PartnersOIDCUsers.down.sql"); !strings.Contains(got, "DROP TABLE PartnersOIDCUsers;") {
					t.Errorf("users down migration = %q", got)
				}
				if got := read(t, a, "Procfile"); !strings.Contains(got, "go run -tags skipAuth ./cmd/bootstrap && go run -tags skipAuth .") {
					t.Errorf("Procfile = %q", got)
				}
				env := read(t, a, ".envrc.template")
				for _, want := range []string{"export PORT=8090\n\n# --- partners auth", "export APP_USERNAME=partners-dev\n", "export APP_ROLES=\n", "# export APP_PARTNERS_OIDC_ISSUER_URL=\n", "export APP_PARTNERS_OIDC_REDIRECT_URL=\n"} {
					if !strings.Contains(env, want) {
						t.Errorf(".envrc.template lacks %q", want)
					}
				}
			},
		},
		{
			name: "an OIDC auth with the directory as authority",
			auth: Auth{Name: "partners", Flavor: FlavorOIDCAzure, Authority: AuthorityDirectory},
			wantDid: []string{
				"pkg/auth/partners: the partners auth package, a copy of the reference skeleton's members auth (Azure OpenID Connect) with its names substituted, role membership the directory's (session.RoleSync) (tables PartnersSessions and PartnersOIDCUsers, cookie partners, store prefix Partners)",
				"schema/migrations: 000005_PartnersAccess, 000006_PartnersSessions, 000007_PartnersOIDCUsers, the partners auth's tables copied from the members auth's under the Partners prefix",
				"schema/roles/partners.json: the partners auth's role configuration, empty (the Administrator role at each scope is implicit)",
				"pkg/config/data.go: the partners auth constructed on DataConfiguration beside the staff auth (field, construction, import); pkg/config/partners.go: its accessor Partners()",
				"Procfile: 2 go run command(s) build with -tags skipAuth, so the partners auth's directory is simulated in development and every partners login is APP_USERNAME",
				"ran go generate ./...",
			},
			wantSkipped: []string{
				"pkg/config/data.go: no environment struct (env := &T{}) to add the partners auth's directory registration to; read APP_PARTNERS_OIDC_ISSUER_URL, _CLIENT_ID, _CLIENT_SECRET, and _REDIRECT_URL and pass them in partners.Settings.Directory",
				"pkg/config/data.go: Close releases the staff auth only; release the partners auth too",
				"no environment template (.envrc.template, .env.template, .env.example) to add APP_USERNAME and the partners auth's APP_PARTNERS_OIDC_* variables to",
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				pkg := read(t, a, "pkg/auth/partners/partners.go")
				for _, want := range []string{"session.RoleSync(accessClient.UserManager(), settings.Domains),", "Domains session.DomainsProvider", "Role membership is the\n// directory's (session.RoleSync)"} {
					if !strings.Contains(pkg, want) {
						t.Errorf("partners.go lacks %q", want)
					}
				}
				if strings.Contains(pkg, "session.DisableRoleSync(),") {
					t.Error("partners.go still disables role sync")
				}
				if got := read(t, a, "pkg/config/data.go"); !strings.Contains(got, "partnersAuth, err := partners.New(ctx, spannerClient, &partners.Settings{\n\t\tCookieKey: cookieKey,\n\t\tLoginURL:  \"/login\",\n\t})") {
					t.Errorf("data.go construction = %q", got)
				}
			},
		},
		{
			name: "a password auth copied from staff",
			auth: Auth{Name: "partners", Flavor: FlavorPassword},
			wantDid: []string{
				"pkg/auth/partners: the partners auth package, a copy of staff with its names substituted (tables PartnersSessions and PartnersSessionUsers, cookie partners, store prefix Partners)",
				"schema/migrations: 000005_PartnersAccess, 000006_PartnersSessions, 000007_PartnersSessionUsers, the partners auth's tables copied from the staff auth's under the Partners prefix",
				"schema/roles/partners.json: the partners auth's role configuration, empty (the Administrator role at each scope is implicit)",
				"pkg/config/data.go: the partners auth constructed on DataConfiguration beside the staff auth (field, construction, import); pkg/config/partners.go: its accessor Partners()",
				"ran go generate ./...",
			},
			wantSkipped: []string{"pkg/config/data.go: Close releases the staff auth only; release the partners auth too"},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				pkg := read(t, a, "pkg/auth/partners/partners.go")
				for _, want := range []string{"package partners", `Name = "partners"`, `TablePrefix = "Partners"`, "session.NewPasswordAuth[", "usersTable    = TablePrefix + \"SessionUsers\""} {
					if !strings.Contains(pkg, want) {
						t.Errorf("partners.go lacks %q", want)
					}
				}
				if strings.Contains(pkg, "staff") || strings.Contains(pkg, "Staff") {
					t.Error("partners.go still mentions staff")
				}
				if diff := cmp.Diff(wantConfig, read(t, a, "pkg/config/data.go")); diff != "" {
					t.Errorf("data.go mismatch (-want +got):\n%s", diff)
				}
				if got := read(t, a, "schema/migrations/000006_PartnersSessions.up.sql"); !strings.Contains(got, "CREATE TABLE PartnersSessions") || !strings.Contains(got, "PartnersSessions_Expired_idx") {
					t.Errorf("sessions migration = %q", got)
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
			name: "a preauth auth swapped from the password staff",
			auth: Auth{Name: "devices", Flavor: FlavorPreauth},
			wantDid: []string{
				"pkg/auth/devices: the devices auth package, a copy of staff with its names substituted and the constructor swapped from password to preauth (tables DevicesSessions, cookie devices, store prefix Devices); read it over, since the swap is textual",
				"schema/migrations: 000005_DevicesAccess, 000006_DevicesSessions, the devices auth's tables copied from the staff auth's under the Devices prefix",
				"schema/roles/devices.json: the devices auth's role configuration, empty (the Administrator role at each scope is implicit)",
				"pkg/config/data.go: the devices auth constructed on DataConfiguration beside the staff auth (field, construction, import); pkg/config/devices.go: its accessor Devices()",
				"ran go generate ./...",
			},
			wantSkipped: []string{"pkg/config/data.go: Close releases the staff auth only; release the devices auth too"},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				pkg := read(t, a, "pkg/auth/devices/devices.go")
				for _, want := range []string{"session.NewPreauth[session.NoCustomData](", "sessionstorage.NewSpannerPreauth(db)", "*session.Preauth[session.NoCustomData]"} {
					if !strings.Contains(pkg, want) {
						t.Errorf("devices.go lacks %q", want)
					}
				}
				for _, absent := range []string{"WithUserTableName", "usersTable", "PasswordAuth"} {
					if strings.Contains(pkg, absent) {
						t.Errorf("devices.go still has %q", absent)
					}
				}
				if _, err := os.Stat(a.Abs("schema/migrations/000007_DevicesSessionUsers.up.sql")); !errors.Is(err, os.ErrNotExist) {
					t.Error("a preauth auth got a users table")
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
			if diff := cmp.Diff(tt.wantDid, ch.Did); diff != "" {
				t.Errorf("Did mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantSkipped, ch.Skipped); diff != "" {
				t.Errorf("Skipped mismatch (-want +got):\n%s", diff)
			}
			if tt.check != nil {
				tt.check(t, a)
			}
		})
	}
}
