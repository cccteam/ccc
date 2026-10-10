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
// clauses joined by OR, each excepting the surfaces declared beneath its surface.
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
		{
			name:     "a looser child under its parent: the root sampled, the outlet always",
			surfaces: []Surface{{Prefix: "/", Log: RequestLogSampled, Fraction: 0.1}, {Prefix: "/api/", Log: RequestLogAlways}},
			want: &RequestLogExclusion{
				Surfaces: []Surface{{Prefix: "/", Log: RequestLogSampled, Fraction: 0.1}},
				Clause:   `((httpRequest.requestUrl =~ "^https://[^/]+/" AND NOT httpRequest.requestUrl =~ "^https://[^/]+/api/" AND httpRequest.status < 400 AND NOT sample(insertId, 0.1)))`,
			},
		},
		{
			name:     "a stricter child under its parent: the outlet sampled, a stored file never",
			surfaces: []Surface{{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.1}, {Prefix: "/api/manifests/{id}/file", Log: RequestLogNever}},
			want: &RequestLogExclusion{
				Surfaces: []Surface{{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.1}, {Prefix: "/api/manifests/{id}/file", Log: RequestLogNever}},
				Clause:   `((httpRequest.requestUrl =~ "^https://[^/]+/api/" AND NOT httpRequest.requestUrl =~ "^https://[^/]+/api/manifests/[^/]+/file" AND httpRequest.status < 400 AND NOT sample(insertId, 0.1)) OR (httpRequest.requestUrl =~ "^https://[^/]+/api/manifests/[^/]+/file"))`,
			},
		},
		{
			name: "a grandchild is excepted from every surface above it",
			surfaces: []Surface{
				{Prefix: "/", Log: RequestLogOnEvent},
				{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.5},
				{Prefix: "/api/manifests/{id}/file", Log: RequestLogNever},
			},
			want: &RequestLogExclusion{
				Surfaces: []Surface{
					{Prefix: "/", Log: RequestLogOnEvent},
					{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.5},
					{Prefix: "/api/manifests/{id}/file", Log: RequestLogNever},
				},
				Clause: `((httpRequest.requestUrl =~ "^https://[^/]+/" AND NOT httpRequest.requestUrl =~ "^https://[^/]+/api/" AND NOT httpRequest.requestUrl =~ "^https://[^/]+/api/manifests/[^/]+/file" AND httpRequest.status < 400) OR (httpRequest.requestUrl =~ "^https://[^/]+/api/" AND NOT httpRequest.requestUrl =~ "^https://[^/]+/api/manifests/[^/]+/file" AND httpRequest.status < 400 AND NOT sample(insertId, 0.5)) OR (httpRequest.requestUrl =~ "^https://[^/]+/api/manifests/[^/]+/file"))`,
			},
		},
		{
			name:     "a trace setting alone beneath a surface keeps its word and is not excepted",
			surfaces: []Surface{{Prefix: "/", Log: RequestLogOnEvent}, {Prefix: "/portal/api/", Traces: TracesCapped, Rate: 0.5}},
			want: &RequestLogExclusion{
				Surfaces: []Surface{{Prefix: "/", Log: RequestLogOnEvent}},
				Clause:   `((httpRequest.requestUrl =~ "^https://[^/]+/" AND httpRequest.status < 400))`,
			},
		},
		{
			name:     "a surface sharing the letters of another is not beneath it",
			surfaces: []Surface{{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.1}, {Prefix: "/apix/", Log: RequestLogAlways}},
			want: &RequestLogExclusion{
				Surfaces: []Surface{{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.1}},
				Clause:   `((httpRequest.requestUrl =~ "^https://[^/]+/api/" AND httpRequest.status < 400 AND NOT sample(insertId, 0.1)))`,
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
// the URL alone for never; a parameter matches one segment and a dot itself; each
// excepted surface's match is negated after the surface's own, in the order given.
func TestSurfaceClause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		surface  Surface
		excepted []Surface
		want     string
	}{
		{name: "on event", surface: Surface{Prefix: "/beacons/", Log: RequestLogOnEvent}, want: `(httpRequest.requestUrl =~ "^https://[^/]+/beacons/" AND httpRequest.status < 400)`},
		{name: "sampled", surface: Surface{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.05}, want: `(httpRequest.requestUrl =~ "^https://[^/]+/api/" AND httpRequest.status < 400 AND NOT sample(insertId, 0.05))`},
		{name: "sampled at the whole", surface: Surface{Prefix: "/api/", Log: RequestLogSampled, Fraction: 1}, want: `(httpRequest.requestUrl =~ "^https://[^/]+/api/" AND httpRequest.status < 400 AND NOT sample(insertId, 1))`},
		{name: "never", surface: Surface{Prefix: "/healthz", Log: RequestLogNever}, want: `(httpRequest.requestUrl =~ "^https://[^/]+/healthz")`},
		{name: "the application default", surface: Surface{Prefix: "/", Log: RequestLogOnEvent}, want: `(httpRequest.requestUrl =~ "^https://[^/]+/" AND httpRequest.status < 400)`},
		{name: "a route with a parameter", surface: Surface{Prefix: "/droids/sectors/{sectorID}/ingest-droid-reports", Log: RequestLogNever}, want: `(httpRequest.requestUrl =~ "^https://[^/]+/droids/sectors/[^/]+/ingest-droid-reports")`},
		{name: "a dot in the prefix", surface: Surface{Prefix: "/hooks/v1.2/", Log: RequestLogNever}, want: `(httpRequest.requestUrl =~ "^https://[^/]+/hooks/v1[.]2/")`},
		{
			name:     "sampled, excepting a surface beneath it",
			surface:  Surface{Prefix: "/", Log: RequestLogSampled, Fraction: 0.1},
			excepted: []Surface{{Prefix: "/api/", Log: RequestLogAlways}},
			want:     `(httpRequest.requestUrl =~ "^https://[^/]+/" AND NOT httpRequest.requestUrl =~ "^https://[^/]+/api/" AND httpRequest.status < 400 AND NOT sample(insertId, 0.1))`,
		},
		{
			name:     "on event, excepting two surfaces beneath it in their order",
			surface:  Surface{Prefix: "/", Log: RequestLogOnEvent},
			excepted: []Surface{{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.5}, {Prefix: "/api/manifests/{id}/file", Log: RequestLogNever}},
			want:     `(httpRequest.requestUrl =~ "^https://[^/]+/" AND NOT httpRequest.requestUrl =~ "^https://[^/]+/api/" AND NOT httpRequest.requestUrl =~ "^https://[^/]+/api/manifests/[^/]+/file" AND httpRequest.status < 400)`,
		},
		{
			name:     "never, excepting a surface beneath it",
			surface:  Surface{Prefix: "/api/", Log: RequestLogNever},
			excepted: []Surface{{Prefix: "/api/manifests/{id}/file", Log: RequestLogAlways}},
			want:     `(httpRequest.requestUrl =~ "^https://[^/]+/api/" AND NOT httpRequest.requestUrl =~ "^https://[^/]+/api/manifests/[^/]+/file")`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.surface.Clause(tt.excepted); got != tt.want {
				t.Errorf("Clause() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestSurfaceBeneath holds the rule a surface sits beneath another by: its prefix starts
// with the other's and is longer, the plain prefix match the router applies to the
// request's path, on the prefixes as the release file spells them.
func TestSurfaceBeneath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		surface Surface
		other   Surface
		want    bool
	}{
		{name: "an outlet beneath the application default", surface: Surface{Prefix: "/api/"}, other: Surface{Prefix: "/"}, want: true},
		{name: "the application default is beneath nothing", surface: Surface{Prefix: "/"}, other: Surface{Prefix: "/api/"}},
		{name: "a route beneath its outlet", surface: Surface{Prefix: "/api/manifests/{id}/file"}, other: Surface{Prefix: "/api/"}, want: true},
		{name: "a route beneath the application default", surface: Surface{Prefix: "/api/manifests/{id}/file"}, other: Surface{Prefix: "/"}, want: true},
		{name: "a prefix sharing the letters of another, which ends in its slash", surface: Surface{Prefix: "/apix/"}, other: Surface{Prefix: "/api/"}},
		{name: "a prefix holding another's as a later segment", surface: Surface{Prefix: "/portal/api/"}, other: Surface{Prefix: "/api/"}},
		{name: "a sibling", surface: Surface{Prefix: "/beacons/"}, other: Surface{Prefix: "/api/"}},
		{name: "the same prefix", surface: Surface{Prefix: "/api/"}, other: Surface{Prefix: "/api/"}},
		{name: "a hand-mounted prefix without its slash, as the router reads it", surface: Surface{Prefix: "/beaconsx/"}, other: Surface{Prefix: "/beacons"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.surface.Beneath(&tt.other); got != tt.want {
				t.Errorf("Surface{%q}.Beneath(%q) = %v, want %v", tt.surface.Prefix, tt.other.Prefix, got, tt.want)
			}
		})
	}
}

// TestSurfaceExcepted lists the surfaces a surface's clause excepts: those beneath it
// that declare a word, in the surfaces' order, a grandchild among them; a surface
// declaring its trace setting alone, a sibling and the surface itself are left out.
func TestSurfaceExcepted(t *testing.T) {
	t.Parallel()

	nested := []Surface{
		{Prefix: "/", Log: RequestLogOnEvent},
		{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.5},
		{Prefix: "/api/manifests/{id}/file", Log: RequestLogNever},
		{Prefix: "/beacons/", Log: RequestLogAlways},
		{Prefix: "/portal/api/", Traces: TracesCapped, Rate: 0.5},
	}
	tests := []struct {
		name     string
		surface  Surface
		surfaces []Surface
		want     []Surface
	}{
		{name: "no surface"},
		{name: "the application default excepts every worded surface beneath it", surface: nested[0], surfaces: nested, want: []Surface{nested[1], nested[2], nested[3]}},
		{name: "an outlet excepts the route beneath it", surface: nested[1], surfaces: nested, want: []Surface{nested[2]}},
		{name: "a route excepts nothing", surface: nested[2], surfaces: nested},
		{name: "a surface declaring its trace setting alone is left out", surface: Surface{Prefix: "/", Log: RequestLogOnEvent}, surfaces: []Surface{{Prefix: "/", Log: RequestLogOnEvent}, {Prefix: "/portal/api/", Traces: TracesOff}}},
		{name: "a sibling is left out", surface: Surface{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.1}, surfaces: []Surface{{Prefix: "/api/", Log: RequestLogSampled, Fraction: 0.1}, {Prefix: "/apix/", Log: RequestLogAlways}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.surface.Excepted(tt.surfaces)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Excepted() mismatch (-want +got):\n%s", diff)
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
