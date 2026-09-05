package rpc

import (
	"context"
	"time"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

// The state-moving RPCs declare their edges (@transition), so the generated handlers
// own edge legality, the status stamps, the tenancy check, and any row condition the
// caller's Execute grant carries. The helpers below carry the business effects that
// ride alongside — and the write paths that are not state transitions at all.

// appendMissionNote appends one line to a mission's notes, the plainest place for a
// hold reason or a failure reason to land.
func appendMissionNote(ctx context.Context, txn resource.ReadWriteTransaction, missionID ccc.UUID, line string) error {
	row, err := resources.NewMissionQuery().AddColumns(resources.NewMissionColumns().Notes()).SetID(missionID).Read(ctx, txn)
	if err != nil {
		return errors.Wrap(err, "resources.MissionQuery.Read()")
	}
	if row == nil {
		return httpio.NewNotFoundMessagef("mission %s does not exist", missionID)
	}

	notes := line
	if row.Data.Notes != nil && *row.Data.Notes != "" {
		notes = *row.Data.Notes + "\n" + line
	}
	if err := resources.NewMissionUpdatePatch(missionID).SetNotes(&notes).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.MissionUpdatePatch.Buffer()")
	}

	return nil
}

// returnOpenSorties stamps ReturnedAt on every sortie of the mission still in flight:
// a mission that completes or fails brings its ships home.
func returnOpenSorties(ctx context.Context, txn resource.ReadWriteTransaction, missionID ccc.UUID) ([]ccc.UUID, error) {
	now := time.Now().UTC()
	var sortieIDs []ccc.UUID
	query := resources.NewSortieQuery().
		AddColumns(resources.NewSortieColumns().All()).
		Where(resources.NewSortieQueryClause().MissionID().Equal(missionID))
	for row, err := range query.List(ctx, txn) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.SortieQuery.List()")
		}
		sortieIDs = append(sortieIDs, row.Data.ID)
		if row.Data.ReturnedAt != nil {
			continue
		}
		if err := resources.NewSortieUpdatePatch(row.Data.ID).SetReturnedAt(&now).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
			return nil, errors.Wrap(err, "resources.SortieUpdatePatch.Buffer()")
		}
	}

	return sortieIDs, nil
}

// settleMission computes the mission's settlement — fee minus every expense booked
// against its sorties — into the output_only Settlement field. CompleteMission is the
// field's only writer.
func settleMission(ctx context.Context, txn resource.ReadWriteTransaction, missionID ccc.UUID, sortieIDs []ccc.UUID) error {
	row, err := resources.NewMissionQuery().AddColumns(resources.NewMissionColumns().Fee()).SetID(missionID).Read(ctx, txn)
	if err != nil {
		return errors.Wrap(err, "resources.MissionQuery.Read()")
	}
	if row == nil {
		return httpio.NewNotFoundMessagef("mission %s does not exist", missionID)
	}

	expenses := decimal.Zero
	for _, sortieID := range sortieIDs {
		query := resources.NewSortieExpenseQuery().
			AddColumns(resources.NewSortieExpenseColumns().All()).
			Where(resources.NewSortieExpenseQueryClause().SortieID().Equal(sortieID))
		for expense, err := range query.List(ctx, txn) {
			if err != nil {
				return errors.Wrap(err, "resources.SortieExpenseQuery.List()")
			}
			expenses = expenses.Add(expense.Data.Amount)
		}
	}

	settlement := decimal.NewNullDecimal(row.Data.Fee.Sub(expenses))
	if err := resources.NewMissionUpdatePatch(missionID).SetSettlement(settlement).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.MissionUpdatePatch.Buffer()")
	}

	return nil
}

// stampRefitInspected writes Refit.InspectedAt with the commit timestamp — domain
// data owned by the InspectShip transition, an explicit update rather than an update
// function.
func stampRefitInspected(ctx context.Context, txn resource.ReadWriteTransaction, refitID ccc.UUID) error {
	if err := resources.NewRefitUpdatePatch(refitID).SetInspectedAt(&cloudspanner.CommitTimestamp).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.RefitUpdatePatch.Buffer()")
	}

	return nil
}

// stampShipRefitted writes Ship.LastRefitAt for the refit's ship with the commit
// timestamp — PassFlightTest is the field's only writer.
func stampShipRefitted(ctx context.Context, txn resource.ReadWriteTransaction, refitID ccc.UUID) error {
	row, err := resources.NewRefitQuery().AddColumns(resources.NewRefitColumns().ShipID()).SetID(refitID).Read(ctx, txn)
	if err != nil {
		return errors.Wrap(err, "resources.RefitQuery.Read()")
	}
	if row == nil {
		return httpio.NewNotFoundMessagef("refit %s does not exist", refitID)
	}
	if err := resources.NewShipUpdatePatch(row.Data.ShipID).SetLastRefitAt(&cloudspanner.CommitTimestamp).Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.ShipUpdatePatch.Buffer()")
	}

	return nil
}

// ingestDroidReport writes one telemetry row, resolving the tenant through the ship's
// hangar — the payload never asserts its own tenancy.
func ingestDroidReport(ctx context.Context, txn resource.ReadWriteTransaction, shipID ccc.UUID, subsystem string, reading float64, recordedAt time.Time) error {
	ship, err := resources.NewShipQuery().AddColumns(resources.NewShipColumns().HangarID()).SetID(shipID).Read(ctx, txn)
	if err != nil {
		return errors.Wrap(err, "resources.ShipQuery.Read()")
	}
	if ship == nil {
		return httpio.NewNotFoundMessagef("ship %s does not exist", shipID)
	}

	hangar, err := resources.NewHangarQuery().AddColumns(resources.NewHangarColumns().SectorID()).SetID(ship.Data.HangarID).Read(ctx, txn)
	if err != nil {
		return errors.Wrap(err, "resources.HangarQuery.Read()")
	}
	if hangar == nil {
		return httpio.NewNotFoundMessagef("hangar %s does not exist", ship.Data.HangarID)
	}

	if recordedAt.IsZero() {
		recordedAt = time.Now().UTC()
	}

	patch, err := resources.NewDroidReportCreatePatch()
	if err != nil {
		return errors.Wrap(err, "resources.NewDroidReportCreatePatch()")
	}
	patch.SetSectorID(hangar.Data.SectorID).
		SetShipID(shipID).
		SetSubsystem(subsystem).
		SetReading(reading).
		SetRecordedAt(recordedAt)
	if err := patch.Buffer(ctx, txn, resource.UserEvent(ctx)); err != nil {
		return errors.Wrap(err, "resources.DroidReportCreatePatch.Buffer()")
	}

	return nil
}
