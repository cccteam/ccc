package transition

import (
	"errors"
	"io/fs"
	"os"
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
		{name: "an OIDC flavor", auth: Auth{Name: "partners", Flavor: app.FlavorOIDCAzure}, wantErr: `flavor "oidc-azure"`},
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
		wantDid     []string
		wantSkipped []string
		check       func(t *testing.T, a *app.App)
	}{
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

			a := beacon(t, authFiles(t))
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
