package derive

import (
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
