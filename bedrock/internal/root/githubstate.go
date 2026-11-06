// githubstate.go is the root organization's repository and its GitHub organization's
// settings as the engine keeps them: the repository's merge settings and the ruleset on
// its default branch, the organization's larger runner, its group and the variable that
// names it. Read and written with the infrastructure app's token, or a person's.

package root

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/github"
)

// githubSection brings the repository and the organization's Actions settings to the
// placement, or notes that GitHub was not read.
func (e *engine) githubSection(ctx context.Context) error {
	if e.gh == nil {
		e.note("GitHub was not checked: no token (GITHUB_TOKEN, or the infrastructure app's in a trigger's run)")

		return nil
	}
	owner, repo := e.p.GithubOrganization, e.p.SourceRepo
	if _, err := e.gh.Repository(ctx, owner, repo); err != nil {
		if github.NotFound(err) {
			e.note(fmt.Sprintf("the repository %s/%s does not exist yet: the bootstrap makes it, and its rules wait", owner, repo))

			return nil
		}

		return errors.Wrapf(err, "reading %s/%s", owner, repo)
	}
	if err := e.repositorySettings(ctx, owner, repo); err != nil {
		return err
	}
	if err := e.ruleset(ctx, owner, repo); err != nil {
		return err
	}

	return e.runner(ctx)
}

// repositorySettings brings the merge settings to squash alone, titled and bodied from
// the pull request, the head branch deleted, the branch update offered.
func (e *engine) repositorySettings(ctx context.Context, owner, repo string) error {
	got, err := e.gh.RepositorySettings(ctx, owner, repo)
	if err != nil {
		return errors.Wrapf(err, "reading the settings of %s/%s", owner, repo)
	}
	want := e.d.RepositorySettings()
	if cmp.Equal(got, want) {
		return nil
	}

	return e.change(ctx, sectionGitHub, "sets the merge settings of "+owner+"/"+repo+": squash alone, titled and described from the pull request, the branch deleted on merge, the branch update offered", func(ctx context.Context) error {
		return e.gh.UpdateRepositorySettings(ctx, owner, repo, want)
	})
}

// ruleset makes the default branch's ruleset or brings it to the placement.
func (e *engine) ruleset(ctx context.Context, owner, repo string) error {
	cloudBuild, err := e.gh.AppBySlug(ctx, cloudBuildAppSlug)
	if err != nil {
		return errors.Wrapf(err, "reading Google's Cloud Build app (%s)", cloudBuildAppSlug)
	}
	var infrastructure int64
	if id := e.p.GithubInfrastructureAppID; id != "" {
		if infrastructure, err = strconv.ParseInt(id, 10, 64); err != nil {
			return errors.Newf("githubInfrastructureAppId %q is not a number", id)
		}
	}
	want := e.d.Ruleset(cloudBuild.ID, infrastructure)
	listed, err := e.gh.Rulesets(ctx, owner, repo)
	if err != nil {
		return errors.Wrapf(err, "listing the rulesets of %s/%s", owner, repo)
	}
	i := slices.IndexFunc(listed, func(rs github.Ruleset) bool { return rs.Name == rulesetName })
	if i < 0 {
		return e.change(ctx, sectionGitHub, fmt.Sprintf("rules the default branch of %s/%s: pull requests alone with %d approving review(s), squash merges, the plan trigger's build required, the infrastructure app bypassing", owner, repo, e.p.ApprovingReviewCount()), func(ctx context.Context) error {
			_, err := e.gh.CreateRuleset(ctx, owner, repo, want)

			return err
		})
	}
	got, err := e.gh.Ruleset(ctx, owner, repo, listed[i].ID)
	if err != nil {
		return errors.Wrapf(err, "reading the ruleset %s of %s/%s", rulesetName, owner, repo)
	}
	want.ID = got.ID
	if diff := rulesetDiff(got, want); diff != "" {
		return e.change(ctx, sectionGitHub, "brings the default branch's ruleset of "+owner+"/"+repo+" to the placement: "+diff, func(ctx context.Context) error {
			return e.gh.UpdateRuleset(ctx, owner, repo, got.ID, want)
		})
	}

	return nil
}

// rulesetDiff says what differs between the ruleset that stands and the one wanted:
// the bypass actors, the conditions or the rules, empty for nothing.
func rulesetDiff(got, want *github.Ruleset) string {
	var parts []string
	if got.Enforcement != want.Enforcement || got.Target != want.Target {
		parts = append(parts, "enforcement")
	}
	if !cmp.Equal(got.BypassActors, want.BypassActors, cmp.Transformer("empty", emptyActors)) {
		parts = append(parts, "bypass actors")
	}
	if !cmp.Equal(got.Conditions, want.Conditions, cmp.Transformer("empty", emptySlice)) {
		parts = append(parts, "branches")
	}
	if rulesDiffer(got.Rules, want.Rules) {
		parts = append(parts, "rules")
	}

	return joined(parts)
}

// rulesDiffer compares the rules that stand with the ones wanted, by type, so that order
// does not count: a rule wanted and missing, a rule standing that is not wanted, or a
// parameter declared whose value differs. A parameter GitHub adds beyond the declaration
// (required_reviewers, dismissal_restriction and the like, which it fills in with
// defaults on every ruleset it stores; the lab, 2026-11-06) does not count.
func rulesDiffer(got, want []github.Rule) bool {
	g, w := ruleMap(got), ruleMap(want)
	if len(g) != len(w) {
		return true
	}
	for kind, wanted := range w {
		standing, ok := g[kind]
		if !ok {
			return true
		}
		for key, value := range wanted {
			if !cmp.Equal(standing[key], value) {
				return true
			}
		}
	}

	return false
}

func emptyActors(a []github.BypassActor) []github.BypassActor {
	if a == nil {
		return []github.BypassActor{}
	}

	return a
}

// ruleMap keys the rules by type with their parameters, for rulesDiffer.
func ruleMap(rules []github.Rule) map[string]map[string]any {
	m := map[string]map[string]any{}
	for _, r := range rules {
		params := map[string]any{}
		for k, v := range r.Parameters {
			params[k] = v
		}
		m[r.Type] = params
	}

	return m
}

// runner brings the organization's larger runner to the placement: the variable that
// names it, and when the placement sizes it, the group and the runner.
func (e *engine) runner(ctx context.Context) error {
	org := e.p.GithubOrganization
	want := e.d.RunnerVariable()
	got, err := e.gh.OrgVariable(ctx, org, runnerVariable)
	if err != nil && !github.NotFound(err) {
		return errors.Wrapf(err, "reading the %s variable of %s", runnerVariable, org)
	}
	switch {
	case want == nil && got != nil:
		if err := e.change(ctx, sectionGitHub, "removes the organization variable "+runnerVariable+" (the applications' CI runs on GitHub's standard runner)", func(ctx context.Context) error {
			return e.gh.DeleteOrgVariable(ctx, org, runnerVariable)
		}); err != nil {
			return err
		}
	case want != nil && got == nil:
		if err := e.change(ctx, sectionGitHub, "sets the organization variable "+runnerVariable+" to "+want.Value, func(ctx context.Context) error {
			return e.gh.CreateOrgVariable(ctx, org, *want)
		}); err != nil {
			return err
		}
	case want != nil && (got.Value != want.Value || got.Visibility != want.Visibility):
		if err := e.change(ctx, sectionGitHub, "sets the organization variable "+runnerVariable+" to "+want.Value+" (was "+got.Value+")", func(ctx context.Context) error {
			return e.gh.UpdateOrgVariable(ctx, org, *want)
		}); err != nil {
			return err
		}
	}
	hosted := e.d.HostedRunner()
	if hosted == nil {
		return nil
	}
	groups, err := e.gh.RunnerGroups(ctx, org)
	if err != nil {
		return errors.Wrapf(err, "listing the runner groups of %s", org)
	}
	var groupID int64
	if i := slices.IndexFunc(groups, func(g github.RunnerGroup) bool { return g.Name == runnerGroupName }); i >= 0 {
		groupID = groups[i].ID
	} else {
		if err := e.change(ctx, sectionGitHub, "makes the runner group "+runnerGroupName+", open to every repository", func(ctx context.Context) error {
			g, err := e.gh.CreateRunnerGroup(ctx, org, github.RunnerGroup{Name: runnerGroupName, Visibility: github.VisibilityAll})
			if err == nil {
				groupID = g.ID
			}

			return err
		}); err != nil {
			return err
		}
	}
	hosted.RunnerGroupID = groupID
	runners, err := e.gh.HostedRunners(ctx, org)
	if err != nil {
		return errors.Wrapf(err, "listing the hosted runners of %s", org)
	}
	i := slices.IndexFunc(runners, func(r github.HostedRunner) bool { return r.Name == hosted.Name })
	if i < 0 {
		return e.change(ctx, sectionGitHub, fmt.Sprintf("makes the larger runner %s (%s, at most %d at once) in the group %s", hosted.Name, hosted.Size, hosted.MaximumRunners, runnerGroupName), func(ctx context.Context) error {
			_, err := e.gh.CreateHostedRunner(ctx, org, *hosted)

			return err
		})
	}
	got2 := runners[i]
	if got2.MachineSize != nil && got2.MachineSize.ID != hosted.Size {
		e.finding(fmt.Sprintf("the larger runner %s is a %s, and placement.json sizes it %s (ciLargeRunnerSize): a runner's size cannot change in place; delete the runner on GitHub and let the apply make it again", hosted.Name, got2.MachineSize.ID, hosted.Size))
	}
	if groupID != 0 && (got2.RunnerGroupID != groupID || got2.MaximumRunners != hosted.MaximumRunners) {
		return e.change(ctx, sectionGitHub, fmt.Sprintf("moves the larger runner %s into the group %s with at most %d at once", hosted.Name, runnerGroupName, hosted.MaximumRunners), func(ctx context.Context) error {
			return e.gh.UpdateHostedRunner(ctx, org, got2.ID, groupID, hosted.MaximumRunners)
		})
	}

	return nil
}
