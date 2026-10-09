package spanner_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/database/spanner"
	"github.com/cccteam/ccc/resource/database/spanner/declaration"
	"github.com/cccteam/ccc/resource/filestore"
	"github.com/cccteam/ccc/resource/internal/declarationtest"
	"github.com/google/go-cmp/cmp"
	"github.com/sethvargo/go-envconfig"
)

// TestSettings reads the settings from an environment: the three variables are required,
// each named in the refusal when it is missing, and the database path is composed from
// them.
func TestSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		env      map[string]string
		want     spanner.Settings
		wantPath string
		wantErr  string
	}{
		{
			name:     "the three variables name the database",
			env:      map[string]string{"GOOGLE_CLOUD_SPANNER_PROJECT": "harbor-dev", "GOOGLE_CLOUD_SPANNER_INSTANCE_ID": "harbor", "GOOGLE_CLOUD_SPANNER_DATABASE_NAME": "harbor"},
			want:     spanner.Settings{ProjectID: "harbor-dev", InstanceID: "harbor", DatabaseName: "harbor"},
			wantPath: "projects/harbor-dev/instances/harbor/databases/harbor",
		},
		{
			name:    "the project is required",
			env:     map[string]string{"GOOGLE_CLOUD_SPANNER_INSTANCE_ID": "harbor", "GOOGLE_CLOUD_SPANNER_DATABASE_NAME": "harbor"},
			wantErr: "GOOGLE_CLOUD_SPANNER_PROJECT",
		},
		{
			name:    "the instance is required",
			env:     map[string]string{"GOOGLE_CLOUD_SPANNER_PROJECT": "harbor-dev", "GOOGLE_CLOUD_SPANNER_DATABASE_NAME": "harbor"},
			wantErr: "GOOGLE_CLOUD_SPANNER_INSTANCE_ID",
		},
		{
			name:    "the database is required",
			env:     map[string]string{"GOOGLE_CLOUD_SPANNER_PROJECT": "harbor-dev", "GOOGLE_CLOUD_SPANNER_INSTANCE_ID": "harbor"},
			wantErr: "GOOGLE_CLOUD_SPANNER_DATABASE_NAME",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got spanner.Settings
			err := envconfig.ProcessWith(t.Context(), &envconfig.Config{Target: &got, Lookuper: envconfig.MapLookuper(tt.env)})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("envconfig.ProcessWith() error = %v, want one naming %s", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("envconfig.ProcessWith() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("settings mismatch (-want +got):\n%s", diff)
			}
			if got.DatabasePath() != tt.wantPath {
				t.Errorf("DatabasePath() = %q, want %q", got.DatabasePath(), tt.wantPath)
			}
		})
	}
}

// TestOpen opens the driver against the emulator: the resource client is the Spanner
// client's, the file stores the options wire are on it, a store wired twice is refused as
// the resource client refuses it, and Close releases the client.
func TestOpen(t *testing.T) {
	t.Parallel()

	db, err := spannerEmulator(t).CreateDatabase(t.Context(), "driver")
	if err != nil {
		t.Fatalf("CreateDatabase() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("SpannerDB.Close() error = %v", err)
		}
	})
	settings := settingsOf(t, db.DatabaseName())

	tests := []struct {
		name      string
		opts      []resource.ClientOption
		wantStore bool
		wantPanic string
	}{
		{name: "no store"},
		{name: "the default store wired", opts: []resource.ClientOption{resource.WithFileStore(filestore.NewMem())}, wantStore: true},
		{name: "a store wired twice is refused", opts: []resource.ClientOption{resource.WithFileStore(filestore.NewMem()), resource.WithFileStore(filestore.NewMem())}, wantPanic: "wired twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.wantPanic != "" {
				defer func() {
					if r := recover(); r == nil || !strings.Contains(fmtPanic(r), tt.wantPanic) {
						t.Errorf("Open() panic = %v, want one containing %q", r, tt.wantPanic)
					}
				}()
			}
			d, err := spanner.Open(t.Context(), settings, tt.opts...)
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			defer d.Close()
			if d.ResourceClient.DBType() != resource.SpannerDBType {
				t.Errorf("ResourceClient.DBType() = %v, want Spanner", d.ResourceClient.DBType())
			}
			if got := d.ResourceClient.FileStore(resource.DefaultStore) != nil; got != tt.wantStore {
				t.Errorf("ResourceClient.FileStore(default) wired = %v, want %v", got, tt.wantStore)
			}
			// The Spanner client answers the database it opened, the emulator's.
			if got := d.SpannerClient.DatabaseName(); got != settings.DatabasePath() {
				t.Errorf("SpannerClient.DatabaseName() = %q, want %q", got, settings.DatabasePath())
			}
		})
	}
}

// settingsOf reads the settings back from a database's resource name
// (projects/<project>/instances/<instance>/databases/<database>), the emulator's.
func settingsOf(t *testing.T, databasePath string) spanner.Settings {
	t.Helper()

	parts := strings.Split(databasePath, "/")
	if len(parts) != 6 || parts[0] != "projects" || parts[2] != "instances" || parts[4] != "databases" {
		t.Fatalf("%q is not a database's resource name", databasePath)
	}

	return spanner.Settings{ProjectID: parts[1], InstanceID: parts[3], DatabaseName: parts[5]}
}

// fmtPanic renders a recovered value for a message.
func fmtPanic(r any) string {
	if err, ok := r.(error); ok {
		return err.Error()
	}
	if s, ok := r.(string); ok {
		return s
	}

	return "unexpected panic value"
}

// TestSettingsDeclaration holds the declaration the declaration package publishes to the
// struct, as the cloud driver's test does.
func TestSettingsDeclaration(t *testing.T) {
	t.Parallel()

	declarationtest.Hold(t, reflect.TypeFor[spanner.Settings](), declaration.Settings())
}
