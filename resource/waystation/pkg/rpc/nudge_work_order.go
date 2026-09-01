package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// NudgeWorkOrder flags a stalled work order for attention without changing it:
	// the whole business effect is the update pipeline itself. It fires the generated
	// NewWorkOrderTouch — an update carried entirely by the resource's update
	// functions — so UpdatedAt bumps (surfacing the order at the top of a
	// last-activity sort) and the audit trail records who nudged, while no field of
	// the order changes. A plain update patch with no fields set is a silent no-op,
	// which is exactly why Touch exists.
	//
	// @rpc
	// @permissionScope(domain)
	NudgeWorkOrder struct {
		WorkOrderID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *NudgeWorkOrder) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	return nudgeWorkOrder(ctx, txn, m.WorkOrderID)
}
