// Package jobs is the seam between the application's scheduled methods and its job
// process: Starter, which starts one execution of the job with the command's arguments.
// The Cloud Run driver (resource/jobs/cloudrun) is the starter in production, built
// from the settings the served site's configuration embeds; None is the starter of an
// application with no job configured, and Fake records the starts in a test.
//
// The rule the seam serves: a job is started by the running service and never by a
// schedule or a hand. Cloud Scheduler calls a scheduled route on the service
// (resource/scheduled), and the method behind it starts the job through the Starter the
// configuration built, so a traffic rollback rolls the job back too, since each
// revision names the job built with it.
package jobs

import (
	"context"

	"github.com/go-playground/errors/v5"
)

// Starter starts the job process: the Cloud Run job in production, a fake in tests, or
// None where no job is configured.
type Starter interface {
	// Start starts one execution of the job with args as the container's arguments (the
	// job process's command and its flags, "cleanup-files" for the orphaned-file
	// cleanup) and answers the execution's resource name. The execution runs on; a
	// caller that wants its outcome reads it from Cloud Run.
	Start(ctx context.Context, args ...string) (execution string, err error)
}

// None is the starter of an application with no job configured: every start is refused,
// saying so.
type None struct{}

// Start refuses: no job process is configured.
func (None) Start(context.Context, ...string) (string, error) {
	return "", errors.New("no job process is configured, so the job cannot be started")
}
