package generation

import (
	"net/http"
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
		name       string
		outlets    []routerOutlet
		scheduled  []*scheduledRoute
		fileRoutes []resource.FileRoute
		want       string
		wantRead   resource.ReleaseFile
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
		{
			name:    "file routes, an upload and a stored file",
			outlets: []routerOutlet{{name: "default", prefix: "api", servesSessions: true, auth: crew}},
			fileRoutes: []resource.FileRoute{
				{Kind: resource.FileRouteUpload, Method: "POST", Path: "/api/attach-photo", Source: "AttachPhoto"},
				{Kind: resource.FileRouteStored, Method: "GET", Path: "/api/photos/{id}/file", Source: "Photo.Key"},
			},
			want: `{
  "outlets": {
    "default": {
      "oldestAnswered": ""
    }
  },
  "fileRoutes": [
    {
      "kind": "upload",
      "method": "POST",
      "path": "/api/attach-photo",
      "source": "AttachPhoto"
    },
    {
      "kind": "file",
      "method": "GET",
      "path": "/api/photos/{id}/file",
      "source": "Photo.Key"
    }
  ]
}
`,
			wantRead: resource.ReleaseFile{
				Outlets: map[string]resource.ReleaseOutlet{"default": {}},
				FileRoutes: []resource.FileRoute{
					{Kind: resource.FileRouteUpload, Method: "POST", Path: "/api/attach-photo", Source: "AttachPhoto"},
					{Kind: resource.FileRouteStored, Method: "GET", Path: "/api/photos/{id}/file", Source: "Photo.Key"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := renderReleaseFile(releaseFileOf(tt.outlets, tt.scheduled, tt.fileRoutes))
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

// Test_fileRoutesOf lists the routes that carry a file, in path order: each served
// @upload method's route on every outlet it is on, under the outlet's prefix, and each
// outlet's stored-file routes, each naming its declaration; a JSON method, a suppressed
// upload and an upload on another outlet are left out.
func Test_fileRoutesOf(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "rpcform"))
	upload := &rpcUpload{MaxBytes: 1}
	outlets := []routerOutlet{{name: "default", prefix: "api"}, {name: "portal", prefix: "portal/api"}}
	tests := []struct {
		name       string
		methods    []*rpcMethodInfo
		fileRoutes map[string][]*generatedRoute
		want       []resource.FileRoute
	}{
		{name: "nothing declared"},
		{
			name: "an upload on the default outlet, one on the portal, a JSON method and a suppressed upload",
			methods: []*rpcMethodInfo{
				{Struct: structs["UploadForm"], Form: rpcFormTxn, Upload: upload},
				{Struct: structs["UploadAnswers"], Form: rpcFormTxn, Upload: upload, outletMembership: outletMembership{OutletNames: []string{"portal"}}},
				{Struct: structs["UploadUndeclared"], Form: rpcFormTxn},
				{Struct: structs["AnswersDuplicate"], Form: rpcFormTxn, Upload: upload, SuppressHandler: true},
			},
			want: []resource.FileRoute{
				{Kind: resource.FileRouteUpload, Method: http.MethodPost, Path: "/api/upload-form", Source: "UploadForm"},
				{Kind: resource.FileRouteUpload, Method: http.MethodPost, Path: "/portal/api/upload-answers", Source: "UploadAnswers"},
			},
		},
		{
			name: "stored files by outlet, sorted with the uploads by path",
			methods: []*rpcMethodInfo{
				{Struct: structs["UploadForm"], Form: rpcFormTxn, Upload: upload, outletMembership: outletMembership{OutletNames: []string{"default", "portal"}}},
			},
			fileRoutes: map[string][]*generatedRoute{
				"default": {{Method: http.MethodGet, Path: "/api/photos/{id}/file", Source: "Photo.Key"}},
				"portal":  {{Method: http.MethodGet, Path: "/portal/api/photos/{id}/file", Source: "Photo.Key"}, {Method: http.MethodGet, Path: "/portal/api/manifests/{id}/content", Source: "Manifest"}},
			},
			want: []resource.FileRoute{
				{Kind: resource.FileRouteStored, Method: http.MethodGet, Path: "/api/photos/{id}/file", Source: "Photo.Key"},
				{Kind: resource.FileRouteUpload, Method: http.MethodPost, Path: "/api/upload-form", Source: "UploadForm"},
				{Kind: resource.FileRouteStored, Method: http.MethodGet, Path: "/portal/api/manifests/{id}/content", Source: "Manifest"},
				{Kind: resource.FileRouteStored, Method: http.MethodGet, Path: "/portal/api/photos/{id}/file", Source: "Photo.Key"},
				{Kind: resource.FileRouteUpload, Method: http.MethodPost, Path: "/portal/api/upload-form", Source: "UploadForm"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := &resourceGenerator{client: &client{}}
			r.rpcMethods = tt.methods
			got := r.fileRoutesOf(outlets, tt.fileRoutes)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("fileRoutesOf() mismatch (-want +got):\n%s", diff)
			}
		})
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
