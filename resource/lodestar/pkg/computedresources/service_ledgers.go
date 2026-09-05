package computedresources

import (
	"context"
	"iter"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/go-playground/errors/v5"
	"github.com/shopspring/decimal"
)

type (
	// ServiceLedger is headquarters' per-sector rollup: open missions, fees
	// outstanding on them, and settlements made. It is list-only — the read handler is
	// suppressed — and global: the ledger is a headquarters concern.
	//
	// @computed
	// @suppress(readHandler)
	ServiceLedger struct {
		SectorID        string          `spanner:"SectorId"` // @primarykey
		Name            string          `spanner:"Name"`
		OpenMissions    int64           `spanner:"OpenMissions"`
		FeesOutstanding decimal.Decimal `spanner:"FeesOutstanding"`
		Settlements     decimal.Decimal `spanner:"Settlements"`
	}
)

// Resource implements resource.Resourcer; computed resources declare their resource
// name by hand (there is no generated file to carry it).
func (ServiceLedger) Resource() accesstypes.Resource {
	return "ServiceLedgers"
}

// ListServiceLedger computes one ledger row per sector.
func ListServiceLedger(ctx context.Context, _ *resource.QuerySet[ServiceLedger], client resource.Client, _ *Client) iter.Seq2[*ServiceLedger, error] {
	return func(yield func(*ServiceLedger, error) bool) {
		for sector, err := range resources.NewSectorQuery().AddColumns(resources.NewSectorColumns().All()).List(ctx, client) {
			if err != nil {
				yield(nil, errors.Wrap(err, "resources.SectorQuery.List()"))

				return
			}

			ledger := &ServiceLedger{SectorID: sector.Data.ID, Name: sector.Data.Name, FeesOutstanding: decimal.Zero, Settlements: decimal.Zero}

			missions := resources.NewMissionQuery().
				AddColumns(resources.NewMissionColumns().All()).
				Where(resources.NewMissionQueryClause().SectorID().Equal(sector.Data.ID))
			for mission, err := range missions.List(ctx, client) {
				if err != nil {
					yield(nil, errors.Wrap(err, "resources.MissionQuery.List()"))

					return
				}
				switch resources.MissionStatus(mission.Data.StatusID) {
				case resources.CompletedMissionStatus:
					if mission.Data.Settlement.Valid {
						ledger.Settlements = ledger.Settlements.Add(mission.Data.Settlement.Decimal)
					}
				case resources.FailedMissionStatus, resources.StoodDownMissionStatus:
				default:
					ledger.OpenMissions++
					ledger.FeesOutstanding = ledger.FeesOutstanding.Add(mission.Data.Fee)
				}
			}

			if !yield(ledger, nil) {
				return
			}
		}
	}
}
