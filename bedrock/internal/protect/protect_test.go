package protect_test

import (
	"slices"
	"strconv"
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
			name: "a matching ruleset is left alone, even with its patterns and rules in another order",
			existing: []github.Ruleset{{
				Name: protect.TagRuleset, Target: "tag", Enforcement: "active",
				BypassActors: []github.BypassActor{{ActorID: releaseApp, ActorType: "Integration", BypassMode: "always"}},
				Conditions:   github.Conditions{RefName: github.RefName{Include: []string{"refs/tags/*/v*", "refs/tags/v*"}, Exclude: []string{}}},
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
			name: "the admin bypass is removed",
			existing: []github.Ruleset{{
				Name: protect.TagRuleset, Target: "tag", Enforcement: "active",
				BypassActors: []github.BypassActor{{ActorID: 5, ActorType: "RepositoryRole", BypassMode: "always"}, {ActorID: releaseApp, ActorType: "Integration", BypassMode: "always"}},
				Conditions:   github.Conditions{RefName: github.RefName{Include: []string{"refs/tags/v*", "refs/tags/*/v*"}, Exclude: []string{}}},
				Rules:        []github.Rule{{Type: "creation"}, {Type: "update"}, {Type: "deletion"}, {Type: "non_fast_forward"}},
			}},
			req:  protect.Request{Owner: "acme", Repo: "quill", DefaultBranch: "master", ReleaseApp: "acme-release"},
			want: []string{"release tags=updated", "master branch=created", "hotfix branches=created"},
		},
		{
			name: "a tag ruleset over v* alone is updated to cover component tags too",
			existing: []github.Ruleset{{
				Name: protect.TagRuleset, Target: "tag", Enforcement: "active",
				BypassActors: []github.BypassActor{{ActorID: releaseApp, ActorType: "Integration", BypassMode: "always"}},
				Conditions:   github.Conditions{RefName: github.RefName{Include: []string{"refs/tags/v*"}, Exclude: []string{}}},
				Rules:        []github.Rule{{Type: "creation"}, {Type: "update"}, {Type: "deletion"}, {Type: "non_fast_forward"}},
			}},
			req:  protect.Request{Owner: "acme", Repo: "quill", DefaultBranch: "master", ReleaseApp: "acme-release"},
			want: []string{"release tags=updated", "master branch=created", "hotfix branches=created"},
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
				if found.Enforcement != "active" || found.Target != want.Target || len(found.Rules) != len(want.Rules) {
					t.Errorf("ruleset %q after Apply() = %+v, want %+v", want.Name, *found, want)
				}
				if !slices.Equal(actors(found.BypassActors), actors(want.BypassActors)) {
					t.Errorf("ruleset %q bypass actors after Apply() = %v, want %v", want.Name, actors(found.BypassActors), actors(want.BypassActors))
				}
				if !slices.Equal(sorted(found.Conditions.RefName.Include), sorted(want.Conditions.RefName.Include)) {
					t.Errorf("ruleset %q includes after Apply() = %v, want %v", want.Name, found.Conditions.RefName.Include, want.Conditions.RefName.Include)
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

func TestRulesets(t *testing.T) {
	t.Parallel()

	const releaseApp = int64(5080645)
	tests := []struct {
		name string
		// ruleset is the name of the ruleset under test.
		ruleset     string
		wantTarget  string
		wantActors  []string
		wantInclude []string
		wantRules   []string
	}{
		{
			name:        "the release app alone bypasses the tag ruleset, over both tag patterns",
			ruleset:     protect.TagRuleset,
			wantTarget:  "tag",
			wantActors:  []string{"Integration/5080645/always"},
			wantInclude: []string{"refs/tags/*/v*", "refs/tags/v*"},
			wantRules:   []string{"creation", "deletion", "non_fast_forward", "update"},
		},
		{
			name:        "no one bypasses the default branch ruleset",
			ruleset:     "master branch",
			wantTarget:  "branch",
			wantActors:  []string{},
			wantInclude: []string{"refs/heads/master"},
			wantRules:   []string{"deletion", "non_fast_forward", "pull_request"},
		},
		{
			name:        "no one bypasses the hotfix ruleset",
			ruleset:     protect.HotfixRuleset,
			wantTarget:  "branch",
			wantActors:  []string{},
			wantInclude: []string{"refs/heads/hotfix/**"},
			wantRules:   []string{"deletion", "non_fast_forward", "pull_request"},
		},
	}
	rulesets := protect.Rulesets("master", releaseApp)
	if len(rulesets) != len(tests) {
		t.Fatalf("Rulesets() returned %d rulesets, want %d", len(rulesets), len(tests))
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			i := slices.IndexFunc(rulesets, func(rs github.Ruleset) bool {
				return rs.Name == tt.ruleset
			})
			if i < 0 {
				t.Fatalf("Rulesets() has no ruleset %q", tt.ruleset)
			}
			rs := rulesets[i]
			if rs.Target != tt.wantTarget || rs.Enforcement != "active" {
				t.Errorf("ruleset %q target, enforcement = %s, %s, want %s, active", tt.ruleset, rs.Target, rs.Enforcement, tt.wantTarget)
			}
			if got := actors(rs.BypassActors); !slices.Equal(got, tt.wantActors) {
				t.Errorf("ruleset %q bypass actors = %v, want %v", tt.ruleset, got, tt.wantActors)
			}
			if got := sorted(rs.Conditions.RefName.Include); !slices.Equal(got, tt.wantInclude) {
				t.Errorf("ruleset %q includes = %v, want %v", tt.ruleset, got, tt.wantInclude)
			}
			if len(rs.Conditions.RefName.Exclude) != 0 {
				t.Errorf("ruleset %q excludes = %v, want none", tt.ruleset, rs.Conditions.RefName.Exclude)
			}
			rules := make([]string, 0, len(rs.Rules))
			for _, rule := range rs.Rules {
				rules = append(rules, rule.Type)
			}
			if got := sorted(rules); !slices.Equal(got, tt.wantRules) {
				t.Errorf("ruleset %q rules = %v, want %v", tt.ruleset, got, tt.wantRules)
			}
		})
	}
}

// actors is each bypass actor as type/ID/mode, sorted.
func actors(list []github.BypassActor) []string {
	out := make([]string, 0, len(list))
	for _, actor := range list {
		out = append(out, actor.ActorType+"/"+strconv.FormatInt(actor.ActorID, 10)+"/"+actor.BypassMode)
	}

	return sorted(out)
}

// sorted is a sorted copy of the list.
func sorted(list []string) []string {
	out := slices.Clone(list)
	slices.Sort(out)

	return out
}
