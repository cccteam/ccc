package rpc

import (
	"context"
	"slices"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

type (
	// InspectShip moves a refit docked -> inspected and writes Refit.InspectedAt
	// explicitly — transition-owned domain data, not an update function. The
	// Engineer's estimate grant carries `inspectedAt IS NOT NULL`, so the stamp is
	// what unlocks the estimate.
	//
	// Refit's tenancy is a two-hop join path (Ship → Hangar → SectorId): the frame
	// verifies it with one check-SELECT in this transaction (c2b62ab).
	//
	// It is the method that ANSWERS: Execute returns a RefitReport beside its error,
	// a nested shape three levels deep (report → subsystem → reading). The generated
	// handler captures it inside the transaction, encodes it after the commit through
	// mirrors of every struct it reaches, and the browser client's handle resolves
	// with the typed InspectShipResult instead of re-reading. The report carries
	// identifiers and readings, never a row: rows are read through their routes.
	//
	// @rpc
	// @permissionScope(domain)
	// @transition(Refit, from: docked, to: inspected)
	InspectShip struct {
		// @target
		RefitID ccc.UUID
	}

	// RefitReport is the inspection's answer: which refit and ship were inspected,
	// and the ship's subsystems with the telemetry the droids reported on each.
	RefitReport struct {
		RefitID    ccc.UUID
		ShipID     ccc.UUID
		ShipName   string
		Subsystems []SubsystemReport
	}

	// SubsystemReport is one subsystem of the inspected ship with its readings,
	// newest first.
	SubsystemReport struct {
		Name     string
		Readings []Reading
	}

	// Reading is one droid reading as the report carries it.
	Reading struct {
		Value      float64
		RecordedAt time.Time
	}
)

// Execute runs inside the handler's transaction and answers with the report.
func (m *InspectShip) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) (*RefitReport, error) {
	if err := stampRefitInspected(ctx, txn, m.RefitID); err != nil {
		return nil, err
	}

	return refitReport(ctx, txn, m.RefitID)
}

// refitReport reads the inspected refit's ship and folds the ship's droid reports
// into subsystems, in the same transaction the inspection runs in.
func refitReport(ctx context.Context, txn resource.ReadWriteTransaction, refitID ccc.UUID) (*RefitReport, error) {
	refit, err := resources.NewRefitQuery().AddColumns(resources.NewRefitColumns().ShipID()).SetID(refitID).Read(ctx, txn)
	if err != nil {
		return nil, errors.Wrap(err, "resources.RefitQuery.Read()")
	}
	if refit == nil {
		return nil, httpio.NewNotFoundMessagef("refit %s does not exist", refitID)
	}
	ship, err := resources.NewShipQuery().AddColumns(resources.NewShipColumns().Name()).SetID(refit.Data.ShipID).Read(ctx, txn)
	if err != nil {
		return nil, errors.Wrap(err, "resources.ShipQuery.Read()")
	}
	if ship == nil {
		return nil, httpio.NewNotFoundMessagef("ship %s does not exist", refit.Data.ShipID)
	}

	report := &RefitReport{RefitID: refitID, ShipID: refit.Data.ShipID, ShipName: ship.Data.Name}
	bySubsystem := make(map[string]int)
	query := resources.NewDroidReportQuery().
		AddColumns(resources.NewDroidReportColumns().All()).
		Where(resources.NewDroidReportQueryClause().ShipID().Equal(refit.Data.ShipID))
	for row, err := range query.List(ctx, txn) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.DroidReportQuery.List()")
		}
		i, ok := bySubsystem[row.Data.Subsystem]
		if !ok {
			i = len(report.Subsystems)
			bySubsystem[row.Data.Subsystem] = i
			report.Subsystems = append(report.Subsystems, SubsystemReport{Name: row.Data.Subsystem})
		}
		report.Subsystems[i].Readings = append(report.Subsystems[i].Readings, Reading{Value: row.Data.Reading, RecordedAt: row.Data.RecordedAt})
	}
	slices.SortFunc(report.Subsystems, func(a, b SubsystemReport) int { return cmpString(a.Name, b.Name) })
	for i := range report.Subsystems {
		slices.SortFunc(report.Subsystems[i].Readings, func(a, b Reading) int { return b.RecordedAt.Compare(a.RecordedAt) })
	}

	return report, nil
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
