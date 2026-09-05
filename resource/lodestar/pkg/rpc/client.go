// Package rpc defines Lodestar's RPC methods, wiring them up to implementation code.
// The generator uses this package to generate handlers for RPC endpoints. The methods
// are the only writers of workflow state: the two StatusId columns are structurally
// unwritable from the wire (@state), so every mission and refit transition is an
// Execute-gated method whose declared @transition owns edge legality — and only edge
// legality. What each role may do in each state, and which rows a role may move at
// all, is conditional grants (cmd/bootstrap/demo_access.json), never code here.
package rpc

// Client carries application dependencies into RPC method implementations.
type Client struct{}

// NewClient constructs a Client.
func NewClient() *Client {
	return &Client{}
}
