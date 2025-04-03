package rpc

import (
	"context"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
)

type Apple struct{}

func (a Apple) Method() accesstypes.Resource { return "" }

type Banana struct{}

func (c *Banana) Method() accesstypes.Resource                              { return "" }
func (c *Banana) Execute(ctx context.Context, txn resource.TxnBuffer) error { return nil }

type Cofveve struct{}

func (c Cofveve) Method() accesstypes.Resource { return "" }
