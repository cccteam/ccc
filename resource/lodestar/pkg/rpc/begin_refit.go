package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// BeginRefit moves a refit inspected -> in_refit. The declared edge is the whole
	// effect.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Refit, from: inspected, to: in_refit)
	BeginRefit struct {
		// @target
		RefitID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *BeginRefit) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}
