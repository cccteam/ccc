// Package rpc defines RPC Methods, wiring them up to implementation code. The generator
// uses this package to generate handlers for RPC endpoints.
package rpc

// Client carries application dependencies into RPC method implementations. The starport
// has none; the type exists to exercise the generated RPC wiring.
type Client struct{}

// NewClient constructs a Client.
func NewClient() *Client {
	return &Client{}
}
