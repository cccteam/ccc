package rpc

import (
	"context"

	"github.com/cccteam/ccc/resource"
)

// Apple carries no Execute, so it never classifies as a runner.
type Apple struct{}

type Banana struct{}

func (c *Banana) Execute(ctx context.Context, txn resource.ReadWriteTransaction, client *resource.Client) error {
	return nil
}

type Cofveve struct{}
