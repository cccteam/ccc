package rpc

import (
	"context"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

// TxnRunner is implemented by RPC methods that execute inside a read-write transaction.
// The generator detects implementations to wire the generated handler to Execute.
type TxnRunner interface {
	Method() accesstypes.Resource
	Execute(ctx context.Context, txn resource.ReadWriteTransaction, client *Client) error
}
