// releasefile.go reads the generated release file: the oldest release of the browser
// application each router outlet still answers, which decides whether a release needs
// a maintenance window.

package derive

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/semver"
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
// WithRouterOutlet otherwise).
type ReleaseFile struct {
	Outlets map[string]ReleaseOutlet `json:"outlets"`
}

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

	return &f, nil
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
