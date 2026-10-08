//go:build !dev

package tracer

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/go-playground/errors/v5"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func Test_testPackageMissmatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		wantErr bool
	}{
		{
			name: "Do not error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkPackageMissmatch()
			if (err != nil) != tt.wantErr {
				t.Errorf("traceResource() error = %v, wantErr %v", errors.Cause(err), tt.wantErr)
				return
			}
		})
	}
}

// collector is an OTLP trace collector that keeps what it receives.
type collector struct {
	coltracepb.UnimplementedTraceServiceServer

	mu    sync.Mutex
	spans []*tracepb.ResourceSpans
}

func (c *collector) Export(_ context.Context, req *coltracepb.ExportTraceServiceRequest) (*coltracepb.ExportTraceServiceResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.spans = append(c.spans, req.GetResourceSpans()...)

	return &coltracepb.ExportTraceServiceResponse{}, nil
}

// TestExportOverOTLP sends a span to an OTLP collector of the test's own and reads it
// back with the service name and the project the resource carries.
func TestExportOverOTLP(t *testing.T) {
	t.Parallel()

	lis, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// A loopback server for the test alone, so its transport is insecure on purpose.
	srv := grpc.NewServer(grpc.Creds(insecure.NewCredentials()))
	col := &collector{}
	coltracepb.RegisterTraceServiceServer(srv, col)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	p, err := NewGoogleCloudTracerProviderWithOptions("proof-project", "harbor",
		WithEndpoint(lis.Addr().String()), WithInsecure(),
		WithTracerProviderOptions(sdktrace.WithSampler(sdktrace.AlwaysSample())))
	if err != nil {
		t.Fatalf("NewGoogleCloudTracerProviderWithOptions() error = %v", err)
	}
	ctx, span := Start(context.Background())
	span.End()
	if err := p.ForceFlush(ctx); err != nil {
		t.Fatalf("ForceFlush() error = %v", err)
	}
	if err := p.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	col.mu.Lock()
	defer col.mu.Unlock()
	if len(col.spans) == 0 {
		t.Fatal("the collector received no spans")
	}
	attrs := map[string]string{}
	for _, kv := range col.spans[0].GetResource().GetAttributes() {
		attrs[kv.GetKey()] = kv.GetValue().GetStringValue()
	}
	for key, want := range map[string]string{"service.name": "harbor", projectIDAttribute: "proof-project", "telemetry.sdk.language": "go"} {
		if attrs[key] != want {
			t.Errorf("resource attribute %s = %q, want %q (attributes: %v)", key, attrs[key], want, attrs)
		}
	}
	names := 0
	for _, rs := range col.spans {
		for _, ss := range rs.GetScopeSpans() {
			for _, s := range ss.GetSpans() {
				if s.GetName() == "TestExportOverOTLP()" {
					names++
				}
			}
		}
	}
	if names != 1 {
		t.Errorf("spans named TestExportOverOTLP() = %d, want 1", names)
	}
}
