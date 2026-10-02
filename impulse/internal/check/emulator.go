package check

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/cccteam/ccc/impulse/app"
)

// emulatorVersion verifies that every place naming an emulator version agrees, emulator
// by emulator: the Spanner emulator's version in the generator option, the process
// files' image tags, and the test harnesses; the Firestore emulator's in the process
// files and test harnesses that start it from the Cloud SDK emulators image. An
// application naming no Firestore emulator serves no live pages in development, which
// the summary says and nothing fails on.
type emulatorVersion struct{}

func (emulatorVersion) Name() string { return "emulator-version" }

func (emulatorVersion) Describe() string {
	return "generator, process files, and test harnesses name one Spanner emulator version and one Firestore emulator version"
}

// emulatorRefs is every place one emulator's version is named.
type emulatorRefs struct {
	name string
	refs []app.EmulatorRef
}

func (c emulatorVersion) Run(_ context.Context, env *Env) Result {
	a := env.App
	var spanner []app.EmulatorRef
	for _, g := range a.Generators {
		if v := g.EmulatorVersion(); v != "" {
			spanner = append(spanner, app.EmulatorRef{File: g.File, Version: v})
		}
	}
	spanner = append(spanner, a.EmulatorImages...)
	spanner = append(spanner, a.EmulatorHarnesses...)
	if len(spanner) == 0 && len(a.FirestoreEmulatorImages) == 0 {
		return skip(c.Name(), "no emulator version is named anywhere")
	}

	var summaries, details []string
	for _, e := range []emulatorRefs{{name: "Spanner", refs: spanner}, {name: "Firestore", refs: a.FirestoreEmulatorImages}} {
		summary, lines := agreement(e)
		summaries = append(summaries, e.name+": "+summary)
		details = append(details, lines...)
	}
	if len(details) > 0 {
		return fail(c.Name(), strings.Join(summaries, "; "), details...)
	}

	return pass(c.Name(), strings.Join(summaries, "; "))
}

// agreement says whether one emulator's references name one version: the summary
// clause, and for a disagreement one detail line per reference, grouped by version.
func agreement(e emulatorRefs) (summary string, details []string) {
	if len(e.refs) == 0 {
		return "no emulator named", nil
	}

	byVersion := map[string][]app.EmulatorRef{}
	for _, r := range e.refs {
		byVersion[r.Version] = append(byVersion[r.Version], r)
	}
	if len(byVersion) == 1 {
		return fmt.Sprintf("%d reference(s) agree on %s", len(e.refs), e.refs[0].Version), nil
	}

	versions := make([]string, 0, len(byVersion))
	for v := range byVersion {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	for _, v := range versions {
		for _, r := range byVersion[v] {
			details = append(details, fmt.Sprintf("%-9s %-10s %s", e.name, v, refPos(r)))
		}
	}

	return fmt.Sprintf("%d different versions in use", len(versions)), details
}

func refPos(r app.EmulatorRef) string {
	if r.Line == 0 {
		return r.File
	}

	return fmt.Sprintf("%s:%d", r.File, r.Line)
}
