package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/waystation/pkg/resources"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

type (
	// SubmitRequisition moves a requisition draft -> submitted, recomputing and
	// freezing TotalCost from its lines inside the same transaction — the client can
	// never assert its own total, which is what makes the approval-limit conditions
	// trustworthy. The declared transition owns the edge check and the status stamp;
	// the body owns the total.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Requisition, from: draft, to: submitted)
	SubmitRequisition struct {
		// @target
		RequisitionID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *SubmitRequisition) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	total := decimal.Zero
	lines := resources.NewRequisitionLineQuery().
		AddColumns(resources.NewRequisitionLineColumns().All()).
		SetRequisitionID(m.RequisitionID)
	for line, err := range lines.List(ctx, txn) {
		if err != nil {
			return errors.Wrap(err, "resources.RequisitionLineQuery.List()")
		}
		total = total.Add(line.Data.UnitCostSnapshot.Mul(decimal.NewFromInt(line.Data.Quantity)))
	}

	if err := resources.NewRequisitionUpdatePatch(m.RequisitionID).SetTotalCost(total).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.RequisitionUpdatePatch.Buffer()")
	}

	return nil
}
