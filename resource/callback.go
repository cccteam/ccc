package resource

import (
	"context"
	"fmt"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/callback"
)

type Callback[R Resourcer] interface {
	Callback(ctx context.Context, txn TxnBuffer, patchset *PatchSet[R]) error
}

type CallbackFunc[R Resourcer] func(ctx context.Context, txn TxnBuffer, res *PatchSet[R]) error

func (c CallbackFunc[R]) Callback(ctx context.Context, txn TxnBuffer, res *PatchSet[R]) error {
	return c(ctx, txn, res)
}

func TypeAssertCallbacks[R Resourcer](registry *callback.Registry, res accesstypes.Resource) {
	for _, fn := range registry.Callbacks(res) {
		if _, ok := fn.(Callback[R]); !ok {
			panic(fmt.Sprintf("invalid callback registered for %s, callback = %T", res, fn))
		}
	}
}
