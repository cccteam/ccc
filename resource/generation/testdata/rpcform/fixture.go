// Package rpcform holds one struct per Execute shape the RPC extraction
// classifies or refuses: the two accepted forms on either receiver, and each
// departure from them.
package rpcform

import (
	"context"

	"github.com/cccteam/ccc/resource"
)

// Client stands in for the application's RPC client.
type Client struct{}

type (
	// @rpc
	TxnForm struct{}

	// @rpc
	ClientForm struct{}

	// @rpc
	ValueReceiver struct{}

	// @rpc
	NoExecute struct{}

	// @rpc
	TwoParams struct{}

	// @rpc
	Variadic struct{}

	// @rpc
	FirstNotContext struct{}

	// @rpc
	SecondUnknown struct{}

	// @rpc
	ThirdNotPointer struct{}

	// @rpc
	NoResult struct{}

	// @rpc
	ResultNotError struct{}

	// @rpc
	TwoResults struct{}

	// @rpc
	AnswersPointer struct{}

	// @rpc
	AnswersBasic struct{}

	// @rpc
	ThreeResults struct{}

	// Report is the struct the answering methods return.
	Report struct {
		Note string
	}
)

func (*TxnForm) Execute(context.Context, resource.ReadWriteTransaction, *Client) error { return nil }
func (*ClientForm) Execute(context.Context, resource.Client, *Client) error            { return nil }
func (ValueReceiver) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}
func (*TwoParams) Execute(context.Context, resource.ReadWriteTransaction) error { return nil }
func (*Variadic) Execute(context.Context, resource.ReadWriteTransaction, ...*Client) error {
	return nil
}
func (*FirstNotContext) Execute(string, resource.ReadWriteTransaction, *Client) error { return nil }
func (*SecondUnknown) Execute(context.Context, *resource.Client, *Client) error       { return nil }
func (*ThirdNotPointer) Execute(context.Context, resource.ReadWriteTransaction, Client) error {
	return nil
}
func (*NoResult) Execute(context.Context, resource.ReadWriteTransaction, *Client) {}
func (*ResultNotError) Execute(context.Context, resource.ReadWriteTransaction, *Client) string {
	return ""
}
func (*TwoResults) Execute(context.Context, resource.ReadWriteTransaction, *Client) (Report, error) {
	return Report{}, nil
}
func (*AnswersPointer) Execute(context.Context, resource.Client, *Client) (*Report, error) {
	return nil, nil
}
func (*AnswersBasic) Execute(context.Context, resource.ReadWriteTransaction, *Client) (string, error) {
	return "", nil
}
func (*ThreeResults) Execute(context.Context, resource.ReadWriteTransaction, *Client) (Report, int, error) {
	return Report{}, 0, nil
}
