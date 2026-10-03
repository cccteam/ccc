package check

import (
	"context"
	"fmt"
	"strings"

	"github.com/cccteam/ccc/impulse/ci"
)

// ciWorkflow compares the committed CI workflow with what impulse renders from the code.
// The file is impulse's: impulse render writes it, with a browser job per workspace and
// the pins this impulse carries, and a hand edit, a stale rendering or a workspace without
// its job all read as a difference. A missing file is a failure of its own: the pull
// requests run no checks at all.
type ciWorkflow struct{}

func (ciWorkflow) Name() string { return "ci-workflow" }

func (ciWorkflow) Describe() string {
	return "the committed " + ci.File + " equals what impulse renders from the code (a browser job per workspace, the pins this impulse carries)"
}

// renderAdvice is the fix for every difference: the file is rendered, never edited.
const renderAdvice = "run impulse render (go tool impulse render) to rewrite it; the file is impulse's: change the code or impulse, not the file"

func (c ciWorkflow) Run(_ context.Context, env *Env) Result {
	d, err := ci.Compare(env.App)
	if err != nil {
		return fail(c.Name(), fmt.Sprintf("%s: %v", ci.File, err))
	}
	switch {
	case d == nil:
		checks := ci.Checks(env.App)

		return pass(c.Name(), fmt.Sprintf("%s matches what impulse renders from the code (%d job(s): %s)", ci.File, len(checks), strings.Join(checks, ", ")))
	case d.Missing:
		return fail(c.Name(), fmt.Sprintf("%s is missing: the pull requests run no checks", ci.File), "run impulse render (go tool impulse render) to write it from the code")
	default:
		return fail(c.Name(), fmt.Sprintf("%s differs from what impulse renders from the code", ci.File), d.String(), renderAdvice)
	}
}
