// comments.go edits a comment on an issue or pull request: bedrock org plan keeps one
// comment per layer's plan current across a pull request's pushes.

package github

import (
	"context"
	"fmt"
	"net/http"
)

// UpdateComment replaces the body of a comment on an issue or pull request, by the
// comment's id, as the token's account.
func (c *Client) UpdateComment(ctx context.Context, owner, repo string, id int64, body string) error {
	in := struct {
		Body string `json:"body"`
	}{Body: body}

	return c.do(ctx, http.MethodPatch, fmt.Sprintf("/repos/%s/%s/issues/comments/%d", owner, repo, id), in, nil)
}
