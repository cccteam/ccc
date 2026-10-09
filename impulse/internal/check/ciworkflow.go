package check

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/cccteam/ccc/impulse/ci"
)

// ciWorkflow compares the committed CI workflows with what impulse renders from the code.
// The files are impulse's: impulse render writes them, the pull request workflow with a
// browser job per workspace, the jobs the //impulse:ci line puts on the larger runner and
// the pins this impulse carries, the cache-filling workflow beside it, and the security
// scan that runs the vulnerability check and the image scan over the default branch and
// the latest release every day; a hand edit, a stale rendering or a workspace without its
// job all read as a difference. A missing file is a failure of its own: without the pull
// request workflow the pull requests run no checks at all, without the cache-filling one
// the checks start cold on every pull request, and without the security scan nothing
// rescans the default branch and the latest release between pull requests. The
// cache-filling workflow runs on the default branch, which it names (main and master, or
// the //impulse:ci line's default-branch), so the check also asks origin which branch
// that is: a default branch the workflow does not name never fills the caches, and no
// error says so; without a remote, or offline, the check says it could not ask.
type ciWorkflow struct{}

func (ciWorkflow) Name() string { return "ci-workflow" }

func (ciWorkflow) Describe() string {
	return "the committed " + ci.List(ci.Files) + " equal what impulse renders from the code (a browser job per workspace, the //impulse:ci line's choices, the pins this impulse carries, the daily security scan)"
}

// renderAdvice is the fix for every difference: the files are rendered, never edited.
const renderAdvice = "run impulse render (go tool impulse render) to rewrite it; the file is impulse's: change the code or impulse, not the file"

func (c ciWorkflow) Run(ctx context.Context, env *Env) Result {
	d, err := ci.Compare(env.App)
	if err != nil {
		return fail(c.Name(), fmt.Sprintf("%s: %v", ci.List(ci.Files), err))
	}
	switch {
	case d == nil:
		return c.defaultBranch(ctx, env)
	case d.Missing && d.File == ci.File:
		return fail(c.Name(), fmt.Sprintf("%s is missing: the pull requests run no checks", d.File), "run impulse render (go tool impulse render) to write it from the code")
	case d.Missing && d.File == ci.ScanFile:
		return fail(c.Name(), fmt.Sprintf("%s is missing: nothing rescans the default branch and the latest release between pull requests", d.File), "run impulse render (go tool impulse render) to write it from the code")
	case d.Missing:
		return fail(c.Name(), fmt.Sprintf("%s is missing: the checks start cold on every pull request", d.File), "run impulse render (go tool impulse render) to write it from the code")
	default:
		return fail(c.Name(), fmt.Sprintf("%s differs from what impulse renders from the code", d.File), d.String(), renderAdvice)
	}
}

// defaultBranch, once the files match, compares the branches the cache-filling workflow
// fills with origin's default branch (git ls-remote --symref origin HEAD): the workflow
// names it, the names it fills include it, or the application's pull requests start cold
// on every run. Without an Execer, a remote or a network the branch cannot be read, and
// the pass says so.
func (c ciWorkflow) defaultBranch(ctx context.Context, env *Env) Result {
	checks := ci.Checks(env.App)
	settings, err := ci.SettingsOf(env.App)
	if err != nil {
		return fail(c.Name(), fmt.Sprintf("%s: %v", ci.List(ci.Files), err))
	}
	fills := ci.List(settings.DefaultBranches)
	summary := fmt.Sprintf("%s match what impulse renders from the code (%d job(s): %s; %s fills the caches on %s; %s scans the default branch and the latest release daily)", ci.List(ci.Files), len(checks), strings.Join(checks, ", "), ci.CacheFile, fills, ci.ScanFile)
	remote, ok := originDefaultBranch(ctx, env)
	switch {
	case !ok:
		return pass(c.Name(), summary+"; origin's default branch could not be read")
	case slices.Contains(settings.DefaultBranches, remote):
		return pass(c.Name(), summary+", and origin's default branch is "+remote)
	case env.App.CI != nil && env.App.CI.DefaultBranch != "":
		return fail(c.Name(), fmt.Sprintf("%s fills the caches on %s, the //impulse:ci line's default-branch (%s:%d), and origin's default branch is %s: the pull requests start cold on every run", ci.CacheFile, fills, env.App.CI.File, env.App.CI.Line, remote),
			fmt.Sprintf("set default-branch=%s on the line, or drop the setting if that is main or master, then run impulse render (go tool impulse render)", remote))
	default:
		return fail(c.Name(), fmt.Sprintf("%s fills the caches on %s, and origin's default branch is %s: the pull requests start cold on every run", ci.CacheFile, fills, remote),
			fmt.Sprintf("declare default-branch=%s on the //impulse:ci line (a comment line in any non-test Go file), then run impulse render (go tool impulse render)", remote))
	}
}

// symrefRE reads the branch out of git ls-remote --symref's first line: ref: refs/heads/<name>\tHEAD.
var symrefRE = regexp.MustCompile(`^ref: refs/heads/(\S+)\s+HEAD`)

// originDefaultBranch asks origin for its default branch; ok is false when there is no
// Execer, no remote, no network, or an answer that is not a branch.
func originDefaultBranch(ctx context.Context, env *Env) (string, bool) {
	if env.Exec == nil {
		return "", false
	}
	out, err := env.Exec.Run(ctx, env.App.Root, nil, "git", "ls-remote", "--symref", "origin", "HEAD")
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if m := symrefRE.FindStringSubmatch(line); m != nil {
			return m[1], true
		}
	}

	return "", false
}
