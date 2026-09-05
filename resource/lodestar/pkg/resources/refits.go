package resources

import (
	"context"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/httpio"
	"github.com/shopspring/decimal"
)

type (
	// Refit is the second workflow root — a ship's passage through the hangar bays.
	// It carries no tenant column of its own: tenancy runs two hops through Ship and
	// Hangar (@domain(via: HangarID.SectorID) on ShipId), so a stateful root
	// demonstrates join-path tenancy on every one of its six transitions (c2b62ab).
	//
	// InspectedAt is domain data written explicitly by InspectShip (output_only keeps
	// clients out); the Engineer's Update grant carries `inspectedAt IS NOT NULL`, so
	// an estimate is only ever set after inspection. OpenedBy is server-stamped. The
	// update path runs both a defaults type and a validator type. Change tracking is
	// on, feeding the ship's log.
	//
	// @resource
	// @permissionScope(domain)
	// @defaultsUpdateType(RefitUpdateDefaults)
	// @validateUpdateType(RefitUpdateValidator)
	Refit struct {
		ID ccc.UUID `spanner:"Id"`
		// @domain(via: HangarID.SectorID)
		ShipID ccc.UUID `spanner:"ShipId"`
		// @state(default: docked)
		StatusID string `spanner:"StatusId"`
		// @attribute(estimate)
		Estimate decimal.NullDecimal `spanner:"Estimate"`
		// @attribute(inspectedAt)
		InspectedAt *time.Time `spanner:"InspectedAt" conditions:"output_only"`
		// @attribute(openedBy)
		OpenedBy string  `spanner:"OpenedBy" conditions:"output_only" default_create_fn:"currentUser"`
		Notes    *string `spanner:"Notes"`
	}
)

// Config enables change tracking: Refit mutations write DataChangeEvents rows in the
// same transaction.
func (Refit) Config() resource.Config {
	return defaultConfig().SetTrackChanges(true)
}

// RefitUpdateDefaults is wired in by the @defaultsUpdateType annotation; the
// generated update patch calls Defaults inside the mutation transaction.
type RefitUpdateDefaults struct{}

// Defaults rounds an estimate to whole credits: the hangar does not quote fractions.
func (d *RefitUpdateDefaults) Defaults(_ context.Context, _ resource.ReadWriteTransaction, p *RefitUpdatePatch) error {
	if p.EstimateIsSet() && p.Estimate().Valid {
		p.SetEstimate(decimal.NewNullDecimal(p.Estimate().Decimal.Round(0)))
	}

	return nil
}

// RefitUpdateValidator is wired in by the @validateUpdateType annotation; the
// generated update patch calls Validate inside the mutation transaction.
type RefitUpdateValidator struct{}

// Validate rejects a negative estimate.
func (v *RefitUpdateValidator) Validate(_ context.Context, _ resource.ReadWriteTransaction, p *RefitUpdatePatch) error {
	if p.EstimateIsSet() && p.Estimate().Valid && p.Estimate().Decimal.IsNegative() {
		return httpio.NewBadRequestMessage("estimate must not be negative")
	}

	return nil
}
