package tracer

import (
	"context"
	"slices"
	"strings"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// surfaceTable is the declared surfaces (Surfaces), the longest pattern first, so the
// first match is the nearest declaration.
type surfaceTable []surfacePattern

// surfacePattern is one declared surface: the path the router mounts it at and its
// setting.
type surfacePattern struct {
	pattern string
	traces  Traces
}

// newSurfaceTable orders the declared surfaces for matching: the longest pattern first,
// equal lengths in lexical order, so the table reads the same whatever the map's order.
func newSurfaceTable(table map[string]Traces) surfaceTable {
	surfaces := make(surfaceTable, 0, len(table))
	for pattern, traces := range table {
		surfaces = append(surfaces, surfacePattern{pattern: pattern, traces: traces})
	}
	slices.SortFunc(surfaces, func(a, b surfacePattern) int {
		if len(a.pattern) != len(b.pattern) {
			return len(b.pattern) - len(a.pattern)
		}

		return strings.Compare(a.pattern, b.pattern)
	})

	return surfaces
}

// match returns the setting of the longest declared surface the path sits under, and
// whether one does.
func (t surfaceTable) match(path string) (Traces, bool) {
	for _, surface := range t {
		if matchesPattern(surface.pattern, path) {
			return surface.traces, true
		}
	}

	return Traces{}, false
}

// matchesPattern reports whether the path begins with the pattern, as the router mounts
// it: a literal prefix is matched as written, and a segment in braces ({sectorID})
// matches any one non-empty segment of the path, so /api/widgets/{widgetID}/content
// matches /api/widgets/7/content and what sits under it.
func matchesPattern(pattern, path string) bool {
	for pattern != "" {
		open := strings.IndexByte(pattern, '{')
		if open < 0 {
			return strings.HasPrefix(path, pattern)
		}
		if !strings.HasPrefix(path, pattern[:open]) {
			return false
		}
		path = path[open:]
		closing := strings.IndexByte(pattern, '}')
		if closing < open {
			return false
		}
		pattern = pattern[closing+1:]
		end := strings.IndexByte(path, '/')
		if end < 0 {
			end = len(path)
		}
		if end == 0 {
			return false
		}
		path = path[end:]
	}

	return true
}

// surfaceKey is the context key the handler carries a request's setting under, for the
// sampler.
type surfaceKey struct{}

// contextWithTraces returns a context carrying the setting of the surface the request
// sits under.
func contextWithTraces(ctx context.Context, t Traces) context.Context {
	return context.WithValue(ctx, surfaceKey{}, t)
}

// tracesFromContext reads the setting the handler set, and whether it set one.
func tracesFromContext(ctx context.Context) (Traces, bool) {
	t, ok := ctx.Value(surfaceKey{}).(Traces)

	return t, ok
}

// sampler is the sampler the provider installs: every span, or the front end's decision
// with nothing started here, each narrowed by the declared surfaces (Surfaces).
func sampler(s Sampling) sdktrace.Sampler {
	if s == SamplingAll {
		return newSurfaceSampler(sdktrace.AlwaysSample())
	}

	return newSurfaceSampler(sdktrace.ParentBased(sdktrace.NeverSample()))
}

// surfaceSampler is the sampler the provider builds: the base sampler decides as it does
// today, parent-based or every span, and the surface's setting then narrows the decision
// of the request's own span, the one started with no local parent. A span started inside
// the request follows that span, which the setting already decided, so a surface that is
// off or capped out records no child span either, whatever the base sampler would do.
type surfaceSampler struct {
	base sdktrace.Sampler
}

// newSurfaceSampler wraps the base sampler with the surface settings.
func newSurfaceSampler(base sdktrace.Sampler) surfaceSampler {
	return surfaceSampler{base: base}
}

// ShouldSample applies the base sampler, then the surface's setting to a span with no
// local parent: off drops it, capped drops it unless the trace falls under the rate, and
// follow the front end leaves the base decision alone. A capped surface decides by the
// trace ID, through the SDK's ratio sampler, so one trace is kept or dropped the same
// way on every surface capped at the rate and in every service it crosses.
func (s surfaceSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	result := s.base.ShouldSample(p)
	if result.Decision != sdktrace.RecordAndSample {
		return result
	}
	t, ok := tracesFromContext(p.ParentContext)
	if !ok {
		return result
	}
	if parent := trace.SpanContextFromContext(p.ParentContext); parent.IsValid() && !parent.IsRemote() {
		if !parent.IsSampled() {
			result.Decision = sdktrace.Drop
		}

		return result
	}
	switch t.setting {
	case tracesOff:
		result.Decision = sdktrace.Drop
	case tracesCapped:
		if sdktrace.TraceIDRatioBased(t.rate).ShouldSample(p).Decision != sdktrace.RecordAndSample {
			result.Decision = sdktrace.Drop
		}
	case tracesFollowFrontEnd:
	}

	return result
}

// Description names the sampler for the SDK.
func (s surfaceSampler) Description() string {
	return "Surfaces(" + s.base.Description() + ")"
}
