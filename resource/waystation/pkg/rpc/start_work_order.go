package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/waystation/pkg/resources"
)

type (
	// StartWorkOrder moves a work order scheduled -> in_progress. Starting an
	// unscheduled, held, or finished work order is refused with the edge named in
	// the error.
	//
	// @rpc
	// @permissionScope(domain)
	StartWorkOrder struct {
		WorkOrderID ccc.UUID
	}
)

// Execute implements TxnRunner.
func (m *StartWorkOrder) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	_, err := transitionWorkOrder(ctx, txn, m.WorkOrderID, resources.ScheduledWorkOrderStatus, resources.InProgressWorkOrderStatus)

	return err
}
