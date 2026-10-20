// Package config loads the application's configuration from the environment and
// constructs the shared clients its processes depend on.
//
// Configuration is a chain of levels, one per kind of process, each embedding the one
// below it: core (every process), data (every process that opens the database), and
// site (the served site). A variable lives at the lowest level whose consumer
// uses it, so a process declares — through the constructor it calls — exactly the
// environment it needs, and a deploy step supplies no more than that.
package config

import (
	"context"
	"log"

	cloud "github.com/cccteam/ccc/cloud/gcp"
	"github.com/cccteam/logger"
	"github.com/go-playground/errors/v5"
	"github.com/sethvargo/go-envconfig"
)

// coreConfiguration is the first level: what every process of the application shares.
type coreConfiguration struct {
	env *coreConfig
	// cloud is the cloud driver: where the process's logs and spans go.
	cloud *cloud.Driver
}

func newCoreConfiguration(ctx context.Context) (*coreConfiguration, error) {
	env := &coreConfig{}
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: env, Lookuper: envconfig.OsLookuper()}); err != nil {
		return nil, errors.Wrap(err, "envconfig.ProcessWith()")
	}

	// The cloud driver builds the log exporter and the trace provider from the settings
	// the configuration embeds; without a logging project the logs go to the console and
	// no span is exported.
	driver, err := cloud.Open(ctx, env.Settings, env.ServiceName)
	if err != nil {
		return nil, errors.Wrap(err, "cloud.Open()")
	}

	return &coreConfiguration{env: env, cloud: driver}, nil
}

// Close releases the level's clients: the spans still in hand are sent first.
func (c *coreConfiguration) Close() {
	if err := c.cloud.Close(); err != nil {
		log.Print(err)
	}
}

// LogExporter returns where request logs go: Cloud Logging when a logging project is
// configured, the console otherwise.
func (c *coreConfiguration) LogExporter() logger.Exporter {
	return c.cloud.LogExporter
}

// AppVersion returns the build-time or runtime application version.
func (c *coreConfiguration) AppVersion() string {
	return c.env.AppVersion
}

// ServiceName returns the name the process reports in logs.
func (c *coreConfiguration) ServiceName() string {
	return c.env.ServiceName
}

// coreConfig holds the environment every process reads.
type coreConfig struct {
	// AppVersion is the build-time or runtime application version.
	AppVersion string `env:"APP_VERSION,default=dev"`

	// ServiceName names the process in logs.
	ServiceName string `env:"APP_SERVICE_NAME,required"`

	// The cloud driver's variables (cloud.Settings, whichever driver the import names: the
	// Google Cloud driver's logging project and trace sampling).
	cloud.Settings
}
