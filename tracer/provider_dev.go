//go:build dev

package tracer

import (
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// NewGoogleCloudTracerProvider creates and configures a noop OpenTelemetry TracerProvider
// for disabling tracing in your dev environment.
func NewGoogleCloudTracerProvider(_, _ string, _ sdktrace.Sampler) (*Provider, error) {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)

	return tp
}
