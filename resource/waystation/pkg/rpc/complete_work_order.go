package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// CompleteWorkOrder moves a work order in_progress -> completed, stamping the
	// equipment's LastServicedAt with the commit timestamp in the same transaction —
	// a transition with a visible side effect. Completion is the only writer of
	// that field; output_only keeps clients from setting it directly.
	//
	// @rpc
	// @permissionScope(domain)
	CompleteWorkOrder struct {
		WorkOrderID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *CompleteWorkOrder) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	return completeWorkOrder(ctx, txn, m.WorkOrderID)
}
