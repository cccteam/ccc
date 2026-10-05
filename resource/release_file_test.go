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
// this release, no declaration, a machine outlet, and the scheduled routes), a missing
// file, and the files it refuses: malformed JSON, no outlets, entries that are neither a
// machine outlet nor a session outlet naming its oldest answered release, and a
// scheduled route outside the scheduled prefix or without its schedule or zone.
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
