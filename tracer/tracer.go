// Package tracer provides convenience functions for setting up OpenTelemetry tracing
// with Google Cloud Trace. It simplifies the creation of HTTP middleware for tracing
// requests and configuring the global tracer provider.
package tracer

import (
	"net/http"
	"path"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	"github.com/GoogleCloudPlatform/opentelemetry-operations-go/propagator"
	"github.com/go-playground/errors/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// NewGoogleCloudHandler creates a new HTTP middleware for OpenTelemetry tracing,
// specifically configured for Google Cloud Trace.
//
// It uses the CloudTraceFormatPropagator and sets the span name to the request URL path.
// The returned function can be used to wrap an http.Handler to add tracing.
// Additional otelhttp.Option arguments can be passed to customize the behavior.
func NewGoogleCloudHandler(o ...otelhttp.Option) func(http.Handler) http.Handler {
	opts := []otelhttp.Option{
		otelhttp.WithPropagators(propagator.CloudTraceFormatPropagator{}),
		otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.URL.Path
		}),
	}

	opts = append(opts, o...)

	return func(next http.Handler) http.Handler {
		return otelhttp.NewHandler(next, "", opts...)
	}
}

// Provider is an OpenTelemetry Provider. It provides Tracers to
// instrumentation so it can trace operational flow through a system.
type Provider = sdktrace.TracerProvider

// NewGoogleCloudTracerProvider creates and configures a new OpenTelemetry TracerProvider
// for use with Google Cloud Trace. It sets up an exporter to send traces to the
// specified Google Cloud project.
//
// The created TracerProvider is also set as the global tracer provider for the application.
func NewGoogleCloudTracerProvider(loggingProjectID, serviceName string, sampler sdktrace.Sampler) (*Provider, error) {
	exporter, err := texporter.New(texporter.WithProjectID(loggingProjectID))
	if err != nil {
		return nil, errors.Wrap(err, "texporter.New()")
	}

	res, err := traceResource(serviceName)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)
	otel.SetTracerProvider(tp)

	return tp, nil
}

// NewNoopTracerProvider creates a new no-op TracerProvider and sets it as the
// global tracer provider. A no-op provider discards all spans and is useful
// for disabling tracing in environments like tests.
func NewNoopTracerProvider() *Provider {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)

	return tp
}

func traceResource(serviceName string) (*resource.Resource, error) {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		if resource.Default().SchemaURL() != semconv.SchemaURL {
			return nil, errors.Newf("conflicting package versions installed: upgrade semconv package to go.opentelemetry.io/otel/semconv/v%s", path.Base(resource.Default().SchemaURL()))
		}

		return nil, errors.Wrap(err, "resource.Merge()")
	}

	return res, nil
}
