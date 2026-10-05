package resource

import (
	"encoding/json"
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
}

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

	return file, nil
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
