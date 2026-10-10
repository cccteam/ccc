package config

import (
	"github.com/cccteam/ccc/resource/jobs"
	"github.com/cccteam/ccc/resource/scheduled"
)

// Scheduler returns the guard the scheduled routes sit behind: Cloud Scheduler's tokens of
// the invoker identity APP_SCHEDULER_INVOKER names, and nothing else. With the variable
// unset, as in development, every scheduled call is refused.
func (c *SiteConfiguration) Scheduler() *scheduled.Guard {
	return c.scheduler
}

// Jobs starts the application's job process: the job driver (resource/jobs/cloudrun),
// which names the job of this build from the template job the stack sets in
// APP_JOBS_TEMPLATE and the version the image bakes in, or refuses every start where no
// template is configured.
func (c *SiteConfiguration) Jobs() jobs.Starter {
	return c.jobs
}

// Close releases the job driver, then the levels below.
func (c *SiteConfiguration) Close() {
	c.jobs.Close()
	c.DataConfiguration.Close()
}
