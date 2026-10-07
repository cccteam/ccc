package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func boolPtr(b bool) *bool { return &b }

func TestFindCIDirective(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		src     string
		want    *CIDirective
		wantErr string
	}{
		{name: "no line", src: "package main\n\n//go:generate go run ./gen\n"},
		{
			name: "both settings",
			src:  "// Harbor's entry point.\n//impulse:ci large-runner=go-test,go-test-skipauth,image test-cache=on\npackage main\n",
			want: &CIDirective{File: "main.go", Line: 2, LargeRunner: []string{"go-test", "go-test-skipauth", "image"}, TestCache: boolPtr(true)},
		},
		{
			name: "none, and off",
			src:  "//impulse:ci test-cache=off large-runner=none\npackage main\n",
			want: &CIDirective{File: "main.go", Line: 1, LargeRunner: []string{}, TestCache: boolPtr(false)},
		},
		{
			name: "one setting leaves the other unset",
			src:  "//impulse:ci large-runner=image\npackage main\n",
			want: &CIDirective{File: "main.go", Line: 1, LargeRunner: []string{"image"}},
		},
		{
			name: "the bare line sets nothing",
			src:  "//impulse:ci\npackage main\n",
			want: &CIDirective{File: "main.go", Line: 1},
		},
		{
			name: "the default branch",
			src:  "//impulse:ci default-branch=trunk test-cache=on\npackage main\n",
			want: &CIDirective{File: "main.go", Line: 1, DefaultBranch: "trunk", TestCache: boolPtr(true)},
		},
		{name: "a default branch with a slash", src: "//impulse:ci default-branch=releases/current\npackage main\n", want: &CIDirective{File: "main.go", Line: 1, DefaultBranch: "releases/current"}},
		{name: "a default branch that is not a name", src: "//impulse:ci default-branch=-x\npackage main\n", wantErr: "default-branch=-x: the value is a branch name"},
		{name: "a default branch with two dots", src: "//impulse:ci default-branch=a..b\npackage main\n", wantErr: "default-branch=a..b: the value is a branch name"},
		{name: "an unknown setting", src: "//impulse:ci runner=big\npackage main\n", wantErr: `"runner" is not a setting (the settings are large-runner, test-cache and default-branch)`},
		{name: "a bare word", src: "//impulse:ci large-runner\npackage main\n", wantErr: `"large-runner" is not a name=value setting`},
		{name: "a bad test-cache value", src: "//impulse:ci test-cache=yes\npackage main\n", wantErr: "test-cache=yes: the value is on or off"},
		{name: "a setting twice", src: "//impulse:ci test-cache=on test-cache=off\npackage main\n", wantErr: "test-cache is set twice"},
		{name: "two lines in one file", src: "//impulse:ci test-cache=on\npackage main\n\n//impulse:ci large-runner=none\n", wantErr: "main.go:4: a second //impulse:ci line; the application has one, at main.go:1"},
		{name: "a line not at the start is prose", src: "package main\n\n// Not a directive: //impulse:ci test-cache=on\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := findCIDirective("main.go", []byte(tt.src))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("findCIDirective() error = %v, want containing %q", err, tt.wantErr)
				}
				if !strings.Contains(err.Error(), "main.go:") {
					t.Errorf("findCIDirective() error = %v, names no file and line", err)
				}

				return
			}
			if err != nil {
				t.Fatalf("findCIDirective() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("findCIDirective() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestDiscoverCIDirective: the line is read from any non-test Go file, a generated file's
// is not, and a second line in the tree fails discovery.
func TestDiscoverCIDirective(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		files   map[string]string
		want    *CIDirective
		wantErr string
	}{
		{
			name:  "none",
			files: map[string]string{"main.go": "package main\n"},
		},
		{
			name:  "in a package file",
			files: map[string]string{"main.go": "package main\n", "pkg/config/config.go": "// The CI choices.\n//impulse:ci large-runner=none\npackage config\n"},
			want:  &CIDirective{File: "pkg/config/config.go", Line: 2, LargeRunner: []string{}},
		},
		{
			name:  "a generated file's line is not read",
			files: map[string]string{"main.go": "package main\n", "app/zz_gen_x.go": "//impulse:ci test-cache=on\npackage app\n"},
		},
		{
			name:  "a test file's line is not read",
			files: map[string]string{"main.go": "package main\n", "main_test.go": "//impulse:ci test-cache=on\npackage main\n"},
		},
		{
			name:    "two files",
			files:   map[string]string{"main.go": "//impulse:ci test-cache=on\npackage main\n", "pkg/config/config.go": "//impulse:ci large-runner=none\npackage config\n"},
			wantErr: "a second //impulse:ci line; the application has one, at",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			files := map[string]string{"go.mod": "module example.com/harbor\n\ngo 1.26.6\n"}
			for rel, content := range tt.files {
				files[rel] = content
			}
			for rel, content := range files {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := Discover(root)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Discover() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Discover() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, a.CI); diff != "" {
				t.Errorf("CI mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
