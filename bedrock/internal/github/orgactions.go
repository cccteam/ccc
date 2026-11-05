// orgactions.go is the GitHub organization's own Actions settings: the variable every
// application's CI reads the larger runner's name from, the runner group that holds it,
// and the GitHub-hosted runner itself, which the root organization's apply makes when the
// placement sizes it.

package github

import (
	"context"
	"fmt"
	"net/http"
)

// Variable is an organization's Actions variable.
type Variable struct {
	Name       string `json:"name"`
	Value      string `json:"value"`
	Visibility string `json:"visibility"`
}

// VisibilityAll is a variable's or a runner group's visibility when every repository of
// the organization sees it.
const VisibilityAll = "all"

// OrgVariable reads the organization's variable; an absent one is NotFound.
func (c *Client) OrgVariable(ctx context.Context, org, name string) (*Variable, error) {
	out := &Variable{}
	if err := c.do(ctx, http.MethodGet, "/orgs/"+org+"/actions/variables/"+name, nil, out); err != nil {
		return nil, err
	}

	return out, nil
}

// CreateOrgVariable makes the variable.
func (c *Client) CreateOrgVariable(ctx context.Context, org string, v Variable) error {
	return c.do(ctx, http.MethodPost, "/orgs/"+org+"/actions/variables", v, nil)
}

// UpdateOrgVariable sets the variable's value and visibility.
func (c *Client) UpdateOrgVariable(ctx context.Context, org string, v Variable) error {
	in := struct {
		Value      string `json:"value"`
		Visibility string `json:"visibility"`
	}{Value: v.Value, Visibility: v.Visibility}

	return c.do(ctx, http.MethodPatch, "/orgs/"+org+"/actions/variables/"+v.Name, in, nil)
}

// DeleteOrgVariable removes the variable.
func (c *Client) DeleteOrgVariable(ctx context.Context, org, name string) error {
	return c.do(ctx, http.MethodDelete, "/orgs/"+org+"/actions/variables/"+name, nil, nil)
}

// RunnerGroup is a runner group of the organization.
type RunnerGroup struct {
	ID         int64  `json:"id,omitempty"`
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
	Default    bool   `json:"default,omitempty"`
}

// RunnerGroups lists the organization's runner groups.
func (c *Client) RunnerGroups(ctx context.Context, org string) ([]RunnerGroup, error) {
	var out struct {
		Groups []RunnerGroup `json:"runner_groups"`
	}
	if err := c.do(ctx, http.MethodGet, "/orgs/"+org+"/actions/runner-groups?per_page=100", nil, &out); err != nil {
		return nil, err
	}

	return out.Groups, nil
}

// CreateRunnerGroup makes the group and answers it with its id.
func (c *Client) CreateRunnerGroup(ctx context.Context, org string, in RunnerGroup) (*RunnerGroup, error) {
	out := &RunnerGroup{}
	if err := c.do(ctx, http.MethodPost, "/orgs/"+org+"/actions/runner-groups", in, out); err != nil {
		return nil, err
	}

	return out, nil
}

// HostedRunner is a GitHub-hosted runner of the organization: its image, its machine
// size, its group and how many may run at once.
type HostedRunner struct {
	ID             int64        `json:"id,omitempty"`
	Name           string       `json:"name"`
	RunnerGroupID  int64        `json:"runner_group_id"`
	Image          RunnerImage  `json:"image"`
	MachineSize    *MachineSize `json:"machine_size_details,omitempty"`
	MaximumRunners int          `json:"maximum_runners,omitempty"`
	Status         string       `json:"status,omitempty"`
}

// RunnerImage is a hosted runner's image, by GitHub's id of it and its source.
type RunnerImage struct {
	ID     string `json:"id"`
	Source string `json:"source"`
}

// MachineSize is a hosted runner's size as the listing gives it (its id is the size
// name the creation took: 4-core).
type MachineSize struct {
	ID       string `json:"id"`
	CPUCores int    `json:"cpu_cores"`
}

// HostedRunnerRequest makes a hosted runner: GitHub's image, the machine size (4-core,
// 8-core, as GitHub names them), the group and the ceiling.
type HostedRunnerRequest struct {
	Name           string      `json:"name"`
	Image          RunnerImage `json:"image"`
	Size           string      `json:"size"`
	RunnerGroupID  int64       `json:"runner_group_id"`
	MaximumRunners int         `json:"maximum_runners,omitempty"`
	EnableStaticIP bool        `json:"enable_static_ip"`
}

// HostedRunners lists the organization's GitHub-hosted runners.
func (c *Client) HostedRunners(ctx context.Context, org string) ([]HostedRunner, error) {
	var out struct {
		Runners []HostedRunner `json:"runners"`
	}
	if err := c.do(ctx, http.MethodGet, "/orgs/"+org+"/actions/hosted-runners?per_page=100", nil, &out); err != nil {
		return nil, err
	}

	return out.Runners, nil
}

// CreateHostedRunner makes the runner and answers it with its id.
func (c *Client) CreateHostedRunner(ctx context.Context, org string, in HostedRunnerRequest) (*HostedRunner, error) {
	out := &HostedRunner{}
	if err := c.do(ctx, http.MethodPost, "/orgs/"+org+"/actions/hosted-runners", in, out); err != nil {
		return nil, err
	}

	return out, nil
}

// UpdateHostedRunner moves the runner to the group and sets its ceiling; the size and
// the image cannot change in place.
func (c *Client) UpdateHostedRunner(ctx context.Context, org string, id, group int64, maximum int) error {
	in := struct {
		RunnerGroupID  int64 `json:"runner_group_id"`
		MaximumRunners int   `json:"maximum_runners,omitempty"`
	}{RunnerGroupID: group, MaximumRunners: maximum}

	return c.do(ctx, http.MethodPatch, fmt.Sprintf("/orgs/%s/actions/hosted-runners/%d", org, id), in, nil)
}
