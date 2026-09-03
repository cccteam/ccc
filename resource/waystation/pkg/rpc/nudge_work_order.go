package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/waystation/pkg/resources"
	"github.com/go-playground/errors/v5"
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
	// The method moves no state, but it declares its target row: the generated frame
	// locates the order (absent or cross-station is NotFound) and evaluates the
	// caller's Execute condition against it — the demo grants carry
	// `state NOT IN ('completed', 'cancelled')`, so "no nudging finished work" is
	// policy, never code here (design plan §12).
	//
	// @rpc
	// @permissionScope(domain)
	NudgeWorkOrder struct {
		// @target(WorkOrder)
		WorkOrderID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *NudgeWorkOrder) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	if err := resources.NewWorkOrderTouch(m.WorkOrderID).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.WorkOrderTouch.Buffer()")
	}

	return nil
}
