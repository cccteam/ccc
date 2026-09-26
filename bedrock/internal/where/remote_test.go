package where

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// config is the git configuration written; worktree writes .git as a pointer
		// to a main repository holding that configuration instead.
		config    string
		worktree  bool
		wantOwner string
		wantRepo  string
		wantErr   string
	}{
		{
			name:      "ssh form",
			config:    "[core]\n\tbare = false\n[remote \"origin\"]\n\turl = git@github.com:impulseframework/harbor.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n",
			wantOwner: "impulseframework", wantRepo: "harbor",
		},
		{
			name:      "https form with .git",
			config:    "[remote \"origin\"]\n\turl = https://github.com/impulseframework/harbor.git\n",
			wantOwner: "impulseframework", wantRepo: "harbor",
		},
		{
			name:      "https form without .git",
			config:    "[remote \"origin\"]\n\turl = https://github.com/acme/quill\n",
			wantOwner: "acme", wantRepo: "quill",
		},
		{
			name:      "ssh URL form",
			config:    "[remote \"origin\"]\n\turl = ssh://git@github.com/acme/quill.git\n",
			wantOwner: "acme", wantRepo: "quill",
		},
		{
			name:      "the origin among other remotes",
			config:    "[remote \"upstream\"]\n\turl = git@github.com:other/quill.git\n[remote \"origin\"]\n\turl = git@github.com:acme/quill.git\n",
			wantOwner: "acme", wantRepo: "quill",
		},
		{
			name:      "a worktree follows its pointer to the main repository",
			config:    "[remote \"origin\"]\n\turl = git@github.com:acme/quill.git\n",
			worktree:  true,
			wantOwner: "acme", wantRepo: "quill",
		},
		{
			name:    "no origin",
			config:  "[remote \"upstream\"]\n\turl = git@github.com:other/quill.git\n",
			wantErr: "names no origin remote",
		},
		{
			name:    "not GitHub",
			config:  "[remote \"origin\"]\n\turl = git@gitlab.com:acme/quill.git\n",
			wantErr: "is not a GitHub repository URL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := filepath.Join(t.TempDir(), "repo")
			gitDirectory := filepath.Join(root, ".git")
			if tt.worktree {
				main := filepath.Join(t.TempDir(), "main")
				gitDirectory = filepath.Join(main, ".git")
				worktreeDir := filepath.Join(gitDirectory, "worktrees", "repo")
				if err := os.MkdirAll(worktreeDir, 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(root, 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: "+worktreeDir+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.MkdirAll(gitDirectory, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(gitDirectory, "config"), []byte(tt.config), 0o600); err != nil {
				t.Fatal(err)
			}
			owner, repo, err := Remote(root)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Remote() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Remote() error = %v", err)
			}
			if owner != tt.wantOwner || repo != tt.wantRepo {
				t.Errorf("Remote() = %q/%q, want %q/%q", owner, repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}
