//go:build dev

package tracer

import (
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/api/option"
)

// NewGoogleCloudTracerProvider creates and configures a noop OpenTelemetry TracerProvider
// for disabling tracing in your dev environment.
func NewGoogleCloudTracerProvider(_, _ string, _ ...sdktrace.TracerProviderOption) (*Provider, error) {
	return NewGoogleCloudTracerProviderWithOptions("", "")
}

// NewGoogleCloudTracerProviderWithOptions creates and configures a noop OpenTelemetry TracerProvider.
func NewGoogleCloudTracerProviderWithOptions(_, _ string, _ ...ProviderOption) (*Provider, error) {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)

	return &Provider{tp}, nil
}

type providerConfig struct {
	clientOpts []option.ClientOption
	tracerOpts []sdktrace.TracerProviderOption
}

// ProviderOption configures the Provider.
type ProviderOption func(*providerConfig)

// WithClientOptions adds Google API client options.
func WithClientOptions(opts ...option.ClientOption) ProviderOption {
	return func(c *providerConfig) {
		c.clientOpts = append(c.clientOpts, opts...)
	}
}

// WithTracerProviderOptions adds OpenTelemetry SDK tracer provider options.
func WithTracerProviderOptions(opts ...sdktrace.TracerProviderOption) ProviderOption {
	return func(c *providerConfig) {
		c.tracerOpts = append(c.tracerOpts, opts...)
	}
}
