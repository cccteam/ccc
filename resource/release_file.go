package resource

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cccteam/ccc/resource/scheduled"
	"github.com/go-playground/errors/v5"
)

// ReleaseFileName is the generated release file's name. The generator writes it beside
// the generated router (zz_gen_router.go) in the router package's directory whenever a
// router is generated, and the stale sweep removes it when the router goes. It names
// every outlet the generator knows and, for each session outlet, the oldest release of
// the browser application the outlet still answers (OldestAnswered in the generator
// program), so a deploy reads from the checkout, never from a binary, whether a release
// needs a maintenance window: a release whose outlet answers only the server's own does.
// It also lists the scheduled routes (@schedule) with their schedules, which the
// application's stack reads to create a Cloud Scheduler job for each.
const ReleaseFileName = "zz_gen_release.json"

// How the release file spells its values: ThisRelease as an outlet's oldest answered
// release, and a machine outlet's kind.
const (
	releaseFileThis   = "this"
	releaseFileAPIKey = "api-key"
)

// ReleaseFile is the generated release file: one entry per outlet, keyed by the outlet's
// name (default for the outlet GenerateRoutes declares, the name given to
// WithRouterOutlet otherwise), and the scheduled routes in path order, absent when the
// application declares none.
type ReleaseFile struct {
	Outlets   map[string]ReleaseOutlet `json:"outlets"`
	Scheduled []ScheduledRoute         `json:"scheduled,omitempty"`
	// FileRoutes are the routes that carry a file rather than JSON, in path order: each
	// @upload method's route and each stored file's read route (@file). None when the
	// code declares neither.
	FileRoutes []FileRoute `json:"fileRoutes,omitempty"`
	// Surfaces are the surfaces the code declares a request log word or a trace setting
	// for, in prefix order: the application default as the surface at /
	// (WithRequestLog), each outlet that declares a word or a setting as its prefix
	// (OutletRequestLog, OutletTraces), each prefix the application mounts routes under
	// by hand (WithMountedRoutes), and each generated route with its own words (@rpc,
	// @file, @schedule) as the path the router mounts it at. Each says whether it is a
	// prefix or a route (Kind), since the router matches the two differently. The
	// nearest declaration wins, so a reader resolves a path by its longest matching
	// surface. The application's stack reads them to render the cloud's own request-log
	// exclusion. None when the code declares nothing, and then every request's entry is
	// written and every span follows the front end.
	Surfaces []Surface `json:"surfaces,omitempty"`
}

// Surface is one declared surface in the release file: a path prefix or a route pattern
// as the router mounts it, with the request log word, the trace setting, or both.
type Surface struct {
	// Prefix is the path as mounted: a prefix such as /beacons/ or /droids/, or a route
	// pattern such as /api/widgets/{id}/content, where a segment in braces stands for any
	// one segment; / is the application default.
	Prefix string `json:"prefix"`
	// Kind says how the router matches the prefix against a request's path:
	// SurfacePrefix by prefix, so everything under /beacons/ is the surface's, or
	// SurfaceRoute to the end of the path, so /api/widgets/{id}/content is the surface's
	// and /api/widgets/{id}/content-extra is not. The application default, an outlet and
	// a hand-mounted prefix are prefixes; an annotated method's route (@rpc, @file,
	// @schedule) is a route. A file written before the surfaces carried their kind reads
	// as prefixes throughout.
	Kind SurfaceKind `json:"kind"`
	// Log is the request log word: RequestLogAlways, RequestLogOnEvent, RequestLogSampled
	// or RequestLogNever; empty when the surface declares its trace setting alone.
	Log string `json:"log,omitempty"`
	// Fraction is the share of the quiet requests a sampled surface writes the entry for,
	// above 0 and at most 1; set with RequestLogSampled alone.
	Fraction float64 `json:"fraction,omitempty"`
	// Traces is the trace setting: TracesFollowFrontEnd, TracesCapped or TracesOff; empty
	// when the surface declares its request log word alone.
	Traces string `json:"traces,omitempty"`
	// Rate is the share of the front end's traces a capped surface keeps, above 0 and at
	// most 1; set with TracesCapped alone.
	Rate float64 `json:"rate,omitempty"`
}

// The request log words, as the release file and the annotations spell them: the entry
// is written for every request, when a line attached or the request failed, as on event
// plus a fraction of the quiet requests, or never.
const (
	RequestLogAlways  = "always"
	RequestLogOnEvent = "onEvent"
	RequestLogSampled = "sampled"
	RequestLogNever   = "never"
)

// The trace settings, as the release file and the annotations spell them: a surface's
// spans follow the front end, are capped at a rate, or are off.
const (
	TracesFollowFrontEnd = "followFrontEnd"
	TracesCapped         = "capped"
	TracesOff            = "off"
)

// SurfaceKind is how the router matches a surface's prefix against a request's path, as
// the release file spells it.
type SurfaceKind string

// The surface kinds: a prefix is matched against the start of the path and a route to
// its end.
const (
	// SurfacePrefix is a path prefix, matched by strings.HasPrefix as the router does: the
	// application default at /, an outlet's prefix, or a prefix mounted by hand.
	SurfacePrefix SurfaceKind = "prefix"
	// SurfaceRoute is a route pattern, matched whole as the router does: an annotated
	// method's path, a parameter in braces standing for one segment.
	SurfaceRoute SurfaceKind = "route"
)

// FileRoute is one route in the release file that carries a file rather than JSON: an
// @upload method's route, whose body is multipart, or a stored file's read route (@file
// on a key column), whose answer is the object. The application's stack reads them to
// put each ahead of its web application firewall's rules: a body of bytes trips rules
// written for text, so the route is matched before the rules and never evaluated by
// them, and the rule the stack writes names the declaration here.
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

// The kinds of file route.
const (
	// FileRouteUpload is an @upload method's route.
	FileRouteUpload = "upload"
	// FileRouteStored is a stored file's read route, a @file column's.
	FileRouteStored = "file"
)

// ScheduledRoute is one scheduled route in the release file: the method declared with
// @schedule, as the generated router mounts it and Cloud Scheduler calls it.
type ScheduledRoute struct {
	// Path is the route's path, under the scheduled prefix: /_scheduled/<method in kebab
	// case>.
	Path string `json:"path"`
	// Schedule is the cron expression as declared: five fields, minute, hour, day of the
	// month, month and day of the week.
	Schedule string `json:"schedule"`
	// TimeZone is the zone the schedule is read in, an IANA zone name as declared, or
	// UTC when the declaration names none.
	TimeZone string `json:"timeZone"`
}

// ReleaseOutlet is one outlet's entry in the release file. A session outlet carries its
// oldest answered release; a machine outlet is never checked against a release and
// carries only its kind.
type ReleaseOutlet struct {
	// APIKey marks a machine outlet (APIKey in the generator program), written as
	// {"kind": "api-key"}: its clients carry no release, so nothing is checked against
	// one, and the entry carries no oldestAnswered.
	APIKey bool
	// OldestAnswered is a session outlet's oldest answered release as the generator
	// program declared it: a release such as 1.5.0; ThisRelease, written as "this", for
	// a release that changes the API inside a maintenance window; or empty, written as
	// "", when the option is absent and every release that sends the header is
	// answered. Empty on a machine outlet.
	OldestAnswered string
}

// releaseOutletEntry is an outlet's entry as the file spells it: kind on a machine
// outlet, oldestAnswered on a session outlet, each absent on the other.
type releaseOutletEntry struct {
	Kind           string  `json:"kind,omitempty"`
	OldestAnswered *string `json:"oldestAnswered,omitempty"`
}

// MarshalJSON writes the entry as the file spells it: {"kind": "api-key"} for a machine
// outlet, {"oldestAnswered": <release>} for a session outlet with ThisRelease as "this".
func (o ReleaseOutlet) MarshalJSON() ([]byte, error) {
	if o.APIKey {
		data, err := json.Marshal(releaseOutletEntry{Kind: releaseFileAPIKey})
		if err != nil {
			return nil, errors.Wrap(err, "json.Marshal()")
		}

		return data, nil
	}

	oldest := o.OldestAnswered
	if oldest == ThisRelease {
		oldest = releaseFileThis
	}
	data, err := json.Marshal(releaseOutletEntry{OldestAnswered: &oldest})
	if err != nil {
		return nil, errors.Wrap(err, "json.Marshal()")
	}

	return data, nil
}

// UnmarshalJSON reads an entry as the file spells it and refuses one that is neither a
// machine outlet nor a session outlet naming its oldest answered release: a kind other
// than api-key, a machine outlet carrying oldestAnswered, a session outlet without it,
// or an oldestAnswered that is neither a release, "this" nor empty.
func (o *ReleaseOutlet) UnmarshalJSON(data []byte) error {
	var entry releaseOutletEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return errors.Wrap(err, "json.Unmarshal()")
	}

	switch {
	case entry.Kind == releaseFileAPIKey:
		if entry.OldestAnswered != nil {
			return errors.Newf("an %s outlet carries no oldestAnswered: its clients carry no release", releaseFileAPIKey)
		}
		*o = ReleaseOutlet{APIKey: true}

		return nil
	case entry.Kind != "":
		return errors.Newf("kind %q is not %q, the one kind an entry declares; a session outlet declares no kind", entry.Kind, releaseFileAPIKey)
	case entry.OldestAnswered == nil:
		return errors.New("a session outlet names its oldestAnswered: a release, \"this\" for the server's own, or \"\" for every release")
	}

	oldest := *entry.OldestAnswered
	switch oldest {
	case releaseFileThis:
		oldest = ThisRelease
	case "":
	default:
		if _, ok := releaseVersion(oldest); !ok {
			return errors.Newf("oldestAnswered %q is not a release: write a semantic version such as \"1.5.0\", \"this\" for the server's own, or \"\" for every release", oldest)
		}
	}
	*o = ReleaseOutlet{OldestAnswered: oldest}

	return nil
}

// ReadReleaseFile reads the generated release file from dir, the router package's
// directory. A missing file is an error wrapping fs.ErrNotExist: no router is generated
// there, or the application predates the file. A file that is not the shape the
// generator writes, or that names no outlet, is refused naming what is wrong.
func ReadReleaseFile(dir string) (ReleaseFile, error) {
	path := filepath.Join(dir, ReleaseFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		return ReleaseFile{}, errors.Wrapf(err, "reading the release file %s", path)
	}

	var file ReleaseFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return ReleaseFile{}, errors.Wrapf(err, "the release file %s is not the shape the generator writes", path)
	}
	if len(file.Outlets) == 0 {
		return ReleaseFile{}, errors.Newf("the release file %s names no outlet; a generated router has at least its default outlet", path)
	}
	for _, route := range file.Scheduled {
		if err := route.validate(); err != nil {
			return ReleaseFile{}, errors.Wrapf(err, "the release file %s", path)
		}
	}
	for _, route := range file.FileRoutes {
		if err := route.validate(); err != nil {
			return ReleaseFile{}, errors.Wrapf(err, "the release file %s", path)
		}
	}
	for i := range file.Surfaces {
		surface := &file.Surfaces[i]
		if surface.Kind == "" {
			// A file written before the surfaces carried their kind listed prefixes
			// alone, and reads as it did.
			surface.Kind = SurfacePrefix
		}
		if err := surface.validate(); err != nil {
			return ReleaseFile{}, errors.Wrapf(err, "the release file %s", path)
		}
	}

	return file, nil
}

// validate refuses a surface the generator would not write: a prefix not under the root,
// a kind that is neither a prefix nor a route, neither word nor setting, a word or a
// setting outside the vocabulary, a fraction or a rate outside (0, 1], a fraction without
// the sampled word, or a rate without the capped setting.
func (s *Surface) validate() error {
	switch {
	case !strings.HasPrefix(s.Prefix, "/"):
		return errors.Newf("the surface %q is not a path under the root", s.Prefix)
	case s.Kind != SurfacePrefix && s.Kind != SurfaceRoute:
		return errors.Newf("the surface %s has the kind %q; a surface is a %s or a %s", s.Prefix, s.Kind, SurfacePrefix, SurfaceRoute)
	case s.Log == "" && s.Traces == "":
		return errors.Newf("the surface %s declares neither a request log word nor a trace setting", s.Prefix)
	}
	switch s.Log {
	case "", RequestLogAlways, RequestLogOnEvent, RequestLogNever:
		if s.Fraction != 0 {
			return errors.Newf("the surface %s carries a fraction without the %s word", s.Prefix, RequestLogSampled)
		}
	case RequestLogSampled:
		if s.Fraction <= 0 || s.Fraction > 1 {
			return errors.Newf("the surface %s is sampled at %v; the fraction is above 0 and at most 1", s.Prefix, s.Fraction)
		}
	default:
		return errors.Newf("the surface %s has the request log word %q; the words are %s, %s, %s and %s", s.Prefix, s.Log, RequestLogAlways, RequestLogOnEvent, RequestLogSampled, RequestLogNever)
	}
	switch s.Traces {
	case "", TracesFollowFrontEnd, TracesOff:
		if s.Rate != 0 {
			return errors.Newf("the surface %s carries a rate without the %s setting", s.Prefix, TracesCapped)
		}
	case TracesCapped:
		if s.Rate <= 0 || s.Rate > 1 {
			return errors.Newf("the surface %s is capped at %v; the rate is above 0 and at most 1", s.Prefix, s.Rate)
		}
	default:
		return errors.Newf("the surface %s has the trace setting %q; the settings are %s, %s and %s", s.Prefix, s.Traces, TracesFollowFrontEnd, TracesCapped, TracesOff)
	}

	return nil
}

// validate refuses a file route the generator would not write: a kind that is neither
// an upload nor a stored file, a method other than the kind's, a path not under the
// root, or no source.
func (f FileRoute) validate() error {
	switch {
	case f.Kind != FileRouteUpload && f.Kind != FileRouteStored:
		return errors.Newf("the file route %q has the kind %q; a file route is an %s or a %s", f.Path, f.Kind, FileRouteUpload, FileRouteStored)
	case f.Kind == FileRouteUpload && f.Method != http.MethodPost, f.Kind == FileRouteStored && f.Method != http.MethodGet:
		return errors.Newf("the file route %q (%s) answers %s; an %s answers POST and a %s GET", f.Path, f.Kind, f.Method, FileRouteUpload, FileRouteStored)
	case !strings.HasPrefix(f.Path, "/") || len(f.Path) < 2:
		return errors.Newf("the file route %q is not a path under the root", f.Path)
	case f.Source == "":
		return errors.Newf("the file route %s names no source", f.Path)
	}

	return nil
}

// validate refuses a scheduled route the generator would not write: a path outside the
// scheduled prefix, or no schedule or zone.
func (s ScheduledRoute) validate() error {
	name, ok := strings.CutPrefix(s.Path, scheduled.Prefix+"/")
	switch {
	case !ok || name == "" || strings.Contains(name, "/"):
		return errors.Newf("the scheduled route %q is not a path under %s/", s.Path, scheduled.Prefix)
	case s.Schedule == "":
		return errors.Newf("the scheduled route %s names no schedule", s.Path)
	case s.TimeZone == "":
		return errors.Newf("the scheduled route %s names no time zone", s.Path)
	}

	return nil
}
