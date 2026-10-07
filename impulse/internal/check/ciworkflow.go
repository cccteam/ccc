package check

import (
	"context"
	"fmt"
	"strings"

	"github.com/cccteam/ccc/impulse/ci"
)

// ciWorkflow compares the committed CI workflows with what impulse renders from the code.
// The files are impulse's: impulse render writes them, the pull request workflow with a
// browser job per workspace, the jobs the //impulse:ci line puts on the larger runner and
// the pins this impulse carries, and the cache-filling workflow beside it; a hand edit, a
// stale rendering or a workspace without its job all read as a difference. A missing file
// is a failure of its own: without the pull request workflow the pull requests run no
// checks at all, and without the cache-filling one the checks start cold on every pull
// request.
type ciWorkflow struct{}

func (ciWorkflow) Name() string { return "ci-workflow" }

func (ciWorkflow) Describe() string {
	return "the committed " + ci.List(ci.Files) + " equal what impulse renders from the code (a browser job per workspace, the //impulse:ci line's choices, the pins this impulse carries)"
}

// renderAdvice is the fix for every difference: the files are rendered, never edited.
const renderAdvice = "run impulse render (go tool impulse render) to rewrite it; the file is impulse's: change the code or impulse, not the file"

func (c ciWorkflow) Run(_ context.Context, env *Env) Result {
	d, err := ci.Compare(env.App)
	if err != nil {
		return fail(c.Name(), fmt.Sprintf("%s: %v", ci.List(ci.Files), err))
	}
	switch {
	case d == nil:
		checks := ci.Checks(env.App)

		return pass(c.Name(), fmt.Sprintf("%s match what impulse renders from the code (%d job(s): %s)", ci.List(ci.Files), len(checks), strings.Join(checks, ", ")))
	case d.Missing && d.File == ci.File:
		return fail(c.Name(), fmt.Sprintf("%s is missing: the pull requests run no checks", d.File), "run impulse render (go tool impulse render) to write it from the code")
	case d.Missing:
		return fail(c.Name(), fmt.Sprintf("%s is missing: the checks start cold on every pull request", d.File), "run impulse render (go tool impulse render) to write it from the code")
	default:
		return fail(c.Name(), fmt.Sprintf("%s differs from what impulse renders from the code", d.File), d.String(), renderAdvice)
	}
}
