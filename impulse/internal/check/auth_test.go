package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
)

// authFile renders a config file constructing session Auths with methods registered on
// them: each construction is one statement, its registrations the statements after it,
// and constructions are separated by a blank line. The first statement is on line 12.
func authFile(constructions ...string) string {
	body := ""
	for i, c := range constructions {
		body += "\t" + c + "\n"
		if i < len(constructions)-1 {
			body += "\n"
		}
	}

	return `package config

import (
	"github.com/cccteam/session"
	"github.com/cccteam/session/azure"
	"github.com/cccteam/session/google"
	"github.com/cccteam/session/sessionstorage"
	"github.com/cccteam/session/signin"
)

func newSessions(client any, key string, opts ...session.AuthOption) {
` + body + `}
`
}

const (
	defaultAuth = `authA, _ := session.NewAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerAuth(client), key)
	_ = session.PasswordSignIn(authA)`
	customAuth = `partners, _ := session.NewAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerAuth(client, sessionstorage.WithImpersonation(sessionstorage.NewImpersonationTable("Impersonations"))), key, session.WithSessionTableName("PartnerSessions"), session.WithUserTableName("PartnerUsers"), session.WithIdentitiesTableName("PartnerIdentities"), session.WithPendingSignInsTableName("PartnerPending"), session.WithSignInAttemptsTableName("PartnerAttempts"), session.WithCookieName("partner"))
	_ = session.PasswordSignIn(partners)`
	googleDirectory = `staff, _ := session.NewAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerAuth(client, sessionstorage.WithAuthEvents(sessionstorage.NewAuthEventsTable("StaffEvents")), sessionstorage.WithSpannerCustomSessionData(sessionstorage.NewSpannerCustomSessionData("StaffSessionData", nil))), key, session.WithSessionTableName("StaffSessions"), session.WithUserTableName("StaffUsers"), session.WithIdentitiesTableName("StaffIdentities"), session.WithPendingSignInsTableName("StaffPending"), session.WithSignInAttemptsTableName("StaffAttempts"))
	_, _ = google.SignIn(staff, "workspace", signin.MembersOfDirectory, &google.Config{Workspaces: google.Workspace("workspace", "example.com"), RoleSync: google.RoleSync(nil, "", nil)})`
	partnersShared = `partners, _ := session.NewAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerAuth(client), key, session.WithUserTableName("PartnerUsers"), session.WithIdentitiesTableName("PartnerIdentities"), session.WithPendingSignInsTableName("PartnerPending"), session.WithSignInAttemptsTableName("PartnerAttempts"))
	_ = session.PasswordSignIn(partners)`
	// defaultAttempts names every table but the sign-in attempts table, which the library
	// then reads under its default name, the way an auth written before the limiter does.
	defaultAttempts = `partners, _ := session.NewAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerAuth(client), key, session.WithSessionTableName("PartnerSessions"), session.WithUserTableName("PartnerUsers"), session.WithIdentitiesTableName("PartnerIdentities"), session.WithPendingSignInsTableName("PartnerPending"))
	_ = session.PasswordSignIn(partners)`
	forwarded = `authA, _ := session.NewAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerAuth(client), key, opts...)
	_ = session.PasswordSignIn(authA)`
	// mailed sends mail (WithMail), so it reads its mail links table too, and registers
	// the password method with open sign-up and the address as the sign-in name.
	mailed = `staff, _ := session.NewAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerAuth(client), key, session.WithSessionTableName("StaffSessions"), session.WithUserTableName("StaffUsers"), session.WithIdentitiesTableName("StaffIdentities"), session.WithPendingSignInsTableName("StaffPending"), session.WithSignInAttemptsTableName("StaffAttempts"), session.WithMailLinksTableName("StaffMailLinks"), session.WithMail(nil, session.MailSettings{}))
	_ = session.PasswordSignIn(staff, session.OpenSignUp(), session.UsernameIsEmail())`
	noMethods      = `_, _ = session.NewAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerAuth(client), key)`
	unreadableName = `authA, _ := session.NewAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerAuth(client), key)
	_, _ = azure.SignIn(authA, registration(), signin.MembersOfDirectory, &azure.Config{Tenants: azure.AnyTenant()})`
	registeredTwice = `authA, _ := session.NewAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerAuth(client), key)
	_, _ = azure.SignIn(authA, "directory", signin.MembersOfDirectory, &azure.Config{Tenants: azure.AnyTenant()})
	_, _ = google.SignIn(authA, "directory", signin.MembersOfDirectory, &google.Config{Workspaces: google.AnyWorkspace()})`
	unattached = `_ = session.PasswordSignIn(other)`
)

func tables(names ...string) string {
	sql := ""
	for _, n := range names {
		sql += "CREATE TABLE " + n + " (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n"
	}

	return sql
}

// constNamed constructs an Auth whose table and cookie names are package constants and
// registers its method in a function of its own, as an auth package writes them.
const constNamed = `package staff

import (
	"context"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
)

const (
	Name        = "staff"
	TablePrefix = "Staff"

	XSRFCookie = Name + "-xsrf"

	sessionsTable   = TablePrefix + "Sessions"
	usersTable      = TablePrefix + "SessionUsers"
	identitiesTable = TablePrefix + "SessionIdentities"
	pendingTable    = TablePrefix + "SessionPendingSignIns"
	attemptsTable   = TablePrefix + "SessionSignInAttempts"
)

type Session = session.Auth[session.NoCustomData, session.NoCustomData, *cloudspanner.ReadWriteTransaction]

func New(ctx context.Context, db *cloudspanner.Client, key string) (*Session, error) {
	authSession, err := session.NewAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerAuth(db), key,
		session.WithSessionTableName(sessionsTable),
		session.WithUserTableName(usersTable),
		session.WithIdentitiesTableName(identitiesTable),
		session.WithPendingSignInsTableName(pendingTable),
		session.WithSignInAttemptsTableName(attemptsTable),
		session.WithCookieName(Name),
		session.WithXSRFCookieName(XSRFCookie),
	)
	if err != nil {
		return nil, err
	}
	registerPassword(authSession)

	return authSession, nil
}

func registerPassword(authSession *Session) *session.PasswordMethod {
	return session.PasswordSignIn(authSession)
}
`

// impersonationNamed constructs an Auth in an auth package whose impersonation table is
// defined by the given statements and handed to the storage by name, the way a caller
// writes it to check NewImpersonationTable's error.
func impersonationNamed(definition string) string {
	return `package crew

import (
	"context"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
)

const impersonationTable = "CrewSessionImpersonations"

func New(ctx context.Context, db *cloudspanner.Client, key string) (*session.PasswordMethod, error) {
	` + definition + `

	authSession, err := session.NewAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerAuth(db, sessionstorage.WithImpersonation(impersonation)), key,
		session.WithSessionTableName("CrewSessions"),
		session.WithUserTableName("CrewSessionUsers"),
		session.WithIdentitiesTableName("CrewSessionIdentities"),
		session.WithPendingSignInsTableName("CrewSessionPendingSignIns"),
		session.WithSignInAttemptsTableName("CrewSessionSignInAttempts"),
	)
	if err != nil {
		return nil, err
	}

	return session.PasswordSignIn(authSession), nil
}
`
}

func TestSessionTables(t *testing.T) {
	t.Parallel()

	site := program("pkg/resources",
		`generation.GenerateHandlers("app"),`,
		`generation.GenerateRoutes("pkg/router", "api"),`,
	)
	// issuing declares a machine outlet whose tokens the Auth in pkg/config issues.
	issuing := program("pkg/resources",
		`generation.GenerateHandlers("app"),`,
		`generation.GenerateRouter(),`,
		`generation.GenerateRoutes("pkg/router", "api", generation.Auth("example.com/harbor/pkg/config", generation.PasswordMethod()), generation.WebApp("/")),`,
		`generation.WithRouterOutlet("machines", "machines", generation.Tokens("example.com/harbor/pkg/config")),`,
	)
	const crewTables = "CrewSessions, CrewSessionUsers, CrewSessionIdentities, CrewSessionPendingSignIns, CrewSessionSignInAttempts, CrewSessionImpersonations"

	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "two Auths over their tables",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(customAuth, googleDirectory),
				"schema/migrations/000003_Sessions.up.sql": tables("PartnerSessions", "PartnerUsers", "PartnerIdentities", "PartnerPending", "PartnerAttempts", "Impersonations", "StaffSessions", "StaffUsers", "StaffIdentities", "StaffPending", "StaffAttempts", "StaffEvents", "StaffSessionData"),
			},
			wantStatus:  Pass,
			wantSummary: "2 auth(s): methods password; tables PartnerSessions, PartnerUsers, PartnerIdentities, PartnerPending, PartnerAttempts, Impersonations, cookie partner; methods workspace (google, directory authority); tables StaffSessions, StaffUsers, StaffIdentities, StaffPending, StaffAttempts, StaffEvents, StaffSessionData",
		},
		{
			name: "an auth package naming its tables through constants",
			files: map[string]string{
				"cmd/generate/main.go":                  site,
				"pkg/auth/staff/staff.go":               constNamed,
				"schema/migrations/000002_Staff.up.sql": tables("StaffSessions", "StaffSessionUsers", "StaffSessionIdentities", "StaffSessionPendingSignIns", "StaffSessionSignInAttempts"),
			},
			wantStatus:  Pass,
			wantSummary: "1 auth(s): staff: methods password; tables StaffSessions, StaffSessionUsers, StaffSessionIdentities, StaffSessionPendingSignIns, StaffSessionSignInAttempts, cookie staff, xsrf cookie staff-xsrf",
		},
		{
			name: "one Auth with forwarded options",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(forwarded),
				"schema/migrations/000003_Sessions.up.sql": tables("Sessions", "SessionUsers", "SessionIdentities", "SessionPendingSignIns", "SessionSignInAttempts"),
			},
			wantStatus:  Pass,
			wantSummary: "1 auth(s): methods password; tables Sessions, SessionUsers, SessionIdentities, SessionPendingSignIns, SessionSignInAttempts",
			wantDetails: []string{
				"pkg/config/session.go:12: options are forwarded from the caller (opts...); tables and cookies beyond the defaults are not visible here",
			},
		},
		{
			name: "a missing table and a shared sessions table",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(defaultAuth, partnersShared),
				"schema/migrations/000003_Sessions.up.sql": tables("Sessions", "SessionUsers", "SessionIdentities", "SessionSignInAttempts", "PartnerUsers", "PartnerIdentities", "PartnerPending", "PartnerAttempts"),
			},
			wantStatus:  Fail,
			wantSummary: "2 auth wiring problem(s)",
			wantDetails: []string{
				"pkg/config/session.go:12: the Auth at pkg/config/session.go:12 reads table SessionPendingSignIns, which no migration creates (the session library's schema is under " + schemaSets + ")",
				"pkg/config/session.go:15: the Auth at pkg/config/session.go:15 shares table Sessions with the Auth at pkg/config/session.go:12; each Auth needs its own tables (the With*TableName options and the storage's table constructors)",
			},
		},
		{
			name: "an Auth that sends mail reads its mail links table",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(mailed),
				"schema/migrations/000003_Sessions.up.sql": tables("StaffSessions", "StaffUsers", "StaffIdentities", "StaffPending", "StaffAttempts", "StaffMailLinks"),
			},
			wantStatus:  Pass,
			wantSummary: "1 auth(s): methods password (open sign-up, the username is the address); tables StaffSessions, StaffUsers, StaffIdentities, StaffPending, StaffAttempts, StaffMailLinks",
		},
		{
			name: "a missing mail links table",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(mailed),
				"schema/migrations/000003_Sessions.up.sql": tables("StaffSessions", "StaffUsers", "StaffIdentities", "StaffPending", "StaffAttempts"),
			},
			wantStatus:  Fail,
			wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{
				"pkg/config/session.go:12: the Auth at pkg/config/session.go:12 reads table StaffMailLinks, which no migration creates (the session library's schema is under " + schemaSets + ")",
			},
		},
		{
			name: "an Auth a machine outlet names as its issuer reads the tokens and device codes tables",
			files: map[string]string{
				"cmd/generate/main.go":                     issuing,
				"pkg/config/session.go":                    authFile(mailed),
				"schema/migrations/000003_Sessions.up.sql": tables("StaffSessions", "StaffUsers", "StaffIdentities", "StaffPending", "StaffAttempts", "StaffMailLinks", "SessionTokens", "SessionDeviceCodes"),
			},
			wantStatus:  Pass,
			wantSummary: "1 auth(s): methods password (open sign-up, the username is the address); tables StaffSessions, StaffUsers, StaffIdentities, StaffPending, StaffAttempts, StaffMailLinks, SessionTokens, SessionDeviceCodes",
		},
		{
			name: "missing tokens and device codes tables",
			files: map[string]string{
				"cmd/generate/main.go":                     issuing,
				"pkg/config/session.go":                    authFile(mailed),
				"schema/migrations/000003_Sessions.up.sql": tables("StaffSessions", "StaffUsers", "StaffIdentities", "StaffPending", "StaffAttempts", "StaffMailLinks"),
			},
			wantStatus:  Fail,
			wantSummary: "2 auth wiring problem(s)",
			wantDetails: []string{
				"pkg/config/session.go:12: the Auth at pkg/config/session.go:12 reads table SessionTokens, which no migration creates (the session library's schema is under " + schemaSets + ")",
				"pkg/config/session.go:12: the Auth at pkg/config/session.go:12 reads table SessionDeviceCodes, which no migration creates (the session library's schema is under " + schemaSets + ")",
			},
		},
		{
			name: "a missing sign-in attempts table",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(defaultAuth),
				"schema/migrations/000003_Sessions.up.sql": tables("Sessions", "SessionUsers", "SessionIdentities", "SessionPendingSignIns"),
			},
			wantStatus:  Fail,
			wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{
				"pkg/config/session.go:12: the Auth at pkg/config/session.go:12 reads table SessionSignInAttempts, which no migration creates (the session library's schema is under " + schemaSets + ")",
			},
		},
		{
			name: "two Auths counting sign-in attempts in the library's default table",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(defaultAuth, defaultAttempts),
				"schema/migrations/000003_Sessions.up.sql": tables("Sessions", "SessionUsers", "SessionIdentities", "SessionPendingSignIns", "SessionSignInAttempts", "PartnerSessions", "PartnerUsers", "PartnerIdentities", "PartnerPending"),
			},
			wantStatus:  Fail,
			wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{
				"pkg/config/session.go:15: the Auth at pkg/config/session.go:15 shares table SessionSignInAttempts with the Auth at pkg/config/session.go:12; each Auth needs its own tables (the With*TableName options and the storage's table constructors)",
			},
		},
		{
			name: "an Auth with no method registered",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(noMethods),
				"schema/migrations/000003_Sessions.up.sql": tables("Sessions", "SessionUsers", "SessionIdentities", "SessionPendingSignIns", "SessionSignInAttempts"),
			},
			wantStatus:  Fail,
			wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{
				"pkg/config/session.go:12: the Auth at pkg/config/session.go:12 registers no sign-in method, so nobody can sign in to it; register session.PasswordSignIn, azure.SignIn, or google.SignIn on it",
			},
		},
		{
			name: "a registration name the scan cannot read",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(unreadableName),
				"schema/migrations/000003_Sessions.up.sql": tables("Sessions", "SessionUsers", "SessionIdentities", "SessionPendingSignIns", "SessionSignInAttempts"),
			},
			wantStatus:  Fail,
			wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{
				"pkg/config/session.go:13: the azure method's registration name is not a literal or a constant of the file, so the routes it serves cannot be read",
			},
		},
		{
			name: "a registration name given twice",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(registeredTwice),
				"schema/migrations/000003_Sessions.up.sql": tables("Sessions", "SessionUsers", "SessionIdentities", "SessionPendingSignIns", "SessionSignInAttempts"),
			},
			wantStatus:  Fail,
			wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{
				"pkg/config/session.go:14: the Auth at pkg/config/session.go:12 registers directory twice; a registration name is unique on an Auth",
			},
		},
		{
			name: "a method registered on an Auth the scan cannot see",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(customAuth, googleDirectory, unattached),
				"schema/migrations/000003_Sessions.up.sql": tables("PartnerSessions", "PartnerUsers", "PartnerIdentities", "PartnerPending", "PartnerAttempts", "Impersonations", "StaffSessions", "StaffUsers", "StaffIdentities", "StaffPending", "StaffAttempts", "StaffEvents", "StaffSessionData"),
			},
			wantStatus:  Fail,
			wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{
				`pkg/config/session.go:18: the password method (password) is registered on "other", which names no session.NewAuth construction the scan can see; register methods in the package that constructs the Auth, on the identifier its construction is assigned to`,
			},
		},
		{
			name: "an impersonation table assigned with its error checked and passed by name",
			files: map[string]string{
				"cmd/generate/main.go":                 site,
				"pkg/auth/crew/crew.go":                impersonationNamed("impersonation, err := sessionstorage.NewImpersonationTable(\"CrewSessionImpersonations\")\n\tif err != nil {\n\t\treturn nil, err\n\t}"),
				"schema/migrations/000002_Crew.up.sql": tables("CrewSessions", "CrewSessionUsers", "CrewSessionIdentities", "CrewSessionPendingSignIns", "CrewSessionSignInAttempts", "CrewSessionImpersonations"),
			},
			wantStatus:  Pass,
			wantSummary: "1 auth(s): crew: methods password; tables " + crewTables,
		},
		{
			name: "an impersonation table assigned from a constant and passed by name",
			files: map[string]string{
				"cmd/generate/main.go":                 site,
				"pkg/auth/crew/crew.go":                impersonationNamed("impersonation := sessionstorage.NewImpersonationTable(impersonationTable)"),
				"schema/migrations/000002_Crew.up.sql": tables("CrewSessions", "CrewSessionUsers", "CrewSessionIdentities", "CrewSessionPendingSignIns", "CrewSessionSignInAttempts", "CrewSessionImpersonations"),
			},
			wantStatus:  Pass,
			wantSummary: "1 auth(s): crew: methods password; tables " + crewTables,
		},
		{
			name: "an impersonation table passed by a name NewImpersonationTable did not define",
			files: map[string]string{
				"cmd/generate/main.go":                 site,
				"pkg/auth/crew/crew.go":                impersonationNamed("impersonation := newImpersonation()"),
				"schema/migrations/000002_Crew.up.sql": tables("CrewSessions", "CrewSessionUsers", "CrewSessionIdentities", "CrewSessionPendingSignIns", "CrewSessionSignInAttempts", "CrewSessionImpersonations"),
			},
			wantStatus:  Fail,
			wantSummary: "1 auth wiring problem(s)",
			wantDetails: []string{
				"pkg/auth/crew/crew.go:16: the crew auth reads table SessionImpersonations, which no migration creates (the session library's schema is under " + schemaSets + ")",
			},
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
			got := sessionTables{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: sessionTables{}.Name(), Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
