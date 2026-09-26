package derive

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/impulse/app"
)

// testPlacement is the lab placement the fixtures are derived under.
func testPlacement(t *testing.T) *Placement {
	t.Helper()

	p, err := ReadPlacement(filepath.Join("testdata", "placement.json"))
	if err != nil {
		t.Fatalf("ReadPlacement() error = %v", err)
	}

	return p
}

// secretLine is what a secret says about itself: variable, container name, declaration.
func secretLine(s Secret) string {
	return s.Variable.Name + " " + s.Name + " " + s.Variable.File + " " + s.Variable.Declaration()
}

func TestDerive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		fixture       string
		wantErr       string
		wantApp       string
		wantSecrets   []string
		wantCore      []string
		wantData      []string
		wantSite      []string
		wantSiteLvls  []string
		wantMigrate   []string
		wantCallback  Route
		wantRedirect  string
		wantSchema    Schema
		wantHostnames map[string]string
		wantSupplies  map[string]Supply
		wantGroupPfx  string
	}{
		{
			name:    "harbor",
			fixture: "harbor",
			wantApp: "harbor",
			wantSecrets: []string{
				"APP_COOKIE_KEY cookie-key pkg/config/data.go dataConfig.CookieKey",
				"APP_STAFF_OIDC_CLIENT_SECRET staff-oidc-client-secret pkg/config/data.go dataConfig.StaffClientSecret",
				"APP_STAFF_OIDC_ADMIN_CREDENTIALS staff-oidc-admin-credentials pkg/config/data.go dataConfig.StaffAdminCredentials",
			},
			wantCore: []string{varVersion, varServiceName, varLoggingProject},
			wantData: []string{
				varDatabaseProject, varDatabaseInstance, varDatabaseName,
				"APP_DEFAULT_SESSION_TIMEOUT", "APP_COOKIE_KEY",
				"APP_STAFF_OIDC_CLIENT_ID", "APP_STAFF_OIDC_CLIENT_SECRET", "APP_STAFF_OIDC_REDIRECT_URL", "APP_STAFF_OIDC_HOSTED_DOMAIN",
				"APP_STAFF_OIDC_GROUP_PREFIX", "APP_STAFF_OIDC_ADMIN_CREDENTIALS", "APP_STAFF_OIDC_ADMIN_SUBJECT",
			},
			wantSite:     []string{varPort, "APP_CONSOLE_DIST"},
			wantSiteLvls: []string{LevelCore, LevelData, LevelSite},
			wantMigrate:  []string{LevelCore, LevelData},
			wantCallback: Route{Method: "GET", Path: "/api/user/callback", File: "pkg/router/zz_gen_router.go"},
			wantRedirect: "APP_STAFF_OIDC_REDIRECT_URL",
			wantSchema:   Schema{MigrationsDir: "schema/migrations", MigrateCall: "pkg/deploy MigrateSchema"},
			wantHostnames: map[string]string{
				"tst": "harbor-tst.impulseframework.dev",
				"stg": "harbor-stg.impulseframework.dev",
				"prd": "harbor.impulseframework.dev",
			},
			wantSupplies: map[string]Supply{
				varVersion:                     SupplyImage,
				varServiceName:                 SupplyDerived,
				varDatabaseProject:             SupplyDerived,
				"APP_DEFAULT_SESSION_TIMEOUT":  SupplyDefault,
				"APP_COOKIE_KEY":               SupplySecret,
				"APP_STAFF_OIDC_CLIENT_ID":     SupplyPlacement,
				"APP_STAFF_OIDC_REDIRECT_URL":  SupplyDerived,
				"APP_STAFF_OIDC_HOSTED_DOMAIN": SupplyPlacement,
				varPort:                        SupplyPlatform,
				"APP_CONSOLE_DIST":             SupplyImage,
			},
			wantGroupPfx: "staff-",
		},
		{
			name:    "no go.mod",
			fixture: ".",
			wantErr: "no go.mod",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a, err := app.Discover(filepath.Join("testdata", tt.fixture))
			if err != nil {
				if tt.wantErr == "" || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("app.Discover() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			m, err := Derive(a, testPlacement(t))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Derive() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Derive() error = %v", err)
			}
			if m.App != tt.wantApp {
				t.Errorf("App = %q, want %q", m.App, tt.wantApp)
			}
			var secrets []string
			for _, s := range m.Secrets {
				secrets = append(secrets, secretLine(s))
			}
			if got := strings.Join(secrets, "\n"); got != strings.Join(tt.wantSecrets, "\n") {
				t.Errorf("Secrets =\n%s\nwant\n%s", got, strings.Join(tt.wantSecrets, "\n"))
			}
			for level, want := range map[string][]string{LevelCore: tt.wantCore, LevelData: tt.wantData, LevelSite: tt.wantSite} {
				var got []string
				for _, v := range m.ByLevel(level) {
					got = append(got, v.Name)
				}
				if strings.Join(got, ",") != strings.Join(want, ",") {
					t.Errorf("ByLevel(%s) = %v, want %v", level, got, want)
				}
			}
			if strings.Join(m.Site.Levels, ",") != strings.Join(tt.wantSiteLvls, ",") {
				t.Errorf("Site.Levels = %v, want %v", m.Site.Levels, tt.wantSiteLvls)
			}
			if m.Migrate == nil || strings.Join(m.Migrate.Levels, ",") != strings.Join(tt.wantMigrate, ",") {
				t.Errorf("Migrate = %+v, want levels %v", m.Migrate, tt.wantMigrate)
			}
			if len(m.Auths) != 1 || m.Auths[0].Callback != tt.wantCallback {
				t.Errorf("Auths = %+v, want one with callback %+v", m.Auths, tt.wantCallback)
			}
			if v := m.Auths[0].Variable(RoleRedirectURL); v == nil || v.Name != tt.wantRedirect {
				t.Errorf("redirect URL variable = %+v, want %s", v, tt.wantRedirect)
			}
			if m.Auths[0].GroupPrefixDefault != tt.wantGroupPfx {
				t.Errorf("GroupPrefixDefault = %q, want %q", m.Auths[0].GroupPrefixDefault, tt.wantGroupPfx)
			}
			if m.Schema != tt.wantSchema {
				t.Errorf("Schema = %+v, want %+v", m.Schema, tt.wantSchema)
			}
			for _, e := range m.Environments {
				if want := tt.wantHostnames[e.Name]; len(e.Hostnames) != 1 || e.Hostnames[0] != want {
					t.Errorf("Environment %s hostnames = %v, want %s", e.Name, e.Hostnames, want)
				}
			}
			for name, want := range tt.wantSupplies {
				v := m.variableNamed(name)
				if v == nil {
					t.Errorf("no variable %s", name)

					continue
				}
				if got := v.Supply(); got != want {
					t.Errorf("%s supply = %v, want %v", name, got, want)
				}
			}
		})
	}
}

// variableNamed returns the variable by environment name, or nil.
func (m *Model) variableNamed(name string) *Variable {
	for i := range m.Variables {
		if m.Variables[i].Name == name {
			return &m.Variables[i]
		}
	}

	return nil
}

func TestPlacementValidate(t *testing.T) {
	t.Parallel()

	valid := func() Placement {
		return Placement{
			Prefix: "imp", Environments: []string{"tst", "prd"}, Regions: []Region{{Name: "us-central1", Code: "uc1"}},
			AppsDomain: "lab.example.com", HostedDomain: "example.com", StateBucket: "b", PlaceholderImage: "i",
			DefaultBranch: "main", Repository: "harbor", ReleaseApp: "acme-release",
		}
	}
	tests := []struct {
		name    string
		mutate  func(p *Placement)
		wantErr string
	}{
		{name: "valid", mutate: func(*Placement) {}},
		{name: "prefix with a hyphen", mutate: func(p *Placement) { p.Prefix = "im-p" }, wantErr: "prefix"},
		{name: "no environments", mutate: func(p *Placement) { p.Environments = nil }, wantErr: "environment"},
		{name: "an uppercase environment", mutate: func(p *Placement) { p.Environments = []string{"TST"} }, wantErr: `environment "TST"`},
		{name: "no regions", mutate: func(p *Placement) { p.Regions = nil }, wantErr: "region"},
		{name: "a long region code", mutate: func(p *Placement) { p.Regions[0].Code = "central" }, wantErr: "code"},
		{name: "no apps domain", mutate: func(p *Placement) { p.AppsDomain = " " }, wantErr: "appsDomain is empty"},
		{name: "no default branch", mutate: func(p *Placement) { p.DefaultBranch = "" }, wantErr: "defaultBranch is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := valid()
			tt.mutate(&p)
			err := p.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() error = %v, want none", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Validate() error = %v, wantErr %q", err, tt.wantErr)
			}
		})
	}
}

func TestSecretName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		variable string
		want     string
	}{
		{name: "the APP_ prefix is dropped", variable: "APP_COOKIE_KEY", want: "cookie-key"},
		{name: "a longer name", variable: "APP_STAFF_OIDC_ADMIN_CREDENTIALS", want: "staff-oidc-admin-credentials"},
		{name: "no prefix", variable: "TWILIO_AUTH_TOKEN", want: "twilio-auth-token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := secretName(tt.variable); got != tt.want {
				t.Errorf("secretName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		variable string
		want     bool
	}{
		{name: "a key", variable: "APP_COOKIE_KEY", want: true},
		{name: "a secret", variable: "APP_STAFF_OIDC_CLIENT_SECRET", want: true},
		{name: "credentials", variable: "APP_STAFF_OIDC_ADMIN_CREDENTIALS", want: true},
		{name: "a token", variable: "APP_TWILIO_AUTH_TOKEN", want: true},
		{name: "a password", variable: "DB_PASSWORD", want: true},
		{name: "a client id", variable: "APP_STAFF_OIDC_CLIENT_ID", want: false},
		{name: "a subject", variable: "APP_STAFF_OIDC_ADMIN_SUBJECT", want: false},
		{name: "a key id", variable: "APP_KEY_ID", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := isSecret(tt.variable); got != tt.want {
				t.Errorf("isSecret(%s) = %v, want %v", tt.variable, got, tt.want)
			}
		})
	}
}

func TestRepositoryURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		module string
		want   string
	}{
		{name: "a GitHub module", module: "github.com/impulseframework/harbor", want: "https://github.com/impulseframework/harbor"},
		{name: "a nested GitHub module", module: "github.com/cccteam/ccc/bedrock", want: "https://github.com/cccteam/ccc"},
		{name: "another host", module: "example.com/acme/beacon", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := repositoryURL(tt.module); got != tt.want {
				t.Errorf("repositoryURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTagDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tag     string
		want    string
		wantSet bool
	}{
		{name: "a default", tag: "PORT,default=8080", want: "8080", wantSet: true},
		{name: "required only", tag: "APP_SERVICE_NAME,required"},
		{name: "a duration", tag: "APP_DEFAULT_SESSION_TIMEOUT,default=10m", want: "10m", wantSet: true},
		{name: "no options", tag: "APP_COOKIE_KEY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, set := tagDefault(tt.tag)
			if got != tt.want || set != tt.wantSet {
				t.Errorf("tagDefault() = %q, %v; want %q, %v", got, set, tt.want, tt.wantSet)
			}
		})
	}
}
