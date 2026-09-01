// Package rpc defines RPC Methods, wiring them up to implementation code. The generator
// uses this package to generate handlers for RPC endpoints. The waystation's methods
// are the only writers of workflow state: StatusId columns are structurally unwritable
// from the wire (@state), so every transition is an Execute-gated method whose body
// enforces edge legality — and only edge legality; what each role may do in each state
// is conditional grants, never code here.
package rpc

// Client carries application dependencies into RPC method implementations.
type Client struct{}

// NewClient constructs a Client.
func NewClient() *Client {
	return &Client{}
}
