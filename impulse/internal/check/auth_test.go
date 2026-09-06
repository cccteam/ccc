package check

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// authFile renders a config file constructing the given session authenticators.
func authFile(constructions ...string) string {
	body := ""
	for i, c := range constructions {
		body += "\t_, _ = " + c + "\n"
		if i < len(constructions)-1 {
			body += "\n"
		}
	}

	return `package config

import (
	"github.com/cccteam/session"
	"github.com/cccteam/session/sessionstorage"
)

func newSessions(client any, key string, opts ...session.PasswordOption) {
` + body + `}
`
}

const (
	passwordDefault = `session.NewPasswordAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerPasswordAuth(client), key)`
	passwordCustom  = `session.NewPasswordAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerPasswordAuth(client, sessionstorage.WithImpersonation(sessionstorage.NewImpersonationTable("Impersonations"))),
		key,
		session.WithSessionTableName("PartnerSessions"),
		session.WithUserTableName("PartnerUsers"),
		session.WithCookieName("partner"),
	)`
	preauthCustom  = `session.NewPreauth[session.NoCustomData](sessionstorage.NewSpannerPreauth(client), key, session.WithSessionTableName("PreauthSessions"), session.WithCookieName("enrollment"))`
	preauthDefault = `session.NewPreauth[session.NoCustomData](sessionstorage.NewSpannerPreauth(client), key)`
	googleAnchored = `session.NewOIDCGoogle[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerGoogleOIDC(client, sessionstorage.WithOIDCUsers(), sessionstorage.WithSpannerCustomSessionData(sessionstorage.NewSpannerCustomSessionData("StaffSessionData", nil))),
		nil, key, "id", "secret", "http://localhost/callback", "example.com",
		session.WithSessionTableName("StaffSessions"),
	)`
	azureAnchorless = `session.NewOIDCAzure[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerOIDC(client), nil, key, "issuer", "id", "secret", "http://localhost/callback", session.WithOIDCUserTableName("Ignored"))`
	azureDirectory  = `session.NewOIDCAzure[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerOIDC(client, sessionstorage.WithOIDCUsers()), session.RoleSync(nil, nil), key, "issuer", "id", "secret", "http://localhost/callback", session.WithSessionTableName("MembersSessions"), session.WithOIDCUserTableName("MembersOIDCUsers"), session.WithCookieName("members"))`
	forwarded       = `session.NewPasswordAuth[session.NoCustomData, session.NoCustomData](sessionstorage.NewSpannerPasswordAuth(client), key, opts...)`
)

func tables(names ...string) string {
	sql := ""
	for _, n := range names {
		sql += "CREATE TABLE " + n + " (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n"
	}

	return sql
}

// constNamed constructs a password auth whose table and cookie names are package
// constants, as an auth package writes them.
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

	sessionsTable = TablePrefix + "Sessions"
	usersTable    = TablePrefix + "SessionUsers"
)

func New(ctx context.Context, db *cloudspanner.Client, key string) (*session.PasswordAuth[session.NoCustomData, session.NoCustomData], error) {
	return session.NewPasswordAuth[session.NoCustomData, session.NoCustomData](
		sessionstorage.NewSpannerPasswordAuth(db), key,
		session.WithSessionTableName(sessionsTable),
		session.WithUserTableName(usersTable),
		session.WithCookieName(Name),
	)
}
`

func TestAuthWired(t *testing.T) {
	t.Parallel()

	site := program("pkg/resources",
		`generation.GenerateHandlers("app"),`,
		`generation.GenerateRoutes("pkg/router", "api"),`,
	)

	tests := []struct {
		name        string
		files       map[string]string
		wantStatus  Status
		wantSummary string
		wantDetails []string
	}{
		{
			name: "three pools over their tables",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(passwordCustom, preauthCustom, googleAnchored),
				"schema/migrations/000003_Sessions.up.sql": tables("PartnerSessions", "PartnerUsers", "Impersonations", "PreauthSessions", "StaffSessions", "GoogleOIDCUsers", "StaffSessionData"),
			},
			wantStatus:  Pass,
			wantSummary: "3 auth(s): oidc-google (StaffSessions, GoogleOIDCUsers, StaffSessionData); password (PartnerSessions, PartnerUsers, Impersonations, cookie partner); preauth (PreauthSessions, cookie enrollment)",
		},
		{
			name: "an auth package naming its tables through constants",
			files: map[string]string{
				"cmd/generate/main.go":                  program("pkg/resources", `generation.GenerateHandlers("app"),`, `generation.GenerateRoutes("pkg/router", "api"),`),
				"pkg/auth/staff/staff.go":               constNamed,
				"schema/migrations/000002_Staff.up.sql": "CREATE TABLE StaffSessions (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\nCREATE TABLE StaffSessionUsers (Id STRING(36) NOT NULL) PRIMARY KEY (Id);\n",
			},
			wantStatus:  Pass,
			wantSummary: "1 auth(s): staff: password (StaffSessions, StaffSessionUsers, cookie staff)",
		},
		{
			name: "one pool with forwarded options",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(forwarded),
				"schema/migrations/000003_Sessions.up.sql": tables("Sessions", "SessionUsers"),
			},
			wantStatus:  Pass,
			wantSummary: "1 auth(s): password (Sessions, SessionUsers)",
			wantDetails: []string{
				"pkg/config/session.go:9: options are forwarded from the caller (opts...); tables and cookie beyond the defaults are not visible here",
			},
		},
		{
			name: "azure without the user anchor reads only its sessions table",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(azureAnchorless),
				"schema/migrations/000003_Sessions.up.sql": tables("Sessions"),
			},
			wantStatus:  Pass,
			wantSummary: "1 auth(s): oidc-azure (Sessions)",
		},
		{
			name: "a directory-run azure auth names its authority",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(azureDirectory),
				"schema/migrations/000003_Sessions.up.sql": tables("MembersSessions", "MembersOIDCUsers"),
			},
			wantStatus:  Pass,
			wantSummary: "1 auth(s): oidc-azure (MembersSessions, MembersOIDCUsers, cookie members, directory authority)",
		},
		{
			name: "missing tables and a shared sessions table",
			files: map[string]string{
				"cmd/generate/main.go":                     site,
				"pkg/config/session.go":                    authFile(passwordDefault, preauthDefault),
				"schema/migrations/000003_Sessions.up.sql": tables("Sessions"),
			},
			wantStatus:  Fail,
			wantSummary: "2 auth wiring problem(s)",
			wantDetails: []string{
				"pkg/config/session.go:9: password auth reads table SessionUsers, which no migration creates (the session library's schema is under schema/spanner/migrations)",
				"pkg/config/session.go:11: preauth auth shares sessions table Sessions with password auth; each flavor needs its own (WithSessionTableName)",
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
			got := authWired{}.Run(context.Background(), &Env{App: a})
			want := Result{Name: authWired{}.Name(), Status: tt.wantStatus, Summary: tt.wantSummary, Details: tt.wantDetails}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Run() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
