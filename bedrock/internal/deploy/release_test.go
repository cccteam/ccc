package deploy

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

// releaseBuild is a tag build's substitutions in stg, after tst; an override with an
// empty value removes the substitution.
func releaseBuild(t *testing.T, overrides map[string]string) string {
	t.Helper()

	subs := map[string]string{
		tagSub: "v1.2.3", commitSub: "c3", repoFullNameSub: "acme/quill", releaseActorsSub: "release-app[bot],other",
		defaultBranchSub: "master", appSub: "quill", envSub: stgEnvironment, previousEnvSub: tstEnvironment, previousRecordsSub: "tst-records",
	}
	for name, value := range overrides {
		if value == "" {
			delete(subs, name)

			continue
		}
		subs[name] = value
	}

	return buildFor(t, subs)
}

// quillRepo is the repository the release checks read: master at c4 over c3, c2, c1;
// v1.2.3 released at c3 by the release app; hotfix/1.2.x at h1, branched from c3.
func quillRepo() *githubtest.Repo {
	return &githubtest.Repo{
		Refs: map[string]github.Object{
			"refs/heads/master":       {Type: "commit", SHA: "c4"},
			"refs/heads/hotfix/1.2.x": {Type: "commit", SHA: "h1"},
			"refs/tags/v1.2.3":        {Type: "commit", SHA: "c3"},
			"refs/tags/v1.2.4":        {Type: "commit", SHA: "h1"},
			"refs/tags/v1.3.0-rc1":    {Type: "commit", SHA: "h1"},
		},
		Ancestry: map[string][]string{
			"c4": {"c4", "c3", "c2", "c1"}, "c3": {"c3", "c2", "c1"}, "h1": {"h1", "c3", "c2", "c1"}, "h2": {"h2", "h1", "c3", "c2", "c1"},
		},
		MergeBase: map[string]string{"c4 h1": "c3", "c4 h2": "c3"},
		Releases: map[string]github.Release{
			"v1.2.3":     {TagName: "v1.2.3", Author: github.User{Login: "release-app[bot]"}},
			"v1.2.4":     {TagName: "v1.2.4", Author: github.User{Login: "release-app[bot]"}},
			"v1.3.0-rc1": {TagName: "v1.3.0-rc1", Author: github.User{Login: "release-app[bot]"}},
		},
	}
}

func TestValidateRelease(t *testing.T) {
	t.Parallel()

	const (
		connected   = "export GITHUB_TOKEN=\"test-token\"\nexport SKIP_DEPLOY=\"\"\n"
		liveRecord  = `{"app": "quill", "env": "tst", "version": "v1.2.3", "status": "live", "timestamp": "2026-09-27T05:30:00Z", "build": "b-0"}`
		previewOnly = `{"app": "quill", "env": "tst", "version": "v1.2.3", "status": "preview", "timestamp": "2026-09-27T05:00:00Z", "build": "b-9"}`
	)
	live := map[string]string{"gs://tst-records/quill/tst/v1.2.3/b-0.json": liveRecord, "gs://tst-records/quill/tst/v1.2.3/b-9.json": previewOnly}
	tests := []struct {
		name string
		env  string
		subs map[string]string
		// repo changes the repository; objects the previous environment's records.
		repo    func(r *githubtest.Repo)
		objects map[string]string
		wantOut []string
		wantErr string
	}{
		{
			name:    "a pull-request build has no release to validate",
			env:     connected,
			subs:    map[string]string{tagSub: ""},
			wantOut: []string{"Pull-request build: no release to validate."},
		},
		{
			name:    "a torn-down environment does nothing",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			wantOut: []string{tornDown},
		},
		{
			name:    "a release on the default branch, live in the previous environment",
			env:     connected,
			objects: live,
			wantOut: []string{"Release v1.2.3 validated: cut by release-app[bot]", "Tag v1.2.3 validated: its commit is on master", "Gate passed: v1.2.3 is live in tst (since 2026-09-27T05:30:00Z, build b-0)"},
		},
		{
			name: "the branch at the commit is identical",
			env:  connected,
			repo: func(r *githubtest.Repo) {
				r.Refs["refs/heads/master"] = github.Object{Type: "commit", SHA: "c3"}
			},
			objects: live,
			wantOut: []string{"Tag v1.2.3 validated: its commit is on master"},
		},
		{
			name:    "the first environment has no gate",
			env:     connected,
			subs:    map[string]string{previousEnvSub: "", previousRecordsSub: "", envSub: tstEnvironment},
			wantOut: []string{"Tag v1.2.3 validated: its commit is on master"},
		},
		{
			name:    "a hand-submitted build without a token skips the GitHub checks",
			env:     "export GITHUB_TOKEN=\"\"\nexport SKIP_DEPLOY=\"\"\n",
			objects: live,
			wantOut: []string{"Gate passed: v1.2.3 is live in tst"},
		},
		{
			name: "a tag without a release is refused",
			env:  connected,
			repo: func(r *githubtest.Repo) {
				delete(r.Releases, "v1.2.3")
			},
			wantErr: "Build REJECTED: tag v1.2.3 has no GitHub Release (HTTP 404); a release is cut by release-please as the release app, never by a tag alone.",
		},
		{
			name: "a release by someone else is refused",
			env:  connected,
			repo: func(r *githubtest.Repo) {
				r.Releases["v1.2.3"] = github.Release{TagName: "v1.2.3", Author: github.User{Login: "mallory"}}
			},
			wantErr: "Build REJECTED: the GitHub Release for v1.2.3 was made by mallory, not by an accepted release actor (release-app[bot],other).",
		},
		{
			name:    "a hotfix at the tip of its line is validated",
			env:     connected,
			subs:    map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			objects: map[string]string{"gs://tst-records/quill/tst/v1.2.4/b-1.json": strings.Replace(liveRecord, "v1.2.3", "v1.2.4", 1)},
			wantOut: []string{"Tag v1.2.4 validated as a hotfix: the tip of hotfix/1.2.x, branched from release v1.2.3 on master", "Gate passed: v1.2.4 is live in tst"},
		},
		{
			name:    "a tag off the branch that is not a version is refused",
			env:     connected,
			subs:    map[string]string{tagSub: "v1.3.0-rc1", commitSub: "h1"},
			wantErr: "Build REJECTED: tag v1.3.0-rc1 is not on master (compare status: diverged) and is not a v<major>.<minor>.<patch> tag a hotfix line could carry.",
		},
		{
			name: "a hotfix line without a branch is refused",
			env:  connected,
			subs: map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			repo: func(r *githubtest.Repo) {
				delete(r.Refs, "refs/heads/hotfix/1.2.x")
			},
			wantErr: "Build REJECTED: tag v1.2.4 is not on master (compare status: diverged) and there is no branch hotfix/1.2.x it could be a hotfix of.",
		},
		{
			name: "a hotfix tag behind its branch's tip is refused",
			env:  connected,
			subs: map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			repo: func(r *githubtest.Repo) {
				r.Refs["refs/heads/hotfix/1.2.x"] = github.Object{Type: "commit", SHA: "h2"}
			},
			wantErr: "Build REJECTED: tag v1.2.4 (commit h1) is not at the tip of hotfix/1.2.x (h2); a hotfix release is the branch's tip.",
		},
		{
			name: "a hotfix line that starts at no release is refused",
			env:  connected,
			subs: map[string]string{tagSub: "v1.2.4", commitSub: "h1"},
			repo: func(r *githubtest.Repo) {
				delete(r.Refs, "refs/tags/v1.2.3")
			},
			wantErr: "Build REJECTED: hotfix/1.2.x branches from c3 on master, which carries no v1.2.* release tag; a hotfix line starts at a release.",
		},
		{
			name:    "no record in the previous environment is refused",
			env:     connected,
			wantErr: "Build REJECTED: release v1.2.3 has no deployment record in tst (nothing under gs://tst-records/quill/tst/v1.2.3/); a release reaches stg after it is live in tst.",
		},
		{
			name:    "records with none live are refused",
			env:     connected,
			objects: map[string]string{"gs://tst-records/quill/tst/v1.2.3/b-9.json": previewOnly},
			wantErr: "Build REJECTED: release v1.2.3 has deployment records in tst but none is live: traffic never shifted to it there.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := githubtest.New(t)
			repo := quillRepo()
			if tt.repo != nil {
				tt.repo(repo)
			}
			srv.AddRepo("acme", "quill", repo)
			store := &memoryStore{objects: map[string]string{}}
			for path, content := range tt.objects {
				store.objects[path] = content
			}
			clients := &Clients{Storage: store.open, GitHub: func(string) *github.Client {
				return srv.Client()
			}}
			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: releaseBuild(t, tt.subs)})
			var out strings.Builder
			err := ValidateRelease(t.Context(), clients, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ValidateRelease() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out.String())
				}

				return
			}
			if err != nil {
				t.Fatalf("ValidateRelease() error = %v; output:\n%s", err, out.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
		})
	}
}
