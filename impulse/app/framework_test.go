package app

import (
	"testing"
)

// TestFrameworkSettings holds the list to the settings structs the framework declares:
// each driver's, by the import path and the struct name an application embeds.
func TestFrameworkSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{name: "the cloud driver", path: "github.com/cccteam/ccc/cloud/gcp"},
		{name: "the Spanner database driver", path: "github.com/cccteam/ccc/resource/database/spanner"},
		{name: "the PostgreSQL database driver", path: "github.com/cccteam/ccc/resource/database/postgres"},
		{name: "the Cloud Run job driver", path: "github.com/cccteam/ccc/resource/jobs/cloudrun"},
		{name: "the Firestore live driver", path: "github.com/cccteam/ccc/resource/live/firestore"},
	}
	if got, want := len(frameworkSettings), len(tests); got != want {
		t.Errorf("frameworkSettings has %d declarations, want %d", got, want)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, d := range frameworkSettings {
				if d.Path == tt.path && d.Name == "Settings" {
					if len(d.Fields) == 0 {
						t.Errorf("%s.Settings declares no field", tt.path)
					}

					return
				}
			}
			t.Errorf("frameworkSettings does not declare %s.Settings", tt.path)
		})
	}
}
