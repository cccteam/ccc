package computedresources

import (
	"context"
	"fmt"
	"iter"
	"slices"
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
	// Recent is a NESTED field: the readings behind the worst one, newest first.
	// The generator walks Reading into a local mirror in the handler and declares
	// its TypeScript interface beside the board's; to the resource machinery the
	// field is one opaque unit, granted and selected whole: `columns=recent` is the
	// only way to ask for it, and nothing inside it is a column, a filter field, or
	// a sort key.
	//
	// The board is also the query demonstration for computed resources: ShipName
	// and Subsystem are filterable (allow_filter, the same tag a table field
	// carries), the list orders by the worst reading first when the request states
	// no sort, and the generated handler applies whatever part of the query the
	// List function does not take — here the ship-name filter is taken and applied
	// to the ship roster before any report is folded, and the rest (any other
	// condition, the sort, the page) is left to the handler, which folds the rows in
	// memory (Collect): the contrast to the ledger's SQL pushdown. The board declares a
	// default page but no maximum, so limit=all is legal on it.
	//
	// Demonstrates: @computed, computed.domain, computed.compound-key, computed.conditional-grant, computed.fold, computed.take-filter, rpc.nested-shape, paging.limit-all, machine-identity.
	//
	// @computed
	// @permissionScope(domain)
	// @order(WorstReading desc)
	// @page(default: 10)
	SectorHazardBoard struct {
		ShipID       ccc.UUID  `spanner:"ShipId"`                           // @primarykey
		Subsystem    string    `spanner:"Subsystem"    allow_filter:"true"` // @primarykey
		ShipName     string    `spanner:"ShipName"     allow_filter:"true"`
		SectorID     string    `spanner:"SectorId"`
		WorstReading float64   `spanner:"WorstReading"`
		RecordedAt   time.Time `spanner:"RecordedAt"`
		Recent       []Reading
	}

	// Reading is one telemetry reading as the board carries it: the value and when
	// the droid recorded it.
	Reading struct {
		Value      float64
		RecordedAt time.Time
	}
)

// recentReadings caps how many readings a board row carries behind its worst one.
const recentReadings = 5

// Resource implements resource.Resourcer; computed resources declare their resource
// name by hand (there is no generated file to carry it).
func (SectorHazardBoard) Resource() accesstypes.Resource {
	return "SectorHazardBoards"
}

// ListSectorHazardBoard computes the worst reading per ship and subsystem in the
// request's sector. It takes the ship-name conditions out of the request's filter
// (qSet.Filter().Take) and applies them to the ship roster, so ships the caller did
// not ask about are never folded; every other part of the query — a subsystem
// condition, the sort, the page — is the generated handler's, applied over the
// rows this function yields.
func ListSectorHazardBoard(ctx context.Context, qSet *resource.QuerySet[SectorHazardBoard], client resource.Client, _ *Client) iter.Seq2[*SectorHazardBoard, error] {
	return func(yield func(*SectorHazardBoard, error) bool) {
		sector, err := sectorOf(qSet)
		if err != nil {
			yield(nil, err)

			return
		}

		var shipNames []string
		for _, condition := range qSet.Filter().Take("ShipName") {
			// Only an equality narrows the Ship query; any other operator goes
			// back to the handler by not being taken. Take is all-or-nothing per
			// field, so the condition is re-applied here for the operators the
			// query cannot express.
			if condition.Operator == opEqual {
				shipNames = append(shipNames, fmt.Sprint(condition.Value))
			} else {
				yield(nil, errors.Newf("ListSectorHazardBoard: shipName supports eq only, got %s", condition.Operator))

				return
			}
		}

		boards, err := worstReadings(ctx, client, sector, nil, "", shipNames)
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
func ReadSectorHazardBoard(ctx context.Context, shipID ccc.UUID, subsystem string, qSet *resource.QuerySet[SectorHazardBoard], client resource.Client, _ *Client) (*SectorHazardBoard, error) {
	sector, err := sectorOf(qSet)
	if err != nil {
		return nil, err
	}

	boards, err := worstReadings(ctx, client, sector, &shipID, subsystem, nil)
	if err != nil {
		return nil, err
	}
	for _, board := range boards {
		return board, nil
	}

	return nil, nil
}

// worstReadings folds droid reports down to the highest reading per (ship,
// subsystem) in the sector, optionally narrowed to one ship and subsystem or to
// the named ships, each row carrying its most recent readings newest first. Rows
// come back in a stable order; the generated handler sorts and pages them.
func worstReadings(ctx context.Context, client resource.Client, domain accesstypes.Domain, shipID *ccc.UUID, subsystem string, shipNames []string) ([]*SectorHazardBoard, error) {
	// The taken ship-name conditions narrow the roster before any report is
	// folded: a ship the caller did not ask about never enters the fold.
	names := make(map[ccc.UUID]string)
	for row, err := range resources.NewShipQuery().AddColumns(resources.NewShipColumns().All()).List(ctx, client) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.ShipQuery.List()")
		}
		if len(shipNames) > 0 && !slices.Contains(shipNames, row.Data.Name) {
			continue
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
		if _, known := names[row.Data.ShipID]; !known {
			// A ship the name filter excluded, or one outside the roster.
			continue
		}
		key := [2]string{row.Data.ShipID.String(), row.Data.Subsystem}
		board, ok := boards[key]
		if !ok {
			order = append(order, key)
			board = &SectorHazardBoard{
				ShipID:    row.Data.ShipID,
				Subsystem: row.Data.Subsystem,
				ShipName:  names[row.Data.ShipID],
				SectorID:  row.Data.SectorID,
			}
			boards[key] = board
		}
		board.Recent = append(board.Recent, Reading{Value: row.Data.Reading, RecordedAt: row.Data.RecordedAt})
		if len(board.Recent) == 1 || row.Data.Reading > board.WorstReading {
			board.WorstReading = row.Data.Reading
			board.RecordedAt = row.Data.RecordedAt
		}
	}
	for _, board := range boards {
		slices.SortFunc(board.Recent, func(a, b Reading) int { return b.RecordedAt.Compare(a.RecordedAt) })
		if len(board.Recent) > recentReadings {
			board.Recent = board.Recent[:recentReadings]
		}
	}

	result := make([]*SectorHazardBoard, 0, len(order))
	for _, key := range order {
		result = append(result, boards[key])
	}

	return result, nil
}
