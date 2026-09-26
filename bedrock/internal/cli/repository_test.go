package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

// harborRepo is a copy of the harbor fixture marked as a repository whose origin is
// impulseframework/harbor on GitHub (no origin when remote is empty).
func harborRepo(t *testing.T, remote string) string {
	t.Helper()

	dir := copyRepo(t, fixtureApp)
	config := "[core]\n\tbare = false\n"
	if remote != "" {
		config += "[remote \"origin\"]\n\turl = " + remote + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

// lab is the stand-in GitHub with the release app installed on impulseframework and
// harbor at v0.1.4 (c4) on master, plus what the test adds.
func labGitHub(t *testing.T) (*githubtest.Server, *githubtest.Repo) {
	t.Helper()

	server := githubtest.New(t)
	server.Installations["impulseframework"] = []github.Installation{{AppID: 5080645, AppSlug: "impulseframework-release"}, {AppID: 10529, AppSlug: "google-cloud-build"}}
	repo := server.AddRepo("impulseframework", "harbor", &githubtest.Repo{
		Refs:     map[string]github.Object{"refs/heads/master": {Type: "commit", SHA: "c4"}, "refs/tags/v0.1.4": {Type: "commit", SHA: "c4"}},
		Ancestry: map[string][]string{"c4": {"c4"}, "c5": {"c5", "c4"}},
		Trees:    map[string]string{"c4": "tree4"},
		Files:    map[string]string{"tree4:.release-please-manifest.json": "{\n  \".\": \"0.1.4\"\n}\n"},
	})

	return server, repo
}

func TestRepositoryProtectAndHotfixStart(t *testing.T) {
	t.Parallel()

	placement := filepath.Join(fixtureApp, "..", "placement.json")
	tests := []struct {
		name string
		// remote is the origin URL written into the copy; empty for none.
		remote string
		// prepare adjusts the stand-in before the run.
		prepare func(server *githubtest.Server, repo *githubtest.Repo)
		args    []string
		wantOut []string
		wantErr string
	}{
		{
			name:   "protect creates the three rulesets",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"repository", "protect", "--placement", placement},
			wantOut: []string{
				"Repository impulseframework/harbor, release app impulseframework-release (app 5080645):",
				"release tags     created (id 1000): tags v* are created, moved or deleted only by the release app and repository admins",
				"master branch    created (id 1001): changes to refs/heads/master arrive by pull request; no force push, no deletion",
				"hotfix branches  created (id 1002): changes to refs/heads/hotfix/** arrive by pull request; no force push, no deletion",
			},
		},
		{
			name:   "protect refuses when the release app is not installed",
			remote: "https://github.com/impulseframework/harbor",
			prepare: func(server *githubtest.Server, _ *githubtest.Repo) {
				server.Installations["impulseframework"] = []github.Installation{{AppID: 10529, AppSlug: "google-cloud-build"}}
			},
			args:    []string{"repository", "protect", "--placement", placement},
			wantErr: "the release app impulseframework-release is not installed on impulseframework (installed: google-cloud-build)",
		},
		{
			name:    "the repository needs an origin on GitHub",
			args:    []string{"repository", "protect", "--placement", placement},
			wantErr: "names no origin remote",
		},
		{
			name:   "hotfix start creates the line at the release",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"hotfix", "start", "v0.1.4", "--placement", placement},
			wantOut: []string{
				"Created hotfix/0.1.x in impulseframework/harbor at c4 (v0.1.4); the line's next release is v0.1.5.",
				"Next: open the fix pull request against hotfix/0.1.x and merge it; release-please releases the branch as v0.1.5, and the tag runs through the environments like any release. When the hotfix is out, merge hotfix/0.1.x back into master by pull request with a merge commit, so master carries the fix and counts releases from it.",
			},
		},
		{
			name:   "hotfix start skips a patch master has already cut",
			remote: "git@github.com:impulseframework/harbor.git",
			prepare: func(_ *githubtest.Server, repo *githubtest.Repo) {
				repo.Refs["refs/tags/v0.1.5"] = github.Object{Type: "commit", SHA: "c5"}
				repo.Refs["refs/heads/master"] = github.Object{Type: "commit", SHA: "c5"}
			},
			args: []string{"hotfix", "start", "v0.1.4", "--placement", placement},
			wantOut: []string{
				"Created hotfix/0.1.x in impulseframework/harbor at a commit on c4 (v0.1.4) that sets the manifest to 0.1.5, which master has already cut; the line's next release is v0.1.6.",
				"release-please releases the branch as v0.1.6",
			},
		},
		{
			name:   "hotfix start reports an existing line",
			remote: "git@github.com:impulseframework/harbor.git",
			prepare: func(_ *githubtest.Server, repo *githubtest.Repo) {
				repo.Refs["refs/heads/hotfix/0.1.x"] = github.Object{Type: "commit", SHA: "c4"}
			},
			args:    []string{"hotfix", "start", "v0.1.4", "--placement", placement},
			wantOut: []string{"hotfix/0.1.x already exists in impulseframework/harbor at c4: open the fix pull request against it."},
		},
		{
			name:    "hotfix start refuses a tag of the wrong shape",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"hotfix", "start", "0.1.4", "--placement", placement},
			wantErr: `tag "0.1.4" is not a release tag`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server, repo := labGitHub(t)
			if tt.prepare != nil {
				tt.prepare(server, repo)
			}
			dir := harborRepo(t, tt.remote)
			d := deps{
				domains: noCloudDomains, secrets: noSecretManager, projects: noProjects, cwd: dir, interactive: never,
				github: func(context.Context) (*github.Client, error) {
					return server.Client(), nil
				},
			}
			out, err := execute(d, "", tt.args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Execute() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out)
				}

				return
			}
			if err != nil {
				t.Fatalf("Execute() error = %v; output:\n%s", err, out)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}
