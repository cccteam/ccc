package resource

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestReadReleaseFile pins the reader over the shapes the generator writes (a release,
// this release, no declaration, a machine outlet, the scheduled routes, the file routes
// and the surfaces), a missing file, and the files it refuses: malformed JSON, no
// outlets, entries that are neither a machine outlet nor a session outlet naming its
// oldest answered release, a scheduled route outside the scheduled prefix or without its
// schedule or zone, a file route of another shape, and a surface outside the vocabulary.
func TestReadReleaseFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// content is the file written into the directory; "" writes no file.
		content string
		want    ReleaseFile
		// wantErr is a fragment of the error, wantErrIs an error it must wrap.
		wantErr   string
		wantErrIs error
	}{
		{
			name: "a release, this release, no declaration and a machine outlet",
			content: `{
  "outlets": {
    "default": {"oldestAnswered": "1.5.0"},
    "portal": {"oldestAnswered": "this"},
    "kiosk": {"oldestAnswered": ""},
    "api": {"kind": "api-key"}
  }
}
`,
			want: ReleaseFile{Outlets: map[string]ReleaseOutlet{
				"default": {OldestAnswered: "1.5.0"},
				"portal":  {OldestAnswered: ThisRelease},
				"kiosk":   {},
				"api":     {APIKey: true},
			}},
		},
		{
			name:    "a release with a leading v",
			content: `{"outlets": {"default": {"oldestAnswered": "v2.0.0-rc1"}}}`,
			want:    ReleaseFile{Outlets: map[string]ReleaseOutlet{"default": {OldestAnswered: "v2.0.0-rc1"}}},
		},
		{
			name:    "a field the reader does not know is tolerated",
			content: `{"outlets": {"default": {"oldestAnswered": "1.5.0", "note": "later"}}, "later": true}`,
			want:    ReleaseFile{Outlets: map[string]ReleaseOutlet{"default": {OldestAnswered: "1.5.0"}}},
		},
		{
			name:      "a missing file",
			wantErr:   "reading the release file",
			wantErrIs: fs.ErrNotExist,
		},
		{
			name:    "malformed JSON",
			content: `{"outlets": {`,
			wantErr: "is not the shape the generator writes",
		},
		{
			name:    "no outlets",
			content: `{"outlets": {}}`,
			wantErr: "names no outlet; a generated router has at least its default outlet",
		},
		{
			name:    "outlets missing",
			content: `{}`,
			wantErr: "names no outlet",
		},
		{
			name:    "a machine outlet carrying an oldest answered release",
			content: `{"outlets": {"api": {"kind": "api-key", "oldestAnswered": "1.0.0"}}}`,
			wantErr: "an api-key outlet carries no oldestAnswered: its clients carry no release",
		},
		{
			name:    "a kind that is not api-key",
			content: `{"outlets": {"default": {"kind": "session", "oldestAnswered": "1.0.0"}}}`,
			wantErr: `kind "session" is not "api-key", the one kind an entry declares; a session outlet declares no kind`,
		},
		{
			name:    "a session outlet without an oldest answered release",
			content: `{"outlets": {"default": {}}}`,
			wantErr: "a session outlet names its oldestAnswered",
		},
		{
			name:    "an oldest answered release that is not a release",
			content: `{"outlets": {"default": {"oldestAnswered": "dev"}}}`,
			wantErr: `oldestAnswered "dev" is not a release`,
		},
		{
			name:    "an oldest answered release that is not a string",
			content: `{"outlets": {"default": {"oldestAnswered": 1}}}`,
			wantErr: "is not the shape the generator writes",
		},
		{
			name: "scheduled routes",
			content: `{
  "outlets": {"default": {"oldestAnswered": ""}},
  "scheduled": [
    {"path": "/_scheduled/prune-droid-reports", "schedule": "30 3 * * *", "timeZone": "America/Denver"},
    {"path": "/_scheduled/send-digest", "schedule": "0 6 * * MON-FRI", "timeZone": "UTC"}
  ]
}
`,
			want: ReleaseFile{
				Outlets: map[string]ReleaseOutlet{"default": {}},
				Scheduled: []ScheduledRoute{
					{Path: "/_scheduled/prune-droid-reports", Schedule: "30 3 * * *", TimeZone: "America/Denver"},
					{Path: "/_scheduled/send-digest", Schedule: "0 6 * * MON-FRI", TimeZone: "UTC"},
				},
			},
		},
		{
			name:    "a scheduled route outside the scheduled prefix",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "scheduled": [{"path": "/api/prune", "schedule": "0 3 * * *", "timeZone": "UTC"}]}`,
			wantErr: `the scheduled route "/api/prune" is not a path under /_scheduled/`,
		},
		{
			name:    "a scheduled route with no schedule",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "scheduled": [{"path": "/_scheduled/prune", "timeZone": "UTC"}]}`,
			wantErr: "the scheduled route /_scheduled/prune names no schedule",
		},
		{
			name:    "a scheduled route with no time zone",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "scheduled": [{"path": "/_scheduled/prune", "schedule": "0 3 * * *"}]}`,
			wantErr: "the scheduled route /_scheduled/prune names no time zone",
		},
		{
			name:    "file routes, an upload and a stored file",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "fileRoutes": [{"kind": "upload", "method": "POST", "path": "/api/attach-photo", "source": "AttachPhoto"}, {"kind": "file", "method": "GET", "path": "/api/photos/{id}/file", "source": "Photo.Key"}]}`,
			want: ReleaseFile{
				Outlets: map[string]ReleaseOutlet{"default": {}},
				FileRoutes: []FileRoute{
					{Kind: FileRouteUpload, Method: "POST", Path: "/api/attach-photo", Source: "AttachPhoto"},
					{Kind: FileRouteStored, Method: "GET", Path: "/api/photos/{id}/file", Source: "Photo.Key"},
				},
			},
		},
		{
			name:    "a file route of a third kind",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "fileRoutes": [{"kind": "stream", "method": "GET", "path": "/api/stream", "source": "Stream"}]}`,
			wantErr: `the file route "/api/stream" has the kind "stream"; a file route is an upload or a file`,
		},
		{
			name:    "an upload answering GET",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "fileRoutes": [{"kind": "upload", "method": "GET", "path": "/api/attach-photo", "source": "AttachPhoto"}]}`,
			wantErr: `the file route "/api/attach-photo" (upload) answers GET; an upload answers POST and a file GET`,
		},
		{
			name:    "a stored file answering POST",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "fileRoutes": [{"kind": "file", "method": "POST", "path": "/api/photos/{id}/file", "source": "Photo.Key"}]}`,
			wantErr: `the file route "/api/photos/{id}/file" (file) answers POST`,
		},
		{
			name:    "a file route not under the root",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "fileRoutes": [{"kind": "upload", "method": "POST", "path": "api/attach-photo", "source": "AttachPhoto"}]}`,
			wantErr: `the file route "api/attach-photo" is not a path under the root`,
		},
		{
			name:    "a file route naming no source",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "fileRoutes": [{"kind": "upload", "method": "POST", "path": "/api/attach-photo"}]}`,
			wantErr: "the file route /api/attach-photo names no source",
		},
		{
			name: "surfaces: the default, an outlet, a hand-mounted prefix and a route",
			content: `{
  "outlets": {"default": {"oldestAnswered": ""}},
  "surfaces": [
    {"prefix": "/", "log": "onEvent"},
    {"prefix": "/beacons/", "log": "onEvent", "traces": "off"},
    {"prefix": "/droids/", "log": "sampled", "fraction": 0.01, "traces": "capped", "rate": 0.1},
    {"prefix": "/api/ships/{shipID}/content", "log": "never"},
    {"prefix": "/portal/api/", "traces": "followFrontEnd"}
  ]
}
`,
			want: ReleaseFile{
				Outlets: map[string]ReleaseOutlet{"default": {}},
				Surfaces: []Surface{
					{Prefix: "/", Log: RequestLogOnEvent},
					{Prefix: "/beacons/", Log: RequestLogOnEvent, Traces: TracesOff},
					{Prefix: "/droids/", Log: RequestLogSampled, Fraction: 0.01, Traces: TracesCapped, Rate: 0.1},
					{Prefix: "/api/ships/{shipID}/content", Log: RequestLogNever},
					{Prefix: "/portal/api/", Traces: TracesFollowFrontEnd},
				},
			},
		},
		{
			name:    "a surface not under the root",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "surfaces": [{"prefix": "beacons/", "log": "onEvent"}]}`,
			wantErr: `the surface "beacons/" is not a path under the root`,
		},
		{
			name:    "a surface declaring nothing",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "surfaces": [{"prefix": "/beacons/"}]}`,
			wantErr: "the surface /beacons/ declares neither a request log word nor a trace setting",
		},
		{
			name:    "a request log word that is not one",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "surfaces": [{"prefix": "/beacons/", "log": "sometimes"}]}`,
			wantErr: `the surface /beacons/ has the request log word "sometimes"; the words are always, onEvent, sampled and never`,
		},
		{
			name:    "a fraction without the sampled word",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "surfaces": [{"prefix": "/beacons/", "log": "onEvent", "fraction": 0.5}]}`,
			wantErr: "the surface /beacons/ carries a fraction without the sampled word",
		},
		{
			name:    "a sampled surface with a fraction over one",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "surfaces": [{"prefix": "/beacons/", "log": "sampled", "fraction": 2}]}`,
			wantErr: "the surface /beacons/ is sampled at 2; the fraction is above 0 and at most 1",
		},
		{
			name:    "a sampled surface without a fraction",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "surfaces": [{"prefix": "/beacons/", "log": "sampled"}]}`,
			wantErr: "the surface /beacons/ is sampled at 0",
		},
		{
			name:    "a trace setting that is not one",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "surfaces": [{"prefix": "/beacons/", "traces": "sometimes"}]}`,
			wantErr: `the surface /beacons/ has the trace setting "sometimes"; the settings are followFrontEnd, capped and off`,
		},
		{
			name:    "a rate without the capped setting",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "surfaces": [{"prefix": "/beacons/", "traces": "off", "rate": 0.5}]}`,
			wantErr: "the surface /beacons/ carries a rate without the capped setting",
		},
		{
			name:    "a capped surface with a rate of zero",
			content: `{"outlets": {"default": {"oldestAnswered": ""}}, "surfaces": [{"prefix": "/beacons/", "traces": "capped"}]}`,
			wantErr: "the surface /beacons/ is capped at 0; the rate is above 0 and at most 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if tt.content != "" {
				if err := os.WriteFile(filepath.Join(dir, ReleaseFileName), []byte(tt.content), 0o600); err != nil {
					t.Fatalf("os.WriteFile() error = %v", err)
				}
			}

			got, err := ReadReleaseFile(dir)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ReadReleaseFile() error = %v, want it to contain %q", err, tt.wantErr)
				}
				if tt.wantErrIs != nil && !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("ReadReleaseFile() error = %v, want it to wrap %v", err, tt.wantErrIs)
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

// TestReleaseOutlet_MarshalJSON pins how an entry is spelled on disk, and that what is
// written reads back as the same entry.
func TestReleaseOutlet_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		outlet ReleaseOutlet
		want   string
	}{
		{name: "a release", outlet: ReleaseOutlet{OldestAnswered: "1.5.0"}, want: `{"oldestAnswered":"1.5.0"}`},
		{name: "this release", outlet: ReleaseOutlet{OldestAnswered: ThisRelease}, want: `{"oldestAnswered":"this"}`},
		{name: "no declaration", outlet: ReleaseOutlet{}, want: `{"oldestAnswered":""}`},
		{name: "a machine outlet", outlet: ReleaseOutlet{APIKey: true}, want: `{"kind":"api-key"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := json.Marshal(tt.outlet)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("json.Marshal() = %s, want %s", got, tt.want)
			}

			var back ReleaseOutlet
			if err := json.Unmarshal(got, &back); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}
			if back != tt.outlet {
				t.Errorf("round trip = %+v, want %+v", back, tt.outlet)
			}
		})
	}
}
