// pulls.go is what the pipeline's steps say and ask about a pull request: its state, a
// comment on it, the deployments GitHub shows beside it, a directory as a commit holds it,
// and the installation token a GitHub App talks with.

package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// PullRequestState is the pull request's state: open or closed (merged is closed).
func (c *Client) PullRequestState(ctx context.Context, owner, repo string, number int) (string, error) {
	var pr struct {
		State string `json:"state"`
	}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number), nil, &pr); err != nil {
		return "", err
	}

	return pr.State, nil
}

// CreateComment posts a comment on an issue or pull request as the token's account.
func (c *Client) CreateComment(ctx context.Context, owner, repo string, number int, body string) error {
	in := struct {
		Body string `json:"body"`
	}{Body: body}

	return c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number), in, nil)
}

// PullRequest is a pull request as the API answers it: its number, its state (open or
// closed; a merged one is closed with MergedAt set), its page, and its head and base.
type PullRequest struct {
	Number   int            `json:"number"`
	State    string         `json:"state"`
	HTMLURL  string         `json:"html_url"`
	MergedAt string         `json:"merged_at"`
	Title    string         `json:"title"`
	Head     PullRequestRef `json:"head"`
	Base     PullRequestRef `json:"base"`
}

// PullRequestRef is a pull request's head or base: the branch and its commit.
type PullRequestRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// PullRequestRequest is a pull request to open: its title, its body, the head branch
// (a branch of the repository) and the base branch it goes into.
type PullRequestRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"`
	Base  string `json:"base"`
}

// CreatePullRequest opens a pull request as the token's account.
func (c *Client) CreatePullRequest(ctx context.Context, owner, repo string, in PullRequestRequest) (*PullRequest, error) {
	pr := &PullRequest{}
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/%s/pulls", owner, repo), in, pr); err != nil {
		return nil, err
	}

	return pr, nil
}

// PullRequestsFrom lists the pull requests whose head is the branch, open and closed,
// newest first.
func (c *Client) PullRequestsFrom(ctx context.Context, owner, repo, branch string) ([]PullRequest, error) {
	var prs []PullRequest
	q := url.Values{"head": {owner + ":" + branch}, "state": {"all"}, "per_page": {"100"}}
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/%s/pulls?%s", owner, repo, q.Encode()), nil, &prs); err != nil {
		return nil, err
	}

	return prs, nil
}

// DirEntry is one entry of a directory as a commit holds it: its name, its git object
// SHA (for a file, the blob's, which git hash-object computes from the content) and its
// type (file, dir, symlink, submodule).
type DirEntry struct {
	Name string `json:"name"`
	SHA  string `json:"sha"`
	Type string `json:"type"`
}

// Directory lists the directory at path as it is at ref. A path that is not there is
// the API's 404 (NotFound reports it); a path that names a file is refused.
func (c *Client) Directory(ctx context.Context, owner, repo, path, ref string) ([]DirEntry, error) {
	var entries []DirEntry
	if err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/contents/"+path+"?ref="+url.QueryEscape(ref), nil, &entries); err != nil {
		return nil, err
	}

	return entries, nil
}

// DeploymentRequest is a deployment to create: the commit it deploys and the environment
// it goes to. A transient environment is one that goes away (a pull request's).
type DeploymentRequest struct {
	Ref                  string   `json:"ref"`
	Environment          string   `json:"environment"`
	Description          string   `json:"description"`
	TransientEnvironment bool     `json:"transient_environment"`
	AutoMerge            bool     `json:"auto_merge"`
	RequiredContexts     []string `json:"required_contexts"`
}

// DeploymentStatus is a deployment's state (success, inactive, failure) with where the
// environment is served and where the build's log is. AutoInactive marks the earlier
// deployments of the environment inactive when this one succeeds.
type DeploymentStatus struct {
	State          string `json:"state"`
	EnvironmentURL string `json:"environment_url,omitempty"`
	LogURL         string `json:"log_url,omitempty"`
	Description    string `json:"description,omitempty"`
	AutoInactive   bool   `json:"auto_inactive,omitempty"`
}

// Deployments lists the ids of the environment's deployments, newest first, up to a
// hundred.
func (c *Client) Deployments(ctx context.Context, owner, repo, environment string) ([]int64, error) {
	var deployments []struct {
		ID int64 `json:"id"`
	}
	path := fmt.Sprintf("/repos/%s/%s/deployments?environment=%s&per_page=%d", owner, repo, url.QueryEscape(environment), perPage)
	if err := c.do(ctx, http.MethodGet, path, nil, &deployments); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(deployments))
	for _, d := range deployments {
		ids = append(ids, d.ID)
	}

	return ids, nil
}

// CreateDeployment creates the deployment and answers its id.
func (c *Client) CreateDeployment(ctx context.Context, owner, repo string, d *DeploymentRequest) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/repos/"+owner+"/"+repo+"/deployments", d, &out); err != nil {
		return 0, err
	}

	return out.ID, nil
}

// CreateDeploymentStatus adds a status to the deployment.
func (c *Client) CreateDeploymentStatus(ctx context.Context, owner, repo string, id int64, s *DeploymentStatus) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/%s/deployments/%d/statuses", owner, repo, id), s, nil)
}

// RepositoryInstallation is the id of the GitHub App's installation on the repository.
// The client's token is the app's JWT, not an installation token.
func (c *Client) RepositoryInstallation(ctx context.Context, owner, repo string) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/installation", nil, &out); err != nil {
		return 0, err
	}

	return out.ID, nil
}

// InstallationToken mints a token for the installation, good for an hour. The client's
// token is the app's JWT.
func (c *Client) InstallationToken(ctx context.Context, installation int64) (string, error) {
	var out struct {
		Token string `json:"token"`
	}
	if err := c.do(ctx, http.MethodPost, fmt.Sprintf("/app/installations/%d/access_tokens", installation), nil, &out); err != nil {
		return "", err
	}

	return out.Token, nil
}
