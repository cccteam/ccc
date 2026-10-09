package tracer

import (
	"context"
	"encoding/binary"
	"strconv"
	"strings"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// cloudTraceHeader is the legacy header Google's load balancers and Cloud Run set on every
// request they forward: TRACE_ID/SPAN_ID;o=TRACE_TRUE, a 32-digit hexadecimal trace id, a
// decimal span id, and o=1 when the request is sampled.
const cloudTraceHeader = "X-Cloud-Trace-Context"

// Propagator is the context propagator the handler uses and the provider sets as the
// global one: W3C Trace Context (traceparent, tracestate) and Baggage, with the legacy
// X-Cloud-Trace-Context header read as well, so a caller that sends only it continues its
// trace; when both headers arrive, traceparent wins. Outgoing requests carry the W3C
// headers alone.
func Propagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(cloudTraceReader{}, propagation.TraceContext{}, propagation.Baggage{})
}

// cloudTraceReader extracts the legacy header and injects nothing.
type cloudTraceReader struct{}

// Inject writes nothing: the W3C headers carry the context out.
func (cloudTraceReader) Inject(context.Context, propagation.TextMapCarrier) {}

// Fields lists the keys Inject writes: none.
func (cloudTraceReader) Fields() []string { return nil }

// Extract reads the legacy header into a remote span context; a header that is missing or
// malformed leaves the context as it was.
func (cloudTraceReader) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	sc, ok := parseCloudTraceHeader(carrier.Get(cloudTraceHeader))
	if !ok {
		return ctx
	}

	return trace.ContextWithRemoteSpanContext(ctx, sc)
}

// parseCloudTraceHeader reads TRACE_ID/SPAN_ID;o=TRACE_TRUE. A span context without a span
// id is not a parent, so a header that lacks the span id, or carries 0, yields nothing and
// the request starts a trace of its own.
func parseCloudTraceHeader(value string) (trace.SpanContext, bool) {
	traceHex, rest, _ := strings.Cut(value, "/")
	traceID, err := trace.TraceIDFromHex(traceHex)
	if err != nil {
		return trace.SpanContext{}, false
	}
	spanDecimal, options, _ := strings.Cut(rest, ";")
	spanNumber, err := strconv.ParseUint(spanDecimal, 10, 64)
	if err != nil || spanNumber == 0 {
		return trace.SpanContext{}, false
	}
	var spanID trace.SpanID
	binary.BigEndian.PutUint64(spanID[:], spanNumber)
	var flags trace.TraceFlags
	if options == "o=1" {
		flags = trace.FlagsSampled
	}

	return trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: flags, Remote: true}), true
}
