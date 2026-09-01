package rpc

import (
	"context"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
)

type (
	// ScheduleWorkOrder moves a work order draft -> scheduled, assigning the team and
	// due date in the same transition. AssignedTeamID's enumerated tag ties the field
	// to the Teams resource in the generated TypeScript metadata, so the gui renders
	// a team picker.
	//
	// @rpc
	// @permissionScope(domain)
	ScheduleWorkOrder struct {
		WorkOrderID    ccc.UUID
		AssignedTeamID ccc.UUID `enumerated:"Teams"`
		DueAt          time.Time
	}
)

// Execute implements TxnRunner.
func (m *ScheduleWorkOrder) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) error {
	if m.DueAt.IsZero() {
		return httpio.NewBadRequestMessage("dueAt is required")
	}

	return scheduleWorkOrder(ctx, txn, m.WorkOrderID, m.AssignedTeamID, m.DueAt)
}
