package rpc

import (
	"context"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/waystation/pkg/resources"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

type (
	// CompleteWorkOrder moves a work order in_progress -> completed, stamping the
	// equipment's LastServicedAt with the commit timestamp in the same transaction —
	// a transition with a visible side effect. The declared transition owns the edge
	// check and the status stamp; the body owns the side effect. Completion is the
	// only writer of LastServicedAt; output_only keeps clients from setting it
	// directly.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(WorkOrder, from: in_progress, to: completed)
	CompleteWorkOrder struct {
		// @target
		WorkOrderID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *CompleteWorkOrder) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	row, err := resources.NewWorkOrderQuery().
		AddColumns(resources.NewWorkOrderColumns().AssetID()).
		SetID(m.WorkOrderID).
		Read(ctx, txn)
	if err != nil {
		return errors.Wrap(err, "resources.WorkOrderQuery.Read()")
	}
	if row == nil {
		return httpio.NewNotFoundMessagef("work order %s does not exist", m.WorkOrderID)
	}

	if err := resources.NewAssetUpdatePatch(row.Data.AssetID).SetLastServicedAt(&cloudspanner.CommitTimestamp).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.AssetUpdatePatch.Buffer()")
	}

	return nil
}
