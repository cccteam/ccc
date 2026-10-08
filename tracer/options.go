package tracer

import (
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// providerConfig is what the provider options set.
type providerConfig struct {
	// endpoint is the OTLP gRPC host:port the spans go to; "" is Google Cloud's Telemetry
	// API.
	endpoint string
	// insecure sends without TLS and without Google credentials: a collector of your own.
	insecure   bool
	tracerOpts []sdktrace.TracerProviderOption
}

// ProviderOption configures the Provider.
type ProviderOption func(*providerConfig)

// WithEndpoint sends the spans to an OTLP gRPC endpoint (host:port) in place of Google
// Cloud's Telemetry API: a collector of your own.
func WithEndpoint(hostport string) ProviderOption {
	return func(c *providerConfig) {
		c.endpoint = hostport
	}
}

// WithInsecure sends without TLS and without Google credentials, for a collector on a
// loopback or private address; the Telemetry API refuses it.
func WithInsecure() ProviderOption {
	return func(c *providerConfig) {
		c.insecure = true
	}
}

// WithTracerProviderOptions adds OpenTelemetry SDK tracer provider options.
func WithTracerProviderOptions(opts ...sdktrace.TracerProviderOption) ProviderOption {
	return func(c *providerConfig) {
		c.tracerOpts = append(c.tracerOpts, opts...)
	}
}
