//go:build dev

package tracer

import (
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// NewProvider creates a noop OpenTelemetry TracerProvider, for a development build
// (-tags dev) that exports nothing; the propagator is still set, so a request's trace
// context is read.
func NewProvider(_ string, _ ...ProviderOption) (*Provider, error) {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(Propagator())

	return &Provider{tp}, nil
}
