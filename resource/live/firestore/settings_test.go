package firestore_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/resource/internal/declarationtest"
	livefirestore "github.com/cccteam/ccc/resource/live/firestore"
	"github.com/cccteam/ccc/resource/live/firestore/declaration"
)

// TestOpenProject holds the project Open opens the database in to the configurations a
// process can start with: a deployment names the database and its project; a database
// named without its project is refused, naming both variables, rather than opened under
// the driver's emulator project; the emulator opens under the driver's own project id
// when the settings name none; and with neither a database nor the emulator the live
// service has nothing to open. Against the emulator the token route answers the project
// the service opened under, which is how the browser connects under the same one.
func TestOpenProject(t *testing.T) {
	t.Parallel()

	const envProject = "environment-project"
	tests := []struct {
		name     string
		settings livefirestore.Settings
		// want is the project the service opens under; empty with wantErr.
		want    string
		wantErr string
	}{
		{
			name:     "the emulator opens under the driver's own project when none is named",
			settings: livefirestore.Settings{DatabaseID: "live"},
			want:     livefirestore.EmulatorProject,
		},
		{
			name:     "the emulator takes the project named",
			settings: livefirestore.Settings{ProjectID: envProject, DatabaseID: "live"},
			want:     envProject,
		},
		{
			name:     "a database named without its project is refused",
			settings: livefirestore.Settings{DatabaseID: "app"},
			wantErr:  "APP_FIRESTORE_DATABASE names a Firestore database and GOOGLE_CLOUD_FIRESTORE_PROJECT names no project for it",
		},
		{
			name:     "neither a database nor the emulator is refused",
			settings: livefirestore.Settings{ProjectID: envProject},
			wantErr:  "the live service needs a Firestore database",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			settings := tt.settings
			if tt.want != "" {
				settings.EmulatorHost = firestoreEmulator(t)
			}
			svc, err := livefirestore.Open(t.Context(), settings)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Open() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			defer func() {
				if err := svc.Close(); err != nil {
					t.Errorf("Close() error = %v", err)
				}
			}()
			payload, err := svc.Token(t.Context(), "crew|alice")
			if err != nil {
				t.Fatalf("Token() error = %v", err)
			}
			if payload.Project != tt.want || payload.Database != "live" || payload.Emulator != settings.EmulatorHost || payload.Token != "" {
				t.Errorf("Token() = %+v, want project %q, database live, the emulator host and no token", payload, tt.want)
			}
		})
	}
}

// TestBrowserOrigins names the origins the content security policy admits for the
// change feed: the emulator in development, Firebase's hosts against a database, none
// while neither is configured.
func TestBrowserOrigins(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings livefirestore.Settings
		want     []string
	}{
		{name: "the emulator", settings: livefirestore.Settings{EmulatorHost: "127.0.0.1:9024", DatabaseID: "app"}, want: []string{"http://127.0.0.1:9024"}},
		{name: "a database", settings: livefirestore.Settings{ProjectID: "p", DatabaseID: "app"}, want: []string{"https://firestore.googleapis.com", "https://identitytoolkit.googleapis.com", "https://securetoken.googleapis.com"}},
		{name: "neither", settings: livefirestore.Settings{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, tt.settings.BrowserOrigins()); diff != "" {
				t.Errorf("BrowserOrigins() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSettingsDeclaration holds the declaration the declaration package publishes to the
// struct, as the cloud driver's test does.
func TestSettingsDeclaration(t *testing.T) {
	t.Parallel()

	declarationtest.Hold(t, reflect.TypeFor[livefirestore.Settings](), declaration.Settings())
}
