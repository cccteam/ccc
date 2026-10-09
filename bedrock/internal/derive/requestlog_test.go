package derive

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// lodestarSurfaces are the surfaces Lodestar declares: its beacons logged on event with
// traces off, and the droids' ingest route logged on event.
var lodestarSurfaces = []Surface{
	{Prefix: "/beacons/", Log: RequestLogOnEvent, Traces: TracesOff},
	{Prefix: "/droids/sectors/{sectorID}/ingest-droid-reports", Log: RequestLogOnEvent},
}

// lodestarClause is the surfaces' clause the exclusion carries for Lodestar: each
// surface's quiet requests dropped, failures kept, the two joined with OR.
const lodestarClause = `((httpRequest.requestUrl =~ "^https://[^/]+/beacons/" AND httpRequest.status < 400) OR (httpRequest.requestUrl =~ "^https://[^/]+/droids/sectors/[^/]+/ingest-droid-reports" AND httpRequest.status < 400))`

// TestNewRequestLogExclusion derives the exclusion from the surfaces: none when no
// surface's word excludes an entry, else the excluding surfaces in their order with the
// clauses joined by OR.
func TestNewRequestLogExclusion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		surfaces []Surface
		want     *RequestLogExclusion
	}{
		{name: "no surface"},
		{name: "every surface logged always", surfaces: []Surface{{Prefix: "/", Log: RequestLogAlways}, {Prefix: "/api/", Log: RequestLogAlways}}},
		{name: "a trace setting alone", surfaces: []Surface{{Prefix: "/beacons/", Traces: TracesOff}, {Prefix: "/api/", Traces: TracesCapped, Rate: 0.5}}},
		{
			name:     "Lodestar: the beacons and the droids' ingest route, on event",
			surfaces: lodestarSurfaces,
			want:     &RequestLogExclusion{Surfaces: lodestarSurfaces, Clause: lodestarClause},
		},
		{
			name:     "a sampled surface",
			surfaces: []Surface{{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.1}},
			want: &RequestLogExclusion{
				Surfaces: []Surface{{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.1}},
				Clause:   `((httpRequest.requestUrl =~ "^https://[^/]+/api/" AND httpRequest.status < 400 AND NOT sample(insertId, 0.1)))`,
			},
		},
		{
			name:     "a never surface",
			surfaces: []Surface{{Prefix: "/healthz", Log: RequestLogNever}},
			want: &RequestLogExclusion{
				Surfaces: []Surface{{Prefix: "/healthz", Log: RequestLogNever}},
				Clause:   `((httpRequest.requestUrl =~ "^https://[^/]+/healthz"))`,
			},
		},
		{
			name: "always and a trace setting alone among the others have no clause",
			surfaces: []Surface{
				{Prefix: "/", Log: RequestLogAlways},
				{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.25, Traces: TracesCapped, Rate: 0.5},
				{Prefix: "/beacons/", Log: RequestLogOnEvent, Traces: TracesOff},
				{Prefix: "/healthz", Log: RequestLogNever},
				{Prefix: "/portal/api/", Traces: TracesFollowFrontEnd},
			},
			want: &RequestLogExclusion{
				Surfaces: []Surface{
					{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.25, Traces: TracesCapped, Rate: 0.5},
					{Prefix: "/beacons/", Log: RequestLogOnEvent, Traces: TracesOff},
					{Prefix: "/healthz", Log: RequestLogNever},
				},
				Clause: `((httpRequest.requestUrl =~ "^https://[^/]+/api/" AND httpRequest.status < 400 AND NOT sample(insertId, 0.25)) OR (httpRequest.requestUrl =~ "^https://[^/]+/beacons/" AND httpRequest.status < 400) OR (httpRequest.requestUrl =~ "^https://[^/]+/healthz"))`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := NewRequestLogExclusion(tt.surfaces)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("NewRequestLogExclusion() mismatch (-want +got):\n%s", diff)
			}
			if got != nil && strings.Contains(got.Clause, `\`) {
				t.Errorf("NewRequestLogExclusion().Clause = %q carries a backslash", got.Clause)
			}
		})
	}
}

// TestSurfaceClause writes one surface's clause by its word: the URL matched after any
// host, the status for on event and sampled, the fraction as declared for sampled, and
// the URL alone for never; a parameter matches one segment and a dot itself.
func TestSurfaceClause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		surface Surface
		want    string
	}{
		{name: "on event", surface: Surface{Prefix: "/beacons/", Log: RequestLogOnEvent}, want: `(httpRequest.requestUrl =~ "^https://[^/]+/beacons/" AND httpRequest.status < 400)`},
		{name: "sampled", surface: Surface{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.05}, want: `(httpRequest.requestUrl =~ "^https://[^/]+/api/" AND httpRequest.status < 400 AND NOT sample(insertId, 0.05))`},
		{name: "sampled at the whole", surface: Surface{Prefix: "/api/", Log: RequestLogSampled, Fraction: 1}, want: `(httpRequest.requestUrl =~ "^https://[^/]+/api/" AND httpRequest.status < 400 AND NOT sample(insertId, 1))`},
		{name: "never", surface: Surface{Prefix: "/healthz", Log: RequestLogNever}, want: `(httpRequest.requestUrl =~ "^https://[^/]+/healthz")`},
		{name: "the application default", surface: Surface{Prefix: "/", Log: RequestLogOnEvent}, want: `(httpRequest.requestUrl =~ "^https://[^/]+/" AND httpRequest.status < 400)`},
		{name: "a route with a parameter", surface: Surface{Prefix: "/droids/sectors/{sectorID}/ingest-droid-reports", Log: RequestLogNever}, want: `(httpRequest.requestUrl =~ "^https://[^/]+/droids/sectors/[^/]+/ingest-droid-reports")`},
		{name: "a dot in the prefix", surface: Surface{Prefix: "/hooks/v1.2/", Log: RequestLogNever}, want: `(httpRequest.requestUrl =~ "^https://[^/]+/hooks/v1[.]2/")`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.surface.Clause(); got != tt.want {
				t.Errorf("Clause() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSurfacePolicy says a surface's word in prose.
func TestSurfacePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		surface Surface
		want    string
	}{
		{name: "always", surface: Surface{Prefix: "/", Log: RequestLogAlways}, want: "logged always"},
		{name: "on event", surface: Surface{Prefix: "/beacons/", Log: RequestLogOnEvent, Traces: TracesOff}, want: "logged on event"},
		{name: "sampled", surface: Surface{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.1}, want: "sampled at 0.1"},
		{name: "never", surface: Surface{Prefix: "/healthz", Log: RequestLogNever}, want: "never logged"},
		{name: "a trace setting alone", surface: Surface{Prefix: "/portal/api/", Traces: TracesCapped, Rate: 0.5}, want: "no request log word, its trace setting alone"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.surface.Policy(); got != tt.want {
				t.Errorf("Policy() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPathRegex turns a route into the expression Cloud Armor matches the path against
// and Cloud Logging the request's URL: a parameter matches one segment, a last star the
// subtree, a dot itself through a class, and nothing carries a backslash.
func TestPathRegex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		route string
		want  string
	}{
		{name: "a plain route", route: "/api/attach-manifest", want: "/api/attach-manifest"},
		{name: "a parameter", route: "/api/manifests/{id}/file", want: "/api/manifests/[^/]+/file"},
		{name: "two parameters", route: "/api/domains/{domainId}/photos/{id}/file", want: "/api/domains/[^/]+/photos/[^/]+/file"},
		{name: "a subtree", route: "/streams/*", want: "/streams/.*"},
		{name: "a dot", route: "/hooks/v1.2/registry", want: "/hooks/v1[.]2/registry"},
		{name: "a prefix", route: "portal/api", want: "portal/api"},
		{name: "a prefix with its trailing slash", route: "/beacons/", want: "/beacons/"},
		{name: "the root", route: "/", want: "/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := PathRegex(tt.route)
			if got != tt.want {
				t.Errorf("PathRegex(%q) = %q, want %q", tt.route, got, tt.want)
			}
			if strings.Contains(got, `\`) {
				t.Errorf("PathRegex(%q) = %q carries a backslash", tt.route, got)
			}
		})
	}
}
