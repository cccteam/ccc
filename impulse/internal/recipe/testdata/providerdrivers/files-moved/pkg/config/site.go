package config

import (
	"context"

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
	env       *siteConfig
	validator *validator.Validate
	// scheduler is the guard the scheduled routes sit behind (scheduled.go).
	scheduler *scheduled.Guard
	// jobs is the job driver: the starter of this build's job, or of none (scheduled.go).
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

	// The scheduled routes' guard (scheduled.go): it reads the invoker identity the stack
	// names in APP_SCHEDULER_INVOKER, and without one logs that the scheduled routes are off.
	scheduler, err := scheduled.FromEnvironment(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "scheduled.FromEnvironment()")
	}

	// The job driver (scheduled.go): it names the job of this build from the template job the
	// stack sets in APP_JOBS_TEMPLATE, the setting the site's environment embeds, and the
	// version the image bakes in, and without a template refuses every start.
	starter, err := jobstarter.Open(ctx, env.Settings, data.AppVersion())
	if err != nil {
		return nil, errors.Wrap(err, "jobstarter.Open()")
	}

	return &SiteConfiguration{
		DataConfiguration: data,
		env:               env,
		validator:         validator.New(),
		scheduler:         scheduler,
		jobs:              starter,
	}, nil
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

// siteConfig holds the environment only the served site reads.
type siteConfig struct {
	// Port is the TCP port the server listens on.
	Port string `env:"PORT,default=8080"`

	// ConsoleDist is the directory holding the console's built Angular bundle.
	ConsoleDist string `env:"APP_CONSOLE_DIST,default=web/dist/console"`

	// The job driver's variables (jobstarter.Settings, whichever driver the import names:
	// the Cloud Run driver's template job this build's job is named from).
	jobstarter.Settings
}
