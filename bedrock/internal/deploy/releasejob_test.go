package deploy

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
	"github.com/cccteam/ccc/bedrock/internal/release"
)

// patchedRepo is a repository whose master (m3) carries v0.1.0 at m1, with the patch
// line 0.1.x carrying v0.1.1 at p1, which master cannot reach.
func patchedRepo() *githubtest.Repo {
	return &githubtest.Repo{
		Refs: map[string]github.Object{
			"refs/heads/master":      {Type: "commit", SHA: "m3"},
			"refs/heads/patch/0.1.x": {Type: "commit", SHA: "p1"},
			"refs/tags/v0.1.0":       {Type: "commit", SHA: "m1"},
			"refs/tags/v0.1.1":       {Type: "commit", SHA: "p1"},
		},
		Ancestry:  map[string][]string{"m1": {"m1"}, "m3": {"m3", "m1"}, "p1": {"p1", "m1"}},
		MergeBase: map[string]string{"m3 p1": "m1"},
	}
}

func TestRunRelease(t *testing.T) {
	t.Parallel()

	request := func() *ReleaseRequest {
		return &ReleaseRequest{AppID: deployerAppID, KeyVersion: deployerKeyVersion, RepoFullName: "acme/quill", Branch: "master"}
	}
	tests := []struct {
		name string
		req  *ReleaseRequest
		fail map[string]error
		// repo is the repository as GitHub shows it; empty when nil.
		repo    *githubtest.Repo
		wantRan []string
		wantOut []string
		wantErr string
	}{
		{
			name:    "a push to the default branch is refused while a patch release is not merged back, before release-please runs",
			req:     request(),
			repo:    patchedRepo(),
			wantOut: []string{"Build REJECTED: no regular release is cut while a patch release is not merged back; master owes v0.1.1 on patch/0.1.x (no pull request from merge-back/v0.1.1). Bring each back with bedrock patch merge <tag> and merge its pull request, then push master again; a further patch release on the line is allowed meanwhile"},
			wantErr: "Build REJECTED: no regular release is cut while a patch release is not merged back; master owes v0.1.1 on patch/0.1.x (no pull request from merge-back/v0.1.1)",
		},
		{
			name: "an open merge-back pull request is not enough",
			req:  request(),
			repo: func() *githubtest.Repo {
				repo := patchedRepo()
				repo.PullRequests = []github.PullRequest{{Number: 13, State: "open", HTMLURL: "https://github.com/acme/quill/pull/13", Head: github.PullRequestRef{Ref: "merge-back/v0.1.1"}}}

				return repo
			}(),
			wantErr: "master owes v0.1.1 on patch/0.1.x (pull request #13 from merge-back/v0.1.1 is open, not merged)",
		},
		{
			name: "the merge-back merged, the release runs",
			req:  request(),
			repo: func() *githubtest.Repo {
				repo := patchedRepo()
				repo.PullRequests = []github.PullRequest{{Number: 13, State: "closed", MergedAt: "2026-10-15T20:00:00Z", HTMLURL: "https://github.com/acme/quill/pull/13", Head: github.PullRequestRef{Ref: "merge-back/v0.1.1"}}}

				return repo
			}(),
			wantRan: []string{"npx --yes release-please@" + release.PleaseVersion + " github-release --repo-url=acme/quill --target-branch=master --config-file=release-please-config.json --manifest-file=.release-please-manifest.json --token=test-token", "npx --yes release-please@" + release.PleaseVersion + " release-pr --repo-url=acme/quill --target-branch=master --config-file=release-please-config.json --manifest-file=.release-please-manifest.json --token=test-token"},
			wantOut: []string{"Merge-back guard passed: every patch release master does not carry is merged back (v0.1.1 by #13)."},
		},
		{
			name:    "a push to a patch line runs without the guard: a further patch release is allowed meanwhile",
			req:     &ReleaseRequest{AppID: deployerAppID, KeyVersion: deployerKeyVersion, RepoFullName: "acme/quill", Branch: "patch/0.1.x"},
			repo:    patchedRepo(),
			wantRan: []string{"npx --yes release-please@" + release.PleaseVersion + " github-release --repo-url=acme/quill --target-branch=patch/0.1.x --config-file=release-please-config.json --manifest-file=.release-please-manifest.json --token=test-token", "npx --yes release-please@" + release.PleaseVersion + " release-pr --repo-url=acme/quill --target-branch=patch/0.1.x --config-file=release-please-config.json --manifest-file=.release-please-manifest.json --token=test-token"},
		},
		{
			name:    "the token minted, release-please runs github-release then release-pr for the branch",
			req:     request(),
			wantRan: []string{"npx --yes release-please@" + release.PleaseVersion + " github-release --repo-url=acme/quill --target-branch=master --config-file=release-please-config.json --manifest-file=.release-please-manifest.json --token=test-token", "npx --yes release-please@" + release.PleaseVersion + " release-pr --repo-url=acme/quill --target-branch=master --config-file=release-please-config.json --manifest-file=.release-please-manifest.json --token=test-token"},
			wantOut: []string{"Release app " + deployerAppID + ": a token minted for its installation on acme/quill; release-please " + release.PleaseVersion + " runs for master.", "Merge-back guard passed: master carries every release.", "=== release-please github-release on acme/quill (master) ===", "=== release-please release-pr on acme/quill (master) ==="},
		},
		{
			name:    "a patch line is released on its own branch",
			req:     &ReleaseRequest{AppID: deployerAppID, KeyVersion: deployerKeyVersion, RepoFullName: "acme/quill", Branch: "patch/1.2.x"},
			wantRan: []string{"npx --yes release-please@" + release.PleaseVersion + " github-release --repo-url=acme/quill --target-branch=patch/1.2.x --config-file=release-please-config.json --manifest-file=.release-please-manifest.json --token=test-token", "npx --yes release-please@" + release.PleaseVersion + " release-pr --repo-url=acme/quill --target-branch=patch/1.2.x --config-file=release-please-config.json --manifest-file=.release-please-manifest.json --token=test-token"},
		},
		{
			name:    "a failed github-release stops before release-pr",
			req:     request(),
			fail:    map[string]error{"npx --yes": errors.New("npx failed: exit status 1")},
			wantRan: []string{"npx --yes release-please@" + release.PleaseVersion + " github-release --repo-url=acme/quill --target-branch=master --config-file=release-please-config.json --manifest-file=.release-please-manifest.json --token=test-token"},
			wantErr: "release-please github-release",
		},
		{
			name:    "a key version that is not there is refused before anything runs",
			req:     &ReleaseRequest{AppID: deployerAppID, KeyVersion: "projects/p/secrets/release-key/versions/9", RepoFullName: "acme/quill", Branch: "master"},
			wantErr: "the release app's key (projects/p/secrets/release-key/versions/9)",
		},
		{
			name:    "the step's variables are required",
			req:     &ReleaseRequest{AppID: deployerAppID, RepoFullName: "acme/quill", Branch: "master"},
			wantErr: "RELEASE_KEY_SECRET is not set: the release trigger's step passes the release app, its key's pinned version, the repository and the pushed branch to the command",
		},
		{
			name:    "a repository that is not owner/name is refused",
			req:     &ReleaseRequest{AppID: deployerAppID, KeyVersion: deployerKeyVersion, RepoFullName: "quill", Branch: "master"},
			wantErr: `REPO_FULL_NAME "quill" is not <organization>/<repository>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo := tt.repo
			if repo == nil {
				repo = &githubtest.Repo{}
			}
			_, gh := githubStandIn(t, repo)
			run := &fakeRunner{fail: tt.fail}
			clients := &Clients{Secrets: deployerSecrets(t).open, GitHub: gh, Exec: run}
			var out bytes.Buffer
			err := RunRelease(t.Context(), clients, tt.req, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("RunRelease() error = %v, wantErr %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("RunRelease() error = %v; output:\n%s", err, out.String())
			}
			var ran []string
			for _, c := range run.ran {
				ran = append(ran, c.String())
				if len(c.Env) != 0 {
					t.Errorf("%s ran with env %v, want none: release-please reads its token from --token alone", c.String(), c.Env)
				}
			}
			if strings.Join(ran, "\n") != strings.Join(tt.wantRan, "\n") {
				t.Errorf("ran:\n%s\nwant:\n%s", strings.Join(ran, "\n"), strings.Join(tt.wantRan, "\n"))
			}
			containsAll(t, out.String(), tt.wantOut...)
			// The fake runner echoes each command line; the step's own lines carry no token.
			for _, line := range strings.Split(out.String(), "\n") {
				if !strings.HasPrefix(line, "ran ") && strings.Contains(line, "test-token") {
					t.Errorf("output carries the token:\n%s", out.String())
				}
			}
		})
	}
}
