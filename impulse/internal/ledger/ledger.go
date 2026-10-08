// Package ledger records the steps that take an application from the framework pins an
// earlier impulse skeleton named to the pins the current one names. A step is a coherent
// pin set, the resource, access, session and accesstypes versions the skeleton's go.mod
// named, together with the recipes that take an application from the step before it, or a
// note that none is needed. impulse upgrade walks the ledger step by step, reading where
// the application stands from the framework pins in its go.mod and committing each step as
// it goes; the pin is the checkpoint, and nothing else records progress. The impulse tool
// pin is not a step's: the walk moves it to the running impulse before the first step, so
// an impulse release that changes nothing an application builds against needs no entry
// here, and no entry names an impulse version.
//
// A step is appended when the skeleton's pins move or a recipe is needed, and never
// otherwise; the ledger's test holds the last step's pins to the skeleton's, so a pin bump
// without its step fails the build. A breaking change in resource, access, session or
// accesstypes is not done until the step that carries it records a recipe for it here, or
// says none is needed.
package ledger

import (
	"context"
	"fmt"
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
// in an application's go.mod say which step the application stands at.
const FrameworkPrefix = "github.com/cccteam/"

// Step is one step the ledger records.
type Step struct {
	// Pins are the framework modules' versions the skeleton named at the step, by module
	// path: what an application at this step builds against.
	Pins map[string]string
	// Recipes are the code changes the step needs of an application at the step before
	// it, applied in order; none when the pin bump is the whole step.
	Recipes []Recipe
	// Note says in one line what the step changes for an application, for the plan and
	// the commit; "no code change is needed" when the pins are all that move.
	Note string
}

// Recipe is one code change a step needs: a detector that names what in the application
// is in the old form, and an edit that moves only what it finds, so the recipe is safe to
// run twice and safe on an application whose pins were bumped by hand ahead of its code.
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

// Steps are the recorded steps, oldest first. The first is the first published impulse
// release's pin set (impulse v0.1.0).
var Steps = []Step{
	{
		Pins: map[string]string{
			"github.com/cccteam/access":          "v0.10.1",
			"github.com/cccteam/ccc":             "v0.3.3",
			"github.com/cccteam/ccc/accesstypes": "v0.6.0",
			"github.com/cccteam/ccc/resource":    "v0.11.0",
			"github.com/cccteam/ccc/tracer":      "v0.1.7",
			"github.com/cccteam/db-initiator":    "v0.4.1",
			"github.com/cccteam/httpio":          "v0.7.19",
			"github.com/cccteam/logger":          "v0.1.27",
			"github.com/cccteam/session":         "v0.12.0",
		},
		Note: "the first impulse release's pins; an application created by it stands here already",
	},
}

// Short names a framework module without the prefix (ccc/resource, access).
func Short(path string) string {
	return strings.TrimPrefix(path, FrameworkPrefix)
}

// PinNames lists the module paths a step pins, sorted.
func (s *Step) PinNames() []string {
	names := make([]string, 0, len(s.Pins))
	for name := range s.Pins {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// Reached reports whether an application with the pins stands at or beyond the step:
// every module the step pins is required at its version or a later one.
func (s *Step) Reached(pins map[string]string) bool {
	for name, version := range s.Pins {
		have, ok := pins[name]
		if !ok || semver.Compare(have, version) < 0 {
			return false
		}
	}

	return true
}

// Moves lists the pins the step moves from the pins given, "ccc/resource v0.12.0" each
// in module order: a module the pins lack or hold at an earlier version. A pin already at
// or beyond the step's is not moved.
func (s *Step) Moves(from map[string]string) []string {
	var moves []string
	for _, name := range s.PinNames() {
		if have, ok := from[name]; !ok || semver.Compare(have, s.Pins[name]) < 0 {
			moves = append(moves, Short(name)+" "+s.Pins[name])
		}
	}

	return moves
}

// AppPins reads the framework pins from an application's go.mod: every required module
// under the framework prefix but impulse itself, whose pin is the tool's.
func AppPins(mod *modfile.File) map[string]string {
	pins := map[string]string{}
	if mod == nil {
		return pins
	}
	for _, r := range mod.Require {
		if strings.HasPrefix(r.Mod.Path, FrameworkPrefix) && r.Mod.Path != check.ImpulseModule {
			pins[r.Mod.Path] = r.Mod.Version
		}
	}

	return pins
}

// Position is the index of the latest step the application's pins reach, -1 when they
// reach none: where the application stands in the ledger.
func Position(steps []Step, pins map[string]string) int {
	position := -1
	for i := range steps {
		if steps[i].Reached(pins) {
			position = i
		}
	}

	return position
}

// Pending lists the steps after the application's position, in order: none when its pins
// reach the last.
func Pending(steps []Step, pins map[string]string) []Step {
	return steps[Position(steps, pins)+1:]
}

// Names labels each step by the pins it moves from the step before it (the first by all of
// its pins), for the plan and the tests.
func Names(steps []Step) []string {
	names := make([]string, 0, len(steps))
	var prev map[string]string
	for i := range steps {
		names = append(names, strings.Join(steps[i].Moves(prev), ", "))
		prev = steps[i].Pins
	}

	return names
}

// Validate checks the ledger's shape: at least one step, each with a pin set of framework
// modules at valid versions (impulse's own pin is the tool's, not a step's) and a note,
// no pin moving backward from the step before, every step after the first moving a pin or
// naming a recipe, and no recipe named twice in one step. The ledger's test holds Steps to
// it.
func Validate(steps []Step) error {
	if len(steps) == 0 {
		return errors.New("the ledger records no step; the first is the first impulse release's pins")
	}
	for i := range steps {
		s := &steps[i]
		at := fmt.Sprintf("step %d", i+1)
		switch {
		case len(s.Pins) == 0:
			return errors.Newf("%s pins no framework module", at)
		case strings.TrimSpace(s.Note) == "":
			return errors.Newf("%s has no note", at)
		}
		for _, name := range s.PinNames() {
			switch {
			case name == check.ImpulseModule:
				return errors.Newf("%s pins %s, which is the tool pin the walk moves to the running impulse, not a step's", at, name)
			case !strings.HasPrefix(name, FrameworkPrefix):
				return errors.Newf("%s pins %s, which is not a framework module (%s...)", at, name, FrameworkPrefix)
			case !semver.IsValid(s.Pins[name]):
				return errors.Newf("%s pins %s at %q, which is not a semantic version", at, name, s.Pins[name])
			}
		}
		if i > 0 {
			prev := &steps[i-1]
			for _, name := range s.PinNames() {
				if have, ok := prev.Pins[name]; ok && semver.Compare(s.Pins[name], have) < 0 {
					return errors.Newf("%s pins %s at %s, behind step %d's %s; the ledger walks forward only", at, name, s.Pins[name], i, have)
				}
			}
			if len(s.Moves(prev.Pins)) == 0 && len(s.Recipes) == 0 {
				return errors.Newf("%s moves no pin and names no recipe: it is not a step", at)
			}
		}
		seen := map[string]bool{}
		for _, recipe := range s.Recipes {
			if seen[recipe.Name()] {
				return errors.Newf("%s names the recipe %s twice", at, recipe.Name())
			}
			seen[recipe.Name()] = true
		}
	}

	return nil
}
