package tracer

import (
	"context"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	testTraceHex = "4738909f868f8a6fd977944634bbd613"
	w3cTraceHex  = "a9f67d1884cc74a714ba7ffa449e1ac4"
)

// TestParseCloudTraceHeader reads the legacy header's forms: sampled, unsampled, without
// options, and the ones that yield no parent (no span id, span id 0, a bad trace id).
func TestParseCloudTraceHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		value       string
		wantSpanID  string
		wantSampled bool
		wantNone    bool
	}{
		{name: "sampled", value: testTraceHex + "/1;o=1", wantSpanID: "0000000000000001", wantSampled: true},
		{name: "not sampled", value: testTraceHex + "/1;o=0", wantSpanID: "0000000000000001"},
		{name: "no options", value: testTraceHex + "/12345", wantSpanID: "0000000000003039"},
		{name: "a large span id", value: testTraceHex + "/18446744073709551615;o=1", wantSpanID: "ffffffffffffffff", wantSampled: true},
		{name: "span id 0", value: testTraceHex + "/0;o=1", wantNone: true},
		{name: "no span id", value: testTraceHex + "/", wantNone: true},
		{name: "trace id alone", value: testTraceHex, wantNone: true},
		{name: "a bad trace id", value: "xyz/1;o=1", wantNone: true},
		{name: "a short trace id", value: "4738909f/1;o=1", wantNone: true},
		{name: "a span id that is not a number", value: testTraceHex + "/abc;o=1", wantNone: true},
		{name: "empty", value: "", wantNone: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sc, ok := parseCloudTraceHeader(tt.value)
			if tt.wantNone {
				if ok {
					t.Fatalf("parseCloudTraceHeader(%q) = %v, want none", tt.value, sc)
				}

				return
			}
			if !ok {
				t.Fatalf("parseCloudTraceHeader(%q) yields nothing", tt.value)
			}
			if got := sc.TraceID().String(); got != testTraceHex {
				t.Errorf("trace id = %s, want %s", got, testTraceHex)
			}
			if got := sc.SpanID().String(); got != tt.wantSpanID {
				t.Errorf("span id = %s, want %s", got, tt.wantSpanID)
			}
			if sc.IsSampled() != tt.wantSampled {
				t.Errorf("sampled = %v, want %v", sc.IsSampled(), tt.wantSampled)
			}
			if !sc.IsRemote() {
				t.Error("the span context is not remote")
			}
		})
	}
}

// TestPropagator reads a request's trace context from either header, with traceparent
// winning when both arrive, and writes only the W3C headers.
func TestPropagator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		headers     map[string]string
		wantTraceID string
		wantSampled bool
	}{
		{name: "the legacy header alone", headers: map[string]string{cloudTraceHeader: testTraceHex + "/7;o=1"}, wantTraceID: testTraceHex, wantSampled: true},
		{name: "traceparent alone", headers: map[string]string{"traceparent": "00-" + w3cTraceHex + "-0000000000000007-01"}, wantTraceID: w3cTraceHex, wantSampled: true},
		{name: "both: traceparent wins", headers: map[string]string{cloudTraceHeader: testTraceHex + "/7;o=1", "traceparent": "00-" + w3cTraceHex + "-0000000000000007-00"}, wantTraceID: w3cTraceHex},
		{name: "a bad traceparent beside a good legacy header", headers: map[string]string{cloudTraceHeader: testTraceHex + "/7;o=1", "traceparent": "garbage"}, wantTraceID: testTraceHex, wantSampled: true},
		{name: "neither", headers: map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := http.Header{}
			for k, v := range tt.headers {
				h.Set(k, v)
			}
			ctx := Propagator().Extract(context.Background(), propagation.HeaderCarrier(h))
			sc := trace.SpanContextFromContext(ctx)
			if tt.wantTraceID == "" {
				if sc.IsValid() {
					t.Fatalf("extracted %v, want no span context", sc)
				}

				return
			}
			if got := sc.TraceID().String(); got != tt.wantTraceID {
				t.Errorf("trace id = %s, want %s", got, tt.wantTraceID)
			}
			if sc.IsSampled() != tt.wantSampled {
				t.Errorf("sampled = %v, want %v", sc.IsSampled(), tt.wantSampled)
			}
			out := http.Header{}
			Propagator().Inject(ctx, propagation.HeaderCarrier(out))
			if out.Get("traceparent") == "" {
				t.Error("Inject wrote no traceparent")
			}
			if out.Get(cloudTraceHeader) != "" {
				t.Errorf("Inject wrote %s: %q", cloudTraceHeader, out.Get(cloudTraceHeader))
			}
		})
	}
}
