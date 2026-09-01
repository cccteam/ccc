package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/waystation/pkg/resources"
)

type (
	// ApproveRequisition moves a requisition submitted -> approved. The body
	// re-verifies the approver's spending authority against their Staff row: the
	// conditional Read grant keeps over-limit requisitions out of the approval queue,
	// and this check keeps a directly-addressed approval honest too — business logic,
	// not permission logic.
	//
	// @rpc
	// @permissionScope(domain)
	ApproveRequisition struct {
		RequisitionID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *ApproveRequisition) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	if err := verifyApprovalLimit(ctx, txn, m.RequisitionID); err != nil {
		return err
	}

	return transitionRequisition(ctx, txn, m.RequisitionID, resources.SubmittedRequisitionStatus, resources.ApprovedRequisitionStatus, false)
}
