// Package gcp is the Google Cloud driver: from the variables it declares, it builds the
// pieces of an application that are Google's, request logs to Cloud Logging and spans to
// Cloud Trace. An application embeds Settings in its configuration and calls Open, and
// names no cloud in its own code; moving to another cloud swaps this import and the
// embedded settings for that cloud's.
package gcp

import (
	"context"
	"time"

	"cloud.google.com/go/logging"
	"github.com/cccteam/ccc/tracer"
	"github.com/cccteam/logger"
	"github.com/go-playground/errors/v5"
)

// closeTimeout bounds the wait for the spans still in hand when the process ends.
const closeTimeout = 5 * time.Second

// Settings are the variables the driver reads, declared on the application's
// configuration by embedding, so the stack and the development environment render them
// from the declaration.
type Settings struct {
	// LoggingProject is the Google Cloud project request logs ship to and spans are
	// recorded in. Empty logs to the console and exports no span: development.
	LoggingProject string `env:"GOOGLE_CLOUD_LOGGING_PROJECT"`
	// TraceSampling says which spans are recorded: every one (all), or the ones a request
	// Google's edge sampled starts (edge).
	TraceSampling string `env:"APP_TRACE_SAMPLING,default=edge"`
}

// Driver is what Open built: where the process's logs and spans go, closed together.
type Driver struct {
	// LogExporter is where request logs go: Cloud Logging, or the console without a
	// logging project.
	LogExporter logger.Exporter
	// TraceProvider records and exports the process's spans; nil without a logging
	// project, when nothing is exported.
	TraceProvider *tracer.Provider
	client        *logging.Client
}

// Open builds the driver for a process named serviceName: the Cloud Logging client and
// exporter for the logging project, and the trace provider that records the spans the
// sampling says and exports them to Cloud Trace in that project. Without a logging
// project the logs go to the console and no provider is set, so every span is a noop.
func Open(ctx context.Context, s Settings, serviceName string) (*Driver, error) {
	sampling, err := tracer.ParseSampling(s.TraceSampling)
	if err != nil {
		return nil, errors.Wrap(err, "tracer.ParseSampling()")
	}
	if s.LoggingProject == "" {
		return &Driver{LogExporter: logger.NewConsoleExporter()}, nil
	}
	client, err := logging.NewClient(ctx, s.LoggingProject)
	if err != nil {
		return nil, errors.Wrap(err, "logging.NewClient()")
	}
	provider, err := tracer.NewProvider(serviceName, tracer.WithGoogleCloud(s.LoggingProject), tracer.WithSampling(sampling))
	if err != nil {
		if closeErr := client.Close(); closeErr != nil {
			return nil, errors.Wrap(errors.Join(err, closeErr), "tracer.NewProvider()")
		}

		return nil, errors.Wrap(err, "tracer.NewProvider()")
	}

	return &Driver{
		LogExporter:   logger.NewGoogleCloudExporter(client, s.LoggingProject),
		TraceProvider: provider,
		client:        client,
	}, nil
}

// Close sends the spans still in hand, then releases the logging client.
func (d *Driver) Close() error {
	var errs []error
	if d.TraceProvider != nil {
		ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
		defer cancel()
		if err := d.TraceProvider.Shutdown(ctx); err != nil {
			errs = append(errs, errors.Wrap(err, "tracer.Provider.Shutdown()"))
		}
	}
	if d.client != nil {
		if err := d.client.Close(); err != nil {
			errs = append(errs, errors.Wrap(err, "logging.Client.Close()"))
		}
	}

	if len(errs) == 0 {
		return nil
	}

	return errors.Wrap(errors.Join(errs...), "gcp.Driver.Close()")
}
