package check

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// frameworkPrefix is the module path prefix of the Impulse Framework libraries.
const frameworkPrefix = "github.com/cccteam/"

// ImpulseModule is impulse's own module path: the tool directive every application's
// go.mod carries, so that CI runs go tool impulse check at the pin go.mod names, verified
// by Go's checksum database, with no version written into the workflow.
const ImpulseModule = "github.com/cccteam/ccc/impulse"

// develVersion is what the build information reports for a binary built from a
// checkout (go build, go run) rather than from a module version.
const develVersion = "(devel)"

// pins reads the framework pins in go.mod. A pseudo-version or a local replace warns: the
// application builds against unreleased framework code, which is legitimate while a change
// is in flight and a problem once it is not. The impulse tool directive is a failure when
// it is missing, since CI's go tool impulse check has nothing to run, and when the running
// impulse was built from a module version (go tool, go install module@version) other than
// the pin, since the two would render the owned files differently; a build from a checkout
// is a development build, noted and not compared. The remedy for a pin behind the running
// impulse depends on where the application stands against the upgrade ledger (Env.Ledger):
// at its last step, moving the pin alone is right; behind it, impulse upgrade moves the
// pin with each step, since go get -tool ahead of the walk drags the framework pins past
// the steps.
type pins struct {
	// build answers the running impulse's build; nil reads the build information
	// (RunningBuild). Tests inject one.
	build func() Build
}

func (pins) Name() string { return "pins" }

func (pins) Describe() string {
	return "framework pins in go.mod are released versions, and go.mod holds the impulse tool directive at the running impulse's version"
}

// Build is what the running impulse's build information says about it.
type Build struct {
	// Version is the main module's version: a release or pseudo-version when the binary
	// was built from a module version, what version control stamped on a build from a
	// checkout (a pseudo-version, +dirty when the tree was), or "(devel)".
	Version string
	// FromModule reports a binary built from a module version (go tool, go install
	// module@version), which carries the module's checksum; a build from a checkout (go
	// build, go run) carries none, whatever version control stamped on it.
	FromModule bool
}

// RunningBuild reads the running impulse's build information.
func RunningBuild() Build {
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" {
		return Build{Version: develVersion}
	}

	return Build{Version: info.Main.Version, FromModule: info.Main.Sum != ""}
}

// RunningVersion is the running impulse's version: what impulse --version prints, and the
// version the pins check compares go.mod's impulse pin with when the build is a module's.
func RunningVersion() string {
	return RunningBuild().Version
}

func (c pins) Run(_ context.Context, env *Env) Result {
	mod := env.App.GoMod
	var warnings []string
	count := 0
	for _, r := range mod.Require {
		if !strings.HasPrefix(r.Mod.Path, frameworkPrefix) {
			continue
		}
		count++
		if module.IsPseudoVersion(r.Mod.Version) {
			warnings = append(warnings, fmt.Sprintf("%s is pinned to pseudo-version %s", r.Mod.Path, r.Mod.Version))
		}
	}
	for _, r := range mod.Replace {
		if !strings.HasPrefix(r.Old.Path, frameworkPrefix) {
			continue
		}
		if r.New.Version == "" {
			warnings = append(warnings, fmt.Sprintf("%s is replaced by local path %s", r.Old.Path, r.New.Path))
		} else {
			warnings = append(warnings, fmt.Sprintf("%s is replaced by %s %s", r.Old.Path, r.New.Path, r.New.Version))
		}
	}
	failures, notes := c.directive(env)

	switch {
	case len(failures) > 0:
		return fail(c.Name(), fmt.Sprintf("%d framework pin problem(s)", len(failures)), append(append(failures, warnings...), notes...)...)
	case len(warnings) > 0:
		return warn(c.Name(), fmt.Sprintf("%d framework pin(s) point at unreleased code", len(warnings)), append(warnings, notes...)...)
	case len(notes) > 0:
		return passWithDetails(c.Name(), fmt.Sprintf("%d framework pin(s) are released versions; go.mod holds the impulse tool directive", count), notes...)
	default:
		return pass(c.Name(), fmt.Sprintf("%d framework pin(s) are released versions; go.mod holds the impulse tool directive at the running impulse's version", count))
	}
}

// directive applies the tool-directive rule: go.mod carries tool github.com/cccteam/ccc/impulse
// and a require of it, and the pin is the running impulse's version when that impulse was
// built from a module version. It returns the failures and the notes.
func (c pins) directive(env *Env) (failures, notes []string) {
	behind := env.Ledger != nil && !env.Ledger.AtLastStep(env.App.GoMod)
	mod := env.App.GoMod
	held := false
	for _, t := range mod.Tool {
		if t.Path == ImpulseModule {
			held = true
		}
	}
	pinned := ""
	for _, r := range mod.Require {
		if r.Mod.Path == ImpulseModule {
			pinned = r.Mod.Version
		}
	}
	running := c.running()
	released := running.FromModule && semver.IsValid(running.Version)
	switch {
	case !held || pinned == "":
		at := "<version>"
		if released {
			at = running.Version
		}

		return []string{fmt.Sprintf("go.mod has no tool directive for %s, so CI's go tool impulse check has nothing to run: run go get -tool %s@%s, then go tool impulse render, then go tool impulse check", ImpulseModule, ImpulseModule, at)}, nil
	case !released:
		return nil, []string{fmt.Sprintf("go.mod holds the impulse tool directive, pinned at %s; the running impulse is a development build (%s, built from a checkout), so the pin is not compared with it", pinned, running.Version)}
	case running.Version != pinned && behind:
		return []string{fmt.Sprintf("go.mod pins impulse at %s and the running impulse is %s, and the framework pins stand behind the ledger's last step: walk the steps with impulse upgrade (go run %s@%s upgrade), which moves the tool pin with each step; go get -tool ahead of the walk would drag the framework pins past the steps", pinned, running.Version, ImpulseModule, running.Version)}, nil
	case running.Version != pinned:
		return []string{fmt.Sprintf("go.mod pins impulse at %s and the running impulse is %s: move the pin with go get -tool %s@%s, then go tool impulse render, then go tool impulse check", pinned, running.Version, ImpulseModule, running.Version)}, nil
	default:
		return nil, nil
	}
}

// running is the running impulse's build, from the injected reader or the build
// information.
func (c pins) running() Build {
	if c.build != nil {
		return c.build()
	}

	return RunningBuild()
}
