package protect_test

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
	"github.com/cccteam/ccc/bedrock/internal/protect"
)

func TestApply(t *testing.T) {
	t.Parallel()

	const releaseApp = int64(5080645)
	tests := []struct {
		name string
		// existing rulesets on the repository before the run, by name.
		existing []github.Ruleset
		// installed lists the organization's apps; nil means the release app is
		// installed.
		installed []github.Installation
		req       protect.Request
		// want is name=action per ruleset, in order.
		want    []string
		wantErr string
	}{
		{
			name: "a bare repository gets all three",
			req:  protect.Request{Owner: "acme", Repo: "quill", DefaultBranch: "master", ReleaseApp: "acme-release"},
			want: []string{"release tags=created", "master branch=created", "hotfix branches=created"},
		},
		{
			name: "a matching ruleset is left alone, even with its actors in another order",
			existing: []github.Ruleset{{
				Name: protect.TagRuleset, Target: "tag", Enforcement: "active",
				BypassActors: []github.BypassActor{{ActorID: 5, ActorType: "RepositoryRole", BypassMode: "always"}, {ActorID: releaseApp, ActorType: "Integration", BypassMode: "always"}},
				Conditions:   github.Conditions{RefName: github.RefName{Include: []string{"refs/tags/v*"}, Exclude: []string{}}},
				Rules:        []github.Rule{{Type: "non_fast_forward"}, {Type: "deletion"}, {Type: "update", Parameters: map[string]any{"update_allows_fetch_and_merge": false}}, {Type: "creation"}},
			}},
			req:  protect.Request{Owner: "acme", Repo: "quill", DefaultBranch: "master", ReleaseApp: "acme-release"},
			want: []string{"release tags=unchanged", "master branch=created", "hotfix branches=created"},
		},
		{
			name: "a drifted ruleset is updated: the tag ruleset lost the app, the branch ruleset wants a review",
			existing: []github.Ruleset{
				{
					Name: protect.TagRuleset, Target: "tag", Enforcement: "active",
					BypassActors: []github.BypassActor{{ActorID: 5, ActorType: "RepositoryRole", BypassMode: "always"}},
					Conditions:   github.Conditions{RefName: github.RefName{Include: []string{"refs/tags/v*"}, Exclude: []string{}}},
					Rules:        []github.Rule{{Type: "creation"}, {Type: "update"}, {Type: "deletion"}, {Type: "non_fast_forward"}},
				},
				{
					Name: "master branch", Target: "branch", Enforcement: "active",
					BypassActors: []github.BypassActor{},
					Conditions:   github.Conditions{RefName: github.RefName{Include: []string{"refs/heads/master"}, Exclude: []string{}}},
					Rules:        []github.Rule{{Type: "pull_request", Parameters: map[string]any{"required_approving_review_count": 1}}, {Type: "deletion"}, {Type: "non_fast_forward"}},
				},
			},
			req:  protect.Request{Owner: "acme", Repo: "quill", DefaultBranch: "master", ReleaseApp: "acme-release"},
			want: []string{"release tags=updated", "master branch=updated", "hotfix branches=created"},
		},
		{
			name: "a disabled ruleset is updated back to active",
			existing: []github.Ruleset{{
				Name: protect.HotfixRuleset, Target: "branch", Enforcement: "disabled",
				BypassActors: []github.BypassActor{},
				Conditions:   github.Conditions{RefName: github.RefName{Include: []string{"refs/heads/hotfix/**"}, Exclude: []string{}}},
				Rules:        []github.Rule{{Type: "pull_request", Parameters: map[string]any{"required_approving_review_count": 0}}, {Type: "deletion"}, {Type: "non_fast_forward"}},
			}},
			req:  protect.Request{Owner: "acme", Repo: "quill", DefaultBranch: "master", ReleaseApp: "acme-release"},
			want: []string{"release tags=created", "master branch=created", "hotfix branches=updated"},
		},
		{
			name:      "the release app must be installed on the organization",
			installed: []github.Installation{{AppID: 10529, AppSlug: "google-cloud-build"}},
			req:       protect.Request{Owner: "acme", Repo: "quill", DefaultBranch: "master", ReleaseApp: "acme-release"},
			wantErr:   "the release app acme-release is not installed on acme (installed: google-cloud-build): install it on the organization, then rerun",
		},
		{
			name:    "the release app is named",
			req:     protect.Request{Owner: "acme", Repo: "quill", DefaultBranch: "master"},
			wantErr: "the release app is empty",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := githubtest.New(t)
			installed := tt.installed
			if installed == nil {
				installed = []github.Installation{{AppID: releaseApp, AppSlug: "acme-release"}, {AppID: 10529, AppSlug: "google-cloud-build"}}
			}
			server.Installations["acme"] = installed
			repo := server.AddRepo("acme", "quill", &githubtest.Repo{})
			for i := range tt.existing {
				rs := tt.existing[i]
				rs.ID = int64(100 + i)
				repo.Rulesets[rs.ID] = &rs
			}
			result, err := protect.Apply(t.Context(), server.Client(), tt.req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Apply() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			var got []string
			for _, o := range result.Rulesets {
				got = append(got, o.Name+"="+o.Action)
			}
			if strings.Join(got, " ") != strings.Join(tt.want, " ") {
				t.Errorf("Apply() = %v, want %v", got, tt.want)
			}
			if result.ReleaseAppID != releaseApp {
				t.Errorf("ReleaseAppID = %d, want %d", result.ReleaseAppID, releaseApp)
			}
			// Whatever the path, the repository ends up with the three rulesets as wanted.
			for _, want := range protect.Rulesets(tt.req.DefaultBranch, releaseApp) {
				var found *github.Ruleset
				for _, rs := range repo.Rulesets {
					if rs.Name == want.Name {
						found = rs
					}
				}
				if found == nil {
					t.Fatalf("ruleset %q missing after Apply()", want.Name)
				}
				if found.Enforcement != "active" || found.Target != want.Target || len(found.Rules) != len(want.Rules) || len(found.BypassActors) != len(want.BypassActors) {
					t.Errorf("ruleset %q after Apply() = %+v, want %+v", want.Name, *found, want)
				}
			}
			// A second run changes nothing.
			again, err := protect.Apply(t.Context(), server.Client(), tt.req)
			if err != nil {
				t.Fatalf("second Apply() error = %v", err)
			}
			for _, o := range again.Rulesets {
				if o.Action != protect.Unchanged {
					t.Errorf("second Apply(): %s = %s, want unchanged", o.Name, o.Action)
				}
			}
		})
	}
}
