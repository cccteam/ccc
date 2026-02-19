//go:build !dev

package tracer

import (
	"path"

	texporter "github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/trace"
	"github.com/go-playground/errors/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
)

// NewGoogleCloudTracerProvider creates and configures a new OpenTelemetry TracerProvider
// for use with Google Cloud Trace. It sets up an exporter to send traces to the
// specified Google Cloud project.
//
// The created TracerProvider is also set as the global tracer provider for the application.
func NewGoogleCloudTracerProvider(loggingProjectID, serviceName string, opts ...sdktrace.TracerProviderOption) (*Provider, error) {
	exporter, err := texporter.New(texporter.WithProjectID(loggingProjectID))
	if err != nil {
		return nil, errors.Wrap(err, "texporter.New()")
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			resource.Default().SchemaURL(),
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, errors.Wrap(err, "resource.Merge()")
	}

	options := make([]sdktrace.TracerProviderOption, 0, len(opts)+3)
	options = append(options,
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.NeverSample())),
	)
	options = append(options, opts...)

	tp := sdktrace.NewTracerProvider(options...)
	otel.SetTracerProvider(tp)

	return &Provider{tp}, nil
}

// checkPackageMissmatch is used in a test to ensure we keep
// otel/sdk/resource package and the otel/semconv package in sync
func checkPackageMissmatch() error {
	if resource.Default().SchemaURL() != semconv.SchemaURL {
		return errors.Newf("conflicting package versions installed: upgrade semconv package to go.opentelemetry.io/otel/semconv/v%s", path.Base(resource.Default().SchemaURL()))
	}

	return nil
}
