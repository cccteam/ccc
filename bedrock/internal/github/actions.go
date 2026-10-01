package github

import (
	"context"
	"net/http"
	"net/url"
)

// User is the login the client's token belongs to.
func (c *Client) User(ctx context.Context) (string, error) {
	var user struct {
		Login string `json:"login"`
	}
	if err := c.do(ctx, http.MethodGet, "/user", nil, &user); err != nil {
		return "", err
	}

	return user.Login, nil
}

// DispatchWorkflow starts the workflow file's workflow_dispatch event on ref, with the
// inputs the workflow declares.
func (c *Client) DispatchWorkflow(ctx context.Context, owner, repo, file, ref string, inputs map[string]string) error {
	body := map[string]any{"ref": ref, "inputs": inputs}

	return c.do(ctx, http.MethodPost, "/repos/"+owner+"/"+repo+"/actions/workflows/"+url.PathEscape(file)+"/dispatches", body, nil)
}

// Environment is a deployment environment of a repository, as the API describes it.
type Environment struct {
	Name string `json:"name"`
	// DeploymentBranchPolicy says which branches may deploy to it: nil for any.
	DeploymentBranchPolicy *BranchPolicySetting `json:"deployment_branch_policy,omitempty"`
}

// BranchPolicySetting is an environment's branch restriction: the branches with
// protection rules, or the custom policies the environment lists.
type BranchPolicySetting struct {
	ProtectedBranches    bool `json:"protected_branches"`
	CustomBranchPolicies bool `json:"custom_branch_policies"`
}

// BranchPolicy is one custom branch policy of an environment: a branch name pattern.
type BranchPolicy struct {
	ID   int64  `json:"id,omitempty"`
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// Environment reads the deployment environment; an absent one is NotFound.
func (c *Client) Environment(ctx context.Context, owner, repo, name string) (*Environment, error) {
	e := &Environment{}
	if err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/environments/"+url.PathEscape(name), nil, e); err != nil {
		return nil, err
	}

	return e, nil
}

// PutEnvironment creates the deployment environment, or updates the one that exists,
// with the branch restriction.
func (c *Client) PutEnvironment(ctx context.Context, owner, repo string, e *Environment) error {
	body := map[string]any{"deployment_branch_policy": e.DeploymentBranchPolicy}

	return c.do(ctx, http.MethodPut, "/repos/"+owner+"/"+repo+"/environments/"+url.PathEscape(e.Name), body, nil)
}

// DeploymentBranchPolicies lists the environment's custom branch policies.
func (c *Client) DeploymentBranchPolicies(ctx context.Context, owner, repo, environment string) ([]BranchPolicy, error) {
	var answer struct {
		Policies []BranchPolicy `json:"branch_policies"`
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/environments/"+url.PathEscape(environment)+"/deployment-branch-policies", nil, &answer); err != nil {
		return nil, err
	}

	return answer.Policies, nil
}

// CreateDeploymentBranchPolicy adds a custom branch policy to the environment.
func (c *Client) CreateDeploymentBranchPolicy(ctx context.Context, owner, repo, environment string, p *BranchPolicy) (*BranchPolicy, error) {
	created := &BranchPolicy{}
	if err := c.do(ctx, http.MethodPost, "/repos/"+owner+"/"+repo+"/environments/"+url.PathEscape(environment)+"/deployment-branch-policies", map[string]string{"name": p.Name, "type": "branch"}, created); err != nil {
		return nil, err
	}

	return created, nil
}
