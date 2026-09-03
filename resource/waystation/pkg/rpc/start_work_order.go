package rpc

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

type (
	// StartWorkOrder moves a work order scheduled -> in_progress. The declared
	// transition carries the whole edge: the generated handler locates the work
	// order within the station, verifies it is scheduled (anything else is
	// Forbidden with the edge named), and stamps in_progress after the body.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(WorkOrder, from: scheduled, to: in_progress)
	StartWorkOrder struct {
		// @target
		WorkOrderID ccc.UUID
	}
)

// Execute implements TxnRunner. Starting a work order has no effect beyond
// the declared edge, so the body carries nothing.
func (m *StartWorkOrder) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}
