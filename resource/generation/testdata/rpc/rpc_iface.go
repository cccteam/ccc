package rpc

import (
	"context"

	"github.com/cccteam/ccc/resource"
)

// TxnRunner is Execute-only on purpose: Method() is generator-supplied, so a
// fresh package's structs must classify before it exists. No Method() stubs
// appear anywhere in this fixture — that absence is what the filter test pins.
type TxnRunner interface {
	Execute(ctx context.Context, txn resource.ReadWriteTransaction, client *resource.Client) error
}
