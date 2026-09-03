package rpc

import (
	"context"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/waystation/pkg/resources"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

type (
	// ScheduleWorkOrder moves a work order draft -> scheduled, assigning the team and
	// due date in the same transition. The declared transition owns the edge check and
	// the status stamp; the body carries the assignment. AssignedTeamID's enumerated
	// tag ties the field to the Teams resource in the generated TypeScript metadata,
	// so the gui renders a team picker.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(WorkOrder, from: draft, to: scheduled)
	ScheduleWorkOrder struct {
		// @target
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

	patch := resources.NewWorkOrderUpdatePatch(m.WorkOrderID).
		SetAssignedTeamID(ccc.NullUUID{UUID: m.AssignedTeamID, Valid: true}).
		SetDueAt(&m.DueAt)
	if err := patch.Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.WorkOrderUpdatePatch.Buffer()")
	}

	return nil
}
