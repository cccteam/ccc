package hotfix_test

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
	"github.com/cccteam/ccc/bedrock/internal/hotfix"
)

func TestBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tag     string
		want    string
		wantErr string
	}{
		{name: "a patch release", tag: "v0.1.21", want: "hotfix/0.1.x"},
		{name: "a minor release", tag: "v2.0.0", want: "hotfix/2.0.x"},
		{name: "no v", tag: "0.1.21", wantErr: `tag "0.1.21" is not a release tag`},
		{name: "a prerelease", tag: "v0.1.21-rc1", wantErr: "is not a release tag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := hotfix.Branch(tt.tag)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Branch() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil || got != tt.want {
				t.Errorf("Branch() = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

// quill is a repository whose master carries v0.1.20 (c20), v0.1.21 (c21) and, when
// the test says so, v0.1.22 (c22) and v0.2.0 (c30); master's tip is the last of them.
// A side branch holds c99, tagged v0.9.0, off c20.
func quill(t *testing.T, server *githubtest.Server, later ...string) *githubtest.Repo {
	t.Helper()

	repo := server.AddRepo("acme", "quill", &githubtest.Repo{
		Refs: map[string]github.Object{
			"refs/tags/v0.1.20": {Type: "commit", SHA: "c20"},
			"refs/tags/v0.1.21": {Type: "tag", SHA: "t21"},
			"refs/tags/v0.9.0":  {Type: "commit", SHA: "c99"},
			"refs/heads/side":   {Type: "commit", SHA: "c99"},
		},
		TagObjects: map[string]string{"t21": "c21"},
		Ancestry: map[string][]string{
			"c20": {"c20"}, "c21": {"c21", "c20"}, "c22": {"c22", "c21", "c20"}, "c30": {"c30", "c22", "c21", "c20"},
			"c99": {"c99", "c20"},
		},
		MergeBase: map[string]string{"c21 c99": "c20"},
		Trees:     map[string]string{"c21": "tree21"},
		Files:     map[string]string{"tree21:.release-please-manifest.json": "{\n  \".\": \"0.1.21\"\n}\n"},
	})
	tip := "c21"
	for _, tag := range later {
		switch tag {
		case "v0.1.22":
			repo.Refs["refs/tags/v0.1.22"] = github.Object{Type: "commit", SHA: "c22"}
			tip = "c22"
		case "v0.2.0":
			repo.Refs["refs/tags/v0.2.0"] = github.Object{Type: "commit", SHA: "c30"}
			tip = "c30"
		}
	}
	repo.Refs["refs/heads/master"] = github.Object{Type: "commit", SHA: tip}

	return repo
}

func TestStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// later are the releases master has cut after v0.1.21.
		later []string
		// existing puts the line's branch in place at the commit.
		existing string
		tag      string
		want     hotfix.Result
		// wantTip is the commit the branch is created at: the tag's, or the continuation
		// commit made through the API (a name the stand-in makes up).
		wantTip        string
		wantManifest   string
		wantMessage    string
		wantErr        string
		wantNoCreation bool
	}{
		{
			name: "the first hotfix of the latest line",
			tag:  "v0.1.21",
			want: hotfix.Result{Tag: "v0.1.21", Commit: "c21", Branch: "hotfix/0.1.x", Latest: "v0.9.0", Next: "v0.1.22"},
		},
		{
			name:  "master has moved to the next minor: the line's next patch, nothing to tell release-please",
			later: []string{"v0.2.0"},
			tag:   "v0.1.21",
			want:  hotfix.Result{Tag: "v0.1.21", Commit: "c21", Branch: "hotfix/0.1.x", Latest: "v0.9.0", Next: "v0.1.22"},
		},
		{
			name:         "master has cut the line's next patch already: the branch starts at a commit naming the one after, the manifest unchanged",
			later:        []string{"v0.1.22"},
			tag:          "v0.1.21",
			want:         hotfix.Result{Tag: "v0.1.21", Commit: "c21", Branch: "hotfix/0.1.x", Latest: "v0.9.0", Skipped: "v0.1.22", Next: "v0.1.23"},
			wantManifest: "{\n  \".\": \"0.1.21\"\n}\n",
			wantMessage:  "chore(hotfix): the 0.1 line continues after v0.1.22, which master has already cut\n\nRelease-As: 0.1.23",
		},
		{
			name:     "an existing line is reported, not recreated",
			existing: "c21",
			tag:      "v0.1.21",
			want:     hotfix.Result{Tag: "v0.1.21", Commit: "c21", Branch: "hotfix/0.1.x", Latest: "v0.9.0", Existed: true, BranchCommit: "c21"},
		},
		{
			name:    "a release off the default branch is refused",
			tag:     "v0.9.0",
			wantErr: "tag v0.9.0 (commit c99) is not on master (diverged): a hotfix line starts from a release on the default branch",
		},
		{
			name:    "an absent tag is refused",
			tag:     "v0.1.5",
			wantErr: "no tag v0.1.5 in acme/quill: a hotfix line starts from a release that exists",
		},
		{
			name:    "a tag of the wrong shape is refused before anything is looked up",
			tag:     "0.1.21",
			wantErr: `tag "0.1.21" is not a release tag`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := githubtest.New(t)
			repo := quill(t, server, tt.later...)
			if tt.existing != "" {
				repo.Refs["refs/heads/hotfix/0.1.x"] = github.Object{Type: "commit", SHA: tt.existing}
			}
			req := hotfix.Request{Owner: "acme", Repo: "quill", DefaultBranch: "master", Tag: tt.tag}
			got, err := hotfix.Start(t.Context(), server.Client(), req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Start() error = %v, wantErr %q", err, tt.wantErr)
				}
				if _, created := repo.Refs["refs/heads/hotfix/0.1.x"]; created && tt.existing == "" {
					t.Error("Start() created the branch although it refused")
				}

				return
			}
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			if *got != tt.want {
				t.Errorf("Start() = %+v, want %+v", *got, tt.want)
			}
			branch, ok := repo.Refs["refs/heads/hotfix/0.1.x"]
			if !ok {
				t.Fatal("no branch after Start()")
			}
			if tt.want.Existed {
				if branch.SHA != tt.existing {
					t.Errorf("branch moved to %s", branch.SHA)
				}

				return
			}
			if tt.wantManifest == "" {
				if branch.SHA != tt.want.Commit {
					t.Errorf("branch at %s, want the tag's commit %s", branch.SHA, tt.want.Commit)
				}

				return
			}
			if branch.SHA == tt.want.Commit {
				t.Fatal("branch at the tag's commit, want a continuation commit on it")
			}
			tree := repo.Trees[branch.SHA]
			if manifest := repo.Files[tree+":"+hotfix.ManifestFile]; manifest != tt.wantManifest {
				t.Errorf("manifest on the branch = %q, want %q", manifest, tt.wantManifest)
			}
			if len(server.Messages) != 1 || server.Messages[0] != tt.wantMessage {
				t.Errorf("commit messages = %q, want [%q]", server.Messages, tt.wantMessage)
			}
			if ancestry := repo.Ancestry[branch.SHA]; len(ancestry) < 2 || ancestry[1] != tt.want.Commit {
				t.Errorf("the continuation commit's parent is not %s: ancestry %v", tt.want.Commit, ancestry)
			}
		})
	}
}

// quillLine adds the hotfix line of 0.1 to quill: hotfix/0.1.x at h1, a fix commit on
// v0.1.21 (c21), released as v0.1.22 with notes; master has moved on to v0.2.0 (c30),
// so the line and master have diverged at c21.
func quillLine(t *testing.T, server *githubtest.Server) *githubtest.Repo {
	t.Helper()

	repo := quill(t, server, "v0.2.0")
	repo.Refs["refs/heads/hotfix/0.1.x"] = github.Object{Type: "commit", SHA: "h1"}
	repo.Refs["refs/tags/v0.1.22"] = github.Object{Type: "commit", SHA: "h1"}
	repo.Ancestry["h1"] = []string{"h1", "c21", "c20"}
	repo.MergeBase["c30 h1"] = "c21"
	repo.Releases = map[string]github.Release{"v0.1.22": {TagName: "v0.1.22", Body: "### Bug Fixes\n\n* the fix"}}

	return repo
}

func TestMerge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// prepare adjusts the repository before the run.
		prepare func(repo *githubtest.Repo)
		tag     string
		want    hotfix.MergeResult
		// wantPulls is the number of pull requests after the run, and wantBody the
		// opened one's body.
		wantPulls int
		wantBody  string
		wantErr   string
	}{
		{
			name:      "the branch at the release's commit and the pull request into master",
			tag:       "v0.1.22",
			want:      hotfix.MergeResult{Tag: "v0.1.22", Line: "hotfix/0.1.x", Commit: "h1", Branch: "merge-back/v0.1.22", Number: 1, URL: "https://github.com/acme/quill/pull/1", Title: "fix: v0.1.22"},
			wantPulls: 1,
			wantBody:  "### Bug Fixes\n\n* the fix",
		},
		{
			name: "an existing merge-back branch is kept and gets its pull request",
			prepare: func(repo *githubtest.Repo) {
				repo.Refs["refs/heads/merge-back/v0.1.22"] = github.Object{Type: "commit", SHA: "h1"}
			},
			tag:       "v0.1.22",
			want:      hotfix.MergeResult{Tag: "v0.1.22", Line: "hotfix/0.1.x", Commit: "h1", Branch: "merge-back/v0.1.22", BranchExisted: true, BranchCommit: "h1", Number: 1, URL: "https://github.com/acme/quill/pull/1", Title: "fix: v0.1.22"},
			wantPulls: 1,
			wantBody:  "### Bug Fixes\n\n* the fix",
		},
		{
			name: "an existing pull request is reported, not made again",
			prepare: func(repo *githubtest.Repo) {
				repo.Refs["refs/heads/merge-back/v0.1.22"] = github.Object{Type: "commit", SHA: "h1"}
				repo.PullRequests = []github.PullRequest{{Number: 7, State: "open", HTMLURL: "https://github.com/acme/quill/pull/7", Head: github.PullRequestRef{Ref: "merge-back/v0.1.22", SHA: "h1"}}}
			},
			tag:       "v0.1.22",
			want:      hotfix.MergeResult{Tag: "v0.1.22", Line: "hotfix/0.1.x", Commit: "h1", Branch: "merge-back/v0.1.22", BranchExisted: true, BranchCommit: "h1", Number: 7, URL: "https://github.com/acme/quill/pull/7", Existed: true, Title: "fix: v0.1.22"},
			wantPulls: 1,
		},
		{
			name: "a merged pull request is reported as merged",
			prepare: func(repo *githubtest.Repo) {
				repo.Refs["refs/heads/merge-back/v0.1.22"] = github.Object{Type: "commit", SHA: "h1"}
				repo.PullRequests = []github.PullRequest{{Number: 7, State: "closed", MergedAt: "2026-10-02T01:00:00Z", HTMLURL: "https://github.com/acme/quill/pull/7", Head: github.PullRequestRef{Ref: "merge-back/v0.1.22", SHA: "h1"}}}
			},
			tag:       "v0.1.22",
			want:      hotfix.MergeResult{Tag: "v0.1.22", Line: "hotfix/0.1.x", Commit: "h1", Branch: "merge-back/v0.1.22", BranchExisted: true, BranchCommit: "h1", Number: 7, URL: "https://github.com/acme/quill/pull/7", Existed: true, Merged: true, Title: "fix: v0.1.22"},
			wantPulls: 1,
		},
		{
			name:    "the line's base release is already on master",
			tag:     "v0.1.21",
			wantErr: "tag v0.1.21 (commit c21) is already on master: nothing to merge back",
		},
		{
			name:    "a release without a hotfix line is refused",
			tag:     "v0.9.0",
			wantErr: "no hotfix line hotfix/0.9.x in acme/quill: v0.9.0 is not a release on a hotfix line",
		},
		{
			name: "a release without its GitHub Release is refused",
			prepare: func(repo *githubtest.Repo) {
				delete(repo.Releases, "v0.1.22")
			},
			tag:     "v0.1.22",
			wantErr: "no release for tag v0.1.22 in acme/quill: the merge-back carries the release's notes",
		},
		{
			name:    "an absent tag is refused",
			tag:     "v0.1.23",
			wantErr: "no tag v0.1.23 in acme/quill: a merge-back brings a release that exists",
		},
		{
			name:    "a tag of the wrong shape is refused before anything is looked up",
			tag:     "0.1.22",
			wantErr: `tag "0.1.22" is not a release tag`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := githubtest.New(t)
			repo := quillLine(t, server)
			if tt.prepare != nil {
				tt.prepare(repo)
			}
			req := hotfix.MergeRequest{Owner: "acme", Repo: "quill", DefaultBranch: "master", Tag: tt.tag}
			got, err := hotfix.Merge(t.Context(), server.Client(), req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Merge() error = %v, wantErr %q", err, tt.wantErr)
				}
				if _, created := repo.Refs["refs/heads/merge-back/"+tt.tag]; created || len(repo.PullRequests) != 0 {
					t.Error("Merge() created a branch or a pull request although it refused")
				}

				return
			}
			if err != nil {
				t.Fatalf("Merge() error = %v", err)
			}
			if *got != tt.want {
				t.Errorf("Merge() = %+v, want %+v", *got, tt.want)
			}
			if branch := repo.Refs["refs/heads/merge-back/v0.1.22"]; branch.SHA != "h1" {
				t.Errorf("merge-back branch at %q, want h1", branch.SHA)
			}
			if len(repo.PullRequests) != tt.wantPulls {
				t.Fatalf("%d pull request(s) after Merge(), want %d", len(repo.PullRequests), tt.wantPulls)
			}
			if tt.wantBody == "" {
				return
			}
			pr := repo.PullRequests[len(repo.PullRequests)-1]
			if pr.Title != tt.want.Title || pr.Head.Ref != tt.want.Branch || pr.Base.Ref != "master" {
				t.Errorf("pull request = %+v, want title %q from %s into master", pr, tt.want.Title, tt.want.Branch)
			}
			if body := server.Bodies[len(server.Bodies)-1]; body != tt.wantBody {
				t.Errorf("pull request body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}
