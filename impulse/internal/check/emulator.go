package check

import (
	"context"
	"fmt"
	"sort"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// emulatorVersion verifies that every place naming a Spanner emulator version agrees:
// the generator option, the process files' image tags, and the test harnesses.
type emulatorVersion struct{}

func (emulatorVersion) Name() string { return "emulator-version" }

func (emulatorVersion) Describe() string {
	return "generator, process files, and test harnesses name one Spanner emulator version"
}

func (c emulatorVersion) Run(_ context.Context, env *Env) Result {
	a := env.App
	var refs []app.EmulatorRef
	for _, g := range a.Generators {
		if v := g.EmulatorVersion(); v != "" {
			refs = append(refs, app.EmulatorRef{File: g.File, Version: v})
		}
	}
	refs = append(refs, a.EmulatorImages...)
	refs = append(refs, a.EmulatorHarnesses...)

	if len(refs) == 0 {
		return skip(c.Name(), "no Spanner emulator version is named anywhere")
	}

	byVersion := map[string][]app.EmulatorRef{}
	for _, r := range refs {
		byVersion[r.Version] = append(byVersion[r.Version], r)
	}
	if len(byVersion) == 1 {
		return pass(c.Name(), fmt.Sprintf("%d reference(s) agree on %s", len(refs), refs[0].Version))
	}

	versions := make([]string, 0, len(byVersion))
	for v := range byVersion {
		versions = append(versions, v)
	}
	sort.Strings(versions)

	var details []string
	for _, v := range versions {
		for _, r := range byVersion[v] {
			details = append(details, fmt.Sprintf("%-10s %s", v, refPos(r)))
		}
	}

	return fail(c.Name(), fmt.Sprintf("%d different emulator versions in use", len(versions)), details...)
}

func refPos(r app.EmulatorRef) string {
	if r.Line == 0 {
		return r.File
	}

	return fmt.Sprintf("%s:%d", r.File, r.Line)
}
