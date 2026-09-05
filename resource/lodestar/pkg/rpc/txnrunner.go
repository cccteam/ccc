package rpc

import (
	"context"

	"github.com/cccteam/ccc/resource"
)

// TxnRunner is implemented by RPC methods that execute inside a read-write transaction.
// The generator detects implementations to wire the generated handler to Execute.
// It is deliberately Execute-only: Method() is generator-supplied, so a fresh
// package's methods must classify before it exists (4c115a4).
type TxnRunner interface {
	Execute(ctx context.Context, txn resource.ReadWriteTransaction, client *Client) error
}
