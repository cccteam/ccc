package check

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/mod/module"
)

// frameworkPrefix is the module path prefix of the Impulse Framework libraries.
const frameworkPrefix = "github.com/cccteam/"

// pins reads the framework pins in go.mod. This release only warns: a pseudo-version or a
// local replace means the application builds against unreleased framework code, which is
// legitimate while a change is in flight and a problem once it is not.
type pins struct{}

func (pins) Name() string { return "pins" }

func (pins) Describe() string {
	return "framework pins in go.mod are released versions"
}

func (c pins) Run(_ context.Context, env *Env) Result {
	mod := env.App.GoMod
	var details []string
	count := 0
	for _, r := range mod.Require {
		if !strings.HasPrefix(r.Mod.Path, frameworkPrefix) {
			continue
		}
		count++
		if module.IsPseudoVersion(r.Mod.Version) {
			details = append(details, fmt.Sprintf("%s is pinned to pseudo-version %s", r.Mod.Path, r.Mod.Version))
		}
	}
	for _, r := range mod.Replace {
		if !strings.HasPrefix(r.Old.Path, frameworkPrefix) {
			continue
		}
		if r.New.Version == "" {
			details = append(details, fmt.Sprintf("%s is replaced by local path %s", r.Old.Path, r.New.Path))
		} else {
			details = append(details, fmt.Sprintf("%s is replaced by %s %s", r.Old.Path, r.New.Path, r.New.Version))
		}
	}

	if count == 0 {
		return skip(c.Name(), "go.mod requires no "+frameworkPrefix+"* module")
	}
	if len(details) > 0 {
		return warn(c.Name(), fmt.Sprintf("%d framework pin(s) point at unreleased code", len(details)), details...)
	}

	return pass(c.Name(), fmt.Sprintf("%d framework pin(s) are released versions", count))
}
