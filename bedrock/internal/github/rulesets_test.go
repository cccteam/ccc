package github_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

// TestRulesets: a ruleset is made whole, listed by name, read back whole and replaced;
// the repository's merge settings read back as set.
func TestRulesets(t *testing.T) {
	t.Parallel()

	srv := githubtest.New(t)
	srv.AddRepo("acme", "acme-infrastructure", &githubtest.Repo{})
	c := srv.Client()
	ctx := context.Background()
	want := &github.Ruleset{
		Name:         "default branch",
		Target:       github.RulesetTargetBranch,
		Enforcement:  github.RulesetActive,
		BypassActors: []github.BypassActor{{ActorID: 5177065, ActorType: github.BypassIntegration, BypassMode: github.BypassAlways}},
		Conditions:   &github.RulesetConditions{RefName: github.RefNameCondition{Include: []string{github.DefaultBranchPattern}, Exclude: []string{}}},
		Rules: []github.Rule{
			{Type: "deletion"},
			{Type: "non_fast_forward"},
			{Type: "pull_request", Parameters: map[string]any{"required_approving_review_count": float64(1), "allowed_merge_methods": []any{"squash"}}},
		},
	}
	made, err := c.CreateRuleset(ctx, "acme", "acme-infrastructure", want)
	if err != nil {
		t.Fatalf("CreateRuleset() error = %v", err)
	}
	if made.ID == 0 {
		t.Fatal("CreateRuleset() gave no id")
	}
	listed, err := c.Rulesets(ctx, "acme", "acme-infrastructure")
	if err != nil {
		t.Fatalf("Rulesets() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != made.ID || listed[0].Name != "default branch" || listed[0].Rules != nil {
		t.Errorf("Rulesets() = %+v, want the summary of the one made", listed)
	}
	whole, err := c.Ruleset(ctx, "acme", "acme-infrastructure", made.ID)
	if err != nil {
		t.Fatalf("Ruleset() error = %v", err)
	}
	want.ID = made.ID
	if diff := cmp.Diff(want, whole); diff != "" {
		t.Errorf("Ruleset() mismatch (-want +got):\n%s", diff)
	}
	want.Rules = want.Rules[:2]
	if err := c.UpdateRuleset(ctx, "acme", "acme-infrastructure", made.ID, want); err != nil {
		t.Fatalf("UpdateRuleset() error = %v", err)
	}
	whole, err = c.Ruleset(ctx, "acme", "acme-infrastructure", made.ID)
	if err != nil {
		t.Fatalf("Ruleset() after the update error = %v", err)
	}
	if len(whole.Rules) != 2 {
		t.Errorf("Ruleset() after the update has %d rules, want 2", len(whole.Rules))
	}
	if _, err := c.Ruleset(ctx, "acme", "acme-infrastructure", 404); !github.NotFound(err) {
		t.Errorf("an unknown ruleset: error = %v, want not found", err)
	}

	squash, title := true, "PR_TITLE"
	if err := c.UpdateRepositorySettings(ctx, "acme", "acme-infrastructure", &github.RepositorySettings{AllowSquashMerge: &squash, SquashMergeCommitTitle: &title}); err != nil {
		t.Fatalf("UpdateRepositorySettings() error = %v", err)
	}
	settings, err := c.RepositorySettings(ctx, "acme", "acme-infrastructure")
	if err != nil {
		t.Fatalf("RepositorySettings() error = %v", err)
	}
	if settings.AllowSquashMerge == nil || !*settings.AllowSquashMerge || settings.SquashMergeCommitTitle == nil || *settings.SquashMergeCommitTitle != title || settings.AllowMergeCommit != nil {
		t.Errorf("RepositorySettings() = %+v", settings)
	}
}

// TestOrgActions: an organization variable is made, read, changed and removed; a runner
// group and a hosted runner are made and listed; the hosted runner moves groups.
func TestOrgActions(t *testing.T) {
	t.Parallel()

	srv := githubtest.New(t)
	c := srv.Client()
	ctx := context.Background()
	if _, err := c.OrgVariable(ctx, "acme", "CI_LARGE_RUNNER"); !github.NotFound(err) {
		t.Errorf("an absent variable: error = %v, want not found", err)
	}
	if err := c.CreateOrgVariable(ctx, "acme", github.Variable{Name: "CI_LARGE_RUNNER", Value: "ci-large", Visibility: github.VisibilityAll}); err != nil {
		t.Fatalf("CreateOrgVariable() error = %v", err)
	}
	v, err := c.OrgVariable(ctx, "acme", "CI_LARGE_RUNNER")
	if err != nil || v.Value != "ci-large" || v.Visibility != github.VisibilityAll {
		t.Fatalf("OrgVariable() = %+v, %v", v, err)
	}
	if err := c.UpdateOrgVariable(ctx, "acme", github.Variable{Name: "CI_LARGE_RUNNER", Value: "ci-larger", Visibility: github.VisibilityAll}); err != nil {
		t.Fatalf("UpdateOrgVariable() error = %v", err)
	}
	if v, _ := c.OrgVariable(ctx, "acme", "CI_LARGE_RUNNER"); v.Value != "ci-larger" {
		t.Errorf("after the update, value = %q", v.Value)
	}
	if err := c.DeleteOrgVariable(ctx, "acme", "CI_LARGE_RUNNER"); err != nil {
		t.Fatalf("DeleteOrgVariable() error = %v", err)
	}
	if _, err := c.OrgVariable(ctx, "acme", "CI_LARGE_RUNNER"); !github.NotFound(err) {
		t.Errorf("a removed variable: error = %v, want not found", err)
	}

	group, err := c.CreateRunnerGroup(ctx, "acme", github.RunnerGroup{Name: "applications", Visibility: github.VisibilityAll})
	if err != nil || group.ID == 0 {
		t.Fatalf("CreateRunnerGroup() = %+v, %v", group, err)
	}
	groups, err := c.RunnerGroups(ctx, "acme")
	if err != nil || len(groups) != 1 || groups[0].Name != "applications" {
		t.Fatalf("RunnerGroups() = %+v, %v", groups, err)
	}
	runner, err := c.CreateHostedRunner(ctx, "acme", github.HostedRunnerRequest{Name: "ci-large", Image: github.RunnerImage{ID: "2306", Source: "github"}, Size: "4-core", RunnerGroupID: group.ID, MaximumRunners: 3})
	if err != nil || runner.ID == 0 || runner.MachineSize == nil || runner.MachineSize.ID != "4-core" {
		t.Fatalf("CreateHostedRunner() = %+v, %v", runner, err)
	}
	if err := c.UpdateHostedRunner(ctx, "acme", runner.ID, group.ID, 5); err != nil {
		t.Fatalf("UpdateHostedRunner() error = %v", err)
	}
	runners, err := c.HostedRunners(ctx, "acme")
	if err != nil || len(runners) != 1 || runners[0].MaximumRunners != 5 || runners[0].RunnerGroupID != group.ID {
		t.Fatalf("HostedRunners() = %+v, %v", runners, err)
	}

	srv.PublicApps["google-cloud-build"] = github.App{ID: 10529, Slug: "google-cloud-build", Name: "Google Cloud Build"}
	app, err := c.AppBySlug(ctx, "google-cloud-build")
	if err != nil || app.ID != 10529 {
		t.Fatalf("AppBySlug() = %+v, %v", app, err)
	}
	srv.OrgInstallations["acme"] = []github.OrgInstallation{{ID: 77, AppID: 10529, AppSlug: "google-cloud-build", RepositorySelection: github.AllRepositories}}
	installations, err := c.OrgInstallations(ctx, "acme")
	if err != nil || len(installations) != 1 || installations[0].ID != 77 {
		t.Fatalf("OrgInstallations() = %+v, %v", installations, err)
	}
}
