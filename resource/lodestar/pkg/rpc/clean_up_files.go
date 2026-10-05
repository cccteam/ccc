package rpc

import (
	"context"

	"github.com/cccteam/ccc/resource"
	appjobs "github.com/cccteam/ccc/resource/lodestar/pkg/jobs"
	"github.com/cccteam/logger"
	"github.com/go-playground/errors/v5"
)

type (
	// CleanUpFiles is the scheduled method that starts the orphaned-file cleanup. Every
	// day at 09:00 UTC, outside every sector's working hours, Cloud Scheduler calls it,
	// and it starts one execution of the application's job process with the cleanup
	// command (cmd/jobs cleanup-files), through the starter the configuration built from
	// APP_JOBS_JOB (resource/jobs). The service starts its job, the job deployed with this
	// revision, and the cleanup never runs inside a request; where no job is configured
	// (development, a pull-request stack) the start is refused and the call says so.
	//
	// It takes no input and runs as the application; it answers the execution it started,
	// which the call's log carries, and the execution's own outcome is Cloud Run's to show.
	//
	// Demonstrates: jobs.start.
	//
	// @rpc
	// @schedule("0 9 * * *")
	CleanUpFiles struct{}

	// CleanupStarted is the method's outcome: the execution started.
	CleanupStarted struct {
		Execution string
	}
)

// Execute starts the job process on the cleanup command.
func (m *CleanUpFiles) Execute(ctx context.Context, _ resource.ReadWriteTransaction, c *Client) (*CleanupStarted, error) {
	execution, err := c.Jobs().Start(ctx, appjobs.CleanupCommand)
	if err != nil {
		return nil, errors.Wrap(err, "jobs.Starter.Start()")
	}
	logger.FromCtx(ctx).Infof("files: the orphaned-file cleanup started as %s", execution)

	return &CleanupStarted{Execution: execution}, nil
}
