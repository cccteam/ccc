package derive

import (
	"testing"
)

// TestFrameworkSetting finds each settings struct the framework declares by the import
// path and the struct name an application embeds, and nothing else.
func TestFrameworkSetting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		path     string
		typeName string
		want     bool
	}{
		{name: "the cloud driver", path: "github.com/cccteam/ccc/cloud/gcp", typeName: "Settings", want: true},
		{name: "the Spanner database driver", path: "github.com/cccteam/ccc/resource/database/spanner", typeName: "Settings", want: true},
		{name: "the PostgreSQL database driver", path: "github.com/cccteam/ccc/resource/database/postgres", typeName: "Settings", want: true},
		{name: "the Cloud Run job driver", path: "github.com/cccteam/ccc/resource/jobs/cloudrun", typeName: "Settings", want: true},
		{name: "the Firestore live driver", path: "github.com/cccteam/ccc/resource/live/firestore", typeName: "Settings", want: true},
		{name: "a driver's other type", path: "github.com/cccteam/ccc/resource/database/spanner", typeName: "Driver", want: false},
		{name: "a package the framework does not declare", path: "example.com/other/gcp", typeName: "Settings", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d, got := frameworkSetting(tt.path, tt.typeName)
			if got != tt.want {
				t.Fatalf("frameworkSetting(%q, %q) found = %v, want %v", tt.path, tt.typeName, got, tt.want)
			}
			if got && len(d.Fields) == 0 {
				t.Errorf("%s.%s declares no field", tt.path, tt.typeName)
			}
		})
	}
}
