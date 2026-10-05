// Package ledger records the impulse releases an application is moved through. An impulse
// release is a coherent pin set, the framework modules' versions the skeleton's go.mod
// named at that release, together with the recipes that take an application from the
// release before it, or a note that none is needed. impulse upgrade walks the ledger
// release by release, reading where the application stands from the framework pins in its
// go.mod and committing each release as it goes; the pin is the checkpoint, and nothing
// else records progress.
//
// The ledger records releases from the first published impulse beta on. It is empty until
// that beta is cut, and its first entry is written when it is. A breaking change in
// resource, access, session or accesstypes after that is not done until the impulse
// release that carries it records a recipe for it here, or says none is needed.
package ledger

import (
	"context"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/transition"
)

// FrameworkPrefix is the module path prefix of the Impulse Framework libraries, whose pins
// in an application's go.mod say which release the application stands at.
const FrameworkPrefix = "github.com/cccteam/"

// Release is one impulse release the ledger records.
type Release struct {
	// Version is the impulse module's version at the release, the version the tool
	// directive in an application's go.mod pins (v0.1.0).
	Version string
	// Pins are the framework modules' versions the release's skeleton named, by module
	// path: what an application at this release builds against.
	Pins map[string]string
	// Recipes are the code changes the release needs of an application at the release
	// before it, applied in order; none when the pin bump is the whole step.
	Recipes []Recipe
	// Note says in one line what the release changes for an application, for the plan
	// and the commit; "no code change is needed" when the pins are all that move.
	Note string
}

// Recipe is one code change a release needs: a detector that names what in the
// application is in the old form, and an edit that moves only what it finds, so the recipe
// is safe to run twice and safe on an application whose pins were bumped by hand ahead of
// its code.
type Recipe interface {
	// Name is the recipe's short name (paging).
	Name() string
	// Detect names what in the application is in the old form, one line each (file:line:
	// what); none when the recipe has nothing to do here.
	Detect(ctx context.Context, a *app.App) ([]string, error)
	// Apply edits the application where the old form is found and reports what it did and
	// what it left to the agent.
	Apply(ctx context.Context, a *app.App, exec check.Execer) (*transition.Change, error)
	// Meaning says what the change means in this framework, for the brief.
	Meaning() string
}

// Releases are the recorded impulse releases, oldest first. Empty until the first
// published impulse beta is cut (see the package comment).
var Releases = []Release{}

// PinNames lists the module paths a release pins, sorted.
func (r *Release) PinNames() []string {
	names := make([]string, 0, len(r.Pins))
	for name := range r.Pins {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// Reached reports whether an application with the pins stands at or beyond the release:
// every module the release pins is required at its version or a later one.
func (r *Release) Reached(pins map[string]string) bool {
	for name, version := range r.Pins {
		have, ok := pins[name]
		if !ok || semver.Compare(have, version) < 0 {
			return false
		}
	}

	return true
}

// AppPins reads the framework pins from an application's go.mod: every required module
// under the framework prefix, by path.
func AppPins(mod *modfile.File) map[string]string {
	pins := map[string]string{}
	if mod == nil {
		return pins
	}
	for _, r := range mod.Require {
		if strings.HasPrefix(r.Mod.Path, FrameworkPrefix) {
			pins[r.Mod.Path] = r.Mod.Version
		}
	}

	return pins
}

// Position is the index of the latest release the application's pins reach, -1 when they
// reach none: where the application stands in the ledger.
func Position(releases []Release, pins map[string]string) int {
	position := -1
	for i := range releases {
		if releases[i].Reached(pins) {
			position = i
		}
	}

	return position
}

// Pending lists the releases after the application's position, in order, up to and
// including the target version; an empty target takes every later release, and a target
// at the position leaves none. A target the ledger does not record is refused, and so is
// one before the position (there is nothing to walk back to).
func Pending(releases []Release, pins map[string]string, target string) ([]Release, error) {
	position := Position(releases, pins)
	end := len(releases)
	if target != "" {
		at := -1
		for i := range releases {
			if releases[i].Version == target {
				at = i
			}
		}
		switch {
		case at < 0:
			return nil, errors.Newf("%s is not an impulse release the ledger records (%s)", target, strings.Join(Versions(releases), ", "))
		case at < position:
			return nil, errors.Newf("the application already stands beyond %s (its pins reach %s); an upgrade walks forward only", target, releases[position].Version)
		}
		end = at + 1
	}

	return releases[position+1 : end], nil
}

// Versions lists the releases' versions in order.
func Versions(releases []Release) []string {
	versions := make([]string, 0, len(releases))
	for i := range releases {
		versions = append(versions, releases[i].Version)
	}

	return versions
}

// Validate checks the ledger's shape: each release a valid semantic version newer than
// the one before it, each with a pin set of valid versions and a note, and no recipe named
// twice in one release. The ledger's test holds Releases to it.
func Validate(releases []Release) error {
	for i := range releases {
		r := &releases[i]
		switch {
		case !semver.IsValid(r.Version):
			return errors.Newf("release %d: %q is not a semantic version", i, r.Version)
		case i > 0 && semver.Compare(releases[i-1].Version, r.Version) >= 0:
			return errors.Newf("release %s follows %s; the ledger is oldest first", r.Version, releases[i-1].Version)
		case len(r.Pins) == 0:
			return errors.Newf("release %s pins no framework module", r.Version)
		case strings.TrimSpace(r.Note) == "":
			return errors.Newf("release %s has no note", r.Version)
		}
		for _, name := range r.PinNames() {
			switch {
			case !strings.HasPrefix(name, FrameworkPrefix):
				return errors.Newf("release %s pins %s, which is not a framework module (%s...)", r.Version, name, FrameworkPrefix)
			case !semver.IsValid(r.Pins[name]):
				return errors.Newf("release %s pins %s at %q, which is not a semantic version", r.Version, name, r.Pins[name])
			}
		}
		seen := map[string]bool{}
		for _, recipe := range r.Recipes {
			if seen[recipe.Name()] {
				return errors.Newf("release %s names the recipe %s twice", r.Version, recipe.Name())
			}
			seen[recipe.Name()] = true
		}
	}

	return nil
}
