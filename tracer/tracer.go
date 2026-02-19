// Package tracer provides convenience functions for OpenTelemetry tracing.
//
// It offers two main capabilities:
//
// 1. A "Start" function that simplifies trace creation by automatically determining
// the tracer and span names from the calling function's package and name.
//
// 2. Functions for setting up tracing with Google Cloud Trace, including
// an HTTP middleware and a TracerProvider that exports to Google Cloud.
package tracer

import (
	"net/http"

	"github.com/GoogleCloudPlatform/opentelemetry-operations-go/propagator"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Provider is an OpenTelemetry Provider. It provides Tracers to
// instrumentation so it can trace operational flow through a system.
//
// This wrapper is used to keep otel dependency management contained in this package.
// Cordinating package versions between otel/sdk/resource package and the otel/semconv
// package is painful, so eliminating the need to import otel as a direct import in
// your project helps to keep the version cordination in one place.
type Provider struct {
	*sdktrace.TracerProvider
}

// NewGoogleCloudHandler creates a new HTTP middleware for OpenTelemetry tracing,
// specifically configured for Google Cloud Trace.
//
// It uses the CloudTraceFormatPropagator and sets the span name to the request URL path.
// The returned function can be used to wrap an http.Handler to add tracing.
// Additional otelhttp.Option arguments can be passed to customize the behavior.
func NewGoogleCloudHandler(opts ...otelhttp.Option) func(http.Handler) http.Handler {
	options := make([]otelhttp.Option, 0, len(opts)+3)
	options = append(options,
		otelhttp.WithPropagators(propagator.CloudTraceFormatPropagator{}),
		otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.URL.Path
		}),
	)

	options = append(options, opts...)

	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "", options...)
	}
}
