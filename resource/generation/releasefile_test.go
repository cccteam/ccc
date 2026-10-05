package generation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cccteam/ccc/resource"
	"github.com/google/go-cmp/cmp"
)

// Test_renderReleaseFile pins the release file's bytes for the shapes an outlet takes
// (a release, this release, no declaration and a machine outlet) and for the scheduled
// routes, and that what is written reads back through the reader a deploy uses.
func Test_renderReleaseFile(t *testing.T) {
	t.Parallel()

	crew := &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/crew", flavor: Password}

	tests := []struct {
		name      string
		outlets   []routerOutlet
		scheduled []*scheduledRoute
		want      string
		wantRead  resource.ReleaseFile
	}{
		{
			name: "a release, a machine outlet, no declaration and this release",
			outlets: []routerOutlet{
				{name: "default", prefix: "api", servesSessions: true, auth: crew, oldestAnswered: "1.5.0", declaredOldest: true},
				{name: "droids", prefix: "droids", apiKey: true},
				{name: "kiosk", prefix: "kiosk/api", servesSessions: true, auth: crew},
				{name: "portal", prefix: "portal/api", servesSessions: true, auth: crew, oldestAnswered: ThisRelease, declaredOldest: true},
			},
			want: `{
  "outlets": {
    "default": {
      "oldestAnswered": "1.5.0"
    },
    "droids": {
      "kind": "api-key"
    },
    "kiosk": {
      "oldestAnswered": ""
    },
    "portal": {
      "oldestAnswered": "this"
    }
  }
}
`,
			wantRead: resource.ReleaseFile{Outlets: map[string]resource.ReleaseOutlet{
				"default": {OldestAnswered: "1.5.0"},
				"droids":  {APIKey: true},
				"kiosk":   {},
				"portal":  {OldestAnswered: resource.ThisRelease},
			}},
		},
		{
			name:    "a machine outlet alone",
			outlets: []routerOutlet{{name: "default", prefix: "api", apiKey: true}},
			want: `{
  "outlets": {
    "default": {
      "kind": "api-key"
    }
  }
}
`,
			wantRead: resource.ReleaseFile{Outlets: map[string]resource.ReleaseOutlet{"default": {APIKey: true}}},
		},
		{
			name: "the outlets render in name order whatever the declaration order",
			outlets: []routerOutlet{
				{name: "default", prefix: "api", servesSessions: true, auth: crew},
				{name: "beacons", prefix: "beacons", apiKey: true},
			},
			want: `{
  "outlets": {
    "beacons": {
      "kind": "api-key"
    },
    "default": {
      "oldestAnswered": ""
    }
  }
}
`,
			wantRead: resource.ReleaseFile{Outlets: map[string]resource.ReleaseOutlet{"beacons": {APIKey: true}, "default": {}}},
		},
		{
			name:    "the scheduled routes render in path order with their schedules",
			outlets: []routerOutlet{{name: "default", prefix: "api", servesSessions: true, auth: crew}},
			scheduled: []*scheduledRoute{
				{Path: "/_scheduled/send-digest", HandlerFunc: "SendDigest", Cron: "0 6 * * MON-FRI", Zone: "UTC"},
				{Path: "/_scheduled/prune-droid-reports", HandlerFunc: "PruneDroidReports", Cron: "30 3 * * *", Zone: "America/Denver"},
			},
			want: `{
  "outlets": {
    "default": {
      "oldestAnswered": ""
    }
  },
  "scheduled": [
    {
      "path": "/_scheduled/prune-droid-reports",
      "schedule": "30 3 * * *",
      "timeZone": "America/Denver"
    },
    {
      "path": "/_scheduled/send-digest",
      "schedule": "0 6 * * MON-FRI",
      "timeZone": "UTC"
    }
  ]
}
`,
			wantRead: resource.ReleaseFile{
				Outlets: map[string]resource.ReleaseOutlet{"default": {}},
				Scheduled: []resource.ScheduledRoute{
					{Path: "/_scheduled/prune-droid-reports", Schedule: "30 3 * * *", TimeZone: "America/Denver"},
					{Path: "/_scheduled/send-digest", Schedule: "0 6 * * MON-FRI", TimeZone: "UTC"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := renderReleaseFile(releaseFileOf(tt.outlets, tt.scheduled))
			if err != nil {
				t.Fatalf("renderReleaseFile() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, string(got)); diff != "" {
				t.Errorf("renderReleaseFile() mismatch (-want +got):\n%s", diff)
			}

			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, resource.ReleaseFileName), got, 0o600); err != nil {
				t.Fatalf("os.WriteFile() error = %v", err)
			}
			read, err := resource.ReadReleaseFile(dir)
			if err != nil {
				t.Fatalf("resource.ReadReleaseFile() error = %v", err)
			}
			if diff := cmp.Diff(tt.wantRead, read); diff != "" {
				t.Errorf("resource.ReadReleaseFile() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_releaseFileName pins that the reserved stem names the file the reader opens: the
// generator writes resource.ReleaseFileName, and the stem the table reserves is its own.
func Test_releaseFileName(t *testing.T) {
	t.Parallel()

	if got := generatedFileName(releaseOutputName, "json"); got != resource.ReleaseFileName {
		t.Errorf("generatedFileName(%q, json) = %q, want resource.ReleaseFileName %q", releaseOutputName, got, resource.ReleaseFileName)
	}
	if reservedOutputStems[releaseOutputName].file != resource.ReleaseFileName {
		t.Errorf("reservedOutputStems[%q].file = %q, want %q", releaseOutputName, reservedOutputStems[releaseOutputName].file, resource.ReleaseFileName)
	}
}

// Test_runServedRouterGeneration_writesReleaseFile pins that the release file is written
// beside the served router and its test, through the run's output record so the sweep
// keeps it, and that what lands on disk reads back through resource.ReadReleaseFile as
// the outlets were declared.
func Test_runServedRouterGeneration_writesReleaseFile(t *testing.T) {
	t.Parallel()

	crew := &outletAuth{importPath: "example.com/acme/beacon/pkg/auth/crew", flavor: Password}

	tests := []struct {
		name    string
		outlets []routerOutlet
		want    resource.ReleaseFile
	}{
		{
			name: "a release, a machine outlet, no declaration and this release",
			outlets: []routerOutlet{
				{name: "default", prefix: "api", servesSessions: true, auth: crew, oldestAnswered: "1.5.0", declaredOldest: true},
				{name: "droids", prefix: "droids", apiKey: true},
				{name: "kiosk", prefix: "kiosk/api", servesSessions: true, auth: crew},
				{name: "portal", prefix: "portal/api", servesSessions: true, auth: crew, oldestAnswered: ThisRelease, declaredOldest: true},
			},
			want: resource.ReleaseFile{Outlets: map[string]resource.ReleaseOutlet{
				"default": {OldestAnswered: "1.5.0"},
				"droids":  {APIKey: true},
				"kiosk":   {},
				"portal":  {OldestAnswered: resource.ThisRelease},
			}},
		},
		{
			name:    "one session outlet with no declaration",
			outlets: []routerOutlet{{name: "default", prefix: "api", servesSessions: true, auth: crew}},
			want:    resource.ReleaseFile{Outlets: map[string]resource.ReleaseOutlet{"default": {}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// The router package is named after its directory, as an application's is.
			dir := filepath.Join(t.TempDir(), "router")
			r := &resourceGenerator{client: &client{}}
			r.router = packageDir(dir)
			r.resource = packageDir("pkg/resources")
			if err := r.runServedRouterGeneration(tt.outlets, nil, nil); err != nil {
				t.Fatalf("runServedRouterGeneration() error = %v", err)
			}

			// The sweep over the router directory keeps what the run wrote.
			r.output.registerOutput(dir, prefix)
			if err := r.output.removeStaleOutput(); err != nil {
				t.Fatalf("removeStaleOutput() error = %v", err)
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("os.ReadDir() error = %v", err)
			}
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				names = append(names, e.Name())
			}
			wantNames := []string{resource.ReleaseFileName, generatedGoFileName(servedRouterOutputName), generatedGoFileName(servedRouterTestOutputName)}
			if diff := cmp.Diff(wantNames, names); diff != "" {
				t.Errorf("router directory mismatch (-want +got):\n%s", diff)
			}

			got, err := resource.ReadReleaseFile(dir)
			if err != nil {
				t.Fatalf("resource.ReadReleaseFile() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("resource.ReadReleaseFile() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
