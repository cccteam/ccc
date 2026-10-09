package tracer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// Test_surfaceTable_match pins the matching: a prefix matches what sits under it, a
// braced segment matches any one segment, the longest declared surface wins, and a path
// under none matches nothing.
func Test_surfaceTable_match(t *testing.T) {
	t.Parallel()

	table := newSurfaceTable(map[string]Traces{
		"/droids/":                          TracesCapped(0.5),
		"/droids/ingest-droid-reports":      TracesOff(),
		"/beacons/":                         TracesOff(),
		"/api/widgets/{widgetID}/content":   TracesOff(),
		"/api/sectors/{sectorID}/ships-log": TracesCapped(0.1),
	})
	tests := []struct {
		path string
		want Traces
		ok   bool
	}{
		{path: "/droids/beacons", want: TracesCapped(0.5), ok: true},
		{path: "/droids/ingest-droid-reports", want: TracesOff(), ok: true},
		{path: "/droids/ingest-droid-reports/extra", want: TracesOff(), ok: true},
		{path: "/beacons/anvil", want: TracesOff(), ok: true},
		{path: "/beacons", ok: false},
		{path: "/api/widgets/7/content", want: TracesOff(), ok: true},
		{path: "/api/widgets/7", ok: false},
		{path: "/api/widgets//content", ok: false},
		{path: "/api/sectors/anvil/ships-log", want: TracesCapped(0.1), ok: true},
		{path: "/api/sectors/anvil/ships-log/7", want: TracesCapped(0.1), ok: true},
		{path: "/console/api/missions", ok: false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			got, ok := table.match(tt.path)
			if ok != tt.ok || got != tt.want {
				t.Errorf("match(%q) = %v, %v; want %v, %v", tt.path, got, ok, tt.want, tt.ok)
			}
		})
	}
}

// Test_matchesPattern pins the pattern grammar on its own.
func Test_matchesPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		path    string
		want    bool
	}{
		{pattern: "/a/", path: "/a/b", want: true},
		{pattern: "/a", path: "/ab", want: true},
		{pattern: "/a/", path: "/a", want: false},
		{pattern: "/a/{x}", path: "/a/1", want: true},
		{pattern: "/a/{x}", path: "/a/", want: false},
		{pattern: "/a/{x}/b", path: "/a/1/b", want: true},
		{pattern: "/a/{x}/b", path: "/a/1/c", want: false},
		{pattern: "/a/{x", path: "/a/1", want: false},
		{pattern: "", path: "/anything", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+" "+tt.path, func(t *testing.T) {
			t.Parallel()

			if got := matchesPattern(tt.pattern, tt.path); got != tt.want {
				t.Errorf("matchesPattern(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.want)
			}
		})
	}
}

// Test_surfaceSampler_ShouldSample pins the sampler: the base decision stands for a span
// with no setting and for a span the base dropped; off drops the request's span; capped
// drops it unless the draw falls under the rate; follow the front end leaves it alone;
// and a span with a local parent follows the parent's decision whatever the setting.
func Test_surfaceSampler_ShouldSample(t *testing.T) {
	t.Parallel()

	remote := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{1},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	local := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{2},
		TraceFlags: trace.FlagsSampled,
	})
	dropped := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{1},
		SpanID:  trace.SpanID{3},
	})
	tests := []struct {
		name   string
		base   sdktrace.Sampler
		traces *Traces
		parent trace.SpanContext
		draw   float64
		want   sdktrace.SamplingDecision
	}{
		{name: "no setting keeps the base decision", base: sdktrace.AlwaysSample(), want: sdktrace.RecordAndSample},
		{name: "a dropped span stays dropped under follow", base: sdktrace.NeverSample(), traces: tracesOf(TracesFollowFrontEnd()), want: sdktrace.Drop},
		{name: "a dropped span stays dropped under capped", base: sdktrace.NeverSample(), traces: tracesOf(TracesCapped(1)), want: sdktrace.Drop},
		{name: "follow leaves a sampled span alone", base: sdktrace.AlwaysSample(), traces: tracesOf(TracesFollowFrontEnd()), want: sdktrace.RecordAndSample},
		{name: "off drops the request's span", base: sdktrace.AlwaysSample(), traces: tracesOf(TracesOff()), want: sdktrace.Drop},
		{name: "off drops a span the front end sampled", base: sdktrace.ParentBased(sdktrace.NeverSample()), traces: tracesOf(TracesOff()), parent: remote, want: sdktrace.Drop},
		{name: "capped keeps the span when the draw falls under the rate", base: sdktrace.AlwaysSample(), traces: tracesOf(TracesCapped(0.25)), draw: 0.2, want: sdktrace.RecordAndSample},
		{name: "capped drops the span when the draw reaches the rate", base: sdktrace.AlwaysSample(), traces: tracesOf(TracesCapped(0.25)), draw: 0.25, want: sdktrace.Drop},
		{name: "a child span follows its sampled local parent under off", base: sdktrace.ParentBased(sdktrace.NeverSample()), traces: tracesOf(TracesOff()), parent: local, want: sdktrace.RecordAndSample},
		{name: "a child span follows its sampled local parent under capped", base: sdktrace.ParentBased(sdktrace.NeverSample()), traces: tracesOf(TracesCapped(0.01)), parent: local, draw: 0.9, want: sdktrace.RecordAndSample},
		{name: "a child span follows its dropped local parent even when every span is recorded", base: sdktrace.AlwaysSample(), traces: tracesOf(TracesOff()), parent: dropped, want: sdktrace.Drop},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			if tt.traces != nil {
				ctx = contextWithTraces(ctx, *tt.traces)
			}
			if tt.parent.IsValid() {
				ctx = trace.ContextWithSpanContext(ctx, tt.parent)
			}
			s := surfaceSampler{base: tt.base, draw: func() float64 { return tt.draw }}
			got := s.ShouldSample(sdktrace.SamplingParameters{ParentContext: ctx, Name: "probe", Kind: trace.SpanKindServer})
			if got.Decision != tt.want {
				t.Errorf("ShouldSample() = %v, want %v", got.Decision, tt.want)
			}
			if !strings.HasPrefix(s.Description(), "Surfaces(") {
				t.Errorf("Description() = %q, want it to name the surfaces", s.Description())
			}
		})
	}
}

// tracesOf returns a pointer to the setting, for a case that declares one.
func tracesOf(t Traces) *Traces {
	return &t
}

// TestNewHandlerSurfaces proves the handler hands the declared surface's setting to the
// sampler as the span starts: a request under a surface declared off records no span and
// one under a capped surface records a span only when the draw falls in, while a request
// under no declared surface and one under a surface that follows the front end record
// theirs, and a child span started inside the request rides with the request's span.
func TestNewHandlerSurfaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      string
		draw      float64
		wantSpans int
	}{
		{name: "no declared surface", path: "/console/api/missions", wantSpans: 2},
		{name: "a surface that follows the front end", path: "/portal/api/orders", wantSpans: 2},
		{name: "a surface that is off", path: "/beacons/anvil", wantSpans: 0},
		{name: "capped, the draw falls in", path: "/droids/beacons", draw: 0.05, wantSpans: 2},
		{name: "capped, the draw falls out", path: "/droids/beacons", draw: 0.95, wantSpans: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			exporter := tracetest.NewInMemoryExporter()
			sampler := surfaceSampler{base: sdktrace.AlwaysSample(), draw: func() float64 { return tt.draw }}
			provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter), sdktrace.WithSampler(sampler))
			t.Cleanup(func() {
				if err := provider.Shutdown(context.Background()); err != nil {
					t.Errorf("TracerProvider.Shutdown() error = %v", err)
				}
			})
			handler := NewHandler(
				Surfaces(map[string]Traces{
					"/beacons/":    TracesOff(),
					"/droids/":     TracesCapped(0.1),
					"/portal/api/": TracesFollowFrontEnd(),
				}),
				WithOTelHTTPOptions(otelhttp.WithTracerProvider(provider)),
			)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// A span inside the request, as a handler's tracer.Start opens one.
				_, span := provider.Tracer("probe").Start(r.Context(), "inside")
				span.End()
				w.WriteHeader(http.StatusNoContent)
			}))

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, http.NoBody))

			if rr.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
			}
			if got := len(exporter.GetSpans()); got != tt.wantSpans {
				t.Errorf("recorded %d spans, want %d: %+v", got, tt.wantSpans, exporter.GetSpans())
			}
		})
	}
}

// TestTraces pins the settings' names and the refused rates.
func TestTraces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  Traces
		want string
	}{
		{name: "follow", got: TracesFollowFrontEnd(), want: wordFollowFrontEnd},
		{name: "zero value follows", got: Traces{}, want: wordFollowFrontEnd},
		{name: "capped", got: TracesCapped(0.1), want: "capped at 0.1"},
		{name: "capped at one", got: TracesCapped(1), want: "capped at 1"},
		{name: "off", got: TracesOff(), want: wordOff},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.got.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}

	for _, rate := range []float64{0, -0.1, 1.5} {
		t.Run("a refused rate", func(t *testing.T) {
			t.Parallel()

			defer func() {
				msg, _ := recover().(string)
				if !strings.Contains(msg, "the rate must be above 0 and at most 1") {
					t.Errorf("TracesCapped(%v) panic = %q, want the rate named", rate, msg)
				}
			}()
			TracesCapped(rate)
			t.Errorf("TracesCapped(%v) returned; want a panic", rate)
		})
	}
}
