package transition

import (
	"go/format"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

// skeletonFile reads one file of an embedded candidate.
func skeletonFile(t *testing.T, candidate, rel string) string {
	t.Helper()

	sub, err := skeleton.FS(candidate)
	if err != nil {
		t.Fatal(err)
	}
	data, err := fs.ReadFile(sub, rel)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// beaconAppEmbed embeds the staff auth's session manager the way the base's App does.
const beaconAppEmbed = `package app

import (
	"github.com/cccteam/session"

	"example.com/acme/beacon/pkg/auth/staff"
)

type Configurer interface {
	Staff() *staff.Auth
}

type App struct {
	*session.PasswordAuth[session.NoCustomData, session.NoCustomData]
}

func New(cfg Configurer) *App {
	a := &App{}
	if auth := cfg.Staff(); auth != nil {
		a.PasswordAuth = auth.Session()
	}

	return a
}
`

// beaconRouter mounts the password login route the way the base's router does.
const beaconRouter = `package router

import (
	"github.com/cccteam/session"
	"github.com/go-chi/chi/v5"
)

type Handlers interface {
	session.PasswordAuthHandlers
}

func sessionGroup(r chi.Router, h Handlers, prefix string, api func(chi.Router)) {
	r.Group(func(r chi.Router) {
		r.Use(h.StartSession)

		// Login validates the credentials and starts a session.
		r.Post(prefix+"/user/login", h.Login())

		r.Get(prefix+"/user/session", h.Authenticated())
		r.Delete(prefix+"/user/session", h.Logout())
	})
}
`

func TestAuthFlavorValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		flavor  AuthFlavor
		wantErr string
	}{
		{name: "to Azure, the application its authority", flavor: AuthFlavor{Name: "staff", Flavor: FlavorOIDCAzure, Authority: AuthorityApplication}},
		{name: "to Azure, the directory its authority", flavor: AuthFlavor{Name: "staff", Flavor: FlavorOIDCAzure, Authority: AuthorityDirectory}},
		{name: "to Google, the application its authority", flavor: AuthFlavor{Name: "staff", Flavor: FlavorOIDCGoogle, Authority: AuthorityApplication}},
		{name: "to Google, the directory its authority", flavor: AuthFlavor{Name: "staff", Flavor: FlavorOIDCGoogle, Authority: AuthorityDirectory}},
		{name: "without an authority", flavor: AuthFlavor{Name: "staff", Flavor: FlavorOIDCAzure}, wantErr: "an OIDC auth needs --authority"},
		{name: "back to a password", flavor: AuthFlavor{Name: "staff", Flavor: FlavorPassword}, wantErr: "moving one back to a password or preauth"},
		{name: "an unknown flavor", flavor: AuthFlavor{Name: "staff", Flavor: "ldap", Authority: AuthorityApplication}, wantErr: `flavor "ldap"`},
		{name: "a bad name", flavor: AuthFlavor{Name: "Staff", Flavor: FlavorOIDCAzure, Authority: AuthorityApplication}, wantErr: `auth name "Staff"`},
		{name: "no such auth", flavor: AuthFlavor{Name: "partners", Flavor: FlavorOIDCAzure, Authority: AuthorityApplication}, wantErr: `no auth named "partners"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := beacon(t, authFiles(t))
			err := tt.flavor.Validate(a)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Errorf("Validate() error = %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Errorf("Validate() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestAuthFlavorValidateAlreadyThere(t *testing.T) {
	t.Parallel()

	files := authFiles(t)
	files["pkg/auth/staff/staff.go"] = strings.ReplaceAll(strings.ReplaceAll(files["pkg/auth/staff/staff.go"],
		"session.NewPasswordAuth[session.NoCustomData, session.NoCustomData](", "session.NewOIDCAzure[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerOIDC(db), session.DisableRoleSync(), "),
		"sessionstorage.NewSpannerPasswordAuth(db),", `"issuer", "id", "secret", "http://localhost/callback",`)
	a := beacon(t, files)
	err := AuthFlavor{Name: "staff", Flavor: FlavorOIDCAzure, Authority: AuthorityApplication}.Validate(a)
	if err == nil || !strings.Contains(err.Error(), "already signs in through oidc-azure") {
		t.Errorf("Validate() error = %v, want already signs in", err)
	}
}

func TestAuthFlavorApply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		flavor      AuthFlavor
		wantDid     []string
		wantSkipped []string
		check       func(t *testing.T, a *app.App)
	}{
		{
			name:   "staff moves to Azure, roles dropped",
			flavor: AuthFlavor{Name: "staff", Flavor: FlavorOIDCAzure, Authority: AuthorityApplication},
			wantDid: []string{
				"pkg/auth/staff/staff.go: rewritten as the staff auth in the oidc-azure flavor (Azure OpenID Connect) from the reference skeleton's members auth, role membership the application's (session.DisableRoleSync) (tables StaffSessions and StaffOIDCUsers, cookie staff, store prefix Staff); what the file carried beyond the base's shape is in git to re-apply",
				"schema/migrations: 000005_StaffOIDCAzure, the staff auth's session tables (StaffSessions, StaffSessionUsers) dropped and created in the oidc-azure shape, StaffSessions and StaffOIDCUsers, and StaffUserRoles dropped and recreated so no role stays keyed by a password username; down recreates them from 000003_StaffSessions, 000004_StaffSessionUsers",
				"pkg/config/data.go: dataConfig reads the staff auth's directory registration from APP_STAFF_OIDC_ISSUER_URL, _CLIENT_ID, _CLIENT_SECRET, and _REDIRECT_URL",
				"pkg/config/data.go: the staff auth's construction now passes the login page and the directory registration",
				"app/app.go, pkg/router/router.go: session.PasswordAuthHandlers and *session.PasswordAuth[session.NoCustomData, session.NoCustomData] swapped for session.OIDCAzureHandlers and *session.OIDCAzure[session.NoCustomData, session.NoCustomData]",
				"pkg/router/router.go: the password login route replaced by the directory's: GET /user/login (the redirect), GET /user/callback (the return), and GET /user/logout (the directory's front-channel logout)",
				"Procfile: 2 go run command(s) build with -tags skipAuth, so the staff auth's directory is simulated in development and every staff login is APP_USERNAME",
				".envrc.template: APP_USERNAME and APP_ROLES for the simulated directory, and the staff auth's APP_STAFF_OIDC_* registration, to fill in",
				"ran go generate ./...",
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				pkg := read(t, a, "pkg/auth/staff/staff.go")
				for _, want := range []string{"package staff", `Name = "staff"`, "session.NewOIDCAzure[", "session.DisableRoleSync(),", `usersTable    = TablePrefix + "OIDCUsers"`, "session.WithXSRFCookieName(XSRFCookie),"} {
					if !strings.Contains(pkg, want) {
						t.Errorf("staff.go lacks %q", want)
					}
				}
				for _, absent := range []string{"portal's people", "NewPasswordAuth"} {
					if strings.Contains(pkg, absent) {
						t.Errorf("staff.go still has %q", absent)
					}
				}
				if leftover := regexp.MustCompile(`(^|[^A-Za-z])members([^a-z]|$)|Members([^a-z]|$)`).FindString(pkg); leftover != "" {
					t.Errorf("staff.go still names the reference auth: %q", leftover)
				}
				up := read(t, a, "schema/migrations/000005_StaffOIDCAzure.up.sql")
				for i, want := range []string{
					"DROP TABLE StaffSessionUsers;", "DROP INDEX StaffSessions_Expired_idx;\nDROP TABLE StaffSessions;",
					"CREATE TABLE StaffSessions (", "OidcSid", "CREATE INDEX StaffSessionsByOidcSid ON StaffSessions (OidcSid DESC);",
					"CREATE TABLE StaffOIDCUsers (", "CREATE UNIQUE INDEX StaffOIDCUsersByTidOid ON StaffOIDCUsers (Tid, Oid);",
					"DROP INDEX StaffStaffUserRolesByScopeUser;\n\nDROP TABLE StaffUserRoles;\n\nCREATE TABLE StaffUserRoles (", "INTERLEAVE IN PARENT StaffRoles ON DELETE NO ACTION;\n\nCREATE INDEX StaffStaffUserRolesByScopeUser ON StaffUserRoles (IsGlobal, Axis, Domain, User);",
				} {
					at := strings.Index(up, want)
					if at < 0 {
						t.Errorf("up migration lacks %q:\n%s", want, up)

						continue
					}
					if i > 0 && at < strings.Index(up, "DROP TABLE StaffSessionUsers;") {
						t.Errorf("up migration has %q before the drops", want)
					}
				}
				down := read(t, a, "schema/migrations/000005_StaffOIDCAzure.down.sql")
				for _, want := range []string{"DROP INDEX StaffOIDCUsersByTidOid;\n\nDROP TABLE StaffOIDCUsers;", "DROP TABLE StaffSessions;\n\nCREATE TABLE StaffSessions (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\nCREATE INDEX StaffSessions_Expired_idx ON StaffSessions (Id);", "CREATE TABLE StaffSessionUsers (Id STRING(36) NOT NULL) PRIMARY KEY (Id);"} {
					if !strings.Contains(down, want) {
						t.Errorf("down migration lacks %q:\n%s", want, down)
					}
				}
				if strings.Contains(down, "StaffUserRoles") {
					t.Error("down migration touches the role assignments")
				}
				config := read(t, a, "pkg/config/data.go")
				for _, want := range []string{
					"staffAuth, err := staff.New(ctx, spannerClient, &staff.Settings{\n\t\tCookieKey:      cookieKey,\n\t\tSessionTimeout: env.SessionTimeout,\n\t\tLoginURL:       \"/login\",\n\t\tDirectory: staff.Directory{\n\t\t\tIssuerURL:    env.StaffIssuerURL,\n\t\t\tClientID:     env.StaffClientID,",
					"\tStaffRedirectURL  string `env:\"APP_STAFF_OIDC_REDIRECT_URL\"`\n",
				} {
					if !strings.Contains(config, want) {
						t.Errorf("data.go lacks %q:\n%s", want, config)
					}
				}
				if strings.Contains(config, "staff.Settings{CookieKey: cookieKey}") {
					t.Error("data.go still has the password construction")
				}
				appGo := read(t, a, "app/app.go")
				if !strings.Contains(appGo, "\t*session.OIDCAzure[session.NoCustomData, session.NoCustomData]\n") || !strings.Contains(appGo, "a.OIDCAzure = auth.Session()") {
					t.Errorf("app.go = %q", appGo)
				}
				router := read(t, a, "pkg/router/router.go")
				if !strings.Contains(router, "\tsession.OIDCAzureHandlers\n") || !strings.Contains(router, "\t\tr.Get(prefix+\"/user/login\", h.Login())\n\t\tr.Get(prefix+\"/user/callback\", h.CallbackOIDC())\n\t\tr.Get(prefix+\"/user/logout\", h.FrontChannelLogout())\n\n\t\tr.Get(prefix+\"/user/session\"") || strings.Contains(router, "r.Post(") {
					t.Errorf("router.go = %q", router)
				}
				if env := read(t, a, ".envrc.template"); !strings.Contains(env, "export APP_STAFF_OIDC_REDIRECT_URL=\n") {
					t.Errorf(".envrc.template = %q", env)
				}
			},
		},
		{
			name:   "a fresh staff auth from impulse new is born in the Google shape",
			flavor: AuthFlavor{Name: "staff", Flavor: FlavorOIDCGoogle, Authority: AuthorityDirectory, Fresh: true},
			wantDid: []string{
				"pkg/auth/staff/staff.go: rewritten as the staff auth in the oidc-google flavor (Google OpenID Connect) from the reference skeleton's members auth, role membership the directory's (session.GoogleRoleSync) (tables StaffSessions and StaffOIDCUsers, cookie staff, store prefix Staff); what the file carried beyond the base's shape is in git to re-apply",
				"schema/migrations: 000003_StaffOIDCGoogle replaces 000003_StaffSessions, 000004_StaffSessionUsers; the staff auth is born in the oidc-google shape, StaffSessions and StaffOIDCUsers, and its role assignments stay as the base laid them",
				"pkg/config/data.go: dataConfig reads the staff auth's directory registration from APP_STAFF_OIDC_CLIENT_ID, _CLIENT_SECRET, _REDIRECT_URL, _HOSTED_DOMAIN, _GROUP_PREFIX, _ADMIN_CREDENTIALS, and _ADMIN_SUBJECT",
				"pkg/config/data.go: the staff auth's construction now passes the login page and the directory registration",
				"app/app.go, pkg/router/router.go: session.PasswordAuthHandlers and *session.PasswordAuth[session.NoCustomData, session.NoCustomData] swapped for session.OIDCGoogleHandlers and *session.OIDCGoogle[session.NoCustomData, session.NoCustomData]",
				"pkg/router/router.go: the password login route replaced by the directory's: GET /user/login (the redirect), GET /user/callback (the return)",
				"Procfile: 2 go run command(s) build with -tags skipAuth, so the staff auth's directory is simulated in development and every staff login is APP_USERNAME",
				".envrc.template: APP_USERNAME and APP_ROLES for the simulated directory, and the staff auth's APP_STAFF_OIDC_* registration, to fill in",
				"ran go generate ./...",
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				for _, gone := range []string{"000003_StaffSessions", "000004_StaffSessionUsers"} {
					for _, suffix := range []string{".up.sql", ".down.sql"} {
						if _, err := os.Stat(a.Abs("schema/migrations/" + gone + suffix)); !os.IsNotExist(err) {
							t.Errorf("%s%s still exists (err = %v); a fresh auth's password migrations are replaced", gone, suffix, err)
						}
					}
				}
				up := read(t, a, "schema/migrations/000003_StaffOIDCGoogle.up.sql")
				for _, want := range []string{"signs in through Google OpenID Connect from the start", "CREATE TABLE StaffSessions (", "CREATE TABLE StaffOIDCUsers (", "CREATE UNIQUE INDEX StaffOIDCUsersBySub ON StaffOIDCUsers (Sub);"} {
					if !strings.Contains(up, want) {
						t.Errorf("up migration lacks %q:\n%s", want, up)
					}
				}
				for _, absent := range []string{"DROP", "StaffUserRoles", "StaffSessionUsers"} {
					if strings.Contains(up, absent) {
						t.Errorf("up migration has %q; a fresh auth drops nothing:\n%s", absent, up)
					}
				}
				down := read(t, a, "schema/migrations/000003_StaffOIDCGoogle.down.sql")
				for _, want := range []string{"DROP TABLE StaffOIDCUsers;", "DROP TABLE StaffSessions;"} {
					if !strings.Contains(down, want) {
						t.Errorf("down migration lacks %q:\n%s", want, down)
					}
				}
				if strings.Contains(down, "CREATE") {
					t.Errorf("down migration recreates something; a fresh auth's down only drops:\n%s", down)
				}
				if got := read(t, a, "pkg/auth/staff/staff.go"); !strings.Contains(got, "session.NewOIDCGoogle[") {
					t.Error("staff.go is not the Google auth")
				}
			},
		},
		{
			name:   "staff moves to Google with the directory as authority",
			flavor: AuthFlavor{Name: "staff", Flavor: FlavorOIDCGoogle, Authority: AuthorityDirectory},
			wantDid: []string{
				"pkg/auth/staff/staff.go: rewritten as the staff auth in the oidc-google flavor (Google OpenID Connect) from the reference skeleton's members auth, role membership the directory's (session.GoogleRoleSync) (tables StaffSessions and StaffOIDCUsers, cookie staff, store prefix Staff); what the file carried beyond the base's shape is in git to re-apply",
				"schema/migrations: 000005_StaffOIDCGoogle, the staff auth's session tables (StaffSessions, StaffSessionUsers) dropped and created in the oidc-google shape, StaffSessions and StaffOIDCUsers, and StaffUserRoles dropped and recreated so no role stays keyed by a password username; down recreates them from 000003_StaffSessions, 000004_StaffSessionUsers",
				"pkg/config/data.go: dataConfig reads the staff auth's directory registration from APP_STAFF_OIDC_CLIENT_ID, _CLIENT_SECRET, _REDIRECT_URL, _HOSTED_DOMAIN, _GROUP_PREFIX, _ADMIN_CREDENTIALS, and _ADMIN_SUBJECT",
				"pkg/config/data.go: the staff auth's construction now passes the login page and the directory registration",
				"app/app.go, pkg/router/router.go: session.PasswordAuthHandlers and *session.PasswordAuth[session.NoCustomData, session.NoCustomData] swapped for session.OIDCGoogleHandlers and *session.OIDCGoogle[session.NoCustomData, session.NoCustomData]",
				"pkg/router/router.go: the password login route replaced by the directory's: GET /user/login (the redirect), GET /user/callback (the return)",
				"Procfile: 2 go run command(s) build with -tags skipAuth, so the staff auth's directory is simulated in development and every staff login is APP_USERNAME",
				".envrc.template: APP_USERNAME and APP_ROLES for the simulated directory, and the staff auth's APP_STAFF_OIDC_* registration, to fill in",
				"ran go generate ./...",
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				pkg := read(t, a, "pkg/auth/staff/staff.go")
				for _, want := range []string{
					"\t\"github.com/cccteam/session/googlegroups\"\n",
					"groups, err := googlegroups.NewDirectory(ctx, settings.Directory.AdminCredentials, settings.Directory.AdminSubject)",
					"session.GoogleRoleSync(accessClient.UserManager(), settings.Domains, settings.Directory.GroupPrefix, groups),",
					"\tGroupPrefix string\n", "\tAdminCredentials []byte\n", "\tDomains session.DomainsProvider\n",
				} {
					if !strings.Contains(pkg, want) {
						t.Errorf("staff.go lacks %q:\n%s", want, pkg)
					}
				}
				if strings.Contains(pkg, "DisableRoleSync(),") || strings.Contains(pkg, "session.RoleSync(") {
					t.Error("staff.go keeps another authority's slot")
				}
				if _, err := format.Source([]byte(pkg)); err != nil {
					t.Errorf("staff.go does not parse: %v", err)
				}
				config := read(t, a, "pkg/config/data.go")
				for _, want := range []string{"\t\t\tGroupPrefix:      env.StaffGroupPrefix,\n\t\t\tAdminCredentials: env.StaffAdminCredentials,\n", "\tStaffAdminCredentials []byte `env:\"APP_STAFF_OIDC_ADMIN_CREDENTIALS\"`\n"} {
					if !strings.Contains(config, want) {
						t.Errorf("data.go lacks %q:\n%s", want, config)
					}
				}
				if env := read(t, a, ".envrc.template"); !strings.Contains(env, "export APP_STAFF_OIDC_GROUP_PREFIX=staff-\n") || !strings.Contains(env, "# export APP_STAFF_OIDC_ADMIN_SUBJECT=\n") {
					t.Errorf(".envrc.template = %q", env)
				}
			},
		},
		{
			name:   "staff moves to Google, roles carried",
			flavor: AuthFlavor{Name: "staff", Flavor: FlavorOIDCGoogle, Authority: AuthorityApplication, CarryRoles: true},
			wantDid: []string{
				"pkg/auth/staff/staff.go: rewritten as the staff auth in the oidc-google flavor (Google OpenID Connect) from the reference skeleton's members auth, role membership the application's (session.DisableRoleSync) (tables StaffSessions and StaffOIDCUsers, cookie staff, store prefix Staff); what the file carried beyond the base's shape is in git to re-apply",
				"schema/migrations: 000005_StaffOIDCGoogle, the staff auth's session tables (StaffSessions, StaffSessionUsers) dropped and created in the oidc-google shape, StaffSessions and StaffOIDCUsers; down recreates them from 000003_StaffSessions, 000004_StaffSessionUsers",
				"pkg/config/data.go: dataConfig reads the staff auth's directory registration from APP_STAFF_OIDC_CLIENT_ID, _CLIENT_SECRET, _REDIRECT_URL, and _HOSTED_DOMAIN",
				"pkg/config/data.go: the staff auth's construction now passes the login page and the directory registration",
				"app/app.go, pkg/router/router.go: session.PasswordAuthHandlers and *session.PasswordAuth[session.NoCustomData, session.NoCustomData] swapped for session.OIDCGoogleHandlers and *session.OIDCGoogle[session.NoCustomData, session.NoCustomData]",
				"pkg/router/router.go: the password login route replaced by the directory's: GET /user/login (the redirect), GET /user/callback (the return)",
				"Procfile: 2 go run command(s) build with -tags skipAuth, so the staff auth's directory is simulated in development and every staff login is APP_USERNAME",
				".envrc.template: APP_USERNAME and APP_ROLES for the simulated directory, and the staff auth's APP_STAFF_OIDC_* registration, to fill in",
				"ran go generate ./...",
			},
			check: func(t *testing.T, a *app.App) {
				t.Helper()
				pkg := read(t, a, "pkg/auth/staff/staff.go")
				for _, want := range []string{"session.NewOIDCGoogle[", "sessionstorage.NewSpannerGoogleOIDC(", "settings.Directory.HostedDomain,"} {
					if !strings.Contains(pkg, want) {
						t.Errorf("staff.go lacks %q", want)
					}
				}
				up := read(t, a, "schema/migrations/000005_StaffOIDCGoogle.up.sql")
				if strings.Contains(up, "OidcSid") || strings.Contains(up, "StaffUserRoles") || !strings.Contains(up, "CREATE UNIQUE INDEX StaffOIDCUsersBySub ON StaffOIDCUsers (Sub);") {
					t.Errorf("up migration = %q", up)
				}
				router := read(t, a, "pkg/router/router.go")
				if strings.Contains(router, "FrontChannelLogout") || !strings.Contains(router, "session.OIDCGoogleHandlers") {
					t.Errorf("router.go = %q", router)
				}
				if env := read(t, a, ".envrc.template"); !strings.Contains(env, "export APP_STAFF_OIDC_HOSTED_DOMAIN=example.com\n") {
					t.Errorf(".envrc.template = %q", env)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := authFiles(t)
			// The base's access DDL, so the assignment table's interleaving and index are
			// in play.
			files["schema/migrations/000002_StaffAccess.up.sql"] = skeletonFile(t, "solo", "schema/migrations/000002_StaffAccess.up.sql")
			files["schema/migrations/000002_StaffAccess.down.sql"] = skeletonFile(t, "solo", "schema/migrations/000002_StaffAccess.down.sql")
			files["pkg/config/data.go"] = authConfigEnv
			files["app/app.go"] = beaconAppEmbed
			files["pkg/router/router.go"] = beaconRouter
			files[".envrc.template"] = "export PORT=8090\n"
			a := beacon(t, files)
			exec := &fakeExec{}
			ch, err := tt.flavor.Apply(t.Context(), a, exec)
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

func TestAuthFlavorMeaning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		flavor AuthFlavor
		want   []string
		absent []string
	}{
		{
			name:   "Azure, roles dropped",
			flavor: AuthFlavor{Name: "staff", Flavor: FlavorOIDCAzure, Authority: AuthorityApplication},
			want:   []string{"Role membership stays the application's", "`session.OIDCAzureHandlers`", "GET /user/logout (the directory's front-channel logout)", "Role assignments are dropped", "the issuer, client, and secret"},
			absent: []string{"--carry-roles`)"},
		},
		{
			name:   "Google, roles carried, the directory its authority",
			flavor: AuthFlavor{Name: "staff", Flavor: FlavorOIDCGoogle, Authority: AuthorityDirectory, CarryRoles: true},
			want:   []string{"Role membership is now the directory's", "`session.OIDCGoogleHandlers`", "Role assignments are carried", "hosted domain"},
			absent: []string{"front-channel"},
		},
		{
			name:   "fresh from impulse new: born in the shape, nothing to re-sign or drop",
			flavor: AuthFlavor{Name: "crew", Flavor: FlavorOIDCGoogle, Authority: AuthorityDirectory, Fresh: true},
			want:   []string{"The crew auth was composed into the creation in the oidc-google flavor", "Role membership is now the directory's", "No data consequence: the auth was created moments ago"},
			absent: []string{"Data consequence: everyone", "Role assignments are dropped", "Role assignments are carried"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.flavor.Meaning()
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("Meaning() lacks %q", w)
				}
			}
			for _, w := range tt.absent {
				if strings.Contains(got, w) {
					t.Errorf("Meaning() has %q", w)
				}
			}
		})
	}
}
