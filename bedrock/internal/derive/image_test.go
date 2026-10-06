package derive

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cccteam/ccc/impulse/app"
)

func TestImageInputs(t *testing.T) {
	t.Parallel()

	const goMod = "module example.com/quill\n\ngo 1.26\n"
	const pkg = "package main\n"
	tests := []struct {
		name string
		// files are the tree, by root-relative path.
		files          map[string]string
		wantRootGo     bool
		wantGoDirs     []string
		wantWorkspaces []string
	}{
		{
			name:       "the root package and each top-level package directory, test files aside",
			files:      map[string]string{"go.mod": goMod, "main.go": pkg, "cmd/migrate/main.go": pkg, "pkg/config/config.go": "package config\n", "test/smoke_test.go": "package test\n", "schema/001.sql": ""},
			wantRootGo: true,
			wantGoDirs: []string{"cmd", "pkg"},
		},
		{
			name:       "no Go file at the root, and the directories go build never reads are left out",
			files:      map[string]string{"go.mod": goMod, "app/app.go": "package app\n", "testdata/x.go": pkg, "_tools/gen.go": pkg, ".hidden/h.go": pkg},
			wantGoDirs: []string{"app"},
		},
		{
			name:       "a directory that is a module of its own is not the application's package",
			files:      map[string]string{"go.mod": goMod, "main.go": pkg, "web/go.mod": "module example.com/quill/web\n", "web/tool.go": pkg, "cmd/x/main.go": pkg},
			wantRootGo: true,
			wantGoDirs: []string{"cmd"},
		},
		{
			name:           "a workspace copies its manifest and lockfile, and its configuration files when it has them",
			files:          map[string]string{"go.mod": goMod, "main.go": pkg, "web/angular.json": "{}", "web/bunfig.toml": "", "portal/angular.json": "{}", "portal/.npmrc": ""},
			wantRootGo:     true,
			wantWorkspaces: []string{"portal: portal/package.json portal/bun.lock portal/.npmrc", "web: web/package.json web/bun.lock web/bunfig.toml"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			for rel, content := range tt.files {
				abs := filepath.Join(root, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(abs, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			a, err := app.Discover(root)
			if err != nil {
				t.Fatalf("app.Discover() error = %v", err)
			}
			m := &Model{}
			if err := m.imageInputs(a); err != nil {
				t.Fatalf("imageInputs() error = %v", err)
			}
			if m.ImageInputs.RootGo != tt.wantRootGo {
				t.Errorf("RootGo = %v, want %v", m.ImageInputs.RootGo, tt.wantRootGo)
			}
			if !slices.Equal(m.ImageInputs.GoDirs, tt.wantGoDirs) {
				t.Errorf("GoDirs = %v, want %v", m.ImageInputs.GoDirs, tt.wantGoDirs)
			}
			var workspaces []string
			for _, w := range m.ImageInputs.Workspaces {
				workspaces = append(workspaces, w.Dir+": "+strings.Join(w.Files, " "))
			}
			slices.Sort(workspaces)
			if !slices.Equal(workspaces, tt.wantWorkspaces) {
				t.Errorf("Workspaces = %v, want %v", workspaces, tt.wantWorkspaces)
			}
		})
	}
}

func TestWorkspaceInputs(t *testing.T) {
	t.Parallel()

	in := &ImageInputs{Workspaces: []WorkspaceInputs{{Dir: "web", Files: []string{"web/package.json", "web/bun.lock", "web/bunfig.toml"}}}}
	tests := []struct {
		name string
		dir  string
		want []string
	}{
		{name: "a workspace the tree holds answers its files", dir: "web", want: []string{"web/package.json", "web/bun.lock", "web/bunfig.toml"}},
		{name: "one the tree lacks answers the two every install has", dir: "portal", want: []string{"portal/package.json", "portal/bun.lock"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := in.WorkspaceInputs(tt.dir); !slices.Equal(got, tt.want) {
				t.Errorf("WorkspaceInputs(%q) = %v, want %v", tt.dir, got, tt.want)
			}
		})
	}
}
