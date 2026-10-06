package config

import (
	"strings"
	"testing"
)

// TestFirestoreProject holds the project the live service opens its database in to the
// configurations a process can start with: a deployment names the database and its
// project; a database named without its project is refused, naming both variables,
// rather than opened in the Spanner project, which where environments share a Spanner
// instance is the shared instance's; the emulator takes the Spanner project when no
// project is named; and with neither a database nor the emulator the live service has
// nothing to open.
func TestFirestoreProject(t *testing.T) {
	t.Parallel()

	const (
		spannerProject = "spanner-project"
		envProject     = "environment-project"
		emulator       = "127.0.0.1:9024"
	)
	tests := []struct {
		name     string
		settings FirestoreSettings
		want     string
		wantErr  string
	}{
		{
			name:     "a deployment names the database and its project",
			settings: FirestoreSettings{ProjectID: envProject, DatabaseID: "app"},
			want:     envProject,
		},
		{
			name:     "a database named without its project is refused",
			settings: FirestoreSettings{DatabaseID: "app"},
			wantErr:  "APP_FIRESTORE_DATABASE names a Firestore database and GOOGLE_CLOUD_FIRESTORE_PROJECT names no project for it",
		},
		{
			name:     "the emulator takes the Spanner project when no project is named",
			settings: FirestoreSettings{EmulatorHost: emulator},
			want:     spannerProject,
		},
		{
			name:     "the emulator takes the project named",
			settings: FirestoreSettings{ProjectID: envProject, EmulatorHost: emulator},
			want:     envProject,
		},
		{
			name:     "neither a database nor the emulator is refused",
			settings: FirestoreSettings{ProjectID: envProject},
			wantErr:  "no Firestore database and no emulator is configured",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.settings.Project(spannerProject)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Project() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Project() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Project() = %q, want %q", got, tt.want)
			}
		})
	}
}
