//go:build !dev

package tracer

import (
	"context"
	"path"

	"github.com/go-playground/errors/v5"
	"go.opentelemetry.io/contrib/detectors/gcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/oauth"
)

const (
	// telemetryEndpoint is Google Cloud's OTLP endpoint, the Telemetry API: a span sent
	// here lands in Cloud Trace, in the project the gcp.project_id resource attribute
	// names.
	telemetryEndpoint = "telemetry.googleapis.com:443"
	// cloudPlatformScope is the OAuth scope the Telemetry API accepts.
	cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"
	// projectIDAttribute is the resource attribute that routes a span to its project.
	projectIDAttribute = "gcp.project_id"
)

// NewGoogleCloudTracerProvider creates and configures a new OpenTelemetry TracerProvider
// for Google Cloud Trace: the spans are exported over OTLP to the Telemetry API with the
// application's default credentials, into the project given, under the service name. The
// provider is set as the global tracer provider, and Propagator (W3C Trace Context, with
// the legacy X-Cloud-Trace-Context header read) as the global propagator. Which spans are
// recorded is the Sampling (WithSampling): the edge's choice without it.
//
// The application's identity needs roles/telemetry.tracesWriter on the project, and the
// project needs the Telemetry API (telemetry.googleapis.com) enabled.
func NewGoogleCloudTracerProvider(projectID, serviceName string, opts ...sdktrace.TracerProviderOption) (*Provider, error) {
	return NewGoogleCloudTracerProviderWithOptions(projectID, serviceName, WithTracerProviderOptions(opts...))
}

// NewGoogleCloudTracerProviderWithOptions creates and configures a new OpenTelemetry TracerProvider.
func NewGoogleCloudTracerProviderWithOptions(projectID, serviceName string, opts ...ProviderOption) (*Provider, error) {
	cfg := &providerConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	ctx := context.Background()
	exporter, err := newExporter(ctx, cfg)
	if err != nil {
		return nil, err
	}
	res, err := newResource(ctx, projectID, serviceName)
	if err != nil {
		return nil, err
	}

	options := make([]sdktrace.TracerProviderOption, 0, len(cfg.tracerOpts)+3)
	options = append(options,
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler(cfg.sampling)),
	)
	options = append(options, cfg.tracerOpts...)

	tp := sdktrace.NewTracerProvider(options...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(Propagator())

	return &Provider{tp}, nil
}

// newExporter is the OTLP gRPC exporter: to the Telemetry API over TLS with the
// application's default credentials on every call, or to the endpoint configured.
func newExporter(ctx context.Context, cfg *providerConfig) (*otlptrace.Exporter, error) {
	endpoint := cfg.endpoint
	if endpoint == "" {
		endpoint = telemetryEndpoint
	}
	options := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(endpoint)}
	if cfg.insecure {
		options = append(options, otlptracegrpc.WithInsecure())
	} else {
		creds, err := perRPCCredentials(ctx, cfg)
		if err != nil {
			return nil, err
		}
		options = append(options,
			otlptracegrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, "")),
			otlptracegrpc.WithDialOption(grpc.WithPerRPCCredentials(creds)),
		)
	}
	exporter, err := otlptracegrpc.New(ctx, options...)
	if err != nil {
		return nil, errors.Wrap(err, "otlptracegrpc.New()")
	}

	return exporter, nil
}

// perRPCCredentials signs every call to the Telemetry API: with the caller's token source
// when one was given, else with the application's default credentials.
func perRPCCredentials(ctx context.Context, cfg *providerConfig) (credentials.PerRPCCredentials, error) {
	if cfg.tokenSource != nil {
		return oauth.TokenSource{TokenSource: cfg.tokenSource}, nil
	}
	creds, err := oauth.NewApplicationDefault(ctx, cloudPlatformScope)
	if err != nil {
		return nil, errors.Wrap(err, "oauth.NewApplicationDefault()")
	}

	return creds, nil
}

// sampler is the SDK sampler for a Sampling: every span for SamplingAll; for
// SamplingEdge the caller's decision, and nothing without a caller.
func sampler(s Sampling) sdktrace.Sampler {
	if s == SamplingAll {
		return sdktrace.AlwaysSample()
	}

	return sdktrace.ParentBased(sdktrace.NeverSample())
}

// newResource describes the process: the SDK's defaults, what the Google Cloud detector
// finds about where it runs (on Cloud Run, the service, revision and region), the service
// name, and the project the spans belong to. A detector that could not read everything
// leaves a partial resource, and attributes whose schema versions differ leave the
// resource without a schema URL; neither stops the provider, since Cloud Trace reads the
// attributes and not the schema.
func newResource(ctx context.Context, projectID, serviceName string) (*resource.Resource, error) {
	detected, err := resource.New(ctx,
		resource.WithDetectors(gcp.NewDetector()),
		resource.WithAttributes(semconv.ServiceName(serviceName), attribute.String(projectIDAttribute, projectID)),
	)
	if err != nil && !errors.Is(err, resource.ErrPartialResource) && !errors.Is(err, resource.ErrSchemaURLConflict) {
		return nil, errors.Wrap(err, "resource.New()")
	}
	res, err := resource.Merge(resource.Default(), detected)
	if err != nil && !errors.Is(err, resource.ErrSchemaURLConflict) {
		return nil, errors.Wrap(err, "resource.Merge()")
	}

	return res, nil
}

// checkPackageMissmatch is used in a test to ensure we keep
// otel/sdk/resource package and the otel/semconv package in sync
func checkPackageMissmatch() error {
	if resource.Default().SchemaURL() != semconv.SchemaURL {
		return errors.Newf("conflicting package versions installed: upgrade semconv package to go.opentelemetry.io/otel/semconv/v%s", path.Base(resource.Default().SchemaURL()))
	}

	return nil
}
