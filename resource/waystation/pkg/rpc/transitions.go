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

// The state-moving RPCs declare their edges (@transition), so the generated
// handlers own edge legality, the status stamps, and any row condition the
// caller's Execute grant carries (the approval limit rides the grant, not a
// helper here). The helpers below carry the business effects that ride
// alongside — and the write paths that are not state transitions at all
// (Receive stamps an arrival; Nudge's touch lives in its own body).

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
