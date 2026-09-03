package rpc

import (
	"context"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/waystation/pkg/resources"
	"github.com/cccteam/httpio"
	"github.com/cccteam/session/sessioninfo"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

// The state-moving RPCs declare their edges (@transition), so the generated
// handlers own edge legality and the status stamps. The helpers below carry
// the business effects that ride alongside — and the two write paths that are
// not state transitions at all (Nudge touches, Receive stamps an arrival).

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

// nudgeWorkOrder touches an open work order: the generated WorkOrderTouch runs the
// full update pipeline with no caller-set fields, so the update functions supply the
// entire write — UpdatedAt takes the commit timestamp and the change event records
// who nudged. Nudging a finished work order is refused: there is no one left to get
// its attention.
func nudgeWorkOrder(ctx context.Context, txn resource.ReadWriteTransaction, id ccc.UUID) error {
	row, err := resources.NewWorkOrderQuery().AddColumns(resources.NewWorkOrderColumns().All()).SetID(id).Read(ctx, txn)
	if err != nil {
		return errors.Wrap(err, "resources.WorkOrderQuery.Read()")
	}
	if row == nil {
		return httpio.NewNotFoundMessagef("work order %s does not exist", id)
	}

	if status := resources.WorkOrderStatus(row.Data.StatusID); status == resources.CompletedWorkOrderStatus || status == resources.CancelledWorkOrderStatus {
		return httpio.NewBadRequestMessagef("work order %s is %s; only an open work order can be nudged", id, row.Data.StatusID)
	}

	if err := resources.NewWorkOrderTouch(id).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.WorkOrderTouch.Buffer()")
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
