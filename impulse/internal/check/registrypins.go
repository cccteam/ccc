package check

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"
)

// registryPins verifies that every browser app installs its packages from the registry:
// a package.json spec of the form file:.yalc/<package> is a local yalc attachment, a
// development state that never resolves on a clean checkout (the .yalc directory is not
// committed), so a pipeline's install fails and a fresh application cannot be built.
// ccclib.sh attaches and restores that state; the committed file carries the pins.
type registryPins struct{}

func (registryPins) Name() string { return "registry-pins" }

func (registryPins) Describe() string {
	return "the browser apps install published packages, never a yalc attachment"
}

// yalcSpec is the prefix of a spec that names a yalc attachment.
const yalcSpec = "file:.yalc/"

// dependencySections are the package.json sections a spec can appear in.
var dependencySections = []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies", "overrides", "resolutions"}

func (c registryPins) Run(_ context.Context, env *Env) Result {
	a := env.App
	if len(a.WebApps) == 0 {
		return skip(c.Name(), "no browser apps")
	}
	var details []string
	checked := 0
	for _, w := range a.WebApps {
		file := path.Join(w.Dir, "package.json")
		data, err := os.ReadFile(a.Abs(file))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			return fail(c.Name(), fmt.Sprintf("%s: %v", file, err))
		}
		checked++
		var sections map[string]json.RawMessage
		if err := json.Unmarshal(data, &sections); err != nil {
			return fail(c.Name(), fmt.Sprintf("%s: %v", file, err))
		}
		for _, section := range dependencySections {
			raw, ok := sections[section]
			if !ok {
				continue
			}
			var specs map[string]string
			if err := json.Unmarshal(raw, &specs); err != nil {
				continue // a section of another shape (overrides may nest) carries no spec to refuse
			}
			names := make([]string, 0, len(specs))
			for name := range specs {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				if strings.HasPrefix(specs[name], yalcSpec) {
					details = append(details, fmt.Sprintf("%s %s: %s is %s", file, section, name, specs[name]))
				}
			}
		}
		if lock, err := os.ReadFile(a.Abs(path.Join(w.Dir, "bun.lock"))); err == nil && strings.Contains(string(lock), yalcSpec) {
			details = append(details, path.Join(w.Dir, "bun.lock")+": records a yalc attachment; run ccclib.sh restore and commit the lockfile it leaves")
		}
	}
	if checked == 0 {
		return skip(c.Name(), "no browser app has a package.json")
	}
	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d yalc attachment(s) committed; a clean checkout cannot install them (ccclib.sh restore puts the registry pins back)", len(details)), details...)
	}

	return pass(c.Name(), fmt.Sprintf("%d browser app(s) install from the registry", checked))
}
