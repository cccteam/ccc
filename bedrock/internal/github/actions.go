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
