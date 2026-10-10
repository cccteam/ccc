package config

import (
	"context"

	"github.com/cccteam/ccc/resource/jobs"
	jobstarter "github.com/cccteam/ccc/resource/jobs/cloudrun"
	"github.com/cccteam/ccc/resource/scheduled"
	"github.com/go-playground/errors/v5"
	"github.com/go-playground/validator/v10"
	"github.com/sethvargo/go-envconfig"
)

// SiteConfiguration is the third level: the served site. A flat application is one site,
// so the level is the one the sites layout declares once and every site reads.
type SiteConfiguration struct {
	*DataConfiguration
	env          *siteConfig
	validator    *validator.Validate
	droidsAPIKey string
	scheduler    *scheduled.Guard
	// jobs is the job driver: the starter of this build's job, or of none.
	jobs *jobstarter.Driver
}

// NewSiteConfiguration loads every level and constructs the served site's
// dependencies.
func NewSiteConfiguration(ctx context.Context) (*SiteConfiguration, error) {
	data, err := NewDataConfiguration(ctx)
	if err != nil {
		return nil, err
	}

	env := &siteConfig{}
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: env, Lookuper: envconfig.OsLookuper()}); err != nil {
		return nil, errors.Wrap(err, "envconfig.ProcessWith()")
	}

	droidsAPIKey := env.DroidsAPIKey
	if droidsAPIKey == "" {
		// An ephemeral key keeps the droids outlet fail-closed: nothing knows it, so
		// nothing authenticates until APP_DROIDS_API_KEY is configured.
		droidsAPIKey, err = ephemeralKey()
		if err != nil {
			return nil, err
		}
	}

	// The scheduled routes' guard reads the invoker identity the stack names in
	// APP_SCHEDULER_INVOKER; without one it logs that the scheduled routes are off.
	scheduler, err := scheduled.FromEnvironment(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "scheduled.FromEnvironment()")
	}
	// The job driver names the job of this build from the template job the stack sets
	// (jobstarter.Settings, embedded in the site's environment) and the version the image
	// bakes in, and starts it through the Cloud Run Admin API; without a template, as in
	// development, every start is refused saying so, and the start logs it.
	starter, err := jobstarter.Open(ctx, env.Settings, data.AppVersion())
	if err != nil {
		return nil, errors.Wrap(err, "jobstarter.Open()")
	}

	return &SiteConfiguration{
		DataConfiguration: data,
		env:               env,
		validator:         validator.New(),
		droidsAPIKey:      droidsAPIKey,
		scheduler:         scheduler,
		jobs:              starter,
	}, nil
}

// Close releases the job driver, then the levels below.
func (c *SiteConfiguration) Close() {
	c.jobs.Close()
	c.DataConfiguration.Close()
}

// Addr returns the TCP address the site listens on, in the form ":port".
func (c *SiteConfiguration) Addr() string {
	return ":" + c.env.Port
}

// Validator returns the request payload validator.
func (c *SiteConfiguration) Validator() *validator.Validate {
	return c.validator
}

// ConsoleDist returns the directory the console's built Angular bundle is served from.
func (c *SiteConfiguration) ConsoleDist() string {
	return c.env.ConsoleDist
}

// PortalDist returns the directory the portal's built Angular bundle is served from.
func (c *SiteConfiguration) PortalDist() string {
	return c.env.PortalDist
}

// DroidsAPIKey returns the bearer key the droids outlet's API-key middleware validates
// droid clients against.
func (c *SiteConfiguration) DroidsAPIKey() string {
	return c.droidsAPIKey
}

// Scheduler returns the guard the scheduled routes sit behind: Cloud Scheduler's tokens
// of the invoker identity APP_SCHEDULER_INVOKER names, and nothing else.
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

// siteConfig holds the environment only the served site reads.
type siteConfig struct {
	// Port is the TCP port the server listens on.
	Port string `env:"PORT,default=8080"`

	// ConsoleDist is the directory holding the console's built Angular bundle.
	ConsoleDist string `env:"APP_CONSOLE_DIST,default=web/dist/console"`

	// PortalDist is the directory holding the portal's built Angular bundle.
	PortalDist string `env:"APP_PORTAL_DIST,default=web/dist/portal"`

	// DroidsAPIKey is the bearer key droids present on the droids outlet (/droids/...).
	// Unset, an ephemeral key is generated at startup, which keeps the surface
	// fail-closed but unreachable until a key is configured.
	DroidsAPIKey string `env:"APP_DROIDS_API_KEY"`

	// The job driver's variables (jobstarter.Settings, whichever driver the import names:
	// the Cloud Run driver's template job this build's job is named from).
	jobstarter.Settings
}
