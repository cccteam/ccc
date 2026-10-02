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

// labGitHub is the stand-in GitHub with harbor at v0.1.4 (c4) on master, plus what the
// test adds.
func labGitHub(t *testing.T) (*githubtest.Server, *githubtest.Repo) {
	t.Helper()

	server := githubtest.New(t)
	repo := server.AddRepo("impulseframework", "harbor", &githubtest.Repo{
		Refs:     map[string]github.Object{"refs/heads/master": {Type: "commit", SHA: "c4"}, "refs/tags/v0.1.4": {Type: "commit", SHA: "c4"}},
		Ancestry: map[string][]string{"c4": {"c4"}, "c5": {"c5", "c4"}},
		Trees:    map[string]string{"c4": "tree4"},
		Files:    map[string]string{"tree4:.release-please-manifest.json": "{\n  \".\": \"0.1.4\"\n}\n"},
	})

	return server, repo
}

func TestRestoreAndHotfix(t *testing.T) {
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
		// wantDispatch is the workflow_dispatch event the run leaves: file, ref and inputs.
		wantDispatch string
		wantErr      string
	}{
		{
			name:   "restore dispatches the operations workflow for a wired environment",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"restore", "tst", "v0.1.4", "--placement", placement},
			wantOut: []string{
				"Asked, as octocat, for tst to be restored to v0.1.4: the operations workflow of impulseframework/harbor runs it (https://github.com/impulseframework/harbor/actions/workflows/operations.yml). The run replaces tst's database (empty), deploys v0.1.4, and its record names you.",
			},
			wantDispatch: "operations.yml master environment=tst release=v0.1.4",
		},
		{
			name:    "restore refuses production",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"restore", "prd", "v0.1.4", "--placement", placement},
			wantErr: "prd is production, which is never restored by a run",
		},
		{
			name:    "restore refuses a release that does not exist",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"restore", "tst", "v0.9.9", "--placement", placement},
			wantErr: "no release v0.9.9 in impulseframework/harbor: an environment is restored to a release that exists",
		},
		{
			name:    "restore refuses a tag of the wrong shape",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"restore", "tst", "0.1.4", "--placement", placement},
			wantErr: `"0.1.4" is not a release tag`,
		},
		{
			name:    "restore refuses an environment the placement does not list",
			remote:  "git@github.com:impulseframework/harbor.git",
			args:    []string{"restore", "qa", "v0.1.4", "--placement", placement},
			wantErr: `"qa" is not one of the environments (tst, stg, prd)`,
		},
		{
			name:    "the repository needs an origin on GitHub",
			args:    []string{"hotfix", "start", "v0.1.4", "--placement", placement},
			wantErr: "names no origin remote",
		},
		{
			name:   "hotfix start creates the line at the release",
			remote: "git@github.com:impulseframework/harbor.git",
			args:   []string{"hotfix", "start", "v0.1.4", "--placement", placement},
			wantOut: []string{
				"Created hotfix/0.1.x in impulseframework/harbor at c4 (v0.1.4); the line's next release is v0.1.5.",
				"Next: open the fix pull request against hotfix/0.1.x and merge it; release-please releases the branch as v0.1.5, and the tag runs through the environments like any release. When the hotfix is out, bring it to master with bedrock hotfix merge v0.1.5: a branch at the release's commit and its pull request into master, squash-merged; hotfix/0.1.x is never the pull request's head.",
			},
		},
		{
			name:   "hotfix merge makes the merge-back branch and its pull request",
			remote: "git@github.com:impulseframework/harbor.git",
			prepare: func(_ *githubtest.Server, repo *githubtest.Repo) {
				repo.Refs["refs/heads/hotfix/0.1.x"] = github.Object{Type: "commit", SHA: "h5"}
				repo.Refs["refs/tags/v0.1.5"] = github.Object{Type: "commit", SHA: "h5"}
				repo.Ancestry["h5"] = []string{"h5", "c4"}
				repo.Releases = map[string]github.Release{"v0.1.5": {TagName: "v0.1.5", Body: "### Bug Fixes\n\n* the fix"}}
			},
			args: []string{"hotfix", "merge", "v0.1.5", "--placement", placement},
			wantOut: []string{
				"Created merge-back/v0.1.5 in impulseframework/harbor at h5 (v0.1.5) and opened pull request #1 into master: https://github.com/impulseframework/harbor/pull/1",
				"Its title, \"fix: v0.1.5\", is the commit line the squash merge carries and release-please reads",
				"the merge deletes merge-back/v0.1.5, and hotfix/0.1.x stays as it is.",
			},
		},
		{
			name:   "hotfix merge refuses a release on master",
			remote: "git@github.com:impulseframework/harbor.git",
			prepare: func(_ *githubtest.Server, repo *githubtest.Repo) {
				repo.Refs["refs/heads/hotfix/0.1.x"] = github.Object{Type: "commit", SHA: "c4"}
			},
			args:    []string{"hotfix", "merge", "v0.1.4", "--placement", placement},
			wantErr: "tag v0.1.4 (commit c4) is already on master: nothing to merge back",
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
				"Created hotfix/0.1.x in impulseframework/harbor at a commit on c4 (v0.1.4) whose message names the line's next release, v0.1.6, since master has already cut v0.1.5.",
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
			name:   "hotfix start refuses an existing line that does not carry the release",
			remote: "git@github.com:impulseframework/harbor.git",
			prepare: func(_ *githubtest.Server, repo *githubtest.Repo) {
				repo.Refs["refs/heads/hotfix/0.1.x"] = github.Object{Type: "commit", SHA: "c3"}
				repo.Ancestry["c3"] = []string{"c3"}
				repo.Ancestry["c4"] = []string{"c4", "c3"}
			},
			args:    []string{"hotfix", "start", "v0.1.4", "--placement", placement},
			wantErr: "hotfix/0.1.x already exists in impulseframework/harbor at c3, which does not carry v0.1.4: the line started from an earlier release and serves the patches after it",
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
			if tt.wantDispatch != "" {
				if len(repo.Dispatches) != 1 {
					t.Fatalf("dispatches = %+v, want one", repo.Dispatches)
				}
				d := repo.Dispatches[0]
				if got := d.File + " " + d.Ref + " environment=" + d.Inputs["environment"] + " release=" + d.Inputs["release"]; got != tt.wantDispatch {
					t.Errorf("dispatch = %q, want %q", got, tt.wantDispatch)
				}
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}
