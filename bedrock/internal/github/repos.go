// repos.go is the repository itself as the API holds it: the settings a command changes
// on the whole repository rather than on what it contains.

package github

import (
	"context"
	"net/http"
)

// Archive marks the repository archived: read-only on GitHub, with its code, pull
// requests and releases kept, until a person unarchives it in the repository's
// settings. GitHub answers the repository as it now stands, which is discarded.
func (c *Client) Archive(ctx context.Context, owner, repo string) error {
	in := struct {
		Archived bool `json:"archived"`
	}{Archived: true}

	return c.do(ctx, http.MethodPatch, "/repos/"+owner+"/"+repo, in, nil)
}

// Repository is a repository as the API describes it: its name, its default branch and
// whether it is private.
type Repository struct {
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	HTMLURL       string `json:"html_url"`
}

// Repository reads the repository; an absent one is NotFound.
func (c *Client) Repository(ctx context.Context, owner, repo string) (*Repository, error) {
	r := &Repository{}
	if err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo, nil, r); err != nil {
		return nil, err
	}

	return r, nil
}

// RepositoryRequest is a repository to create in an organization: private, with the
// default branch named, and with an initial commit (a README) when AutoInit is set, so
// that the git data API has a commit to build on (an empty repository answers it 409).
type RepositoryRequest struct {
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch,omitempty"`
	AutoInit      bool   `json:"auto_init"`
}

// CreateRepository creates the repository in the organization (POST /orgs/{org}/repos;
// the token's app needs the Administration repository permission).
func (c *Client) CreateRepository(ctx context.Context, org string, in RepositoryRequest) (*Repository, error) {
	r := &Repository{}
	if err := c.do(ctx, http.MethodPost, "/orgs/"+org+"/repos", in, r); err != nil {
		return nil, err
	}

	return r, nil
}

// UpdateRef moves the reference (heads/master, without its refs/ prefix) to the commit;
// a fast-forward unless force is set.
func (c *Client) UpdateRef(ctx context.Context, owner, repo, ref, sha string, force bool) error {
	in := struct {
		SHA   string `json:"sha"`
		Force bool   `json:"force"`
	}{SHA: sha, Force: force}

	return c.do(ctx, http.MethodPatch, "/repos/"+owner+"/"+repo+"/git/refs/"+ref, in, nil)
}
