// Package check holds the invariants `impulse check` verifies against an Impulse
// application. Every check compares code with code: nothing here reads a record the tool
// keeps for itself.
package check

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// Status is the outcome of one check.
type Status int

// The check outcomes. Warn and Skip never fail the run.
const (
	Pass Status = iota
	Fail
	Warn
	Skip
)

func (s Status) String() string {
	switch s {
	case Pass:
		return "PASS"
	case Fail:
		return "FAIL"
	case Warn:
		return "WARN"
	case Skip:
		return "SKIP"
	default:
		return "????"
	}
}

// Result is what one check reports.
type Result struct {
	Name    string
	Status  Status
	Summary string
	Details []string
}

func pass(name, summary string) Result { return Result{Name: name, Status: Pass, Summary: summary} }
func skip(name, reason string) Result  { return Result{Name: name, Status: Skip, Summary: reason} }

func fail(name, summary string, details ...string) Result {
	return Result{Name: name, Status: Fail, Summary: summary, Details: details}
}

func warn(name, summary string, details ...string) Result {
	return Result{Name: name, Status: Warn, Summary: summary, Details: details}
}

// Check is one invariant.
type Check interface {
	// Name is the short identifier used on the report and by --only.
	Name() string
	// Describe says in one line what the check verifies.
	Describe() string
	// Run verifies the invariant against the application.
	Run(ctx context.Context, env *Env) Result
}

// Env is what a check runs against.
type Env struct {
	App *app.App
	// Exec runs external commands; tests substitute a fake.
	Exec Execer
	// SkipGenerate skips the checks that regenerate code (they need the Spanner emulator).
	SkipGenerate bool
	// Fix lets checks with a mechanical remedy apply it.
	Fix bool
	// Out receives progress notes such as "running go generate".
	Out io.Writer
}

func (e *Env) notef(format string, args ...any) {
	if e.Out == nil {
		return
	}
	fmt.Fprintf(e.Out, format+"\n", args...)
}

// Execer runs an external command in a directory with extra environment entries and
// returns its combined output.
type Execer interface {
	Run(ctx context.Context, dir string, extraEnv []string, name string, args ...string) ([]byte, error)
}

// OSExec runs commands with os/exec.
type OSExec struct{}

// Run implements Execer.
func (OSExec) Run(ctx context.Context, dir string, extraEnv []string, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, errors.Wrapf(err, "%s %s", name, strings.Join(args, " "))
	}

	return out, nil
}

// All returns every check in run order: the cheap static reads first, the compiler next,
// and regeneration last because it rewrites the working tree.
func All() []Check {
	return []Check{
		generatorProgram{},
		options{},
		tenancyWired{},
		outletWired{},
		sitesWired{},
		authWired{},
		authsWired{},
		skipAuth{},
		emulatorVersion{},
		prettierIgnore{},
		eslintIgnore{},
		packageManager{},
		rpcExecute{},
		multiSite{},
		envTemplate{},
		pins{},
		goworkOff{},
		regen{},
	}
}

// Select returns the checks whose names are in only, in run order, or every check when
// only is empty. Unknown names are an error.
func Select(only []string) ([]Check, error) {
	all := All()
	if len(only) == 0 {
		return all, nil
	}

	byName := make(map[string]Check, len(all))
	for _, c := range all {
		byName[c.Name()] = c
	}
	wanted := make(map[string]bool, len(only))
	for _, n := range only {
		if _, ok := byName[n]; !ok {
			return nil, errors.Newf("unknown check %q", n)
		}
		wanted[n] = true
	}

	var selected []Check
	for _, c := range all {
		if wanted[c.Name()] {
			selected = append(selected, c)
		}
	}

	return selected, nil
}

// Run runs the checks in order and returns their results.
func Run(ctx context.Context, env *Env, checks []Check) []Result {
	results := make([]Result, 0, len(checks))
	for _, c := range checks {
		results = append(results, c.Run(ctx, env))
	}

	return results
}

// Failed reports whether any result failed.
func Failed(results []Result) bool {
	for _, r := range results {
		if r.Status == Fail {
			return true
		}
	}

	return false
}

// Report writes one line per result, with details indented beneath it.
func Report(w io.Writer, results []Result) {
	width := 0
	for _, r := range results {
		width = max(width, len(r.Name))
	}
	for _, r := range results {
		fmt.Fprintf(w, "%s  %-*s  %s\n", r.Status, width, r.Name, r.Summary)
		for _, d := range r.Details {
			fmt.Fprintf(w, "      %s\n", d)
		}
	}
}

// outputLines trims command output into detail lines, keeping the tail when it is long.
func outputLines(out []byte, keep int) []string {
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if len(lines) > keep {
		lines = append([]string{fmt.Sprintf("... %d earlier lines omitted", len(lines)-keep)}, lines[len(lines)-keep:]...)
	}

	return lines
}
