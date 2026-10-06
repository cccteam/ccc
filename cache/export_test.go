package cache

import "io"

// WithWaitOutput directs the line New prints while it waits for the lock to w, so a test
// reads it.
func WithWaitOutput(w io.Writer) Option {
	return func(c *Cache) *Cache {
		c.waitOutput = w

		return c
	}
}
