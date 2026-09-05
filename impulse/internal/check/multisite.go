package check

import (
	"context"
	"fmt"
	"slices"
)

// multiSite verifies the agreements between the generators of a multi-site application:
// every generator reads the one schema, and the shared generator's TypeScript reaches every
// site's browser app.
type multiSite struct{}

func (multiSite) Name() string { return "multi-site" }

func (multiSite) Describe() string {
	return "generators share one schema and the shared TypeScript reaches every site"
}

func (c multiSite) Run(_ context.Context, env *Env) Result {
	a := env.App
	if len(a.Generators) < 2 {
		return skip(c.Name(), "single generator")
	}

	var details []string

	// One schema: every generator's migration sources equal the first's.
	first := a.Generators[0]
	for _, g := range a.Generators[1:] {
		if !sameSet(first.MigrationSources, g.MigrationSources) {
			details = append(details, fmt.Sprintf("%s reads migrations %v but %s reads %v", g.File, g.MigrationSources, first.File, first.MigrationSources))
		}
	}

	// Shared TypeScript reaches every site: the browser apps the site generators write into.
	siteWebApps := map[string]string{} // web app dir -> site generator file
	for _, g := range a.SiteGenerators() {
		for _, t := range g.TypescriptTargets() {
			if w, ok := a.WebAppFor(t.Dir); ok {
				siteWebApps[w.Dir] = g.File
			}
		}
	}
	for _, g := range a.SharedGenerators() {
		targets := g.TypescriptTargets()
		if len(targets) == 0 {
			continue
		}
		covered := map[string]bool{}
		for _, t := range targets {
			if w, ok := a.WebAppFor(t.Dir); ok {
				covered[w.Dir] = true
			}
		}
		for _, dir := range sortedKeys(siteWebApps) {
			if !covered[dir] {
				details = append(details, fmt.Sprintf("%s emits no TypeScript into %s (site generator %s writes there)", g.File, dir, siteWebApps[dir]))
			}
		}
	}

	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d multi-site disagreement(s)", len(details)), details...)
	}

	return pass(c.Name(), fmt.Sprintf("%d generators agree (%d site, %d shared)", len(a.Generators), len(a.SiteGenerators()), len(a.SharedGenerators())))
}

func sameSet(x, y []string) bool {
	xs, ys := slices.Clone(x), slices.Clone(y)
	slices.Sort(xs)
	slices.Sort(ys)

	return slices.Equal(xs, ys)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	return keys
}
