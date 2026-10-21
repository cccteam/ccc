package github

import (
	"context"
	"time"
)

// SetWait substitutes the client's waiting between a failed read and its retry, so that a
// test does not wait and sees each wait asked for.
func (c *Client) SetWait(wait func(ctx context.Context, d time.Duration) error) {
	c.wait = wait
}
