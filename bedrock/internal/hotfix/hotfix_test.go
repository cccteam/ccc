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
		// wantTip is the commit the branch is created at: the tag's, or the manifest
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
			name:         "master has cut the line's next patch already: the branch starts with the manifest at it, so the hotfix is the one after",
			later:        []string{"v0.1.22"},
			tag:          "v0.1.21",
			want:         hotfix.Result{Tag: "v0.1.21", Commit: "c21", Branch: "hotfix/0.1.x", Latest: "v0.9.0", Skipped: "v0.1.22", Next: "v0.1.23"},
			wantManifest: "{\n  \".\": \"0.1.22\"\n}\n",
			wantMessage:  "chore(hotfix): the 0.1 line continues after v0.1.22, which master has already cut",
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
				t.Fatal("branch at the tag's commit, want a manifest commit on it")
			}
			tree := repo.Trees[branch.SHA]
			if manifest := repo.Files[tree+":"+hotfix.ManifestFile]; manifest != tt.wantManifest {
				t.Errorf("manifest on the branch = %q, want %q", manifest, tt.wantManifest)
			}
			if len(server.Messages) != 1 || server.Messages[0] != tt.wantMessage {
				t.Errorf("commit messages = %q, want [%q]", server.Messages, tt.wantMessage)
			}
			if ancestry := repo.Ancestry[branch.SHA]; len(ancestry) < 2 || ancestry[1] != tt.want.Commit {
				t.Errorf("the manifest commit's parent is not %s: ancestry %v", tt.want.Commit, ancestry)
			}
		})
	}
}
