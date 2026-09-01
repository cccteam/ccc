package rpc

import (
	"context"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/waystation/pkg/resources"
	"github.com/cccteam/httpio"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

// The helpers below are the only writers of workflow state. Each enforces edge
// legality only — what a role may do in each state is conditional grants, never code
// here. State values are the generated enum constants, so a typo'd state fails to
// compile instead of failing closed at runtime.

// transitionRequisition moves a requisition along one edge, optionally recomputing and
// freezing TotalCost from its lines (the submit edge).
func transitionRequisition(ctx context.Context, txn resource.ReadWriteTransaction, id ccc.UUID, from, to resources.RequisitionStatus, recomputeTotal bool) error {
	row, err := resources.NewRequisitionQuery().AddColumns(resources.NewRequisitionColumns().All()).SetID(id).Read(ctx, txn)
	if err != nil {
		return errors.Wrap(err, "resources.RequisitionQuery.Read()")
	}
	if row == nil {
		return httpio.NewNotFoundMessagef("requisition %s does not exist", id)
	}

	if resources.RequisitionStatus(row.Data.StatusID) != from {
		return httpio.NewBadRequestMessagef("requisition %s is %s; only a %s requisition can move to %s", id, row.Data.StatusID, from, to)
	}

	patch := resources.NewRequisitionUpdatePatch(id).SetStatusID(string(to))

	if recomputeTotal {
		total := decimal.Zero
		lines := resources.NewRequisitionLineQuery().
			AddColumns(resources.NewRequisitionLineColumns().All()).
			SetRequisitionID(id)
		for line, err := range lines.List(ctx, txn) {
			if err != nil {
				return errors.Wrap(err, "resources.RequisitionLineQuery.List()")
			}
			total = total.Add(line.Data.UnitCostSnapshot.Mul(decimal.NewFromInt(line.Data.Quantity)))
		}
		patch.SetTotalCost(total)
	}

	if err := patch.Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.RequisitionUpdatePatch.Buffer()")
	}

	return nil
}

// verifyApprovalLimit rejects an approval whose total exceeds the approver's limit on
// their StaffMember row. No row means no approval authority at all.
func verifyApprovalLimit(ctx context.Context, txn resource.ReadWriteTransaction, id ccc.UUID) error {
	row, err := resources.NewRequisitionQuery().AddColumns(resources.NewRequisitionColumns().All()).SetID(id).Read(ctx, txn)
	if err != nil {
		return errors.Wrap(err, "resources.RequisitionQuery.Read()")
	}
	if row == nil {
		return httpio.NewNotFoundMessagef("requisition %s does not exist", id)
	}

	user := sessioninfo.FromCtx(ctx).Username
	var limit *decimal.Decimal
	staff := resources.NewStaffMemberQuery().
		AddColumns(resources.NewStaffMemberColumns().All()).
		Where(resources.NewStaffMemberQueryClause().UserID().Equal(user))
	for member, err := range staff.List(ctx, txn) {
		if err != nil {
			return errors.Wrap(err, "resources.StaffMemberQuery.List()")
		}
		limit = &member.Data.ApprovalLimit
	}
	if limit == nil {
		return httpio.NewForbiddenMessagef("%s has no staff record, so no approval authority", user)
	}

	if row.Data.TotalCost.GreaterThan(*limit) {
		return httpio.NewForbiddenMessagef("requisition total %s exceeds your approval limit of %s", row.Data.TotalCost, *limit)
	}

	return nil
}

// transitionWorkOrder moves a work order along one edge.
func transitionWorkOrder(ctx context.Context, txn resource.ReadWriteTransaction, id ccc.UUID, from, to resources.WorkOrderStatus) (*resources.WorkOrder, error) {
	row, err := resources.NewWorkOrderQuery().AddColumns(resources.NewWorkOrderColumns().All()).SetID(id).Read(ctx, txn)
	if err != nil {
		return nil, errors.Wrap(err, "resources.WorkOrderQuery.Read()")
	}
	if row == nil {
		return nil, httpio.NewNotFoundMessagef("work order %s does not exist", id)
	}

	if resources.WorkOrderStatus(row.Data.StatusID) != from {
		return nil, httpio.NewBadRequestMessagef("work order %s is %s; only a %s work order can move to %s", id, row.Data.StatusID, from, to)
	}

	if err := resources.NewWorkOrderUpdatePatch(id).SetStatusID(string(to)).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return nil, errors.Wrap(err, "resources.WorkOrderUpdatePatch.Buffer()")
	}

	return &row.Data, nil
}

// scheduleWorkOrder assigns the team and due date on the draft -> scheduled edge.
func scheduleWorkOrder(ctx context.Context, txn resource.ReadWriteTransaction, id, teamID ccc.UUID, dueAt time.Time) error {
	row, err := resources.NewWorkOrderQuery().AddColumns(resources.NewWorkOrderColumns().All()).SetID(id).Read(ctx, txn)
	if err != nil {
		return errors.Wrap(err, "resources.WorkOrderQuery.Read()")
	}
	if row == nil {
		return httpio.NewNotFoundMessagef("work order %s does not exist", id)
	}

	if resources.WorkOrderStatus(row.Data.StatusID) != resources.DraftWorkOrderStatus {
		return httpio.NewBadRequestMessagef("work order %s is %s; only a draft work order can be scheduled", id, row.Data.StatusID)
	}

	patch := resources.NewWorkOrderUpdatePatch(id).
		SetStatusID(string(resources.ScheduledWorkOrderStatus)).
		SetAssignedTeamID(ccc.NullUUID{UUID: teamID, Valid: true}).
		SetDueAt(&dueAt)
	if err := patch.Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.WorkOrderUpdatePatch.Buffer()")
	}

	return nil
}

// completeWorkOrder runs the in_progress -> completed edge and stamps the asset's
// LastServicedAt through its output_only_update_fn in the same transaction.
func completeWorkOrder(ctx context.Context, txn resource.ReadWriteTransaction, id ccc.UUID) error {
	order, err := transitionWorkOrder(ctx, txn, id, resources.InProgressWorkOrderStatus, resources.CompletedWorkOrderStatus)
	if err != nil {
		return err
	}

	// A field-empty update patch is a no-op (its output_only_update_fn never runs),
	// so the touch sets the commit-timestamp sentinel explicitly.
	if err := resources.NewAssetUpdatePatch(order.AssetID).SetLastServicedAt(&cloudspanner.CommitTimestamp).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.AssetUpdatePatch.Buffer()")
	}

	return nil
}

// receiveShipment stamps a shipment's arrival; a shipment only arrives once.
func receiveShipment(ctx context.Context, txn resource.ReadWriteTransaction, id ccc.UUID) error {
	row, err := resources.NewShipmentQuery().AddColumns(resources.NewShipmentColumns().All()).SetID(id).Read(ctx, txn)
	if err != nil {
		return errors.Wrap(err, "resources.ShipmentQuery.Read()")
	}
	if row == nil {
		return httpio.NewNotFoundMessagef("shipment %s does not exist", id)
	}
	if row.Data.ArrivedAt != nil {
		return httpio.NewBadRequestMessagef("shipment %s already arrived at %s", id, row.Data.ArrivedAt.Format(time.RFC3339))
	}

	now := time.Now().UTC()
	if err := resources.NewShipmentUpdatePatch(id).SetArrivedAt(&now).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.ShipmentUpdatePatch.Buffer()")
	}

	return nil
}

// ingestSensorReading writes one telemetry row, resolving the tenant through the
// facility's module — the payload never asserts its own tenancy.
func ingestSensorReading(ctx context.Context, txn resource.ReadWriteTransaction, facilityID ccc.UUID, metric string, reading float64, recordedAt time.Time) error {
	facility, err := resources.NewFacilityQuery().AddColumns(resources.NewFacilityColumns().All()).SetID(facilityID).Read(ctx, txn)
	if err != nil {
		return errors.Wrap(err, "resources.FacilityQuery.Read()")
	}
	if facility == nil {
		return httpio.NewNotFoundMessagef("facility %s does not exist", facilityID)
	}

	module, err := resources.NewModuleQuery().AddColumns(resources.NewModuleColumns().All()).SetID(facility.Data.ModuleID).Read(ctx, txn)
	if err != nil {
		return errors.Wrap(err, "resources.ModuleQuery.Read()")
	}
	if module == nil {
		return httpio.NewNotFoundMessagef("module %s does not exist", facility.Data.ModuleID)
	}

	if recordedAt.IsZero() {
		recordedAt = time.Now().UTC()
	}

	patch, err := resources.NewSensorReadingCreatePatch()
	if err != nil {
		return errors.Wrap(err, "resources.NewSensorReadingCreatePatch()")
	}
	patch.SetWaystationID(module.Data.WaystationID).
		SetFacilityID(facilityID).
		SetMetric(metric).
		SetReading(reading).
		SetRecordedAt(recordedAt)
	if err := patch.Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.SensorReadingCreatePatch.Buffer()")
	}

	return nil
}
