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
