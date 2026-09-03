package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// ApproveRequisition moves a requisition submitted -> approved. The declared
	// transition owns the edge check and the status stamp. The body re-verifies the
	// approver's spending authority against their Staff row: the conditional Read
	// grant keeps over-limit requisitions out of the approval queue, and this check
	// keeps a directly-addressed approval honest too — business logic, not
	// permission logic.
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
func (m *ApproveRequisition) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	return verifyApprovalLimit(ctx, txn, m.RequisitionID)
}
