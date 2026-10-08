//go:build !dev

package tracer

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/go-playground/errors/v5"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"golang.org/x/oauth2"
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

// TestExportOverOTLP sends a root span to an OTLP collector of the test's own under each
// sampling: SamplingAll records it, with the service name and the project the resource
// carries, and SamplingEdge records nothing without a sampled caller.
func TestExportOverOTLP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		sampling  Sampling
		wantSpans int
	}{
		{name: "all records a root span", sampling: SamplingAll, wantSpans: 1},
		{name: "edge records nothing without a caller", sampling: SamplingEdge, wantSpans: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
				WithEndpoint(lis.Addr().String()), WithInsecure(), WithSampling(tt.sampling))
			if err != nil {
				t.Fatalf("NewGoogleCloudTracerProviderWithOptions() error = %v", err)
			}
			ctx, span := p.Tracer("test").Start(context.Background(), "TestExportOverOTLP()")
			span.End()
			if err := p.ForceFlush(ctx); err != nil {
				t.Fatalf("ForceFlush() error = %v", err)
			}
			if err := p.Shutdown(ctx); err != nil {
				t.Fatalf("Shutdown() error = %v", err)
			}

			col.mu.Lock()
			defer col.mu.Unlock()
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
			if names != tt.wantSpans {
				t.Fatalf("spans received = %d, want %d", names, tt.wantSpans)
			}
			if tt.wantSpans == 0 {
				return
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
		})
	}
}

// TestPerRPCCredentials signs the calls with the caller's token source when one is given,
// and requires a secure transport for it.
func TestPerRPCCredentials(t *testing.T) {
	t.Parallel()

	creds, err := perRPCCredentials(t.Context(), &providerConfig{tokenSource: oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "device-token", TokenType: "Bearer"})})
	if err != nil {
		t.Fatalf("perRPCCredentials() error = %v", err)
	}
	if !creds.RequireTransportSecurity() {
		t.Error("the token source credentials do not require transport security")
	}
	ts, ok := creds.(oauth.TokenSource)
	if !ok {
		t.Fatalf("perRPCCredentials() = %T, want oauth.TokenSource", creds)
	}
	token, err := ts.Token()
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if token.AccessToken != "device-token" {
		t.Errorf("access token = %q, want the device token", token.AccessToken)
	}
}

// TestParseSampling reads the settings and refuses the rest.
func TestParseSampling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    Sampling
		wantErr string
	}{
		{name: "empty is the edge's", value: "", want: SamplingEdge},
		{name: "edge", value: "edge", want: SamplingEdge},
		{name: "all", value: "all", want: SamplingAll},
		{name: "case and space do not matter", value: " All ", want: SamplingAll},
		{name: "a ratio is not a setting", value: "0.5", wantErr: `"0.5" is not a sampling setting (edge, all)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseSampling(tt.value)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ParseSampling(%q) error = %v, want %q", tt.value, err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("ParseSampling(%q) error = %v", tt.value, err)
			}
			if got != tt.want {
				t.Errorf("ParseSampling(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
