// Package protect puts the repository rules on an application repository: release tags
// are made only by the release app, and the default branch and the hotfix branches
// change only by pull request. It creates the rulesets when they are missing and
// updates them when they drifted, and it leaves rulesets it does not own alone.
package protect

import (
	"context"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
)

const (
	// TagRuleset covers the release tags.
	TagRuleset = "release tags"
	// TagPattern is the release tags: v followed by anything.
	TagPattern = "refs/tags/v*"
	// HotfixRuleset covers the hotfix branches.
	HotfixRuleset = "hotfix branches"
	// HotfixPattern is the hotfix branches: hotfix/<major>.<minor>.x, one per release line.
	HotfixPattern = "refs/heads/hotfix/**"
	// adminRole is the repository role ID of admin, the one role that may bypass the
	// tag ruleset beside the release app.
	adminRole = 5
	// Created is the action an Outcome reports when the ruleset was made.
	Created = "created"
	// Updated is the action when the ruleset differed and was replaced.
	Updated = "updated"
	// Unchanged is the action when the ruleset already matched.
	Unchanged = "unchanged"

	targetTag        = "tag"
	targetBranch     = "branch"
	enforcementOn    = "active"
	actorIntegration = "Integration"
	actorRole        = "RepositoryRole"
	bypassAlways     = "always"
	rulePullRequest  = "pull_request"
	reviewCountKey   = "required_approving_review_count"
)

// Request names the repository and what its rules follow.
type Request struct {
	// Owner and Repo name the repository on GitHub; Owner is the organization the
	// release app is installed on.
	Owner string
	Repo  string
	// DefaultBranch is the branch releases are cut from.
	DefaultBranch string
	// ReleaseApp is the slug of the GitHub App that cuts releases (release-please runs
	// as it); it alone, with the repository's admins, may create a release tag.
	ReleaseApp string
}

// Outcome is what happened to one ruleset.
type Outcome struct {
	Name   string
	Action string
	ID     int64
	// What the ruleset enforces, for the report.
	Effect string
}

// Result is what Apply did.
type Result struct {
	ReleaseAppID int64
	Rulesets     []Outcome
}

// Apply puts the three rulesets on the repository, creating or updating each by name.
func Apply(ctx context.Context, client *github.Client, req Request) (*Result, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	appID, err := releaseAppID(ctx, client, req.Owner, req.ReleaseApp)
	if err != nil {
		return nil, err
	}
	existing, err := client.Rulesets(ctx, req.Owner, req.Repo)
	if err != nil {
		return nil, errors.Wrap(err, "listing the repository's rulesets")
	}
	byName := make(map[string]int64, len(existing))
	for i := range existing {
		byName[existing[i].Name] = existing[i].ID
	}
	result := &Result{ReleaseAppID: appID}
	wanted := Rulesets(req.DefaultBranch, appID)
	for i := range wanted {
		outcome, err := applyOne(ctx, client, req, byName, &wanted[i])
		if err != nil {
			return nil, err
		}
		result.Rulesets = append(result.Rulesets, outcome)
	}

	return result, nil
}

func (req Request) validate() error {
	for name, value := range map[string]string{"owner": req.Owner, "repository": req.Repo, "default branch": req.DefaultBranch, "release app": req.ReleaseApp} {
		if strings.TrimSpace(value) == "" {
			return errors.Newf("the %s is empty", name)
		}
	}

	return nil
}

// releaseAppID is the app ID of the release app, found among the organization's
// installations by its slug.
func releaseAppID(ctx context.Context, client *github.Client, org, slug string) (int64, error) {
	installations, err := client.Installations(ctx, org)
	if err != nil {
		return 0, errors.Wrapf(err, "listing the apps installed on %s (the token must be an organization admin's)", org)
	}
	for _, inst := range installations {
		if inst.AppSlug == slug {
			return inst.AppID, nil
		}
	}
	slugs := make([]string, 0, len(installations))
	for _, inst := range installations {
		slugs = append(slugs, inst.AppSlug)
	}
	slices.Sort(slugs)

	return 0, errors.Newf("the release app %s is not installed on %s (installed: %s): install it on the organization, then rerun", slug, org, strings.Join(slugs, ", "))
}

// applyOne creates the ruleset when no ruleset carries its name, updates it when the
// one that does differs, and leaves it when it matches.
func applyOne(ctx context.Context, client *github.Client, req Request, byName map[string]int64, want *github.Ruleset) (Outcome, error) {
	outcome := Outcome{Name: want.Name, Effect: effect(want)}
	id, exists := byName[want.Name]
	if !exists {
		created, err := client.CreateRuleset(ctx, req.Owner, req.Repo, want)
		if err != nil {
			return outcome, errors.Wrapf(err, "creating ruleset %q", want.Name)
		}
		outcome.Action, outcome.ID = Created, created.ID

		return outcome, nil
	}
	current, err := client.Ruleset(ctx, req.Owner, req.Repo, id)
	if err != nil {
		return outcome, errors.Wrapf(err, "reading ruleset %q", want.Name)
	}
	outcome.ID = id
	if equivalent(current, want) {
		outcome.Action = Unchanged

		return outcome, nil
	}
	want.ID = id
	if _, err := client.UpdateRuleset(ctx, req.Owner, req.Repo, want); err != nil {
		return outcome, errors.Wrapf(err, "updating ruleset %q", want.Name)
	}
	outcome.Action = Updated

	return outcome, nil
}

// Rulesets are the three rulesets a repository carries, as bedrock wants them.
func Rulesets(defaultBranch string, releaseAppID int64) []github.Ruleset {
	protected := []github.Rule{
		{Type: rulePullRequest, Parameters: map[string]any{
			reviewCountKey:                      0,
			"dismiss_stale_reviews_on_push":     false,
			"require_code_owner_review":         false,
			"require_last_push_approval":        false,
			"required_review_thread_resolution": false,
		}},
		{Type: "deletion"},
		{Type: "non_fast_forward"},
	}

	return []github.Ruleset{
		{
			Name:        TagRuleset,
			Target:      targetTag,
			Enforcement: enforcementOn,
			BypassActors: []github.BypassActor{
				{ActorID: releaseAppID, ActorType: actorIntegration, BypassMode: bypassAlways},
				{ActorID: adminRole, ActorType: actorRole, BypassMode: bypassAlways},
			},
			Conditions: github.Conditions{RefName: github.RefName{Include: []string{TagPattern}, Exclude: []string{}}},
			Rules:      []github.Rule{{Type: "creation"}, {Type: "update"}, {Type: "deletion"}, {Type: "non_fast_forward"}},
		},
		{
			Name:         defaultBranch + " branch",
			Target:       targetBranch,
			Enforcement:  enforcementOn,
			BypassActors: []github.BypassActor{},
			Conditions:   github.Conditions{RefName: github.RefName{Include: []string{"refs/heads/" + defaultBranch}, Exclude: []string{}}},
			Rules:        protected,
		},
		{
			Name:         HotfixRuleset,
			Target:       targetBranch,
			Enforcement:  enforcementOn,
			BypassActors: []github.BypassActor{},
			Conditions:   github.Conditions{RefName: github.RefName{Include: []string{HotfixPattern}, Exclude: []string{}}},
			Rules:        protected,
		},
	}
}

// effect says in a sentence what the ruleset enforces.
func effect(rs *github.Ruleset) string {
	if rs.Target == targetTag {
		return "tags v* are created, moved or deleted only by the release app and repository admins"
	}

	return "changes to " + strings.Join(rs.Conditions.RefName.Include, ", ") + " arrive by pull request; no force push, no deletion"
}

// equivalent reports whether the ruleset on GitHub enforces what bedrock wants: the
// same target and enforcement, the same refs, the same bypass actors, the same rule
// types, and the same review count on the pull-request rule. Parameters GitHub adds
// with their defaults are not held against it.
func equivalent(current, want *github.Ruleset) bool {
	if current.Target != want.Target || current.Enforcement != want.Enforcement {
		return false
	}
	if !sameStrings(current.Conditions.RefName.Include, want.Conditions.RefName.Include) || !sameStrings(current.Conditions.RefName.Exclude, want.Conditions.RefName.Exclude) {
		return false
	}
	if !sameActors(current.BypassActors, want.BypassActors) {
		return false
	}

	return sameRules(current.Rules, want.Rules)
}

func sameStrings(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)

	return slices.Equal(a, b)
}

func sameActors(a, b []github.BypassActor) bool {
	key := func(actor github.BypassActor) string {
		return actor.ActorType + "/" + actor.BypassMode + "/" + itoa(actor.ActorID)
	}
	as := make([]string, 0, len(a))
	for _, actor := range a {
		as = append(as, key(actor))
	}
	bs := make([]string, 0, len(b))
	for _, actor := range b {
		bs = append(bs, key(actor))
	}

	return sameStrings(as, bs)
}

func sameRules(a, b []github.Rule) bool {
	key := func(rule github.Rule) string {
		if rule.Type != rulePullRequest {
			return rule.Type
		}

		return rule.Type + "/" + itoa(reviewCount(rule))
	}
	as := make([]string, 0, len(a))
	for _, rule := range a {
		as = append(as, key(rule))
	}
	bs := make([]string, 0, len(b))
	for _, rule := range b {
		bs = append(bs, key(rule))
	}

	return sameStrings(as, bs)
}

// reviewCount is the pull-request rule's required approvals; JSON numbers decode as
// float64, the literal in Rulesets is an int.
func reviewCount(rule github.Rule) int64 {
	switch n := rule.Parameters[reviewCountKey].(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	default:
		return 0
	}
}

func itoa(n int64) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{digits[n%10]}, out...)
		n /= 10
	}

	return string(out)
}
