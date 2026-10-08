// Package tracer provides convenience functions for OpenTelemetry tracing.
//
// It offers two main capabilities:
//
// 1. A "Start" function that simplifies trace creation by automatically determining
// the tracer and span names from the calling function's package and name.
//
// 2. Functions for setting up tracing: an HTTP middleware (NewHandler) and a
// TracerProvider (NewProvider) that exports over OTLP, to Google Cloud Trace or to an
// endpoint of the caller's own, and propagates W3C trace context.
package tracer

import (
	"net/http"

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

// NewHandler creates the HTTP middleware for OpenTelemetry tracing: a server span per
// request, continued from the trace context the request carries (read with Propagator:
// W3C traceparent, or the legacy X-Cloud-Trace-Context header a caller still sends),
// named by the request's URL path. The returned function wraps an http.Handler.
// Additional otelhttp.Option arguments customize the behavior.
func NewHandler(opts ...otelhttp.Option) func(http.Handler) http.Handler {
	options := make([]otelhttp.Option, 0, len(opts)+3)
	options = append(options,
		otelhttp.WithPropagators(Propagator()),
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
