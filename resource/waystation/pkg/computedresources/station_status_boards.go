package computedresources

import (
	"context"
	"iter"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/waystation/pkg/resources"
	"github.com/go-playground/errors/v5"
)

type (
	// StationStatusBoard is the human window onto sensor telemetry: the latest
	// reading per facility and metric. Sensor readings themselves live on the
	// automation outlet only; this computed resource is how the browser sees them.
	// Its compound key is declared with two primarykey annotations.
	//
	// It is domain-scoped: permission checks run in the URL waystation's partition.
	// Like domain-scoped virtual resources, the row set itself is not yet
	// tenancy-filtered by the framework (rows carry their WaystationId); structural
	// tenancy injection is the E2 work.
	//
	// @computed
	// @permissionScope(domain)
	StationStatusBoard struct {
		FacilityID    ccc.UUID  `spanner:"FacilityId"` // @primarykey
		Metric        string    `spanner:"Metric"`     // @primarykey
		FacilityName  string    `spanner:"FacilityName"`
		WaystationID  string    `spanner:"WaystationId"`
		LatestReading float64   `spanner:"LatestReading"`
		RecordedAt    time.Time `spanner:"RecordedAt"`
	}
)

// Resource implements resource.Resourcer; computed resources declare their resource
// name by hand (there is no generated file to carry it).
func (StationStatusBoard) Resource() accesstypes.Resource {
	return "StationStatusBoards"
}

// ListStationStatusBoard computes the latest reading per facility and metric.
func ListStationStatusBoard(ctx context.Context, _ *resource.QuerySet[StationStatusBoard], client resource.Client, _ *Client) iter.Seq2[*StationStatusBoard, error] {
	return func(yield func(*StationStatusBoard, error) bool) {
		boards, err := latestReadings(ctx, client, nil, "")
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

// ReadStationStatusBoard computes the latest reading for one facility and metric; nil
// when that pair has no readings.
func ReadStationStatusBoard(ctx context.Context, facilityID ccc.UUID, metric string, _ *resource.QuerySet[StationStatusBoard], client resource.Client, _ *Client) (*StationStatusBoard, error) {
	boards, err := latestReadings(ctx, client, &facilityID, metric)
	if err != nil {
		return nil, err
	}
	for _, board := range boards {
		return board, nil
	}

	return nil, nil
}

// latestReadings folds sensor readings down to the latest per (facility, metric),
// optionally narrowed to one facility and metric.
func latestReadings(ctx context.Context, client resource.Client, facilityID *ccc.UUID, metric string) (map[[2]string]*StationStatusBoard, error) {
	names := make(map[ccc.UUID]string)
	for row, err := range resources.NewFacilityQuery().AddColumns(resources.NewFacilityColumns().All()).List(ctx, client) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.FacilityQuery.List()")
		}
		names[row.Data.ID] = row.Data.Name
	}

	query := resources.NewSensorReadingQuery().AddColumns(resources.NewSensorReadingColumns().All())
	if facilityID != nil {
		// Metric is not a filterable column (no index, no allow_filter), so the
		// metric narrowing happens in Go below.
		query = query.Where(resources.NewSensorReadingQueryClause().FacilityID().Equal(*facilityID))
	}

	boards := make(map[[2]string]*StationStatusBoard)
	for row, err := range query.List(ctx, client) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.SensorReadingQuery.List()")
		}
		if facilityID != nil && row.Data.Metric != metric {
			continue
		}
		key := [2]string{row.Data.FacilityID.String(), row.Data.Metric}
		if existing, ok := boards[key]; ok && !row.Data.RecordedAt.After(existing.RecordedAt) {
			continue
		}
		boards[key] = &StationStatusBoard{
			FacilityID:    row.Data.FacilityID,
			Metric:        row.Data.Metric,
			FacilityName:  names[row.Data.FacilityID],
			WaystationID:  row.Data.WaystationID,
			LatestReading: row.Data.Reading,
			RecordedAt:    row.Data.RecordedAt,
		}
	}

	return boards, nil
}
