package derive

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

func TestReadReleaseFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// content is the file's content; empty writes no file.
		content string
		want    *ReleaseFile
		// wantMissing says the error wraps os.ErrNotExist.
		wantMissing bool
		wantErr     string
	}{
		{
			name:    "an outlet with a release, one with the server's own, one answering every release and a machine outlet",
			content: `{"outlets": {"default": {"oldestAnswered": "1.5.0"}, "portal": {"oldestAnswered": "this"}, "kiosk": {"oldestAnswered": ""}, "api": {"kind": "api-key"}}}`,
			want:    &ReleaseFile{Outlets: map[string]ReleaseOutlet{"default": {OldestAnswered: "1.5.0"}, "portal": {OldestAnswered: "this"}, "kiosk": {}, "api": {Kind: "api-key"}}},
		},
		{name: "a missing file", wantMissing: true, wantErr: "zz_gen_release.json"},
		{name: "a malformed file", content: `{"outlets": [`, wantErr: "is not the shape the generator writes"},
		{name: "no outlet", content: `{"outlets": {}}`, wantErr: "names no outlet; a generated router has at least its default outlet"},
		{name: "a kind that is not api-key", content: `{"outlets": {"default": {"kind": "session"}}}`, wantErr: `outlet default has kind "session", and the one kind an entry declares is "api-key"`},
		{name: "an oldest answered that is not a release", content: `{"outlets": {"default": {"oldestAnswered": "v1.5.0"}}}`, wantErr: `outlet default answers "v1.5.0" at the oldest, which is not a release (1.5.0), "this" or ""`},
		{
			name:    "scheduled routes, a schedule with names and a zone with a region",
			content: `{"outlets": {"default": {}}, "scheduled": [{"path": "/_scheduled/prune-logs", "schedule": "30 3 * * *", "timeZone": "UTC"}, {"path": "/_scheduled/send-digest", "schedule": "0 7 * JAN-MAR MON-FRI", "timeZone": "America/Argentina/Buenos_Aires"}]}`,
			want: &ReleaseFile{
				Outlets: map[string]ReleaseOutlet{"default": {}},
				Scheduled: []ScheduledRoute{
					{Path: "/_scheduled/prune-logs", Schedule: "30 3 * * *", TimeZone: "UTC"},
					{Path: "/_scheduled/send-digest", Schedule: "0 7 * JAN-MAR MON-FRI", TimeZone: "America/Argentina/Buenos_Aires"},
				},
			},
		},
		{name: "a scheduled route outside the prefix", content: `{"outlets": {"default": {}}, "scheduled": [{"path": "/api/prune-logs", "schedule": "30 3 * * *", "timeZone": "UTC"}]}`, wantErr: `the scheduled route "/api/prune-logs" is not /_scheduled/<method in kebab case>`},
		{name: "a scheduled route whose name is not in kebab case", content: `{"outlets": {"default": {}}, "scheduled": [{"path": "/_scheduled/PruneLogs", "schedule": "30 3 * * *", "timeZone": "UTC"}]}`, wantErr: `the scheduled route "/_scheduled/PruneLogs" is not /_scheduled/<method in kebab case>`},
		{name: "a scheduled route deeper than one segment", content: `{"outlets": {"default": {}}, "scheduled": [{"path": "/_scheduled/logs/prune", "schedule": "30 3 * * *", "timeZone": "UTC"}]}`, wantErr: `the scheduled route "/_scheduled/logs/prune" is not /_scheduled/<method in kebab case>`},
		{name: "a scheduled route listed twice", content: `{"outlets": {"default": {}}, "scheduled": [{"path": "/_scheduled/prune-logs", "schedule": "30 3 * * *", "timeZone": "UTC"}, {"path": "/_scheduled/prune-logs", "schedule": "0 4 * * *", "timeZone": "UTC"}]}`, wantErr: "the scheduled route /_scheduled/prune-logs is listed twice"},
		{name: "a schedule of four fields", content: `{"outlets": {"default": {}}, "scheduled": [{"path": "/_scheduled/prune-logs", "schedule": "30 3 * *", "timeZone": "UTC"}]}`, wantErr: `the scheduled route /_scheduled/prune-logs has the schedule "30 3 * *", which is not five cron fields separated by single spaces`},
		{name: "a schedule carrying a quote", content: `{"outlets": {"default": {}}, "scheduled": [{"path": "/_scheduled/prune-logs", "schedule": "30 3 * * \"", "timeZone": "UTC"}]}`, wantErr: "which is not five cron fields separated by single spaces"},
		{name: "no schedule", content: `{"outlets": {"default": {}}, "scheduled": [{"path": "/_scheduled/prune-logs", "timeZone": "UTC"}]}`, wantErr: `has the schedule "", which is not five cron fields`},
		{name: "no time zone", content: `{"outlets": {"default": {}}, "scheduled": [{"path": "/_scheduled/prune-logs", "schedule": "30 3 * * *"}]}`, wantErr: `the scheduled route /_scheduled/prune-logs has the time zone "", which is not an IANA name (UTC, America/Denver)`},
		{name: "a time zone carrying an interpolation", content: `{"outlets": {"default": {}}, "scheduled": [{"path": "/_scheduled/prune-logs", "schedule": "30 3 * * *", "timeZone": "${var.zone}"}]}`, wantErr: `has the time zone "${var.zone}", which is not an IANA name`},
		{
			name:    "file routes, an upload and a stored file",
			content: `{"outlets": {"default": {}}, "fileRoutes": [{"kind": "upload", "method": "POST", "path": "/api/attach-manifest", "source": "AttachManifest"}, {"kind": "file", "method": "GET", "path": "/api/manifests/{id}/file", "source": "Manifest.Key"}]}`,
			want: &ReleaseFile{
				Outlets: map[string]ReleaseOutlet{"default": {}},
				FileRoutes: []FileRoute{
					{Kind: FileRouteUpload, Method: http.MethodPost, Path: "/api/attach-manifest", Source: "AttachManifest"},
					{Kind: FileRouteStored, Method: http.MethodGet, Path: "/api/manifests/{id}/file", Source: "Manifest.Key"},
				},
			},
		},
		{name: "a file route of a third kind", content: `{"outlets": {"default": {}}, "fileRoutes": [{"kind": "stream", "method": "GET", "path": "/api/streams", "source": "Stream"}]}`, wantErr: `the file route "/api/streams" has the kind "stream", and a file route is an upload or a file`},
		{name: "an upload answering GET", content: `{"outlets": {"default": {}}, "fileRoutes": [{"kind": "upload", "method": "GET", "path": "/api/attach-manifest", "source": "AttachManifest"}]}`, wantErr: `the file route "/api/attach-manifest" (upload) answers GET; an upload answers POST and a file GET`},
		{name: "a stored file answering POST", content: `{"outlets": {"default": {}}, "fileRoutes": [{"kind": "file", "method": "POST", "path": "/api/manifests/{id}/file", "source": "Manifest.Key"}]}`, wantErr: `the file route "/api/manifests/{id}/file" (file) answers POST`},
		{name: "a file route with a path of the wrong shape", content: `{"outlets": {"default": {}}, "fileRoutes": [{"kind": "upload", "method": "POST", "path": "/api/attach manifest", "source": "AttachManifest"}]}`, wantErr: `the file route "/api/attach manifest" is not a path a router mounts (/api/photos/{id}/file)`},
		{name: "a file route carrying an interpolation", content: `{"outlets": {"default": {}}, "fileRoutes": [{"kind": "upload", "method": "POST", "path": "/api/${var.x}", "source": "AttachManifest"}]}`, wantErr: `the file route "/api/${var.x}" is not a path a router mounts`},
		{name: "a file route listed twice", content: `{"outlets": {"default": {}}, "fileRoutes": [{"kind": "upload", "method": "POST", "path": "/api/attach-manifest", "source": "AttachManifest"}, {"kind": "upload", "method": "POST", "path": "/api/attach-manifest", "source": "AttachAgain"}]}`, wantErr: "the file route POST /api/attach-manifest is listed twice"},
		{name: "a file route naming no source", content: `{"outlets": {"default": {}}, "fileRoutes": [{"kind": "upload", "method": "POST", "path": "/api/attach-manifest"}]}`, wantErr: "the file route POST /api/attach-manifest names no source"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if tt.content != "" {
				if err := os.WriteFile(filepath.Join(dir, ReleaseFileName), []byte(tt.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := ReadReleaseFile(dir)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ReadReleaseFile() error = %v, wantErr %q", err, tt.wantErr)
				}
				if errors.Is(err, os.ErrNotExist) != tt.wantMissing {
					t.Errorf("ReadReleaseFile() error wraps os.ErrNotExist = %v, want %v", !tt.wantMissing, tt.wantMissing)
				}

				return
			}
			if err != nil {
				t.Fatalf("ReadReleaseFile() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ReadReleaseFile() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestFileRouteDeclaration names what a file route comes from: the @upload method, the
// @file column of a resource, or the computed resource that renders its file.
func TestFileRouteDeclaration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		route FileRoute
		want  string
	}{
		{name: "an upload", route: FileRoute{Kind: FileRouteUpload, Source: "AttachManifest"}, want: "the @upload method AttachManifest"},
		{name: "a stored file's column", route: FileRoute{Kind: FileRouteStored, Source: "Manifest.Key"}, want: "the @file column Manifest.Key"},
		{name: "a computed resource's file", route: FileRoute{Kind: FileRouteStored, Source: "Statement"}, want: "the @file of the computed resource Statement"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.route.Declaration(); got != tt.want {
				t.Errorf("Declaration() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOldestAnswered(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		outlets map[string]ReleaseOutlet
		want    OldestAnswered
		wantOK  bool
	}{
		{name: "no outlet declares one", outlets: map[string]ReleaseOutlet{"default": {}, "api": {Kind: OutletAPIKey}}},
		{name: "one release", outlets: map[string]ReleaseOutlet{"default": {OldestAnswered: "1.5.0"}}, want: OldestAnswered{Release: "1.5.0", Outlet: "default"}, wantOK: true},
		{name: "the newest release over the outlets", outlets: map[string]ReleaseOutlet{"default": {OldestAnswered: "1.5.0"}, "portal": {OldestAnswered: "1.10.0"}, "kiosk": {OldestAnswered: "1.9.0"}}, want: OldestAnswered{Release: "1.10.0", Outlet: "portal"}, wantOK: true},
		{name: "this is newer than any release", outlets: map[string]ReleaseOutlet{"default": {OldestAnswered: "9.0.0"}, "portal": {OldestAnswered: ReleaseThis}}, want: OldestAnswered{This: true, Outlet: "portal"}, wantOK: true},
		{name: "equal releases name the first outlet by name", outlets: map[string]ReleaseOutlet{"portal": {OldestAnswered: "1.5.0"}, "default": {OldestAnswered: "1.5.0"}}, want: OldestAnswered{Release: "1.5.0", Outlet: "default"}, wantOK: true},
		{name: "a machine outlet counts for nothing, whatever it says", outlets: map[string]ReleaseOutlet{"default": {OldestAnswered: "1.5.0"}, "api": {Kind: OutletAPIKey, OldestAnswered: ReleaseThis}}, want: OldestAnswered{Release: "1.5.0", Outlet: "default"}, wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := &ReleaseFile{Outlets: tt.outlets}
			got, ok := f.OldestAnswered()
			if ok != tt.wantOK {
				t.Errorf("OldestAnswered() ok = %v, want %v", ok, tt.wantOK)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("OldestAnswered() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestOldestAnsweredString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		oldest OldestAnswered
		want   string
	}{
		{name: "a release", oldest: OldestAnswered{Release: "1.5.0", Outlet: "default"}, want: "the default outlet answers 1.5.0 at the oldest"},
		{name: "the server's own", oldest: OldestAnswered{This: true, Outlet: "portal"}, want: "the portal outlet answers this release alone (oldest answered: this)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.oldest.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		v    string
		want bool
	}{
		{name: "a release", v: "1.5.0", want: true},
		{name: "a pre-release", v: "1.5.0-rc.1", want: true},
		{name: "with its v", v: "v1.5.0"},
		{name: "two numbers", v: "1.5"},
		{name: "a word", v: "soon"},
		{name: "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsRelease(tt.v); got != tt.want {
				t.Errorf("IsRelease(%q) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}
