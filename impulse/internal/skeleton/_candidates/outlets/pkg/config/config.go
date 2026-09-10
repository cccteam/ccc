// Package config loads the application's configuration from the environment and
// constructs the shared clients its processes depend on.
//
// Configuration is a chain of levels, one per kind of process, each embedding the one
// below it: core (every process), data (every process that opens the database), and
// server (the served application). A variable lives at the lowest level whose consumer
// uses it, so a process declares — through the constructor it calls — exactly the
// environment it needs, and a deploy step supplies no more than that.
package config

import (
	"context"
	"log"

	"cloud.google.com/go/logging"
	"github.com/cccteam/logger"
	"github.com/go-playground/errors/v5"
	"github.com/sethvargo/go-envconfig"
)

// coreConfiguration is the first level: what every process of the application shares.
type coreConfiguration struct {
	env           *coreConfig
	loggingClient *logging.Client
}

func newCoreConfiguration(ctx context.Context) (*coreConfiguration, error) {
	env := &coreConfig{}
	if err := envconfig.ProcessWith(ctx, &envconfig.Config{Target: env, Lookuper: envconfig.OsLookuper()}); err != nil {
		return nil, errors.Wrap(err, "envconfig.ProcessWith()")
	}

	conf := &coreConfiguration{env: env}
	if env.LoggingProjectID != "" {
		client, err := logging.NewClient(ctx, env.LoggingProjectID)
		if err != nil {
			return nil, errors.Wrap(err, "logging.NewClient()")
		}
		conf.loggingClient = client
	}

	return conf, nil
}

// Close releases the level's clients.
func (c *coreConfiguration) Close() {
	if c.loggingClient != nil {
		if err := c.loggingClient.Close(); err != nil {
			log.Print(errors.Wrap(err, "logging.Client.Close()"))
		}
	}
}

// LogExporter returns where request logs go: Cloud Logging when a logging project is
// configured, the console otherwise.
func (c *coreConfiguration) LogExporter() logger.Exporter {
	if c.loggingClient != nil {
		return logger.NewGoogleCloudExporter(c.loggingClient, c.env.LoggingProjectID)
	}

	return logger.NewConsoleExporter()
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

	// LoggingProjectID is the Google Cloud project request logs ship to. Empty logs
	// to the console.
	LoggingProjectID string `env:"GOOGLE_CLOUD_LOGGING_PROJECT"`
}
