package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/waystation/pkg/resources"
)

type (
	// SubmitRequisition moves a requisition draft -> submitted, recomputing and
	// freezing TotalCost from its lines inside the same transaction — the client can
	// never assert its own total, which is what makes the approval-limit conditions
	// trustworthy.
	//
	// @rpc
	// @permissionScope(domain)
	SubmitRequisition struct {
		RequisitionID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *SubmitRequisition) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	return transitionRequisition(ctx, txn, m.RequisitionID, resources.DraftRequisitionStatus, resources.SubmittedRequisitionStatus, true)
}
