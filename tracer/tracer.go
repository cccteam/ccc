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

// HandlerOption configures NewHandler.
type HandlerOption func(*handlerConfig)

// handlerConfig is what the handler options set.
type handlerConfig struct {
	// otelOptions are the instrumentation's own options, applied after the handler's.
	otelOptions []otelhttp.Option
	// surfaces are the declared surfaces, matched by path as each request starts.
	surfaces surfaceTable
}

// WithOTelHTTPOptions adds options of the OpenTelemetry HTTP instrumentation (otelhttp)
// to the handler, after the handler's own: a filter, a span name formatter of the
// caller's, or the tracer provider to start spans from in a test.
func WithOTelHTTPOptions(opts ...otelhttp.Option) HandlerOption {
	return func(c *handlerConfig) {
		c.otelOptions = append(c.otelOptions, opts...)
	}
}

// Surfaces declares how each surface's spans are sampled, keyed by the path the router
// mounts the surface at: a prefix such as /droids/ or /beacons/, or a route pattern such
// as /api/widgets/{widgetID}/content, where a segment in braces matches any one segment.
// As a request starts, the handler finds the longest declared surface the request's path
// sits under and hands its setting to the sampler the provider built, which applies it
// as the span starts; a request under no declared surface follows the front end. The
// generated router passes the table from the declarations in the generator program.
func Surfaces(table map[string]Traces) HandlerOption {
	return func(c *handlerConfig) {
		c.surfaces = append(c.surfaces, newSurfaceTable(table)...)
	}
}

// NewHandler creates the HTTP middleware for OpenTelemetry tracing: a server span per
// request, continued from the trace context the request carries (read with Propagator:
// W3C traceparent, or the legacy X-Cloud-Trace-Context header a caller still sends),
// named by the request's URL path. The returned function wraps an http.Handler.
// Surfaces sets a per-surface trace setting, and WithOTelHTTPOptions passes options of
// the instrumentation itself.
func NewHandler(opts ...HandlerOption) func(http.Handler) http.Handler {
	cfg := &handlerConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	options := make([]otelhttp.Option, 0, len(cfg.otelOptions)+3)
	options = append(options,
		otelhttp.WithPropagators(Propagator()),
		otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.URL.Path
		}),
	)
	options = append(options, cfg.otelOptions...)
	surfaces := newSurfaceTable(nil)
	surfaces = append(surfaces, cfg.surfaces...)

	return func(next http.Handler) http.Handler {
		traced := otelhttp.NewHandler(next, "", options...)
		if len(surfaces) == 0 {
			return traced
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if t, ok := surfaces.match(r.URL.Path); ok {
				r = r.WithContext(contextWithTraces(r.Context(), t))
			}
			traced.ServeHTTP(w, r)
		})
	}
}
