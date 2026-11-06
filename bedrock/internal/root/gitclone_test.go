package root_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/root"
)

// TestGitClone: a directory holding a commit's files becomes a clone of the repository at
// that commit, on the branch, tracking origin. The repository is a bare one on disk, the
// commit made in a scratch clone of it.
func TestGitClone(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on this computer")
	}
	ctx := context.Background()
	git := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}

		return strings.TrimSpace(string(out))
	}
	bare := filepath.Join(t.TempDir(), "imr-root-infrastructure.git")
	git(t.TempDir(), "init", "--quiet", "--bare", "--initial-branch=master", bare)
	scratch := t.TempDir()
	git(scratch, "init", "--quiet", "--initial-branch=master")
	if err := os.WriteFile(filepath.Join(scratch, "placement.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(scratch, "add", "placement.json")
	git(scratch, "commit", "--quiet", "-m", "the foundation")
	commit := git(scratch, "rev-parse", "HEAD")
	git(scratch, "push", "--quiet", bare, "master")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "placement.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.GitClone(ctx, dir, bare, "master", commit, "test-token"); err != nil {
		t.Fatalf("GitClone() error = %v", err)
	}
	if got := git(dir, "rev-parse", "HEAD"); got != commit {
		t.Errorf("HEAD = %s, want %s", got, commit)
	}
	if got := git(dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "master" {
		t.Errorf("branch = %s, want master", got)
	}
	if got := git(dir, "rev-parse", "--abbrev-ref", "master@{upstream}"); got != "origin/master" {
		t.Errorf("upstream = %s, want origin/master", got)
	}
	if got := git(dir, "status", "--porcelain"); got != "" {
		t.Errorf("the working tree is not clean:\n%s", got)
	}
	// A run again, on the clone, changes nothing and fails nothing.
	if err := root.GitClone(ctx, dir, bare, "master", commit, "test-token"); err != nil {
		t.Fatalf("GitClone() again: error = %v", err)
	}
	// A directory a stopped run left partway (a repository with origin and no commit) is
	// finished.
	half := t.TempDir()
	git(half, "init", "--quiet", "--initial-branch=master")
	git(half, "remote", "add", "origin", "https://example.invalid/old.git")
	if err := root.GitClone(ctx, half, bare, "master", commit, "test-token"); err != nil {
		t.Fatalf("GitClone() on a half-made clone: error = %v", err)
	}
	if got := git(half, "rev-parse", "HEAD"); got != commit {
		t.Errorf("half-made clone's HEAD = %s, want %s", got, commit)
	}
}
