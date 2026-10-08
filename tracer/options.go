package tracer

import (
	"strings"

	"github.com/go-playground/errors/v5"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"golang.org/x/oauth2"
)

// Sampling says which spans are recorded.
type Sampling string

const (
	// SamplingEdge records a request's spans when the trace context the caller sent says
	// the request is sampled, and no others; a span with no parent is never recorded.
	// Google's load balancers and Cloud Run sample a share of the requests at the edge
	// and say so in the headers they forward, so an environment on this setting traces
	// what the edge traces. The default.
	SamplingEdge Sampling = "edge"
	// SamplingAll records every span, whatever the caller said: an environment where
	// every request should be traceable, a pull request's or a test one.
	SamplingAll Sampling = "all"
)

// ParseSampling reads a Sampling from configuration: "edge", "all", or "" for the
// default.
func ParseSampling(value string) (Sampling, error) {
	switch s := Sampling(strings.ToLower(strings.TrimSpace(value))); s {
	case "":
		return SamplingEdge, nil
	case SamplingEdge, SamplingAll:
		return s, nil
	default:
		return "", errors.Newf("%q is not a sampling setting (%s, %s)", value, SamplingEdge, SamplingAll)
	}
}

// providerConfig is what the provider options set.
type providerConfig struct {
	// endpoint is the OTLP gRPC host:port the spans go to; "" is Google Cloud's Telemetry
	// API.
	endpoint string
	// insecure sends without TLS and without Google credentials: a collector of your own.
	insecure bool
	// sampling says which spans are recorded; "" is SamplingEdge.
	sampling Sampling
	// tokenSource signs the calls to the Telemetry API in place of the application's
	// default credentials.
	tokenSource oauth2.TokenSource
	tracerOpts  []sdktrace.TracerProviderOption
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

// WithSampling says which spans are recorded; SamplingEdge without it.
func WithSampling(s Sampling) ProviderOption {
	return func(c *providerConfig) {
		c.sampling = s
	}
}

// WithTokenSource signs the calls to the Telemetry API with a token source of the
// caller's own in place of the application's default credentials: a process whose
// identity is not the environment's, such as a device's.
func WithTokenSource(ts oauth2.TokenSource) ProviderOption {
	return func(c *providerConfig) {
		c.tokenSource = ts
	}
}
