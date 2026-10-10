// releasefile.go reads the generated release file: the oldest release of the browser
// application each router outlet still answers, which decides whether a release needs
// a maintenance window, the scheduled routes, which the stack gives a Cloud Scheduler
// job each, the file routes, which Cloud Armor matches ahead of its rules, and the
// surfaces with their request log words, which the stack renders the request log's
// exclusion from (requestlog.go).

package derive

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/semver"

	"github.com/cccteam/ccc/impulse/app"
)

// ReleaseFileName is the generated release file, written by the resource generator
// beside the generated router (zz_gen_router.go) in the router package's directory. It
// names every outlet the generator knows and, for each session outlet, the oldest
// release of the browser application the outlet still answers, as the generator program
// declares it (OldestAnswered). The deploy reads it from the checkout, never from a
// binary.
const ReleaseFileName = "zz_gen_release.json"

// How the release file spells its values: an outlet whose oldest answered release is
// the server's own, and the kind of an outlet that machines call with an API key.
const (
	ReleaseThis  = "this"
	OutletAPIKey = "api-key"
)

// ReleaseFile is the generated release file: one entry per outlet, keyed by the
// outlet's name (default for the outlet GenerateRoutes declares, the name given to
// WithRouterOutlet otherwise), and the scheduled routes.
type ReleaseFile struct {
	Outlets map[string]ReleaseOutlet `json:"outlets"`
	// Scheduled are the routes the code declares with @schedule, in path order; none
	// when it declares none.
	Scheduled []ScheduledRoute `json:"scheduled,omitempty"`
	// FileRoutes are the routes that carry a file rather than JSON, in path order: each
	// @upload method's route and each stored file's read route (@file). The stack puts
	// each ahead of Cloud Armor's rules (cloud-armor.tf). None when the code declares
	// neither.
	FileRoutes []FileRoute `json:"fileRoutes,omitempty"`
	// Surfaces are the surfaces the code declares a request log word or a trace setting
	// for, in prefix order: the application default at /, an outlet at its prefix, a
	// prefix the application mounts routes under by hand, or a generated route at the
	// path the router mounts it at, each saying which of the two it is (Kind). The stack
	// renders the request log's exclusion from the words (logging.tf). None when the
	// code declares nothing, and then every request's entry is written.
	Surfaces []Surface `json:"surfaces,omitempty"`
}

// Surface is one declared surface of the release file: a path prefix or a route
// pattern as the router mounts it, with its request log word, its trace setting, or
// both. The stack reads the word alone: the trace setting is the tracer's.
type Surface struct {
	// Prefix is the path as mounted: a prefix such as /beacons/ or /portal/api/, a
	// route pattern such as /api/widgets/{id}/content, where a segment in braces stands
	// for any one segment, or / for the application default.
	Prefix string `json:"prefix"`
	// Kind says how the router matches the prefix against a request's path, and so how
	// the exclusion's clause matches the request's URL: SurfacePrefix by prefix, so
	// everything under /beacons/ is the surface's; SurfaceRoute to the end of the path,
	// so /api/widgets/{id}/content is the surface's and /api/widgets/{id}/content-extra
	// is not. The application default, an outlet and a hand-mounted prefix are prefixes;
	// an annotated method's route is a route. A file written before the surfaces carried
	// their kind reads as prefixes throughout, which is how every surface was matched
	// then.
	Kind string `json:"kind,omitempty"`
	// Log is the request log word: RequestLogAlways, RequestLogOnEvent,
	// RequestLogSampled or RequestLogNever; empty when the surface declares its trace
	// setting alone.
	Log string `json:"log,omitempty"`
	// Fraction is the share of the quiet requests a sampled surface writes the entry
	// for, above 0 and at most 1; set with RequestLogSampled alone.
	Fraction float64 `json:"fraction,omitempty"`
	// Traces is the trace setting: TracesFollowFrontEnd, TracesCapped or TracesOff;
	// empty when the surface declares its request log word alone.
	Traces string `json:"traces,omitempty"`
	// Rate is the share of the front end's traces a capped surface keeps, above 0 and
	// at most 1; set with TracesCapped alone.
	Rate float64 `json:"rate,omitempty"`
}

// The request log words, as the release file spells them: the entry is written for
// every request, when a line attached or the request failed, as on event plus a
// fraction of the quiet requests, or never.
const (
	RequestLogAlways  = "always"
	RequestLogOnEvent = "onEvent"
	RequestLogSampled = "sampled"
	RequestLogNever   = "never"
)

// The trace settings, as the release file spells them: a surface's spans follow the
// front end, are capped at a rate, or are off.
const (
	TracesFollowFrontEnd = "followFrontEnd"
	TracesCapped         = "capped"
	TracesOff            = "off"
)

// The surface kinds, as the release file spells them: a prefix is matched against the
// start of the path, a route to its end.
const (
	SurfacePrefix = "prefix"
	SurfaceRoute  = "route"
)

// FileRoute is one route of the release file that carries a file rather than JSON: an
// @upload method's route, whose body is multipart, or a stored file's read route, whose
// answer is the object.
type FileRoute struct {
	// Kind is FileRouteUpload or FileRouteStored.
	Kind string `json:"kind"`
	// Method is the HTTP method the route answers: POST for an upload, GET for a stored
	// file.
	Method string `json:"method"`
	// Path is the route as the router mounts it, under its outlet's prefix, with its
	// parameters in braces (/api/photos/{id}/file).
	Path string `json:"path"`
	// Source names the declaration the route comes from: the method's struct
	// (AttachPhoto) or the resource and its key column (Photo.Key).
	Source string `json:"source"`
}

// The kinds of file route, as the file spells them.
const (
	FileRouteUpload = "upload"
	FileRouteStored = "file"
)

// Declaration says what the route comes from, for a rule's description: the @upload
// method, the @file column (resource and column, Manifest.Key), or the computed resource
// that renders its file (Statement).
func (r *FileRoute) Declaration() string {
	switch {
	case r.Kind == FileRouteUpload:
		return "the @upload method " + r.Source
	case strings.Contains(r.Source, "."):
		return "the @file column " + r.Source
	}

	return "the @file of the computed resource " + r.Source
}

// ScheduledPrefix is the path the generated router serves the scheduled routes under,
// each at ScheduledPrefix/<name>, behind the framework's check of Cloud Scheduler's token.
const ScheduledPrefix = "/_scheduled"

// SchedulerInvokerVariable is the variable that names the invoker identity to the
// service: the email a scheduled call's token must carry. The framework reads it itself,
// so no configuration level declares it; with it empty every scheduled call is refused.
const SchedulerInvokerVariable = "APP_SCHEDULER_INVOKER"

// ScheduledRoute is a method the code declares with @schedule: Cloud Scheduler calls it
// with POST on its schedule, a cron expression read in the time zone.
type ScheduledRoute struct {
	// Path is the route, ScheduledPrefix/<name>.
	Path string `json:"path"`
	// Schedule is the cron expression, five fields (minute, hour, day of the month,
	// month, day of the week), as the code declares it.
	Schedule string `json:"schedule"`
	// TimeZone is the IANA time zone the schedule is read in, UTC when the code names
	// none.
	TimeZone string `json:"timeZone"`
}

// Name is the route's name, the path's segment after ScheduledPrefix: the method's name
// in kebab case, which names its Cloud Scheduler job.
func (r *ScheduledRoute) Name() string {
	return strings.TrimPrefix(r.Path, ScheduledPrefix+"/")
}

// The shapes a scheduled route's values take in the file: the name is the method's name
// in kebab case, the schedule a cron expression of digits, names, *, /, , and -, and the
// zone an IANA name. The stack writes each into a quoted string, so nothing else passes.
var (
	scheduledNameRE     = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	scheduledScheduleRE = regexp.MustCompile(`^[0-9A-Za-z*/,-]+( [0-9A-Za-z*/,-]+){4}$`)
	scheduledZoneRE     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+)*$`)
)

// ReleaseOutlet is one outlet's entry. A session outlet carries its oldest answered
// release: a release such as 1.5.0, "this" for the server's own, or "" when every
// release that sends its version is answered. An API-key outlet carries its kind and is
// never checked against a release.
type ReleaseOutlet struct {
	Kind           string `json:"kind,omitempty"`
	OldestAnswered string `json:"oldestAnswered,omitempty"`
}

// OldestAnswered is the newest value over the session outlets of what each outlet
// answers at the oldest: the one the maintenance window is decided by, with the outlet
// that declares it.
type OldestAnswered struct {
	// Release is the release, as the file writes it (1.5.0); empty when This is set.
	Release string
	// This says the outlet answers the server's own release alone.
	This bool
	// Outlet is the outlet that declares it.
	Outlet string
}

// String says what the outlet answers, for a log line.
func (o OldestAnswered) String() string {
	if o.This {
		return "the " + o.Outlet + " outlet answers this release alone (oldest answered: this)"
	}

	return "the " + o.Outlet + " outlet answers " + o.Release + " at the oldest"
}

// ReadReleaseFile reads the release file from dir, the router package's directory of a
// checkout. A missing file is an error wrapping os.ErrNotExist: the application predates
// the file, or no router is generated there. A file that is not the shape the generator
// writes is refused naming what is wrong.
func ReadReleaseFile(dir string) (*ReleaseFile, error) {
	name := filepath.Join(dir, ReleaseFileName)
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, errors.Wrapf(err, "os.ReadFile(): %s", name)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var f ReleaseFile
	if err := dec.Decode(&f); err != nil {
		return nil, errors.Wrapf(err, "%s is not the shape the generator writes", name)
	}
	if len(f.Outlets) == 0 {
		return nil, errors.Newf("%s names no outlet; a generated router has at least its default outlet", name)
	}
	for _, outlet := range outletNames(&f) {
		o := f.Outlets[outlet]
		switch {
		case o.Kind != "" && o.Kind != OutletAPIKey:
			return nil, errors.Newf("%s: outlet %s has kind %q, and the one kind an entry declares is %q", name, outlet, o.Kind, OutletAPIKey)
		case o.OldestAnswered == "", o.OldestAnswered == ReleaseThis, IsRelease(o.OldestAnswered):
		default:
			return nil, errors.Newf("%s: outlet %s answers %q at the oldest, which is not a release (1.5.0), %q or \"\"", name, outlet, o.OldestAnswered, ReleaseThis)
		}
	}
	if err := validateScheduled(name, f.Scheduled); err != nil {
		return nil, err
	}
	if err := validateFileRoutes(name, f.FileRoutes); err != nil {
		return nil, err
	}
	if err := validateSurfaces(name, f.Surfaces); err != nil {
		return nil, err
	}

	return &f, nil
}

// surfacePrefixRE is the shape of a surface's prefix: the root alone, or segments of
// letters, digits, dashes, underscores and dots, or a parameter in braces, under the
// root, with or without a trailing slash. The stack writes each into the exclusion's
// filter as a regular expression, so nothing else passes.
var surfacePrefixRE = regexp.MustCompile(`^/$|^(/(\{[A-Za-z0-9_]+\}|[A-Za-z0-9_.-]+))+/?$`)

// validateSurfaces refuses a surface of the file name the generator would not write: a
// prefix not of the shape a router mounts, a prefix listed twice, a kind that is neither
// a prefix nor a route, neither a word nor a setting, a word or a setting outside the
// vocabulary, a fraction or a rate outside (0, 1], a fraction without the sampled word,
// or a rate without the capped setting. A surface without a kind, from a file written
// before the surfaces carried one, is a prefix, which is how every surface was matched
// then.
func validateSurfaces(name string, surfaces []Surface) error {
	seen := map[string]bool{}
	for i := range surfaces {
		s := &surfaces[i]
		if s.Kind == "" {
			s.Kind = SurfacePrefix
		}
		switch {
		case !surfacePrefixRE.MatchString(s.Prefix):
			return errors.Newf("%s: the surface %q is not a path a router mounts (/beacons/, /api/photos/{id}/file)", name, s.Prefix)
		case seen[s.Prefix]:
			return errors.Newf("%s: the surface %s is listed twice", name, s.Prefix)
		case s.Kind != SurfacePrefix && s.Kind != SurfaceRoute:
			return errors.Newf("%s: the surface %s has the kind %q; a surface is a %s or a %s", name, s.Prefix, s.Kind, SurfacePrefix, SurfaceRoute)
		case s.Log == "" && s.Traces == "":
			return errors.Newf("%s: the surface %s declares neither a request log word nor a trace setting", name, s.Prefix)
		}
		seen[s.Prefix] = true
		switch s.Log {
		case "", RequestLogAlways, RequestLogOnEvent, RequestLogNever:
			if s.Fraction != 0 {
				return errors.Newf("%s: the surface %s carries a fraction without the %s word", name, s.Prefix, RequestLogSampled)
			}
		case RequestLogSampled:
			if s.Fraction <= 0 || s.Fraction > 1 {
				return errors.Newf("%s: the surface %s is sampled at %v; the fraction is above 0 and at most 1", name, s.Prefix, s.Fraction)
			}
		default:
			return errors.Newf("%s: the surface %s has the request log word %q; the words are %s, %s, %s and %s", name, s.Prefix, s.Log, RequestLogAlways, RequestLogOnEvent, RequestLogSampled, RequestLogNever)
		}
		switch s.Traces {
		case "", TracesFollowFrontEnd, TracesOff:
			if s.Rate != 0 {
				return errors.Newf("%s: the surface %s carries a rate without the %s setting", name, s.Prefix, TracesCapped)
			}
		case TracesCapped:
			if s.Rate <= 0 || s.Rate > 1 {
				return errors.Newf("%s: the surface %s is capped at %v; the rate is above 0 and at most 1", name, s.Prefix, s.Rate)
			}
		default:
			return errors.Newf("%s: the surface %s has the trace setting %q; the settings are %s, %s and %s", name, s.Prefix, s.Traces, TracesFollowFrontEnd, TracesCapped, TracesOff)
		}
	}

	return nil
}

// fileRoutePathRE is the shape of a file route's path: segments of letters, digits,
// dashes, underscores and dots, or a parameter in braces, under the root. The stack
// writes each into a rule's expression, so nothing else passes.
var fileRoutePathRE = regexp.MustCompile(`^(/(\{[A-Za-z0-9_]+\}|[A-Za-z0-9_.-]+))+$`)

// validateFileRoutes refuses a file route of the file name the generator would not
// write: a kind other than the two, a method other than the kind's, a path not of the
// shape a router mounts, a path listed twice for one method, or no source.
func validateFileRoutes(name string, routes []FileRoute) error {
	seen := map[string]bool{}
	for i := range routes {
		r := &routes[i]
		key := r.Method + " " + r.Path
		switch {
		case r.Kind != FileRouteUpload && r.Kind != FileRouteStored:
			return errors.Newf("%s: the file route %q has the kind %q, and a file route is an %s or a %s", name, r.Path, r.Kind, FileRouteUpload, FileRouteStored)
		case r.Kind == FileRouteUpload && r.Method != http.MethodPost, r.Kind == FileRouteStored && r.Method != http.MethodGet:
			return errors.Newf("%s: the file route %q (%s) answers %s; an %s answers POST and a %s GET", name, r.Path, r.Kind, r.Method, FileRouteUpload, FileRouteStored)
		case !fileRoutePathRE.MatchString(r.Path):
			return errors.Newf("%s: the file route %q is not a path a router mounts (/api/photos/{id}/file)", name, r.Path)
		case seen[key]:
			return errors.Newf("%s: the file route %s is listed twice", name, key)
		case r.Source == "":
			return errors.Newf("%s: the file route %s names no source", name, key)
		}
		seen[key] = true
	}

	return nil
}

// validateScheduled refuses a scheduled route of the file name the generator would not
// write: a path outside ScheduledPrefix or whose name is not in kebab case, a path listed
// twice, a schedule that is not five fields of a cron expression, and a zone that is not
// an IANA name.
func validateScheduled(name string, routes []ScheduledRoute) error {
	seen := map[string]bool{}
	for i := range routes {
		r := &routes[i]
		switch {
		case !strings.HasPrefix(r.Path, ScheduledPrefix+"/") || !scheduledNameRE.MatchString(r.Name()):
			return errors.Newf("%s: the scheduled route %q is not %s/<method in kebab case>", name, r.Path, ScheduledPrefix)
		case seen[r.Path]:
			return errors.Newf("%s: the scheduled route %s is listed twice", name, r.Path)
		case !scheduledScheduleRE.MatchString(r.Schedule):
			return errors.Newf("%s: the scheduled route %s has the schedule %q, which is not five cron fields separated by single spaces", name, r.Path, r.Schedule)
		case !scheduledZoneRE.MatchString(r.TimeZone):
			return errors.Newf("%s: the scheduled route %s has the time zone %q, which is not an IANA name (UTC, America/Denver)", name, r.Path, r.TimeZone)
		}
		seen[r.Path] = true
	}

	return nil
}

// releaseFile reads the scheduled routes, the file routes and the surfaces from the
// release file beside the generated router, and derives the request log's exclusion
// from the surfaces. An application with no router, or whose router has no release file
// yet, has none of them; a release file that does not read is refused, since the stack
// cannot say which jobs the code declares, which routes carry files, or which entries
// the code means to keep.
func (m *Model) releaseFile(a *app.App) error {
	if m.RouterDir == "" {
		return nil
	}
	f, err := ReadReleaseFile(a.Abs(m.RouterDir))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return err
	}
	m.Scheduled = f.Scheduled
	m.FileRoutes = f.FileRoutes
	m.Surfaces = f.Surfaces
	m.RequestLog = NewRequestLogExclusion(f.Surfaces)

	return nil
}

// IsRelease reports whether v is a release as the release file and the generator
// program write one: a semantic version without its v, 1.5.0.
func IsRelease(v string) bool {
	return v != "" && v[0] != 'v' && semver.IsValid("v"+v) && semver.Canonical("v"+v) == "v"+v
}

// OldestAnswered is the newest oldest-answered release over the session outlets, and
// whether any declares one: "this" is newer than any release, and among releases the
// newest wins; an API-key outlet and an outlet that answers every release count for
// nothing. Outlets are read by name, so equal values name the same outlet every time.
func (f *ReleaseFile) OldestAnswered() (OldestAnswered, bool) {
	var newest OldestAnswered
	found := false
	for _, name := range outletNames(f) {
		o := f.Outlets[name]
		if o.Kind == OutletAPIKey || o.OldestAnswered == "" {
			continue
		}
		candidate := OldestAnswered{Release: o.OldestAnswered, Outlet: name}
		if o.OldestAnswered == ReleaseThis {
			candidate = OldestAnswered{This: true, Outlet: name}
		}
		if !found || newer(candidate, newest) {
			newest, found = candidate, true
		}
	}

	return newest, found
}

// newer reports whether a answers a newer release at the oldest than b.
func newer(a, b OldestAnswered) bool {
	switch {
	case a.This:
		return !b.This
	case b.This:
		return false
	default:
		return semver.Compare("v"+a.Release, "v"+b.Release) > 0
	}
}

// outletNames lists the file's outlets in name order.
func outletNames(f *ReleaseFile) []string {
	names := make([]string, 0, len(f.Outlets))
	for name := range f.Outlets {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}
