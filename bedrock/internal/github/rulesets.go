// rulesets.go is a repository's rulesets, the rules on its branches: what the root
// organization's apply writes on its own repository's default branch, and reads back to
// say what would change.

package github

import (
	"context"
	"fmt"
	"net/http"
)

// Ruleset is one ruleset of a repository, as the API describes it in full (a listing
// carries the name and the enforcement alone; Ruleset reads one whole).
type Ruleset struct {
	ID          int64  `json:"id,omitempty"`
	Name        string `json:"name"`
	Target      string `json:"target"`
	Enforcement string `json:"enforcement"`
	// BypassActors are who the rules do not hold for: an app (Integration) by its App
	// ID, a team or a role, each with when the bypass applies (always, pull_request).
	BypassActors []BypassActor `json:"bypass_actors"`
	// Conditions name the branches the ruleset rules.
	Conditions *RulesetConditions `json:"conditions,omitempty"`
	Rules      []Rule             `json:"rules"`
}

// BypassActor is one actor the rules do not hold for.
type BypassActor struct {
	ActorID    int64  `json:"actor_id"`
	ActorType  string `json:"actor_type"`
	BypassMode string `json:"bypass_mode"`
}

// RulesetConditions are the ruleset's conditions: the branches, by name pattern
// (~DEFAULT_BRANCH, refs/heads/release/*).
type RulesetConditions struct {
	RefName RefNameCondition `json:"ref_name"`
}

// RefNameCondition includes and excludes branches by pattern.
type RefNameCondition struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

// Rule is one rule of a ruleset: its type (deletion, non_fast_forward, pull_request,
// required_status_checks) and the parameters the type takes, none for the first two.
type Rule struct {
	Type       string         `json:"type"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// The words the API uses for a ruleset on branches, in force, and an app as a bypass
// actor that always bypasses.
const (
	RulesetTargetBranch  = "branch"
	RulesetActive        = "active"
	BypassIntegration    = "Integration"
	BypassAlways         = "always"
	DefaultBranchPattern = "~DEFAULT_BRANCH"
)

// Rulesets lists the repository's own rulesets, each with its id, name and enforcement;
// the rules are read one ruleset at a time (Ruleset).
func (c *Client) Rulesets(ctx context.Context, owner, repo string) ([]Ruleset, error) {
	var out []Ruleset
	if err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/rulesets?includes_parents=false&per_page=100", nil, &out); err != nil {
		return nil, err
	}

	return out, nil
}

// Ruleset reads one ruleset whole: its actors, conditions and rules.
func (c *Client) Ruleset(ctx context.Context, owner, repo string, id int64) (*Ruleset, error) {
	out := &Ruleset{}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/rulesets/%d", owner, repo, id), nil, out); err != nil {
		return nil, err
	}

	return out, nil
}

// CreateRuleset makes the ruleset on the repository and answers it with its id.
func (c *Client) CreateRuleset(ctx context.Context, owner, repo string, in *Ruleset) (*Ruleset, error) {
	out := &Ruleset{}
	if err := c.do(ctx, http.MethodPost, "/repos/"+owner+"/"+repo+"/rulesets", in, out); err != nil {
		return nil, err
	}

	return out, nil
}

// UpdateRuleset replaces the ruleset with the id by what in says.
func (c *Client) UpdateRuleset(ctx context.Context, owner, repo string, id int64, in *Ruleset) error {
	return c.do(ctx, http.MethodPut, fmt.Sprintf("/repos/%s/%s/rulesets/%d", owner, repo, id), in, nil)
}

// AppBySlug reads a public app by its slug (github.com/apps/<slug>): Google's Cloud Build
// app, whose id a required check names as its reporter.
func (c *Client) AppBySlug(ctx context.Context, slug string) (*App, error) {
	out := &App{}
	if err := c.do(ctx, http.MethodGet, "/apps/"+slug, nil, out); err != nil {
		return nil, err
	}

	return out, nil
}

// OrgInstallation is one app installed on an organization, as the organization's
// listing describes it: the installation's id, the app's id and slug.
type OrgInstallation struct {
	ID                  int64  `json:"id"`
	AppID               int64  `json:"app_id"`
	AppSlug             string `json:"app_slug"`
	RepositorySelection string `json:"repository_selection"`
}

// OrgInstallations lists the apps installed on the organization. The token needs the
// organization's administration read: an owner's, or an app's with the Administration
// organization permission.
func (c *Client) OrgInstallations(ctx context.Context, org string) ([]OrgInstallation, error) {
	var out struct {
		Installations []OrgInstallation `json:"installations"`
	}
	if err := c.do(ctx, http.MethodGet, "/orgs/"+org+"/installations?per_page=100", nil, &out); err != nil {
		return nil, err
	}

	return out.Installations, nil
}
