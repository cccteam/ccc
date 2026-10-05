package derive

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/hook"
	"github.com/cccteam/ccc/impulse/app"
)

// testPlacement is the lab placement the fixtures are derived under.
func testPlacement(t *testing.T) *Placement {
	t.Helper()

	return fixturePlacement(t, "placement.json")
}

// fixturePlacement reads a placement of the fixtures by its file name under testdata.
func fixturePlacement(t *testing.T, name string) *Placement {
	t.Helper()

	p, err := ReadPlacement(filepath.Join("testdata", name))
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
		name    string
		fixture string
		// placement is the fixture's placement under testdata, placement.json when empty.
		placement     string
		wantErr       string
		wantApp       string
		wantSecrets   []string
		wantCore      []string
		wantData      []string
		wantSite      []string
		wantSiteLvls  []string
		wantMigrate   []string
		wantJobs      []string
		wantOIDC      bool
		wantCallback  Route
		wantRedirect  string
		wantSchema    Schema
		wantHostnames map[string]string
		wantSupplies  map[string]Supply
		wantGroupPfx  string
		// wantHooks are the stages with a script, wantProgram the hooks program's.
		wantHooks   []hook.Stage
		wantProgram []hook.Stage
		// wantFirestore is what the Firestore files say (firestoreLine), empty for no
		// database.
		wantFirestore string
		// wantFileStores are the file stores (fileStoreLine), none for an application
		// that declares no store.
		wantFileStores []string
		// wantScheduled are the scheduled routes (scheduledLine), none for an application
		// that declares none.
		wantScheduled []string
		// wantFileRoutes are the file routes (fileRouteLine), none for an application that
		// declares no upload and no stored file; wantOutlets the outlets, name and prefix.
		wantFileRoutes []string
		wantOutlets    []string
	}{
		{
			name:    "harbor",
			fixture: "harbor",
			wantApp: "harbor",
			wantSecrets: []string{
				"APP_COOKIE_KEY cookie-key pkg/config/data.go dataConfig.CookieKey",
				"APP_STAFF_OIDC_CLIENT_SECRET staff-oidc-client-secret pkg/config/data.go dataConfig.StaffClientSecret",
			},
			wantCore: []string{varVersion, varServiceName, varLoggingProject},
			wantData: []string{
				varDatabaseProject, varDatabaseInstance, varDatabaseName,
				varFirestoreProject, varFirestoreDatabase, varFirebaseAPIKey, "FIRESTORE_EMULATOR_HOST",
				"APP_DEFAULT_SESSION_TIMEOUT", "APP_COOKIE_KEY",
				"APP_STAFF_OIDC_CLIENT_ID", "APP_STAFF_OIDC_CLIENT_SECRET", "APP_STAFF_OIDC_REDIRECT_URL", "APP_STAFF_OIDC_HOSTED_DOMAIN",
				"APP_STAFF_OIDC_GROUP_PREFIX", "APP_STAFF_OIDC_GROUP_LOOKUP",
				varFileStore, varTasksQueue,
			},
			wantSite:     []string{varPort, "APP_CONSOLE_DIST", "APP_PORTAL_DIST", varJobsJob},
			wantSiteLvls: []string{LevelCore, LevelData, LevelSite},
			wantMigrate:  []string{LevelCore, LevelData},
			wantJobs:     []string{LevelCore, LevelData},
			wantOIDC:     true,
			wantCallback: Route{Method: "GET", Path: "/api/user/callback", File: "pkg/router/zz_gen_router.go"},
			wantRedirect: "APP_STAFF_OIDC_REDIRECT_URL",
			wantSchema:   Schema{MigrationsDir: "schema/migrations", MigrateCall: "pkg/deploy MigrateSchema", GenerateDir: "cmd/generate", GeneratePackage: "generate"},
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
				"APP_PORTAL_DIST":              SupplyImage,
				varJobsJob:                     SupplyImage,
				varFileStore:                   SupplyDerived,
				varTasksQueue:                  SupplyDerived,
				varFirestoreProject:            SupplyDerived,
				varFirestoreDatabase:           SupplyDerived,
				varFirebaseAPIKey:              SupplyDerived,
			},
			wantGroupPfx:   "staff-",
			wantHooks:      []hook.Stage{hook.AfterMigrate, hook.BeforeTraffic, hook.AfterTraffic},
			wantFirestore:  "schema/firestore: 3 index(es) subscriptions_resource_key_expiry, subscriptions_resource_domain_expiry, subscriptions_resource_expiry; 2 field(s) subscriptions_expiry (ttl), changes_expires (ttl); rules_version = '2';",
			wantFileStores: []string{"APP_FILE_STORE data dataConfig.FileStore: default, files, files, google_storage_bucket.files[0]"},
			wantScheduled:  []string{"send-daily-digest: POST /_scheduled/send-daily-digest at 0 7 * * 1-5 in America/New_York"},
			wantFileRoutes: []string{"POST /api/attach-manifest: the @upload method AttachManifest", "GET /api/manifests/{id}/file: the @file column Manifest.Key"},
			wantOutlets:    []string{"default /api"},
		},
		{
			name:        "beacon, a password auth: no registration, no callback",
			fixture:     "beacon",
			placement:   "placement-beacon.json",
			wantApp:     "beacon",
			wantOutlets: []string{"default /api"},
			wantSecrets: []string{
				"APP_COOKIE_KEY cookie-key pkg/config/data.go dataConfig.CookieKey",
			},
			wantCore: []string{varVersion, varServiceName, varLoggingProject},
			wantData: []string{
				varDatabaseProject, varDatabaseInstance, varDatabaseName,
				"APP_DEFAULT_SESSION_TIMEOUT", "APP_COOKIE_KEY",
			},
			wantSite:     []string{varPort, "APP_CONSOLE_DIST"},
			wantSiteLvls: []string{LevelCore, LevelData, LevelSite},
			wantMigrate:  []string{LevelCore, LevelData},
			wantSchema:   Schema{MigrationsDir: "schema/migrations", MigrateCall: "pkg/deploy MigrateSchema", GenerateDir: "cmd/generate", GeneratePackage: "generate"},
			wantHostnames: map[string]string{
				"tst": "beacon-tst.impulseframework.dev",
				"stg": "beacon-stg.impulseframework.dev",
				"prd": "beacon.impulseframework.dev",
			},
			wantSupplies: map[string]Supply{
				varVersion:                    SupplyImage,
				varServiceName:                SupplyDerived,
				varDatabaseProject:            SupplyDerived,
				"APP_DEFAULT_SESSION_TIMEOUT": SupplyDefault,
				"APP_COOKIE_KEY":              SupplySecret,
				varPort:                       SupplyPlatform,
				"APP_CONSOLE_DIST":            SupplyImage,
			},
			wantProgram: []hook.Stage{hook.BeforeMigrate, hook.AfterTraffic},
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
			p := testPlacement(t)
			if tt.placement != "" {
				p = fixturePlacement(t, tt.placement)
			}
			m, err := Derive(a, p)
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
			if (m.Jobs == nil) != (tt.wantJobs == nil) || (m.Jobs != nil && strings.Join(m.Jobs.Levels, ",") != strings.Join(tt.wantJobs, ",")) {
				t.Errorf("Jobs = %+v, want levels %v", m.Jobs, tt.wantJobs)
			}
			if len(m.Auths) != 1 || m.Auths[0].OIDC() != tt.wantOIDC || m.Auths[0].Callback != tt.wantCallback {
				t.Errorf("Auths = %+v, want one with OIDC %t and callback %+v", m.Auths, tt.wantOIDC, tt.wantCallback)
			}
			if v := m.Auths[0].Variable(RoleRedirectURL); (v == nil) != (tt.wantRedirect == "") || (v != nil && v.Name != tt.wantRedirect) {
				t.Errorf("redirect URL variable = %+v, want %q", v, tt.wantRedirect)
			}
			if m.Auths[0].GroupPrefixDefault != tt.wantGroupPfx {
				t.Errorf("GroupPrefixDefault = %q, want %q", m.Auths[0].GroupPrefixDefault, tt.wantGroupPfx)
			}
			if m.Schema != tt.wantSchema {
				t.Errorf("Schema = %+v, want %+v", m.Schema, tt.wantSchema)
			}
			if !slices.Equal(m.Hooks, tt.wantHooks) {
				t.Errorf("Hooks = %v, want %v", m.Hooks, tt.wantHooks)
			}
			if (m.HookProgram == nil) != (tt.wantProgram == nil) || (m.HookProgram != nil && !slices.Equal(m.HookProgram.Stages, tt.wantProgram)) {
				t.Errorf("HookProgram = %+v, want stages %v", m.HookProgram, tt.wantProgram)
			}
			if got := firestoreLine(m.Firestore); got != tt.wantFirestore {
				t.Errorf("Firestore = %q, want %q", got, tt.wantFirestore)
			}
			var stores []string
			for i := range m.FileStores {
				stores = append(stores, fileStoreLine(&m.FileStores[i]))
			}
			if !slices.Equal(stores, tt.wantFileStores) {
				t.Errorf("FileStores = %v, want %v", stores, tt.wantFileStores)
			}
			var scheduled []string
			for i := range m.Scheduled {
				scheduled = append(scheduled, scheduledLine(&m.Scheduled[i]))
			}
			if !slices.Equal(scheduled, tt.wantScheduled) {
				t.Errorf("Scheduled = %v, want %v", scheduled, tt.wantScheduled)
			}
			var fileRoutes, outlets []string
			for i := range m.FileRoutes {
				fileRoutes = append(fileRoutes, fileRouteLine(&m.FileRoutes[i]))
			}
			if !slices.Equal(fileRoutes, tt.wantFileRoutes) {
				t.Errorf("FileRoutes = %v, want %v", fileRoutes, tt.wantFileRoutes)
			}
			for _, o := range m.Outlets {
				outlets = append(outlets, o.Name+" /"+o.Prefix)
			}
			if !slices.Equal(outlets, tt.wantOutlets) {
				t.Errorf("Outlets = %v, want %v", outlets, tt.wantOutlets)
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

// firestoreLine is what the Firestore files say, on one line: the directory, the
// indexes by resource name, the fields by resource name with their policy, and the
// rules' first line; empty for no database.
func firestoreLine(fs *Firestore) string {
	if fs == nil {
		return ""
	}
	indexes := make([]string, 0, len(fs.Indexes))
	for i := range fs.Indexes {
		indexes = append(indexes, fs.Indexes[i].Name())
	}
	fields := make([]string, 0, len(fs.Fields))
	for i := range fs.Fields {
		name := fs.Fields[i].Name()
		if fs.Fields[i].TTL {
			name += " (ttl)"
		}
		fields = append(fields, name)
	}
	rules, _, _ := strings.Cut(fs.Rules, "\n")

	return fmt.Sprintf("%s: %d index(es) %s; %d field(s) %s; %s", fs.Dir, len(indexes), strings.Join(indexes, ", "), len(fields), strings.Join(fields, ", "), rules)
}

// fileStoreLine is what a file store says about itself on one line: the variable, its
// level and declaration, then the store's name (default for the default store), the
// bucket's resource name, the bucket name's suffix and the bucket's address.
func fileStoreLine(s *FileStore) string {
	name := s.Name
	if s.Default() {
		name = "default"
	}

	return fmt.Sprintf("%s %s %s: %s, %s, %s, %s", s.Variable.Name, s.Variable.Level, s.Variable.Declaration(), name, s.Resource, s.Suffix, s.Address())
}

// TestDeriveScheduled reads the scheduled routes from the release file beside the
// generated router, over a copy of the fixture whose release file each case writes: no
// file means no routes, as for an application generated before the file existed, and a
// file that does not read is refused, since the stack could not say which jobs the code
// declares.
func TestDeriveScheduled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// content is the release file's content; empty removes the file.
		content string
		want    []string
		wantErr string
	}{
		{name: "no release file declares no route"},
		{name: "a release file without scheduled routes", content: `{"outlets": {"default": {}}}`},
		{
			name:    "two routes in the file's order",
			content: `{"outlets": {"default": {}}, "scheduled": [{"path": "/_scheduled/prune-logs", "schedule": "30 3 * * *", "timeZone": "UTC"}, {"path": "/_scheduled/send-digest", "schedule": "0 7 * * 1-5", "timeZone": "America/Denver"}]}`,
			want: []string{
				"prune-logs: POST /_scheduled/prune-logs at 30 3 * * * in UTC",
				"send-digest: POST /_scheduled/send-digest at 0 7 * * 1-5 in America/Denver",
			},
		},
		{name: "a release file that does not read is refused", content: `{"outlets": {"default": {}}, "scheduled": [{"path": "/prune-logs"}]}`, wantErr: "pkg/router/zz_gen_release.json: the scheduled route \"/prune-logs\" is not /_scheduled/<method in kebab case>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "app")
			if err := os.CopyFS(dir, os.DirFS(filepath.Join("testdata", "harbor"))); err != nil {
				t.Fatalf("os.CopyFS() error = %v", err)
			}
			file := filepath.Join(dir, "pkg", "router", ReleaseFileName)
			if tt.content == "" {
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(file, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			a, err := app.Discover(dir)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
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
			var got []string
			for i := range m.Scheduled {
				got = append(got, scheduledLine(&m.Scheduled[i]))
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Scheduled = %v, want %v", got, tt.want)
			}
		})
	}
}

// scheduledLine is what a scheduled route says about itself on one line: its name, then
// the call Cloud Scheduler makes and when.
func scheduledLine(r *ScheduledRoute) string {
	return fmt.Sprintf("%s: POST %s at %s in %s", r.Name(), r.Path, r.Schedule, r.TimeZone)
}

// fileRouteLine spells a file route: its method and path, and its declaration.
func fileRouteLine(r *FileRoute) string {
	return fmt.Sprintf("%s %s: %s", r.Method, r.Path, r.Declaration())
}

// TestBucketPolicyAddress reads a bucket policy's address from its bucket's, the way the
// pipeline names the file stores' policies from the buckets its trigger carries.
func TestBucketPolicyAddress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		bucket string
		want   string
	}{
		{name: "the default store's bucket", bucket: "google_storage_bucket.files", want: "google_storage_bucket_iam_policy.files"},
		{name: "a named store's bucket", bucket: "google_storage_bucket.files_client_files", want: "google_storage_bucket_iam_policy.files_client_files"},
		{name: "an address that is not a bucket's", bucket: "google_spanner_database.app", want: ""},
		{name: "a bucket type with no resource name", bucket: "google_storage_bucket.", want: ""},
		{name: "the counted bucket the stack declares: the policy's address carries no index", bucket: "google_storage_bucket.files[0]", want: "google_storage_bucket_iam_policy.files"},
		{name: "a counted bucket with no resource name", bucket: "google_storage_bucket.[0]", want: ""},
		{name: "a policy's own address", bucket: "google_storage_bucket_iam_policy.files", want: ""},
		{name: "nothing", bucket: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := BucketPolicyAddress(tt.bucket); got != tt.want {
				t.Errorf("BucketPolicyAddress(%q) = %q, want %q", tt.bucket, got, tt.want)
			}
		})
	}
}

func TestFileStoreName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		variable string
		// wantStore reports a file-store variable; wantName, wantResource, wantSuffix and
		// wantAddress are the store it declares. wantRole is what the table says of the
		// variable either way.
		wantStore    bool
		wantName     string
		wantResource string
		wantSuffix   string
		wantAddress  string
		wantRole     Role
	}{
		{name: "the default store", variable: "APP_FILE_STORE", wantStore: true, wantName: "", wantResource: "files", wantSuffix: "files", wantAddress: "google_storage_bucket.files[0]", wantRole: RoleFileStore},
		{name: "a named store", variable: "APP_FILE_STORE_DOCUMENTS", wantStore: true, wantName: "documents", wantResource: "files_documents", wantSuffix: "files-documents", wantAddress: "google_storage_bucket.files_documents[0]", wantRole: RoleFileStore},
		{name: "a two-word name: underscores become hyphens in the bucket's name and stay in the resource's", variable: "APP_FILE_STORE_CLIENT_FILES", wantStore: true, wantName: "client-files", wantResource: "files_client_files", wantSuffix: "files-client-files", wantAddress: "google_storage_bucket.files_client_files[0]", wantRole: RoleFileStore},
		{name: "a name with a digit", variable: "APP_FILE_STORE_V2", wantStore: true, wantName: "v2", wantResource: "files_v2", wantSuffix: "files-v2", wantAddress: "google_storage_bucket.files_v2[0]", wantRole: RoleFileStore},
		{name: "letters that continue the prefix are not a store", variable: "APP_FILE_STOREROOM", wantRole: RoleNone},
		{name: "nothing after the underscore is not a store", variable: "APP_FILE_STORE_", wantRole: RoleNone},
		{name: "a name that is not upper snake case is not a store", variable: "APP_FILE_STORE_documents", wantRole: RoleNone},
		{name: "a name with two underscores in a row is not a store", variable: "APP_FILE_STORE__DOCUMENTS", wantRole: RoleNone},
		{name: "a prefix of the variable is not a store", variable: "APP_FILE", wantRole: RoleNone},
		{name: "a well-known variable keeps its exact-name role", variable: "APP_TASKS_QUEUE", wantRole: RoleTasksQueue},
		{name: "a variable of the application's own has no role", variable: "APP_DEFAULT_SESSION_TIMEOUT", wantRole: RoleNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			name, ok := fileStoreName(tt.variable)
			if ok != tt.wantStore || name != tt.wantName {
				t.Fatalf("fileStoreName(%s) = %q, %t, want %q, %t", tt.variable, name, ok, tt.wantName, tt.wantStore)
			}
			if got := roleOf(tt.variable); got != tt.wantRole {
				t.Errorf("roleOf(%s) = %q, want %q", tt.variable, got, tt.wantRole)
			}
			if !ok {
				return
			}
			s := newFileStore(&Variable{Name: tt.variable, Level: LevelData}, name)
			if s.Resource != tt.wantResource || s.Suffix != tt.wantSuffix || s.Address() != tt.wantAddress {
				t.Errorf("newFileStore(%s) = resource %q, suffix %q, address %q, want %q, %q, %q", tt.variable, s.Resource, s.Suffix, s.Address(), tt.wantResource, tt.wantSuffix, tt.wantAddress)
			}
			if want := "google_storage_bucket_iam_policy." + tt.wantResource; s.PolicyAddress() != want {
				t.Errorf("PolicyAddress() = %q, want %q", s.PolicyAddress(), want)
			}
			if s.Default() != (tt.wantName == "") {
				t.Errorf("Default() = %t, want %t", s.Default(), tt.wantName == "")
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
			DefaultBranch: "main", Repository: "harbor", ReleaseApp: "acme-release", BedrockVersion: "v0.1.0", BedrockSHA256: "0000000000000000000000000000000000000000000000000000000000000000",
		}
	}
	tests := []struct {
		name    string
		mutate  func(p *Placement)
		wantErr string
	}{
		{name: "valid", mutate: func(*Placement) {}},
		{name: "unpinned, both empty", mutate: func(p *Placement) { p.BedrockVersion, p.BedrockSHA256 = "", "" }},
		{name: "a release without its checksum", mutate: func(p *Placement) { p.BedrockSHA256 = "" }, wantErr: "bedrockVersion v0.1.0 is a release, and a release pin carries its bedrockSha256"},
		{name: "a version that is not a release", mutate: func(p *Placement) { p.BedrockVersion = "0.1.0" }, wantErr: `bedrockVersion "0.1.0": a release version, v0.4.0 (the tag bedrock/v0.4.0 without its prefix), or a commit pin`},
		{
			name: "a commit pin with no checksum",
			mutate: func(p *Placement) {
				p.BedrockVersion, p.BedrockSHA256 = "v0.0.0-20260928182105-6f6f7795969d", ""
			},
		},
		{
			name: "a commit pin built on a pre-release tag, with no checksum",
			mutate: func(p *Placement) {
				p.BedrockVersion, p.BedrockSHA256 = "v0.0.0-lab.1.0.20260928222237-58b211dce544", ""
			},
		},
		{
			name: "a commit pin with a checksum",
			mutate: func(p *Placement) {
				p.BedrockVersion = "v0.0.0-lab.1.0.20260928222237-58b211dce544"
			},
			wantErr: "a commit pin carries no bedrockSha256: Go's checksum database verifies it",
		},
		{
			name: "a dirty build's version",
			mutate: func(p *Placement) {
				p.BedrockVersion, p.BedrockSHA256 = "v0.0.0-lab.1.0.20260928222237-58b211dce544+dirty", ""
			},
			wantErr: `bedrockVersion "v0.0.0-lab.1.0.20260928222237-58b211dce544+dirty": a release version`,
		},
		{
			name: "a checksum with no version",
			mutate: func(p *Placement) {
				p.BedrockVersion = ""
			},
			wantErr: "bedrockSha256 without a bedrockVersion",
		},
		{name: "a checksum that is not a SHA-256", mutate: func(p *Placement) { p.BedrockSHA256 = "abc" }, wantErr: `bedrockSha256 "abc"`},
		{name: "prefix with a hyphen", mutate: func(p *Placement) { p.Prefix = "im-p" }, wantErr: "prefix"},
		{name: "no environments", mutate: func(p *Placement) { p.Environments = nil }, wantErr: "environment"},
		{name: "an uppercase environment", mutate: func(p *Placement) { p.Environments = []string{"TST"} }, wantErr: `environment "TST"`},
		{name: "no regions", mutate: func(p *Placement) { p.Regions = nil }, wantErr: "region"},
		{name: "a long region code", mutate: func(p *Placement) { p.Regions[0].Code = "central" }, wantErr: "code"},
		{name: "no apps domain", mutate: func(p *Placement) { p.AppsDomain = " " }, wantErr: "appsDomain is empty"},
		{name: "no default branch", mutate: func(p *Placement) { p.DefaultBranch = "" }, wantErr: "defaultBranch is empty"},
		{name: "approvals in an unknown environment", mutate: func(p *Placement) { p.Approvals = []string{"stg"} }, wantErr: `approvals names "stg", which is not one of the environments (tst, prd)`},
		{name: "approvals in a known environment", mutate: func(p *Placement) { p.Approvals = []string{"prd"} }},
		{name: "no approvals at all", mutate: func(p *Placement) { p.Approvals = []string{} }},
		{name: "seed in an unknown environment", mutate: func(p *Placement) { p.Seed = []string{"qa"} }, wantErr: `seed names "qa", which is not one of the environments (tst, prd)`},
		{name: "seed in production", mutate: func(p *Placement) { p.Seed = []string{"prd"} }, wantErr: `seed names "prd", the production environment, which is never seeded`},
		{name: "seed in the first environment", mutate: func(p *Placement) { p.Seed = []string{"tst"} }},
		{name: "release backups in a known environment", mutate: func(p *Placement) { p.ReleaseBackups = []string{"tst", "prd"} }},
		{name: "release backups nowhere", mutate: func(p *Placement) { p.ReleaseBackups = []string{} }},
		{name: "release backups in an unknown environment", mutate: func(p *Placement) { p.ReleaseBackups = []string{"stg"} }, wantErr: `releaseBackups names "stg", which is not one of the environments (tst, prd)`},
		{name: "a retention in days", mutate: func(p *Placement) { p.SpannerRetention = map[string]string{"prd": "7d", "tst": "1d"} }},
		{name: "a retention in hours", mutate: func(p *Placement) { p.SpannerRetention = map[string]string{"tst": "36h"} }},
		{name: "a retention in an unknown environment", mutate: func(p *Placement) { p.SpannerRetention = map[string]string{"stg": "7d"} }, wantErr: `spannerRetention names "stg", which is not one of the environments (tst, prd)`},
		{name: "a retention beyond seven days", mutate: func(p *Placement) { p.SpannerRetention = map[string]string{"prd": "8d"} }, wantErr: `spannerRetention.prd: "8d" is outside what Spanner keeps: between 1h and 7d (168h)`},
		{name: "a retention beyond 168 hours", mutate: func(p *Placement) { p.SpannerRetention = map[string]string{"prd": "169h"} }, wantErr: `spannerRetention.prd: "169h" is outside what Spanner keeps`},
		{name: "a retention in minutes", mutate: func(p *Placement) { p.SpannerRetention = map[string]string{"prd": "90m"} }, wantErr: `spannerRetention.prd: "90m" is not a retention period: a whole number of hours or days between 1h and 7d, such as 7d or 36h`},
		{name: "a retention of nothing", mutate: func(p *Placement) { p.SpannerRetention = map[string]string{"prd": ""} }, wantErr: `spannerRetention.prd: "" is not a retention period`},
		{name: "a build machine Cloud Build offers", mutate: func(p *Placement) { p.BuildMachine = "E2_HIGHCPU_8" }},
		{name: "the smallest build machine", mutate: func(p *Placement) { p.BuildMachine = "E2_MEDIUM" }},
		{
			name:    "a build machine in Compute Engine's spelling",
			mutate:  func(p *Placement) { p.BuildMachine = "e2-highcpu-8" },
			wantErr: `buildMachine "e2-highcpu-8" is not one of Cloud Build's machines (E2_MEDIUM, E2_STANDARD_2, E2_HIGHCPU_8, E2_HIGHCPU_32); absent, the builds run on Cloud Build's default`,
		},
		{name: "a deprecated N1 build machine", mutate: func(p *Placement) { p.BuildMachine = "N1_HIGHCPU_8" }, wantErr: `buildMachine "N1_HIGHCPU_8" is not one of Cloud Build's machines`},
		{name: "the enum's default spelled out", mutate: func(p *Placement) { p.BuildMachine = "UNSPECIFIED" }, wantErr: `buildMachine "UNSPECIFIED" is not one of Cloud Build's machines`},
		{name: "a cap in every environment", mutate: func(p *Placement) { p.MaxInstances = map[string]int{"tst": 2, "prd": 20} }},
		{name: "a cap in one environment", mutate: func(p *Placement) { p.MaxInstances = map[string]int{"prd": 1} }},
		{name: "no cap anywhere", mutate: func(p *Placement) { p.MaxInstances = map[string]int{} }},
		{name: "a cap in an unknown environment", mutate: func(p *Placement) { p.MaxInstances = map[string]int{"stg": 2} }, wantErr: `maxInstances names "stg", which is not one of the environments (tst, prd)`},
		{name: "a cap of zero", mutate: func(p *Placement) { p.MaxInstances = map[string]int{"tst": 0} }, wantErr: "maxInstances.tst is 0: a cap lets the service run at least one instance; leave the environment out for no cap (Cloud Run's default)"},
		{name: "a negative cap", mutate: func(p *Placement) { p.MaxInstances = map[string]int{"prd": -1} }, wantErr: "maxInstances.prd is -1"},
		{name: "projects for a known environment", mutate: func(p *Placement) {
			p.Projects = map[string]Project{"tst": {ID: "imp-tst-gbl-core-b241", Number: "123456789012"}}
		}},
		{name: "projects in an unknown environment", mutate: func(p *Placement) { p.Projects = map[string]Project{"qa": {ID: "p", Number: "1"}} }, wantErr: `projects names "qa", which is not one of the environments (tst, prd)`},
		{name: "a project without an id", mutate: func(p *Placement) { p.Projects = map[string]Project{"tst": {Number: "1"}} }, wantErr: "projects.tst.id is empty"},
		{name: "a project number that is not a number", mutate: func(p *Placement) { p.Projects = map[string]Project{"tst": {ID: "p", Number: "p-123"}} }, wantErr: `projects.tst.number "p-123" is not a project number (digits)`},
		{name: "a build argument of each value of the catalog", mutate: func(p *Placement) {
			p.BuildArguments = map[string]string{"FIREBASE_API_KEY": "firebaseApiKey", "FIRESTORE_DATABASE": "firestoreDatabase", "PROJECT_ID": "projectId", "ENV_NAME": "environment", "HOST2": "hostname"}
		}},
		{name: "no build argument", mutate: func(p *Placement) { p.BuildArguments = map[string]string{} }},
		{
			name:    "a build argument naming a value outside the catalog",
			mutate:  func(p *Placement) { p.BuildArguments = map[string]string{"API_URL": "apiUrl"} },
			wantErr: `buildArguments.API_URL is "apiUrl", which is not one of the values the stack makes: firebaseApiKey, firestoreDatabase, projectId, environment, hostname`,
		},
		{
			name:    "a build argument naming a value in another case",
			mutate:  func(p *Placement) { p.BuildArguments = map[string]string{"PROJECT_ID": "projectID"} },
			wantErr: `buildArguments.PROJECT_ID is "projectID", which is not one of the values the stack makes`,
		},
		{
			name:    "a build argument in lower case",
			mutate:  func(p *Placement) { p.BuildArguments = map[string]string{"firebase_api_key": "firebaseApiKey"} },
			wantErr: `buildArguments names "firebase_api_key": a build argument's name is an uppercase identifier (FIREBASE_API_KEY), the name the Dockerfile declares with ARG`,
		},
		{name: "a build argument starting with a digit", mutate: func(p *Placement) { p.BuildArguments = map[string]string{"1KEY": "projectId"} }, wantErr: `buildArguments names "1KEY"`},
		{name: "a build argument starting with an underscore", mutate: func(p *Placement) { p.BuildArguments = map[string]string{"_PROJECT": "projectId"} }, wantErr: `buildArguments names "_PROJECT"`},
		{name: "a build argument with a hyphen", mutate: func(p *Placement) { p.BuildArguments = map[string]string{"PROJECT-ID": "projectId"} }, wantErr: `buildArguments names "PROJECT-ID"`},
		{
			name:    "a build argument the pipeline passes itself",
			mutate:  func(p *Placement) { p.BuildArguments = map[string]string{"VERSION": "environment"} },
			wantErr: "buildArguments names VERSION, a build argument the pipeline passes itself (VERSION, COMMIT, JOBS_JOB): give the value another name",
		},
		{name: "the build's job as a build argument", mutate: func(p *Placement) { p.BuildArguments = map[string]string{"JOBS_JOB": "hostname"} }, wantErr: "buildArguments names JOBS_JOB, a build argument the pipeline passes itself"},
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

func TestPlacementMachine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		buildMachine string
		want         Machine
		wantNamed    bool
	}{
		{name: "absent: Cloud Build's default", buildMachine: "", want: Machine{}, wantNamed: false},
		{name: "an 8-vCPU machine", buildMachine: "E2_HIGHCPU_8", want: Machine{Name: "E2_HIGHCPU_8", CPUs: 8}, wantNamed: true},
		{name: "a 32-vCPU machine", buildMachine: "E2_HIGHCPU_32", want: Machine{Name: "E2_HIGHCPU_32", CPUs: 32}, wantNamed: true},
		{name: "a name the placement would have refused", buildMachine: "N1_HIGHCPU_32", want: Machine{}, wantNamed: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &Placement{BuildMachine: tt.buildMachine}
			got, named := p.Machine()
			if named != tt.wantNamed {
				t.Errorf("Machine() named = %v, want %v", named, tt.wantNamed)
			}
			if got != tt.want {
				t.Errorf("Machine() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestCreatePlacement: an application's first placement is written, its directory made,
// and reads back as written; a placement already there is refused and left as it was.
func TestCreatePlacement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		held    string
		wantErr string
	}{
		{name: "no placement yet, nor its directory"},
		{name: "a placement there already", held: "{\"prefix\": \"mine\"}\n", wantErr: "exists: an application's first placement is written once and never overwritten"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file := filepath.Join(t.TempDir(), "infrastructure", "placement.json")
			if tt.held != "" {
				if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(tt.held), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			p := testPlacement(t)
			err := CreatePlacement(file, p)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("CreatePlacement() error = %v, wantErr %q", err, tt.wantErr)
				}
				if got, err := os.ReadFile(file); err != nil || string(got) != tt.held {
					t.Errorf("the placement there changed: %q %v", got, err)
				}

				return
			}
			if err != nil {
				t.Fatalf("CreatePlacement() error = %v", err)
			}
			back, err := ReadPlacement(file)
			if err != nil {
				t.Fatalf("ReadPlacement() error = %v", err)
			}
			if !reflect.DeepEqual(back, p) {
				t.Errorf("read back\n%+v\nwant\n%+v", back, p)
			}
		})
	}
}

func TestPlacementMaxInstanceCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		caps       map[string]int
		env        string
		want       int
		wantCapped bool
	}{
		{name: "absent: no cap anywhere", caps: nil, env: "prd", want: 0, wantCapped: false},
		{name: "a capped environment", caps: map[string]int{"tst": 2, "prd": 10}, env: "prd", want: 10, wantCapped: true},
		{name: "an environment the placement leaves out", caps: map[string]int{"tst": 2}, env: "prd", want: 0, wantCapped: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &Placement{MaxInstances: tt.caps}
			got, capped := p.MaxInstanceCount(tt.env)
			if got != tt.want || capped != tt.wantCapped {
				t.Errorf("MaxInstanceCount(%s) = %d, %v, want %d, %v", tt.env, got, capped, tt.want, tt.wantCapped)
			}
		})
	}
}

func TestPlacementOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		envs          []string
		approvals     []string
		wantApprovals []string
		wantPrevious  map[string]string
	}{
		{name: "three environments, default approvals", envs: []string{"tst", "stg", "prd"}, wantApprovals: []string{"stg", "prd"}, wantPrevious: map[string]string{"tst": "", "stg": "tst", "prd": "stg"}},
		{name: "approvals given", envs: []string{"tst", "prd"}, approvals: []string{"prd"}, wantApprovals: []string{"prd"}, wantPrevious: map[string]string{"tst": "", "prd": "tst"}},
		{name: "no approvals anywhere", envs: []string{"tst", "prd"}, approvals: []string{}, wantApprovals: []string{}, wantPrevious: map[string]string{"tst": "", "prd": "tst"}},
		{name: "one environment", envs: []string{"prd"}, wantApprovals: []string{}, wantPrevious: map[string]string{"prd": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &Placement{Environments: tt.envs, Approvals: tt.approvals}
			if got := p.ApprovalEnvironments(); strings.Join(got, ",") != strings.Join(tt.wantApprovals, ",") {
				t.Errorf("ApprovalEnvironments() = %v, want %v", got, tt.wantApprovals)
			}
			for env, want := range tt.wantPrevious {
				if got := p.Previous(env); got != want {
					t.Errorf("Previous(%s) = %q, want %q", env, got, want)
				}
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
		{name: "a longer name", variable: "APP_STAFF_OIDC_CLIENT_SECRET", want: "staff-oidc-client-secret"},
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

func TestSecretFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		v       Variable
		want    bool
		wantErr string
	}{
		{name: "tagged true", v: Variable{Name: "APP_COOKIE_KEY", SecretTag: "true"}, want: true},
		{name: "tagged true with a plain name", v: Variable{Name: "APP_SMTP_PASS", SecretTag: "true"}, want: true},
		{name: "tagged false with a credential-sounding name", v: Variable{Name: "APP_LICENSE_KEY", SecretTag: "false"}, want: false},
		{name: "untagged plain name", v: Variable{Name: "APP_STAFF_OIDC_CLIENT_ID"}, want: false},
		{name: "an untagged credential-sounding name in a role the stack derives is the stack's", v: Variable{Name: varFirebaseAPIKey, Role: RoleFirebaseAPIKey}, want: false},
		{name: "a derived role tagged false is fine", v: Variable{Name: varFirebaseAPIKey, Role: RoleFirebaseAPIKey, SecretTag: "false"}, want: false},
		{
			name:    "a derived role tagged true is refused",
			v:       Variable{Name: varFirebaseAPIKey, Role: RoleFirebaseAPIKey, SecretTag: "true", File: "pkg/config/data.go", Line: 20, Struct: "dataConfig", Field: "FirebaseAPIKey"},
			wantErr: "pkg/config/data.go:20: APP_FIREBASE_API_KEY (dataConfig.FirebaseAPIKey) is a value the stack derives and sets on the process, not a secret: drop the secret tag",
		},
		{
			name:    "untagged credential-sounding name is refused",
			v:       Variable{Name: "APP_MAIL_API_KEY", File: "pkg/config/data.go", Line: 12, Struct: "dataConfig", Field: "MailAPIKey"},
			wantErr: `pkg/config/data.go:12: APP_MAIL_API_KEY sounds like a credential (its name ends in _KEY) and dataConfig.MailAPIKey carries no secret tag: add secret:"true" to mount it from Secret Manager, or secret:"false" if it is a plain value`,
		},
		{
			name:    "another value is refused",
			v:       Variable{Name: "APP_COOKIE_KEY", SecretTag: "yes", File: "pkg/config/data.go", Line: 3, Struct: "dataConfig", Field: "CookieKey"},
			wantErr: `pkg/config/data.go:3: secret:"yes" on dataConfig.CookieKey: the secret tag takes "true" or "false"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			v := tt.v
			got, err := secretFor(&v)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("secretFor() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil || got != tt.want {
				t.Errorf("secretFor() = %v, %v; want %v", got, err, tt.want)
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
		{name: "credentials", variable: "APP_MAIL_CREDENTIALS", want: true},
		{name: "a token", variable: "APP_TWILIO_AUTH_TOKEN", want: true},
		{name: "a password", variable: "DB_PASSWORD", want: true},
		{name: "a client id", variable: "APP_STAFF_OIDC_CLIENT_ID", want: false},
		{name: "a subject", variable: "APP_MAIL_SUBJECT", want: false},
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

func TestPlacementSeedEnvironments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		envs []string
		seed []string
		want []string
	}{
		{name: "default: none", envs: []string{"tst", "stg", "prd"}},
		{name: "given", envs: []string{"tst", "stg", "prd"}, seed: []string{"tst", "stg"}, want: []string{"tst", "stg"}},
		{name: "one environment, none by default", envs: []string{"prd"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &Placement{Environments: tt.envs, Seed: tt.seed}
			if got := p.SeedEnvironments(); !slices.Equal(got, tt.want) {
				t.Errorf("SeedEnvironments() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJobsJob(t *testing.T) {
	t.Parallel()

	jobs := &Process{Name: jobsProcess, Dir: jobsDir}
	tests := []struct {
		name    string
		model   Model
		wantErr string
	}{
		{
			name:  "a site variable naming an existing job process passes",
			model: Model{Jobs: jobs, Variables: []Variable{{Name: varJobsJob, Role: RoleJobsJob, Level: LevelSite, Struct: "siteConfig", Field: "JobsJob"}}},
		},
		{
			name:  "no variable, no process: nothing to check",
			model: Model{},
		},
		{
			name:  "a job process the site does not name is fine",
			model: Model{Jobs: jobs},
		},
		{
			name:    "the variable without the process is refused",
			model:   Model{Variables: []Variable{{Name: varJobsJob, Role: RoleJobsJob, Level: LevelSite, Struct: "siteConfig", Field: "JobsJob"}}},
			wantErr: "APP_JOBS_JOB (siteConfig.JobsJob) names the job process's Cloud Run job, but there is no main package at cmd/jobs",
		},
		{
			name:    "the variable at the data level is refused",
			model:   Model{Jobs: jobs, Variables: []Variable{{Name: varJobsJob, Role: RoleJobsJob, Level: LevelData, Struct: "dataConfig", Field: "JobsJob"}}},
			wantErr: "APP_JOBS_JOB (dataConfig.JobsJob) is declared at the data level; the site alone runs the job process, so it belongs at the site level",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.model.jobsJob()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("jobsJob() error = %v", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("jobsJob() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestPlacementRestore(t *testing.T) {
	t.Parallel()

	p := Placement{Environments: []string{"tst", "stg", "prd"}, Seed: []string{"stg"}, Projects: map[string]Project{"tst": {ID: "p-tst", Number: "1"}}}
	tests := []struct {
		name     string
		p        Placement
		env      string
		wantKind string
		wantWire bool
	}{
		{name: "the first environment is restored empty", p: p, env: "tst", wantKind: RestoreEmpty, wantWire: true},
		{name: "a seeded environment on production's instance is restored empty, and is not wired", p: p, env: "stg", wantKind: RestoreEmpty},
		{name: "an unseeded environment on production's instance takes production's backup", p: Placement{Environments: []string{"tst", "stg", "prd"}}, env: "stg", wantKind: RestoreBackup},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.p.RestoreKind(tt.env); got != tt.wantKind {
				t.Errorf("RestoreKind(%s) = %q, want %q", tt.env, got, tt.wantKind)
			}
			if _, wired := tt.p.Project(tt.env); wired != tt.wantWire {
				t.Errorf("Project(%s) wired = %t, want %t", tt.env, wired, tt.wantWire)
			}
			if got := strings.Join(tt.p.Restorable(), ","); got != "tst,stg" {
				t.Errorf("Restorable() = %q, want tst,stg", got)
			}
		})
	}
}

// writeFirestoreFiles writes the application root a Firestore read works on: the
// schema migrations directory's sibling with the files given (a nil content writes no
// file), and returns the application.
func writeFirestoreFiles(t *testing.T, files map[string]string) *app.App {
	t.Helper()

	root := t.TempDir()
	dir := filepath.Join(root, "schema", FirestoreDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return &app.App{Root: root}
}

func TestFirestore(t *testing.T) {
	t.Parallel()

	const (
		rules   = "rules_version = '2';\nservice cloud.firestore {}\n"
		indexes = `{"indexes":[{"collectionGroup":"subscriptions","queryScope":"COLLECTION","fields":[{"fieldPath":"resource","order":"ASCENDING"},{"fieldPath":"tags","arrayConfig":"CONTAINS"}]}],` +
			`"fieldOverrides":[{"collectionGroup":"subscriptions","fieldPath":"expiry","ttl":true,"indexes":[{"order":"ASCENDING","queryScope":"COLLECTION"},{"arrayConfig":"CONTAINS","queryScope":"COLLECTION_GROUP"}]},` +
			`{"collectionGroup":"changes","fieldPath":"body","indexes":[]},{"collectionGroup":"changes","fieldPath":"expires","ttl":true}]}`
	)
	database := Variable{Name: varFirestoreDatabase, Role: RoleFirestoreDatabase, Level: LevelData, Struct: "dataConfig", Field: "FirestoreDatabase"}
	key := Variable{Name: varFirebaseAPIKey, Role: RoleFirebaseAPIKey, Level: LevelData, Struct: "dataConfig", Field: "FirebaseAPIKey"}
	project := Variable{Name: varFirestoreProject, Role: RoleFirestoreProject, Level: LevelData, Struct: "dataConfig", Field: "FirestoreProject"}
	siteProject := Variable{Name: varFirestoreProject, Role: RoleFirestoreProject, Level: LevelSite, Struct: "siteConfig", Field: "FirestoreProject"}
	tests := []struct {
		name      string
		variables []Variable
		files     map[string]string
		wantErr   string
		// want is firestoreLine's reading; wantFields the fields' shapes, one per
		// field: ttl, the index count, and whether the file lists indexes.
		want       string
		wantFields []string
	}{
		{
			name:      "no database: nothing read",
			variables: []Variable{{Name: "APP_OTHER", Level: LevelData}},
		},
		{
			name:       "the two files are read",
			variables:  []Variable{database, project, key},
			files:      map[string]string{FirestoreIndexesFile: indexes, FirestoreRulesFile: rules},
			want:       "schema/firestore: 1 index(es) subscriptions_resource_tags; 3 field(s) subscriptions_expiry (ttl), changes_body, changes_expires (ttl); rules_version = '2';",
			wantFields: []string{"ttl, 2 index(es), listed", "no ttl, 0 index(es), listed", "ttl, 0 index(es), unlisted"},
		},
		{
			name:      "a database without its project variable is refused",
			variables: []Variable{database},
			wantErr:   "APP_FIRESTORE_DATABASE (dataConfig.FirestoreDatabase) declares a Firestore database, and the config package declares no variable for its project (GOOGLE_CLOUD_FIRESTORE_PROJECT): the stack sets it to the environment project",
		},
		{
			name:      "a project variable without a database is refused",
			variables: []Variable{project},
			wantErr:   "GOOGLE_CLOUD_FIRESTORE_PROJECT (dataConfig.FirestoreProject) names the project of a Firestore database, and the config package declares no database (APP_FIRESTORE_DATABASE)",
		},
		{
			name:      "the project variable at another level than the database is refused",
			variables: []Variable{database, siteProject},
			wantErr:   "GOOGLE_CLOUD_FIRESTORE_PROJECT (siteConfig.FirestoreProject) is declared at the site level and APP_FIRESTORE_DATABASE (dataConfig.FirestoreDatabase) at the data level; the stack sets both on the processes that construct the database's level, so they belong together",
		},
		{
			name:      "the indexes file is missing",
			variables: []Variable{database, project},
			files:     map[string]string{FirestoreRulesFile: rules},
			wantErr:   "APP_FIRESTORE_DATABASE (dataConfig.FirestoreDatabase) declares a Firestore database, and schema/firestore/firestore.indexes.json is missing: the stack applies the database's composite indexes, time-to-live policies and security rules from the files beside the schema migrations",
		},
		{
			name:      "the rules file is missing",
			variables: []Variable{database, project},
			files:     map[string]string{FirestoreIndexesFile: indexes},
			wantErr:   "APP_FIRESTORE_DATABASE (dataConfig.FirestoreDatabase) declares a Firestore database, and schema/firestore/firestore.rules is missing",
		},
		{
			name:      "the indexes file is not JSON",
			variables: []Variable{database, project},
			files:     map[string]string{FirestoreIndexesFile: "{", FirestoreRulesFile: rules},
			wantErr:   "schema/firestore/firestore.indexes.json",
		},
		{
			name:      "an index without a collection",
			variables: []Variable{database, project},
			files:     map[string]string{FirestoreIndexesFile: `{"indexes":[{"fields":[{"fieldPath":"a","order":"ASCENDING"}]}]}`, FirestoreRulesFile: rules},
			wantErr:   "schema/firestore/firestore.indexes.json: indexes[0] names no collectionGroup",
		},
		{
			name:      "an index without fields",
			variables: []Variable{database, project},
			files:     map[string]string{FirestoreIndexesFile: `{"indexes":[{"collectionGroup":"s"}]}`, FirestoreRulesFile: rules},
			wantErr:   "indexes[0] on s lists no fields",
		},
		{
			name:      "a field with both shapes",
			variables: []Variable{database, project},
			files:     map[string]string{FirestoreIndexesFile: `{"indexes":[{"collectionGroup":"s","fields":[{"fieldPath":"a","order":"ASCENDING","arrayConfig":"CONTAINS"}]}]}`, FirestoreRulesFile: rules},
			wantErr:   "indexes[0].fields[0] carries both an order and an arrayConfig; a field takes one",
		},
		{
			name:      "a field with neither shape",
			variables: []Variable{database, project},
			files:     map[string]string{FirestoreIndexesFile: `{"indexes":[{"collectionGroup":"s","fields":[{"fieldPath":"a"}]}]}`, FirestoreRulesFile: rules},
			wantErr:   "indexes[0].fields[0] carries neither an order nor an arrayConfig",
		},
		{
			name:      "an unknown order",
			variables: []Variable{database, project},
			files:     map[string]string{FirestoreIndexesFile: `{"indexes":[{"collectionGroup":"s","fields":[{"fieldPath":"a","order":"UP"}]}]}`, FirestoreRulesFile: rules},
			wantErr:   `indexes[0].fields[0]: order "UP" is not one of ASCENDING, DESCENDING`,
		},
		{
			name:      "an unknown query scope",
			variables: []Variable{database, project},
			files:     map[string]string{FirestoreIndexesFile: `{"indexes":[{"collectionGroup":"s","queryScope":"ALL","fields":[{"fieldPath":"a","order":"ASCENDING"}]}]}`, FirestoreRulesFile: rules},
			wantErr:   `indexes[0]: queryScope "ALL" is not one of COLLECTION, COLLECTION_GROUP, COLLECTION_RECURSIVE`,
		},
		{
			name:      "a field override without a path",
			variables: []Variable{database, project},
			files:     map[string]string{FirestoreIndexesFile: `{"fieldOverrides":[{"collectionGroup":"s","ttl":true}]}`, FirestoreRulesFile: rules},
			wantErr:   "fieldOverrides[0] names no collectionGroup or no fieldPath",
		},
		{
			name:      "the key without the database is refused",
			variables: []Variable{key},
			wantErr:   "APP_FIREBASE_API_KEY (dataConfig.FirebaseAPIKey) names the web API key of a Firestore database, and the config package declares no database (APP_FIRESTORE_DATABASE)",
		},
		{
			name:      "the key at another level than the database is refused",
			variables: []Variable{database, {Name: varFirebaseAPIKey, Role: RoleFirebaseAPIKey, Level: LevelSite, Struct: "siteConfig", Field: "FirebaseAPIKey"}},
			files:     map[string]string{FirestoreIndexesFile: indexes, FirestoreRulesFile: rules},
			wantErr:   "APP_FIREBASE_API_KEY (siteConfig.FirebaseAPIKey) is declared at the site level and APP_FIRESTORE_DATABASE (dataConfig.FirestoreDatabase) at the data level; the stack sets both on the processes that construct the database's level, so they belong together",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := &Model{Variables: tt.variables, Schema: Schema{MigrationsDir: "schema/migrations"}}
			err := m.firestore(writeFirestoreFiles(t, tt.files))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("firestore() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("firestore() error = %v", err)
			}
			if got := firestoreLine(m.Firestore); got != tt.want {
				t.Errorf("Firestore = %q, want %q", got, tt.want)
			}
			if m.Firestore == nil {
				return
			}
			var fields []string
			for _, f := range m.Firestore.Fields {
				ttl, listed := "no ttl", "unlisted"
				if f.TTL {
					ttl = "ttl"
				}
				if f.HasIndexes {
					listed = "listed"
				}
				fields = append(fields, fmt.Sprintf("%s, %d index(es), %s", ttl, len(f.Indexes), listed))
			}
			if !slices.Equal(fields, tt.wantFields) {
				t.Errorf("Fields = %v, want %v", fields, tt.wantFields)
			}
		})
	}
}

// TestBuildArguments proves the build arguments that name a value the stack makes only
// for code declaring it: the Firebase web API key needs APP_FIREBASE_API_KEY and the
// Firestore database APP_FIRESTORE_DATABASE, and the values every stack has need nothing.
func TestBuildArguments(t *testing.T) {
	t.Parallel()

	database := Variable{Name: varFirestoreDatabase, Role: RoleFirestoreDatabase, Level: LevelData}
	key := Variable{Name: varFirebaseAPIKey, Role: RoleFirebaseAPIKey, Level: LevelData}
	tests := []struct {
		name      string
		variables []Variable
		args      map[string]string
		wantErr   string
	}{
		{name: "none declared", variables: nil},
		{name: "the key and the database where the code declares both", variables: []Variable{database, key}, args: map[string]string{"FIREBASE_API_KEY": BuildValueFirebaseAPIKey, "FIRESTORE_DB": BuildValueFirestoreDatabase}},
		{name: "the project, the environment and the hostname in any stack", args: map[string]string{"PROJECT_ID": BuildValueProjectID, "ENVIRONMENT": BuildValueEnvironment, "HOSTNAME": BuildValueHostname}},
		{
			name:      "the key where the code declares none",
			variables: []Variable{database},
			args:      map[string]string{"FIREBASE_API_KEY": BuildValueFirebaseAPIKey},
			wantErr:   "buildArguments.FIREBASE_API_KEY names firebaseApiKey, the Firebase web API key the stack makes only when the config package declares APP_FIREBASE_API_KEY, which it does not",
		},
		{
			name:    "the database where the code declares none",
			args:    map[string]string{"FIRESTORE_DB": BuildValueFirestoreDatabase},
			wantErr: "buildArguments.FIRESTORE_DB names firestoreDatabase, the Firestore database the stack makes only when the config package declares APP_FIRESTORE_DATABASE, which it does not",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := &Model{Variables: tt.variables, Placement: &Placement{BuildArguments: tt.args}}
			err := m.buildArguments()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("buildArguments() error = %v, want none", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("buildArguments() error = %v, wantErr %q", err, tt.wantErr)
			}
		})
	}
}

// TestBuildArgumentSubstitution pins the trigger substitution a build argument rides on,
// which the stack renders and the image build reads by the prefix.
func TestBuildArgumentSubstitution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		arg  string
		want string
	}{
		{name: "the Firebase key's argument", arg: "FIREBASE_API_KEY", want: "_BUILD_ARG_FIREBASE_API_KEY"},
		{name: "a one-letter argument", arg: "X", want: "_BUILD_ARG_X"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := BuildArgumentSubstitution(tt.arg); got != tt.want || !strings.HasPrefix(got, BuildArgumentPrefix) {
				t.Errorf("BuildArgumentSubstitution(%q) = %q, want %q", tt.arg, got, tt.want)
			}
		})
	}
}
