package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/waystation/pkg/resources"
	"github.com/cccteam/httpio"
)

type (
	// DeclineRequisition moves a requisition submitted -> declined with a reason
	// validated against the generated DeclineReason enum constants — the gui offers
	// the same values from the generated TypeScript enums. The declared transition
	// owns the edge check and the status stamp; the body owns the reason.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Requisition, from: submitted, to: declined)
	DeclineRequisition struct {
		// @target
		RequisitionID ccc.UUID
		Reason        string
	}
)

// Execute implements TxnRunner.
func (m *DeclineRequisition) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	switch resources.DeclineReason(m.Reason) {
	case resources.OverBudgetDeclineReason, resources.DuplicateRequestDeclineReason,
		resources.NotNeededDeclineReason, resources.SupplierIssueDeclineReason:
	default:
		return httpio.NewBadRequestMessagef("%q is not a decline reason", m.Reason)
	}

	return nil
}
