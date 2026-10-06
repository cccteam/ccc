package check

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestScanReleaseLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  string
		noFile  bool
		want    []ReleaseLineFinding
		wantErr bool
	}{
		{
			name:   "a feature on the patch below 1.0 is refused",
			config: `{"release-type": "go", "bump-minor-pre-major": true, "bump-patch-for-minor-pre-major": true, "packages": {".": {}}}`,
			want:   []ReleaseLineFinding{{Path: "release-please-config.json", Problem: patchForFeatureProblem}},
		},
		{
			name:   "a feature on the minor passes",
			config: `{"release-type": "go", "bump-minor-pre-major": true, "bump-patch-for-minor-pre-major": false, "packages": {".": {}}}`,
		},
		{
			name:   "the setting left out is release-please's default, a feature on the minor",
			config: `{"release-type": "go", "packages": {".": {}}}`,
		},
		{
			name:   "a package's own setting is refused by its name",
			config: `{"release-type": "go", "packages": {"site": {"bump-patch-for-minor-pre-major": true}, "api": {"bump-patch-for-minor-pre-major": false}}}`,
			want:   []ReleaseLineFinding{{Path: "release-please-config.json", Problem: "package site: " + patchForFeatureProblem}},
		},
		{
			name:   "the top level and a package are each refused, the top level first",
			config: `{"bump-patch-for-minor-pre-major": true, "packages": {"zed": {"bump-patch-for-minor-pre-major": true}, "alpha": {"bump-patch-for-minor-pre-major": true}}}`,
			want: []ReleaseLineFinding{
				{Path: "release-please-config.json", Problem: patchForFeatureProblem},
				{Path: "release-please-config.json", Problem: "package alpha: " + patchForFeatureProblem},
				{Path: "release-please-config.json", Problem: "package zed: " + patchForFeatureProblem},
			},
		},
		{
			name:   "no configuration checks nothing",
			noFile: true,
		},
		{
			name:    "a configuration that is not JSON is an error",
			config:  `{"release-type": "go",`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			if !tt.noFile {
				if err := os.WriteFile(filepath.Join(dir, releasePleaseConfig), []byte(tt.config), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := scanReleaseLines(dir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("scanReleaseLines() error = %v, wantErr %t", err, tt.wantErr)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("findings mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
