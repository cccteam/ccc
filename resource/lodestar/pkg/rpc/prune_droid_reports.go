package rpc

import (
	"context"
	"time"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/cccteam/logger"
	"github.com/go-playground/errors/v5"
)

// DroidReportRetention is how long the telemetry link keeps a droid's reading before the
// nightly prune deletes it: ninety days.
const DroidReportRetention = 90 * 24 * time.Hour

// pruneEvent is the event source the prune's deletes are recorded under: the process,
// since no person makes them.
var pruneEvent = resource.ProcessEvent("PruneDroidReports")

type (
	// PruneDroidReports is the scheduled method. Every night at 03:30 headquarters time
	// (America/Denver, the zone the operations clock keeps) Cloud Scheduler calls it, and
	// it deletes the droid readings recorded more than DroidReportRetention ago, so the
	// telemetry the hazard board folds does not grow without end. No person calls it: the
	// generated router serves it at /_scheduled/prune-droid-reports behind the scheduler's
	// token check alone, the stack creates its Cloud Scheduler job from the release file,
	// and no role names it.
	//
	// It takes no input, since the scheduler sends no body, and it runs as the application
	// over every sector: no caller is stamped, so its read and its deletes are trusted, and
	// each delete is recorded under the process (resource.ProcessEvent). It answers how
	// many readings went and the cutoff, which the call's log carries.
	//
	// Demonstrates: @schedule.
	//
	// @rpc
	// @schedule("30 3 * * *", zone: "America/Denver")
	PruneDroidReports struct{}

	// PrunedReadings is the prune's outcome: how many readings it deleted, all of them
	// recorded before Before.
	PrunedReadings struct {
		Deleted int64
		Before  time.Time
	}
)

// Execute runs inside the handler's transaction: it reads every reading's identity and
// time, then deletes the ones recorded before the cutoff.
func (m *PruneDroidReports) Execute(ctx context.Context, txn resource.ReadWriteTransaction, _ *Client) (*PrunedReadings, error) {
	before := time.Now().UTC().Add(-DroidReportRetention)

	var stale []ccc.UUID
	for row, err := range resources.NewDroidReportQuery().AddColumns(resources.NewDroidReportColumns().ID().RecordedAt()).List(ctx, txn) {
		if err != nil {
			return nil, errors.Wrap(err, "resources.DroidReportQuery.List()")
		}
		if row.Data.RecordedAt.Before(before) {
			stale = append(stale, row.Data.ID)
		}
	}
	for _, id := range stale {
		if err := resources.NewDroidReportDeletePatch(id).Buffer(ctx, txn, pruneEvent); err != nil {
			return nil, errors.Wrap(err, "resources.DroidReportDeletePatch.Buffer()")
		}
	}
	logger.FromCtx(ctx).Infof("PruneDroidReports: deleted %d droid readings recorded before %s", len(stale), before.Format(time.RFC3339))

	return &PrunedReadings{Deleted: int64(len(stale)), Before: before}, nil
}
