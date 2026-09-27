package github

import (
	"context"
	"net/http"
)

// Release is a GitHub Release: the tag it names and who cut it.
type Release struct {
	TagName string `json:"tag_name"`
	Author  User   `json:"author"`
}

// User is an account, by its login.
type User struct {
	Login string `json:"login"`
}

// Release reads the release a tag belongs to; a tag without one is NotFound.
func (c *Client) Release(ctx context.Context, owner, repo, tag string) (*Release, error) {
	r := &Release{}
	if err := c.do(ctx, http.MethodGet, "/repos/"+owner+"/"+repo+"/releases/tags/"+tag, nil, r); err != nil {
		return nil, err
	}

	return r, nil
}
