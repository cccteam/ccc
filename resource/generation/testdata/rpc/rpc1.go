package rpc

import (
	"context"

	"github.com/cccteam/ccc/resource"
)

// Client stands in for the application's RPC client.
type Client struct{}

// Apple carries no Execute, so it never classifies as a runner.
type Apple struct{}

type Banana struct{}

func (c *Banana) Execute(ctx context.Context, txn resource.ReadWriteTransaction, client *Client) error {
	return nil
}

type Cofveve struct{}

// Durian declares Execute on the value receiver.
type Durian struct{}

func (c Durian) Execute(ctx context.Context, client resource.Client, rpcClient *Client) error {
	return nil
}
