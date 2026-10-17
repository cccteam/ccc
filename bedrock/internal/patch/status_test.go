package patch

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

// lineRepo is a repository whose master (m3) carries v0.1.0 at m1 and v0.2.0 at m3, with
// the patch line 0.1.x at p2 carrying v0.1.1 (p1) and v0.1.2 (p2), neither on master.
func lineRepo() *githubtest.Repo {
	return &githubtest.Repo{
		Refs: map[string]github.Object{
			"refs/heads/master":      {Type: "commit", SHA: "m3"},
			"refs/heads/patch/0.1.x": {Type: "commit", SHA: "p2"},
			"refs/tags/v0.1.0":       {Type: "commit", SHA: "m1"},
			"refs/tags/v0.2.0":       {Type: "commit", SHA: "m3"},
			"refs/tags/v0.1.1":       {Type: "commit", SHA: "p1"},
			"refs/tags/v0.1.2":       {Type: "commit", SHA: "p2"},
		},
		Ancestry:  map[string][]string{"m1": {"m1"}, "m2": {"m2", "m1"}, "m3": {"m3", "m2", "m1"}, "p1": {"p1", "m1"}, "p2": {"p2", "p1", "m1"}, "p3": {"p3", "p2", "p1", "m1"}},
		MergeBase: map[string]string{"m3 p1": "m1", "m3 p2": "m1", "m3 p3": "m1"},
	}
}

func TestMergeBacks(t *testing.T) {
	t.Parallel()

	merged := github.PullRequest{Number: 12, State: "closed", MergedAt: "2026-10-15T20:00:00Z", HTMLURL: "https://github.com/acme/quill/pull/12", Head: github.PullRequestRef{Ref: "merge-back/v0.1.2"}}
	open := github.PullRequest{Number: 13, State: "open", HTMLURL: "https://github.com/acme/quill/pull/13", Head: github.PullRequestRef{Ref: "merge-back/v0.1.2"}}
	older := github.PullRequest{Number: 11, State: "closed", MergedAt: "2026-10-14T20:00:00Z", HTMLURL: "https://github.com/acme/quill/pull/11", Head: github.PullRequestRef{Ref: "merge-back/v0.1.1"}}
	tests := []struct {
		name    string
		prepare func(repo *githubtest.Repo)
		want    []MergeBack
	}{
		{
			name: "every release on the default branch: nothing owed",
			prepare: func(repo *githubtest.Repo) {
				delete(repo.Refs, "refs/tags/v0.1.1")
				delete(repo.Refs, "refs/tags/v0.1.2")
			},
			want: []MergeBack{},
		},
		{
			name:    "the line's newest release has no merge-back pull request",
			prepare: func(*githubtest.Repo) {},
			want:    []MergeBack{{Line: "patch/0.1.x", Tag: "v0.1.2"}},
		},
		{
			name: "an older release merged back does not cover the newest",
			prepare: func(repo *githubtest.Repo) {
				repo.PullRequests = []github.PullRequest{older}
			},
			want: []MergeBack{{Line: "patch/0.1.x", Tag: "v0.1.2"}},
		},
		{
			name: "the newest release's merge-back is open, not merged",
			prepare: func(repo *githubtest.Repo) {
				repo.PullRequests = []github.PullRequest{open}
			},
			want: []MergeBack{{Line: "patch/0.1.x", Tag: "v0.1.2", Number: 13, URL: "https://github.com/acme/quill/pull/13"}},
		},
		{
			name: "the newest release merged back covers the line",
			prepare: func(repo *githubtest.Repo) {
				repo.PullRequests = []github.PullRequest{open, merged}
			},
			want: []MergeBack{{Line: "patch/0.1.x", Tag: "v0.1.2", Number: 12, URL: "https://github.com/acme/quill/pull/12", Merged: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := githubtest.New(t)
			repo := server.AddRepo("acme", "quill", lineRepo())
			tt.prepare(repo)
			got, err := MergeBacks(t.Context(), server.Client(), "acme", "quill", "master")
			if err != nil {
				t.Fatalf("MergeBacks() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("MergeBacks() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		release string
		prepare func(repo *githubtest.Repo)
		want    *LineState
		wantErr string
	}{
		{
			name:    "the line at its newest release: nothing pending",
			release: "v0.1.0",
			prepare: func(*githubtest.Repo) {},
			want:    &LineState{Line: "patch/0.1.x", Exists: true, Tip: "p2", Tag: "v0.1.2", TagCommit: "p2"},
		},
		{
			name:    "the line past its newest release: a patch release is pending",
			release: "v0.1.1",
			prepare: func(repo *githubtest.Repo) {
				repo.Refs["refs/heads/patch/0.1.x"] = github.Object{Type: "commit", SHA: "p3"}
			},
			want: &LineState{Line: "patch/0.1.x", Exists: true, Tip: "p3", Tag: "v0.1.2", TagCommit: "p2", Pending: true},
		},
		{
			name:    "a line's branch that does not exist",
			release: "v0.2.0",
			prepare: func(*githubtest.Repo) {},
			want:    &LineState{Line: "patch/0.2.x"},
		},
		{
			name:    "a release that is not a release tag is refused",
			release: "0.2",
			prepare: func(*githubtest.Repo) {},
			wantErr: "is not a release tag",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := githubtest.New(t)
			repo := server.AddRepo("acme", "quill", lineRepo())
			tt.prepare(repo)
			got, err := State(t.Context(), server.Client(), "acme", "quill", tt.release)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("State() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("State() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("State() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
