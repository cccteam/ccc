package check

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/go-playground/errors/v5"
	"golang.org/x/mod/semver"

	"github.com/cccteam/ccc/impulse/app"
)

// webPins verifies that every web root pins the web library at the versions of the ledger
// step the application stands at. A step's Go pins are what the application builds
// against; its web pins (@cccteam/resource, @cccteam/resource-angular,
// @cccteam/resource-firestore in each web root's package.json) are what its browser
// applications install, and the two were released together: a login page from the step's
// session library and a login component from an earlier web library do not agree. The
// step is read from go.mod as the walk reads it (Env.Ledger), so the check never needs a
// record of its own. A pin behind the step's fails, since impulse upgrade is what moves it
// (every web root's package.json, then bun install there); one ahead of it warns, as a Go
// pin bumped by hand ahead of its step is left where it stands. A web root naming none of
// the library's packages is not one of the library's and is passed over.
type webPins struct{}

func (webPins) Name() string { return "web-pins" }

func (webPins) Describe() string {
	return "every web root pins the web library at the versions of the ledger step the application stands at"
}

// WebRoot is one browser application's web root as the web library's pins are read from
// it: the directory holding angular.json, its package.json, and every pin of the
// library's packages the file carries.
type WebRoot struct {
	// Dir is the root-relative web root (web, apps/portal/web); File its package.json.
	Dir  string
	File string
	// Pins are the library's pins the file carries, section by section in the order of
	// dependencySections, each section's packages sorted.
	Pins []WebPin
}

// WebPin is one spec of a web library package in a section of package.json.
type WebPin struct {
	Section string
	Package string
	// Version is the spec as written: an exact version (0.0.16) in the skeletons, but
	// whatever the file holds (a range, a tag, a yalc attachment).
	Version string
}

// WebRoots reads the web roots naming any of the packages: every browser application (a
// directory holding angular.json) whose package.json names one of them in a dependency
// section, in directory order. One naming none of them is not a web root of the library
// and is left out; one without a package.json is left out too.
func WebRoots(a *app.App, packages []string) ([]WebRoot, error) {
	var roots []WebRoot
	for _, w := range a.WebApps {
		file := path.Join(w.Dir, "package.json")
		data, err := os.ReadFile(a.Abs(file))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		pins, err := webPinsOf(data, packages)
		if err != nil {
			return nil, errors.Wrapf(err, "%s", file)
		}
		if len(pins) == 0 {
			continue
		}
		roots = append(roots, WebRoot{Dir: w.Dir, File: file, Pins: pins})
	}

	return roots, nil
}

// webPinsOf reads the specs of the packages from a package.json's dependency sections,
// entry by entry, so an override that nests (another shape than a spec) hides no pin
// beside it.
func webPinsOf(data []byte, packages []string) ([]WebPin, error) {
	var sections map[string]json.RawMessage
	if err := json.Unmarshal(data, &sections); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal()")
	}
	var pins []WebPin
	for _, section := range dependencySections {
		raw, ok := sections[section]
		if !ok {
			continue
		}
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entries); err != nil {
			continue // a section of another shape carries no spec to read
		}
		for _, name := range packages {
			var spec string
			if entry, ok := entries[name]; ok && json.Unmarshal(entry, &spec) == nil {
				pins = append(pins, WebPin{Section: section, Package: name, Version: spec})
			}
		}
	}

	return pins, nil
}

// Spec is the spec a web root holds for a package, "" when it names the package nowhere:
// the first section's, which is the dependencies' in a file the skeleton rendered.
func (r *WebRoot) Spec(name string) string {
	for _, p := range r.Pins {
		if p.Package == name {
			return p.Version
		}
	}

	return ""
}

// WebVersion reads a web pin's spec as a semantic version, "v0.0.16" for "0.0.16", since
// npm versions carry no v; "" when the spec is not an exact release: a range ("^0.0.16"),
// a tag ("latest"), a yalc attachment, a pre-release or a spec with build metadata.
func WebVersion(spec string) string {
	if strings.HasPrefix(spec, "v") {
		return ""
	}
	v := "v" + spec
	if !semver.IsValid(v) || semver.Canonical(v) != v || semver.Prerelease(v) != "" {
		return ""
	}

	return v
}

// WebBehind reports whether a web pin's spec stands behind a version: it is not an exact
// release, or an earlier one. A spec at or beyond the version is not behind, as a Go pin
// bumped by hand ahead of its step is not.
func WebBehind(spec, version string) bool {
	v := WebVersion(spec)

	return v == "" || semver.Compare(v, "v"+version) < 0
}

// WebLine renders web pins as one line, "@cccteam/resource 0.0.12, @cccteam/resource-angular
// 0.0.16, ...", in package order.
func WebLine(pins map[string]string) string {
	parts := make([]string, 0, len(pins))
	for _, name := range sortedKeys(pins) {
		parts = append(parts, name+" "+pins[name])
	}

	return strings.Join(parts, ", ")
}

func (c webPins) Run(_ context.Context, env *Env) Result {
	a := env.App
	if len(a.WebApps) == 0 {
		return skip(c.Name(), "no browser apps")
	}
	if env.Ledger == nil {
		return skip(c.Name(), "no ledger to read the step from")
	}
	want := env.Ledger.WebPins(a.GoMod)
	if len(want) == 0 {
		return skip(c.Name(), "the ledger step the application stands at names no web pins")
	}
	roots, err := WebRoots(a, sortedKeys(want))
	if err != nil {
		return fail(c.Name(), err.Error())
	}
	if len(roots) == 0 {
		return skip(c.Name(), "no web root names the web library")
	}
	var failures, warnings []string
	for i := range roots {
		behind, ahead := c.root(&roots[i], want)
		failures = append(failures, behind...)
		warnings = append(warnings, ahead...)
	}

	switch {
	case len(failures) > 0:
		return fail(c.Name(), fmt.Sprintf("%d web pin(s) differ from the ledger step the application stands at (%s); impulse upgrade moves them", len(failures), WebLine(want)), append(failures, warnings...)...)
	case len(warnings) > 0:
		return warn(c.Name(), fmt.Sprintf("%d web pin(s) stand ahead of the ledger step the application stands at (%s)", len(warnings), WebLine(want)), warnings...)
	default:
		return pass(c.Name(), fmt.Sprintf("%d web root(s) pin the web library at the ledger step's versions (%s)", len(roots), WebLine(want)))
	}
}

// root reads one web root against the step's web pins: every spec of a package the step
// pins, in every section, and every package the step pins that the file does not name.
func (webPins) root(r *WebRoot, want map[string]string) (behind, ahead []string) {
	named := map[string]bool{}
	for _, p := range r.Pins {
		named[p.Package] = true
		step := want[p.Package]
		switch {
		case p.Version == step:
		case WebBehind(p.Version, step):
			behind = append(behind, fmt.Sprintf("%s %s: %s is %s and the step pins %s", r.File, p.Section, p.Package, p.Version, step))
		default:
			ahead = append(ahead, fmt.Sprintf("%s %s: %s is %s, ahead of the step's %s; a pin ahead of its step stays until a step reaches it", r.File, p.Section, p.Package, p.Version, step))
		}
	}
	for _, name := range sortedKeys(want) {
		if !named[name] {
			behind = append(behind, fmt.Sprintf("%s names no %s, which the step pins at %s; add it to the dependencies as the skeleton's web root has it", r.File, name, want[name]))
		}
	}

	return behind, ahead
}

// Meaning explains the invariant for the brief.
func (webPins) Meaning() string {
	return "A ledger step pins the web library beside the Go modules: `@cccteam/resource`, `@cccteam/resource-angular` and `@cccteam/resource-firestore` in every web root's `package.json`, at the versions the skeleton's web roots named when the step's release shipped, since the server libraries and the browser library of one release were written for each other. `impulse upgrade` moves them with the step (every web root's `package.json` is edited in place and `bun install` runs there, so `bun.lock` records the registry versions); each line under the check names the file, the section, the package, what it holds and what the step pins. Running `impulse upgrade` is the fix; moving the versions by hand and running `bun install` in the web root is the same change."
}
