package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// ApproveRequisition moves a requisition submitted -> approved. The declared
	// transition owns the whole frame: the edge check, the status stamp, and the
	// approver's spending authority — `totalCost <= subject.approvalLimit` rides the
	// Execute grant and evaluates against the located row inside this transaction
	// (design plan §12), so a directly addressed over-limit approval is refused
	// without a line of code here. The body carries no further business effect.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Requisition, from: submitted, to: approved)
	ApproveRequisition struct {
		// @target
		RequisitionID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *ApproveRequisition) Execute(_ context.Context, _ resource.ReadWriteTransaction, _ *Client) error {
	return nil
}
