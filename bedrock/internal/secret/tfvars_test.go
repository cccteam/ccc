package secret

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2/hclwrite"
)

// edits is where the tfvars edit cases live: <case>.tfvars and <case>[.<variant>].want.tfvars.
const edits = "testdata/edit"

func TestSetVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		src      string
		env      string
		variable string
		version  string
		want     string
		wantErr  string
	}{
		{name: "an entry joins the environment's map", src: "pins.tfvars", env: "tst", variable: "APP_MAIL_API_KEY", version: "4", want: "pins.added.want.tfvars"},
		{name: "an empty map takes the first entry", src: "pins.tfvars", env: "stg", variable: "APP_COOKIE_KEY", version: "1", want: "pins.empty.want.tfvars"},
		{name: "a pinned value is replaced", src: "pins.tfvars", env: "tst", variable: "APP_COOKIE_KEY", version: "3", want: "pins.replaced.want.tfvars"},
		{name: "latest is pinned as written", src: "pins.tfvars", env: "tst", variable: "APP_COOKIE_KEY", version: "latest", want: "pins.latest.want.tfvars"},
		{name: "a map on one line takes the entry after a comma", src: "oneline.tfvars", env: "stg", variable: "APP_MAIL_API_KEY", version: "4", want: "oneline.added.want.tfvars"},
		{name: "a value in a map on one line is replaced in place", src: "oneline.tfvars", env: "tst", variable: "APP_COOKIE_KEY", version: "3", want: "oneline.replaced.want.tfvars"},
		{name: "quoted keys are matched and kept", src: "quoted.tfvars", env: "tst", variable: "APP_COOKIE_KEY", version: "3", want: "quoted.want.tfvars"},
		{name: "a value that is not a string is replaced by the quoted version", src: "number.tfvars", env: "tst", variable: "APP_COOKIE_KEY", version: "3", want: "number.want.tfvars"},
		{name: "every comment is kept when a value is replaced", src: "comments.tfvars", env: "tst", variable: "APP_COOKIE_KEY", version: "3", want: "comments.replaced.want.tfvars"},
		{name: "every comment is kept when an entry is added", src: "comments.tfvars", env: "tst", variable: "APP_MAIL_API_KEY", version: "4", want: "comments.added.want.tfvars"},
		{name: "a comment after an empty map stays after it", src: "comments.tfvars", env: "stg", variable: "APP_COOKIE_KEY", version: "1", want: "comments.empty.want.tfvars"},
		{name: "a placement without the map is refused", src: "missing.tfvars", env: "tst", variable: "APP_COOKIE_KEY", version: "3", wantErr: "no secret_versions in missing.tfvars"},
		{name: "a map that is not written out is refused", src: "notmap.tfvars", env: "tst", variable: "APP_COOKIE_KEY", version: "3", wantErr: "secret_versions in notmap.tfvars is not a map written out"},
		{name: "an environment that is not a map is refused", src: "envnotmap.tfvars", env: "tst", variable: "APP_COOKIE_KEY", version: "3", wantErr: "secret_versions.tst in envnotmap.tfvars is not a map written out"},
		{name: "an environment the map lacks is refused", src: "unknownenv.tfvars", env: "prd", variable: "APP_COOKIE_KEY", version: "3", wantErr: "secret_versions in unknownenv.tfvars has no prd map: its keys are tst, stg"},
		{name: "a file that does not parse is refused", src: "broken.tfvars", env: "tst", variable: "APP_COOKIE_KEY", version: "3", wantErr: "hclsyntax.ParseConfig()"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(filepath.Join(edits, tt.src))
			if err != nil {
				t.Fatal(err)
			}
			got, err := SetVersion(src, tt.src, tt.env, tt.variable, tt.version)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("SetVersion() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("SetVersion() error = %v", err)
			}
			// The source is already formatted, so the only difference the golden file
			// may show is the entry set.
			if formatted := hclwrite.Format(src); !bytes.Equal(formatted, src) {
				t.Errorf("%s is not formatted; hclwrite.Format() gives:\n%s", tt.src, formatted)
			}
			want, err := os.ReadFile(filepath.Join(edits, tt.want))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("SetVersion() =\n%s\nwant\n%s", got, want)
			}
		})
	}
}

func TestPinned(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		src         string
		env         string
		variable    string
		wantVersion string
		wantPinned  bool
		wantErr     string
	}{
		{name: "a pinned variable", src: "pins.tfvars", env: "tst", variable: "APP_COOKIE_KEY", wantVersion: "2", wantPinned: true},
		{name: "a variable the environment's map lacks", src: "pins.tfvars", env: "tst", variable: "APP_MAIL_API_KEY"},
		{name: "an empty map", src: "pins.tfvars", env: "stg", variable: "APP_COOKIE_KEY"},
		{name: "a quoted key", src: "quoted.tfvars", env: "tst", variable: "APP_COOKIE_KEY", wantVersion: "2", wantPinned: true},
		{name: "a map on one line", src: "oneline.tfvars", env: "tst", variable: "APP_MAIL_API_KEY", wantVersion: "1", wantPinned: true},
		{name: "a pin that is not a string counts as another value", src: "number.tfvars", env: "tst", variable: "APP_COOKIE_KEY", wantPinned: true},
		{name: "a placement without the map is refused", src: "missing.tfvars", env: "tst", variable: "APP_COOKIE_KEY", wantErr: "no secret_versions in"},
		{name: "an environment the map lacks is refused", src: "unknownenv.tfvars", env: "prd", variable: "APP_COOKIE_KEY", wantErr: "has no prd map: its keys are tst, stg"},
		{name: "an environment that is not a map is refused", src: "envnotmap.tfvars", env: "tst", variable: "APP_COOKIE_KEY", wantErr: "secret_versions.tst in envnotmap.tfvars is not a map written out"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(filepath.Join(edits, tt.src))
			if err != nil {
				t.Fatal(err)
			}
			p, err := parsePlacement(src, tt.src)
			if err != nil {
				t.Fatalf("parsePlacement() error = %v", err)
			}
			version, pinned, err := p.pinned(tt.env, tt.variable)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("pinned() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("pinned() error = %v", err)
			}
			if version != tt.wantVersion || pinned != tt.wantPinned {
				t.Errorf("pinned() = %q, %v, want %q, %v", version, pinned, tt.wantVersion, tt.wantPinned)
			}
		})
	}
}

func TestPlacementEnvironments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		want    []string
		wantErr string
	}{
		{name: "the environments in the file's order", src: "pins.tfvars", want: []string{"tst", "stg", "prd"}},
		{name: "a quoted key", src: "quoted.tfvars", want: []string{"tst", "stg", "prd"}},
		{name: "two environments", src: "unknownenv.tfvars", want: []string{"tst", "stg"}},
		{name: "a placement without the map is refused", src: "missing.tfvars", wantErr: "no secret_versions in"},
		{name: "a map that is not written out is refused", src: "notmap.tfvars", wantErr: "is not a map written out"},
		{name: "a file that is not there is refused", src: "absent.tfvars", wantErr: "no placement at"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := PlacementEnvironments(filepath.Join(edits, tt.src))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("PlacementEnvironments() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("PlacementEnvironments() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("PlacementEnvironments() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestContainerSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		variable string
		want     string
	}{
		{name: "two words", variable: "APP_COOKIE_KEY", want: "-cookie-key"},
		{name: "one word", variable: "APP_KEY", want: "-key"},
		{name: "several words", variable: "APP_STAFF_OIDC_CLIENT_SECRET", want: "-staff-oidc-client-secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ContainerSuffix(tt.variable); got != tt.want {
				t.Errorf("ContainerSuffix(%q) = %q, want %q", tt.variable, got, tt.want)
			}
		})
	}
}
