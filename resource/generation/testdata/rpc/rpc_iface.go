package rpc

import (
	"context"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

type TxnRunner interface {
	Method() accesstypes.Resource
	Execute(ctx context.Context, txn resource.TxnBuffer) error
}
