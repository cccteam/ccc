//go:build dev

package tracer

import (
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// NewProvider creates a noop OpenTelemetry TracerProvider, for a development build
// (-tags dev) that exports nothing; the propagator is still set, so a request's trace
// context is read, and the sampler is the one the exporting provider installs, so the
// sampled flag a development build forwards is decided the same way.
func NewProvider(_ string, opts ...ProviderOption) (*Provider, error) {
	cfg := &providerConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sampler(cfg.sampling)))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(Propagator())

	return &Provider{tp}, nil
}
