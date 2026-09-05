package computedresources

import (
	"context"
	"iter"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/go-playground/errors/v5"
)

type (
	// SectorHazardBoard is the human window onto droid telemetry: the WORST reading
	// per ship and subsystem in the sector. Droid reports themselves live on the
	// droids outlet only; this computed resource is how the browser sees them. Its
	// compound key is declared with two primarykey annotations, and it is the one
	// place a CONDITIONAL grant lands on a computed resource: the Hazard Analyst's
	// List grant carries the row-free `now < '2027-01-01T00:00:00Z'`, so the board
	// goes dark when the certification lapses.
	//
	// @computed
	// @permissionScope(domain)
	SectorHazardBoard struct {
		ShipID       ccc.UUID  `spanner:"ShipId"`    // @primarykey
		Subsystem    string    `spanner:"Subsystem"` // @primarykey
		ShipName     string    `spanner:"ShipName"`
		SectorID     string    `spanner:"SectorId"`
		WorstReading float64   `spanner:"WorstReading"`
		RecordedAt   time.Time `spanner:"RecordedAt"`
	}
)

// Resource implements resource.Resourcer; computed resources declare their resource
// name by hand (there is no generated file to carry it).
func (SectorHazardBoard) Resource() accesstypes.Resource {
	return "SectorHazardBoards"
}

// ListSectorHazardBoard computes the worst reading per ship and subsystem in the
// request's sector.
func ListSectorHazardBoard(ctx context.Context, _ *resource.QuerySet[SectorHazardBoard], client resource.Client, _ *Client) iter.Seq2[*SectorHazardBoard, error] {
	return func(yield func(*SectorHazardBoard, error) bool) {
		boards, err := worstReadings(ctx, client, requestDomain(ctx), nil, "")
		if err != nil {
			yield(nil, err)

			return
		}

		for _, board := range boards {
			if !yield(board, nil) {
				return
			}
		}
	}
}

// ReadSectorHazardBoard computes the worst reading for one ship and subsystem; nil
// when that pair has no readings in the sector.
func ReadSectorHazardBoard(ctx context.Context, shipID ccc.UUID, subsystem string, _ *resource.QuerySet[SectorHazardBoard], client resource.Client, _ *Client) (*SectorHazardBoard, error) {
	boards, err := worstReadings(ctx, client, requestDomain(ctx), &shipID, subsystem)
	if err != nil {
		return nil, err
	}
	for _, board := range boards {
		return board, nil
	}

	return nil, nil
}

// worstReadings folds droid reports down to the highest reading per (ship,
// subsystem) in the sector, optionally narrowed to one ship and subsystem. Rows come
// back in a stable order so list pages render deterministically.
func worstReadings(ctx context.Context, client resource.Client, domain accesstypes.Domain, shipID *ccc.UUID, subsystem string) ([]*SectorHazardBoard, error) {
	names := make(map[ccc.UUID]string)
	for row, err := range resources.NewShipQuery().AddColumns(resources.NewShipColumns().All()).List(ctx, client) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.ShipQuery.List()")
		}
		names[row.Data.ID] = row.Data.Name
	}

	query := resources.NewDroidReportQuery().
		AddColumns(resources.NewDroidReportColumns().All()).
		Where(resources.NewDroidReportQueryClause().SectorID().Equal(string(domain)))
	if shipID != nil {
		query = resources.NewDroidReportQuery().
			AddColumns(resources.NewDroidReportColumns().All()).
			Where(resources.NewDroidReportQueryClause().SectorID().Equal(string(domain)).And().ShipID().Equal(*shipID))
	}

	boards := make(map[[2]string]*SectorHazardBoard)
	var order [][2]string
	for row, err := range query.List(ctx, client) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.DroidReportQuery.List()")
		}
		if shipID != nil && row.Data.Subsystem != subsystem {
			continue
		}
		key := [2]string{row.Data.ShipID.String(), row.Data.Subsystem}
		existing, ok := boards[key]
		if ok && row.Data.Reading <= existing.WorstReading {
			continue
		}
		if !ok {
			order = append(order, key)
		}
		boards[key] = &SectorHazardBoard{
			ShipID:       row.Data.ShipID,
			Subsystem:    row.Data.Subsystem,
			ShipName:     names[row.Data.ShipID],
			SectorID:     row.Data.SectorID,
			WorstReading: row.Data.Reading,
			RecordedAt:   row.Data.RecordedAt,
		}
	}

	result := make([]*SectorHazardBoard, 0, len(order))
	for _, key := range order {
		result = append(result, boards[key])
	}

	return result, nil
}
