//go:build dev

package tracer

import (
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// NewGoogleCloudTracerProvider creates and configures a noop OpenTelemetry TracerProvider
// for disabling tracing in your dev environment.
func NewGoogleCloudTracerProvider(_, _ string, _ ...sdktrace.TracerProviderOption) (*Provider, error) {
	return NewGoogleCloudTracerProviderWithOptions("", "")
}

// NewGoogleCloudTracerProviderWithOptions creates and configures a noop OpenTelemetry
// TracerProvider; the propagator is still set, so a request's trace context is read.
func NewGoogleCloudTracerProviderWithOptions(_, _ string, _ ...ProviderOption) (*Provider, error) {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(Propagator())

	return &Provider{tp}, nil
}
