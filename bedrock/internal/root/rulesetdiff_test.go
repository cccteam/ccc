package root

import (
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/github"
)

// TestRulesetDiffStoredDefaults: the ruleset GitHub stores carries parameters the
// declaration never set (required_reviewers, dismissal_restriction and the like, filled in
// with defaults); they do not count, while a declared value that differs, a rule missing
// or a rule beyond the declaration does.
func TestRulesetDiffStoredDefaults(t *testing.T) {
	t.Parallel()

	want := &github.Ruleset{
		Name: rulesetName, Target: github.RulesetTargetBranch, Enforcement: github.RulesetActive,
		BypassActors: []github.BypassActor{{ActorID: 5635255, ActorType: github.BypassIntegration, BypassMode: github.BypassAlways}},
		Conditions:   &github.RulesetConditions{RefName: github.RefNameCondition{Include: []string{github.DefaultBranchPattern}, Exclude: []string{}}},
		Rules: []github.Rule{
			{Type: ruleDeletion},
			{Type: ruleNonFastForward},
			{Type: rulePullRequest, Parameters: map[string]any{
				"required_approving_review_count": float64(1), "dismiss_stale_reviews_on_push": true, "require_code_owner_review": false,
				"require_last_push_approval": true, "required_review_thread_resolution": false, "allowed_merge_methods": []any{"squash"},
			}},
			{Type: ruleRequiredChecks, Parameters: map[string]any{
				"strict_required_status_checks_policy": true, "do_not_enforce_on_create": false,
				"required_status_checks": []any{map[string]any{"context": "imr-root-uc1-plan (imr-root-gbl-core-54b1)", "integration_id": float64(10529)}},
			}},
		},
	}
	stored := func() *github.Ruleset {
		got := *want
		got.ID = 26660448
		got.Rules = []github.Rule{
			{Type: ruleDeletion},
			{Type: ruleNonFastForward},
			{Type: rulePullRequest, Parameters: map[string]any{
				"required_approving_review_count": float64(1), "dismiss_stale_reviews_on_push": true, "required_reviewers": []any{},
				"require_code_owner_review": false, "dismissal_restriction": map[string]any{"enabled": false, "allowed_actors": []any{}},
				"require_last_push_approval": true, "required_review_thread_resolution": false,
				"require_extra_approval_for_unattributed_changes": true, "allowed_merge_methods": []any{"squash"},
			}},
			{Type: ruleRequiredChecks, Parameters: map[string]any{
				"strict_required_status_checks_policy": true, "do_not_enforce_on_create": false,
				"required_status_checks": []any{map[string]any{"context": "imr-root-uc1-plan (imr-root-gbl-core-54b1)", "integration_id": float64(10529)}},
			}},
		}

		return &got
	}
	if diff := rulesetDiff(stored(), want); diff != "" {
		t.Errorf("rulesetDiff(stored, want) = %q, want nothing: GitHub's defaults do not count", diff)
	}
	fewer := stored()
	fewer.Rules[2].Parameters["required_approving_review_count"] = float64(0)
	if diff := rulesetDiff(fewer, want); diff != "rules" {
		t.Errorf("rulesetDiff(fewer reviews, want) = %q, want rules", diff)
	}
	extra := stored()
	extra.Rules = append(extra.Rules, github.Rule{Type: "required_signatures"})
	if diff := rulesetDiff(extra, want); diff != "rules" {
		t.Errorf("rulesetDiff(a rule beyond the declaration, want) = %q, want rules", diff)
	}
	missing := stored()
	missing.Rules = missing.Rules[1:]
	if diff := rulesetDiff(missing, want); diff != "rules" {
		t.Errorf("rulesetDiff(a rule missing, want) = %q, want rules", diff)
	}
}
