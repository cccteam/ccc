package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/handoff"
	"github.com/cccteam/ccc/impulse/internal/ledger"
	transition_ "github.com/cccteam/ccc/impulse/internal/transition"
)

const (
	upgradeResource = "github.com/cccteam/ccc/resource"
	upgradeAccess   = "github.com/cccteam/access"
	markerFile      = "pkg/marker.txt"
	// cleanReport is what the pinned impulse's check answers when nothing fails.
	cleanReport = "PASS  pins  ok\n"
)

// upgradeGoMod is an application's go.mod with the framework pins and the impulse tool pin
// at the versions given.
func upgradeGoMod(resource, access, impulse string) string {
	return "module example.com/acme/beacon\n\ngo 1.26.6\n\nrequire (\n\t" + upgradeResource + " " + resource + "\n\t" + upgradeAccess + " " + access + "\n)\n\nrequire github.com/cccteam/ccc/impulse " + impulse + " // indirect\n\ntool github.com/cccteam/ccc/impulse\n"
}

// markerRecipe moves the marker file from "old" to "new": the old form is the word old.
type markerRecipe struct{}

func (markerRecipe) Name() string { return "marker" }

func (markerRecipe) Detect(_ context.Context, a *app.App) ([]string, error) {
	data, err := os.ReadFile(a.Abs(markerFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	if strings.TrimSpace(string(data)) != "old" {
		return nil, nil
	}

	return []string{markerFile + ":1: the marker is in the old form"}, nil
}

func (markerRecipe) Apply(_ context.Context, a *app.App, _ check.Execer) (*transition_.Change, error) {
	if err := os.WriteFile(a.Abs(markerFile), []byte("new\n"), 0o600); err != nil {
		return nil, errors.Wrap(err, "os.WriteFile()")
	}

	return &transition_.Change{Did: []string{"moved the marker to its new form"}}, nil
}

func (markerRecipe) Meaning() string { return "The marker names the form the step expects." }

// upgradeExec records the commands of a walk and answers git as a clean repository, with
// go get moving the pins in go.mod as the real one would: a framework pin to the version
// asked, and the tool pin too, carrying the framework pins the impulse at that version
// requires (drags) up to them, as the tool directive does through the build list. At each
// commit it reads where go.mod stands (the step its pins reach and the impulse it pins), so
// a test holds every commit to its step. The pinned impulse's check answers with report,
// and fails as the command would when the report has a failing line, with the failing
// check's exit status; a command named by fail could not run, and exits as such.
type upgradeExec struct {
	root    string
	steps   []ledger.Step
	drags   map[string]map[string]string
	report  string
	calls   []string
	commits []string
	dirty   string
	fail    string
}

// exitStatus is the error a fake command exits non-zero with, carrying the status as the
// error of a process does.
type exitStatus struct {
	code int
	line string
}

func (e exitStatus) Error() string {
	return e.line
}

func (e exitStatus) ExitCode() int {
	return e.code
}

func (f *upgradeExec) Run(_ context.Context, _ string, _ []string, name string, args ...string) ([]byte, error) {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	if f.fail != "" && strings.HasPrefix(line, f.fail) {
		return []byte("refused: " + line + "\n"), exitStatus{code: 2, line: line}
	}
	switch {
	case line == "git status --porcelain --untracked-files=all":
		return []byte(f.dirty), nil
	case line == "git diff --cached --name-only":
		return []byte("go.mod\n"), nil
	case strings.HasPrefix(line, "go get -tool "):
		module, version, _ := strings.Cut(args[len(args)-1], "@")
		if err := f.pin(module, version, false); err != nil {
			return nil, err
		}
		for dragged, v := range f.drags[version] {
			if err := f.pin(dragged, v, true); err != nil {
				return nil, err
			}
		}

		return nil, nil
	case strings.HasPrefix(line, "go get "):
		module, version, _ := strings.Cut(args[len(args)-1], "@")

		return nil, f.pin(module, version, false)
	case line == "go generate ./...":
		return []byte("Warning: Sortie resolves its tenant through MissionId\n"), nil
	case strings.HasPrefix(line, "go tool impulse check"):
		if check.Failed(check.ParseReport([]byte(f.report))) {
			return []byte(f.report), exitStatus{code: failedExit, line: line}
		}

		return []byte(f.report), nil
	case strings.HasPrefix(line, "git commit "):
		return nil, f.record()
	}

	return nil, nil
}

// Stream runs the command as Run does, its output written through as well.
func (f *upgradeExec) Stream(ctx context.Context, dir string, out io.Writer, name string, args ...string) ([]byte, error) {
	held, err := f.Run(ctx, dir, nil, name, args...)
	if _, werr := out.Write(held); werr != nil {
		return held, errors.Wrap(werr, "out.Write()")
	}

	return held, err
}

// pin writes the module's version into go.mod; raiseOnly leaves a pin at or beyond it, as
// the build list does.
func (f *upgradeExec) pin(module, version string, raiseOnly bool) error {
	p := filepath.Join(f.root, "go.mod")
	data, err := os.ReadFile(p)
	if err != nil {
		return errors.Wrap(err, "os.ReadFile()")
	}
	var lines []string
	for l := range strings.Lines(string(data)) {
		fields := strings.Fields(l)
		switch {
		case len(fields) >= 2 && fields[0] == module:
			if !raiseOnly || semver.Compare(fields[1], version) < 0 {
				l = "\t" + module + " " + version + "\n"
			}
		case len(fields) >= 3 && fields[0] == "require" && fields[1] == module:
			if !raiseOnly || semver.Compare(fields[2], version) < 0 {
				l = "require " + module + " " + version + " // indirect\n"
			}
		}
		lines = append(lines, l)
	}
	if err := os.WriteFile(p, []byte(strings.Join(lines, "")), 0o600); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}

	return nil
}

// record notes where go.mod stands at a commit: the step its pins reach and the impulse
// it pins.
func (f *upgradeExec) record() error {
	data, err := os.ReadFile(filepath.Join(f.root, "go.mod"))
	if err != nil {
		return errors.Wrap(err, "os.ReadFile()")
	}
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return errors.Wrap(err, "modfile.Parse()")
	}
	f.commits = append(f.commits, fmt.Sprintf("step %d, impulse %s", ledger.Position(f.steps, ledger.AppPins(mod))+1, goModImpulsePin(mod)))

	return nil
}

// upgradeLedger is three steps: the first the application stands at, the second with the
// marker recipe, added by impulse v0.2.0, the third a pin bump alone, added by v0.3.0.
func upgradeLedger() []ledger.Step {
	return []ledger.Step{
		{Pins: map[string]string{upgradeResource: "v0.1.0", upgradeAccess: "v0.1.0"}, Note: "the first beta"},
		{Pins: map[string]string{upgradeResource: "v0.2.0", upgradeAccess: "v0.1.0"}, Recipes: []ledger.Recipe{markerRecipe{}}, Note: "the marker moves to its new form", Impulse: "v0.2.0"},
		{Pins: map[string]string{upgradeResource: "v0.3.0", upgradeAccess: "v0.2.0"}, Note: "no code change is needed", Impulse: "v0.3.0"},
	}
}

// stagedLedger is upgradeLedger with the marker step added by the first release whose
// handoff takes the staged tree, and the last step by a release after it, so a step with
// a recipe and one without are each handed to the release that added them.
func stagedLedger(t *testing.T) []ledger.Step {
	t.Helper()

	steps := upgradeLedger()
	steps[1].Impulse = handoffStagedSince
	steps[2].Impulse = stagedLater(t)

	return steps
}

// stagedLater is a release after handoffStagedSince, its minor bumped.
func stagedLater(t *testing.T) string {
	t.Helper()

	major, minor, ok := strings.Cut(strings.TrimPrefix(semver.MajorMinor(handoffStagedSince), "v"), ".")
	n, err := strconv.Atoi(minor)
	if !ok || err != nil {
		t.Fatalf("handoffStagedSince = %q: no major.minor to bump", handoffStagedSince)
	}

	return "v" + major + "." + strconv.Itoa(n+1) + ".0"
}

// upgradeDrags is what each impulse release's tool directive puts into an application's
// build list: the resource its own step pins, and for v0.3.1, a patch that added no step,
// the last step's. Moved ahead of the steps, v0.3.1 would carry resource to v0.3.0 while
// access stayed at v0.1.0, and the pins would read as step 2 reached with its recipe never
// run: the shape of the defect the walk's order answers. The staged ledger's releases drag
// their own steps' resource.
func upgradeDrags(t *testing.T) map[string]map[string]string {
	t.Helper()

	return map[string]map[string]string{
		"v0.2.0":           {upgradeResource: "v0.2.0"},
		"v0.3.0":           {upgradeResource: "v0.3.0"},
		"v0.3.1":           {upgradeResource: "v0.3.0"},
		handoffStagedSince: {upgradeResource: "v0.2.0"},
		stagedLater(t):     {upgradeResource: "v0.3.0"},
	}
}

// pseudoLedger is upgradeLedger with the last step's pins at pseudo-versions, which run
// too long for a commit subject.
func pseudoLedger() []ledger.Step {
	steps := upgradeLedger()
	steps[2].Pins = map[string]string{upgradeResource: "v0.3.1-0.20261008035330-fb5dde92d6da", upgradeAccess: "v0.2.1-0.20261008034601-9d76e3c2d0b8"}

	return steps
}

func passing(context.Context, *check.Env) []check.Result {
	return []check.Result{{Name: "pins", Status: check.Pass, Summary: "ok"}}
}

// The commands a walk runs, by step: a step moves its framework pins, then the tool pin
// to the release that added it, and the pinned impulse renders and checks it; the
// tool-only move to the running release comes last and is checked in process.
var (
	markerStepCalls = []string{
		"go get github.com/cccteam/ccc/resource@v0.2.0", "go get -tool github.com/cccteam/ccc/impulse@v0.2.0", "go mod tidy",
		"go tool impulse render", "go generate ./...", "go tool impulse check --fix", "git add -A",
		"git commit -q -m upgrade: ccc/resource v0.2.0, ccc/impulse v0.2.0 (recipe marker) -m the marker moves to its new form\n\nRecipes applied by impulse upgrade: marker.",
	}
	lastStepCalls = []string{
		"go get github.com/cccteam/access@v0.2.0", "go get github.com/cccteam/ccc/resource@v0.3.0", "go get -tool github.com/cccteam/ccc/impulse@v0.3.0", "go mod tidy",
		"go tool impulse render", "go generate ./...", "go tool impulse check --fix", "git add -A",
		"git commit -q -m upgrade: access v0.2.0, ccc/resource v0.3.0, ccc/impulse v0.3.0 -m no code change is needed",
	}
	toolMoveCalls = []string{
		"go get -tool github.com/cccteam/ccc/impulse@v0.3.1", "go mod tidy", "go generate ./...", "git add -A",
		"git commit -q -m upgrade: impulse v0.3.1 -m " + toolPinNote,
	}
)

// TestUpgrade walks an application through the ledger: the plan, the dry run, each step's
// recipes, pin bump, tool pin, owned files, regeneration, check and commit in order with
// the tool-only move last, every commit at exactly its step, a step the running release
// added checked in process, a pin already ahead left alone, the recipe idempotent, a
// checkout build making no tool-only move, a failing check stopping with the brief and
// the step staged (by the pinned release through handoff --staged, with the agent when
// asked, from the release that has the flag; from the pinned check's report before it),
// and the refusals (a dirty tree, a tool pin newer than the running impulse, a pin bump go
// refuses, a pinned render, check or handoff that could not run).
func TestUpgrade(t *testing.T) {
	t.Parallel()

	failingReport := "FAIL  paging  1 offset\n      pkg/x.go:3: Offset\n"
	markerChange := "`impulse upgrade (recipe marker, step 2)` made these changes and staged them:\n\n- moved the marker to its new form\n"
	tests := []struct {
		name         string
		goMod        string
		marker       string
		steps        []ledger.Step
		running      check.Build
		dryRun       bool
		skipGenerate bool
		agent        bool
		agentArgs    []string
		dirty        string
		fail         string
		report       string
		verify       func(context.Context, *check.Env) []check.Result
		// wantOut are fragments of the output in order, wantNoOut fragments it must not
		// hold; wantCalls the commands run (the git status, rev-parse and diff --cached
		// reads left out); wantCommits where go.mod stood at each commit; wantMarker the
		// marker after the walk; wantBrief whether the handoff brief exists after,
		// wantInBrief a fragment of it; wantErr the error.
		wantOut     []string
		wantNoOut   []string
		wantCalls   []string
		wantCommits []string
		wantMarker  string
		wantBrief   bool
		wantInBrief string
		wantErr     string
	}{
		{
			name:    "at the last step and the running impulse: nothing to do",
			goMod:   upgradeGoMod("v0.3.0", "v0.2.0", "v0.3.0"),
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.3.0", FromModule: true},
			wantOut: []string{"beacon stands at step 3 of 3, the ledger's last, and pins impulse v0.3.0, the running impulse: nothing to do."},
		},
		{
			name:    "at the last step with the tool pin behind: the tool-only move alone",
			goMod:   upgradeGoMod("v0.3.0", "v0.2.0", "v0.1.0"),
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.3.1", FromModule: true},
			verify:  passing,
			wantOut: []string{
				"beacon stands at step 3 of 3 and pins impulse v0.1.0; the walk:\n  impulse v0.3.1: " + toolPinNote + "\n\n",
				"=== impulse v0.3.1: " + toolPinNote + " ===\nPin the impulse tool to v0.3.1.",
				"Committed: upgrade: impulse v0.3.1.",
				"beacon stands at step 3 of 3 and pins impulse v0.3.1. Review the commits and open the pull request.",
			},
			wantCalls:   toolMoveCalls,
			wantCommits: []string{"step 3, impulse v0.3.1"},
		},
		{
			name:    "a dry run prints the plan, the steps first and the tool-only move last, and changes nothing",
			goMod:   upgradeGoMod("v0.1.0", "v0.1.0", "v0.1.0"),
			marker:  "old\n",
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.3.1", FromModule: true},
			dryRun:  true,
			wantOut: []string{
				"beacon stands at step 1 of 3 and pins impulse v0.1.0; the walk:",
				"  step 2, ccc/resource v0.2.0 (recipe marker), pins ccc/impulse v0.2.0: the marker moves to its new form",
				"  step 3, access v0.2.0, ccc/resource v0.3.0, pins ccc/impulse v0.3.0: no code change is needed",
				"  impulse v0.3.1: " + toolPinNote,
			},
			wantMarker: "old\n",
		},
		{
			name:    "the walk: each step's recipes, pins, tool pin, owned files, generate and check by its release, commit; the tool-only move last; every commit at its step",
			goMod:   upgradeGoMod("v0.1.0", "v0.1.0", "v0.1.0"),
			marker:  "old\n",
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.3.1", FromModule: true},
			verify:  passing,
			wantOut: []string{
				"=== step 2: the marker moves to its new form ===",
				"Recipe marker finds 1 place(s) in the old form:\n  - pkg/marker.txt:1: the marker is in the old form",
				"moved the marker to its new form",
				"github.com/cccteam/access is at v0.1.0 already.\nPin github.com/cccteam/ccc/resource to v0.2.0.\nPin the impulse tool to v0.2.0.",
				"The pinned impulse v0.2.0 renders the owned files and checks the step.",
				"Running go generate ./...\n  Warning: Sortie resolves its tenant through MissionId",
				"PASS  pins  ok",
				"Committed: upgrade: ccc/resource v0.2.0, ccc/impulse v0.2.0 (recipe marker).",
				"=== step 3: no code change is needed ===",
				"Pin github.com/cccteam/access to v0.2.0.\nPin github.com/cccteam/ccc/resource to v0.3.0.\nPin the impulse tool to v0.3.0.",
				"The pinned impulse v0.3.0 renders the owned files and checks the step.",
				"Committed: upgrade: access v0.2.0, ccc/resource v0.3.0, ccc/impulse v0.3.0.",
				"=== impulse v0.3.1: " + toolPinNote + " ===\nPin the impulse tool to v0.3.1.",
				"Committed: upgrade: impulse v0.3.1.",
				"beacon stands at step 3 of 3 and pins impulse v0.3.1. Review the commits and open the pull request.",
			},
			wantCalls:   concat(markerStepCalls, lastStepCalls, toolMoveCalls),
			wantCommits: []string{"step 2, impulse v0.2.0", "step 3, impulse v0.3.0", "step 3, impulse v0.3.1"},
			wantMarker:  "new\n",
		},
		{
			name:    "the step the running release added is checked in process",
			goMod:   upgradeGoMod("v0.1.0", "v0.1.0", "v0.1.0"),
			marker:  "old\n",
			steps:   upgradeLedger()[:2],
			running: check.Build{Version: "v0.2.0", FromModule: true},
			verify:  passing,
			wantOut: []string{
				"Pin github.com/cccteam/ccc/resource to v0.2.0.\nPin the impulse tool to v0.2.0.\nRunning go generate ./...",
				"Committed: upgrade: ccc/resource v0.2.0, ccc/impulse v0.2.0 (recipe marker).",
				"beacon stands at step 2 of 2 and pins impulse v0.2.0. Review the commits and open the pull request.",
			},
			wantCalls: []string{
				"go get github.com/cccteam/ccc/resource@v0.2.0", "go get -tool github.com/cccteam/ccc/impulse@v0.2.0", "go mod tidy", "go generate ./...", "git add -A",
				"git commit -q -m upgrade: ccc/resource v0.2.0, ccc/impulse v0.2.0 (recipe marker) -m the marker moves to its new form\n\nRecipes applied by impulse upgrade: marker.",
			},
			wantCommits: []string{"step 2, impulse v0.2.0"},
			wantMarker:  "new\n",
		},
		{
			name:       "a recipe finds nothing in an application already in the new form; a tool pin ahead of the step stays",
			goMod:      upgradeGoMod("v0.1.0", "v0.1.0", "v0.3.0"),
			marker:     "new\n",
			steps:      upgradeLedger()[:2],
			running:    check.Build{Version: "v0.3.0", FromModule: true},
			verify:     passing,
			wantOut:    []string{"Recipe marker: nothing in the old form.", "github.com/cccteam/ccc/impulse is at v0.3.0 already.", "Committed: upgrade: ccc/resource v0.2.0 (recipe marker)."},
			wantCalls:  []string{"go get github.com/cccteam/ccc/resource@v0.2.0", "go mod tidy", "go generate ./...", "git add -A", "git commit -q -m upgrade: ccc/resource v0.2.0 (recipe marker) -m the marker moves to its new form\n\nRecipes applied by impulse upgrade: marker."},
			wantMarker: "new\n",
		},
		{
			name:    "a pin bumped by hand ahead of the step stays",
			goMod:   upgradeGoMod("v0.4.0", "v0.1.0", "v0.3.0"),
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.3.0", FromModule: true},
			verify:  passing,
			wantOut: []string{
				"beacon stands at step 2 of 3 and pins impulse v0.3.0; the walk:\n  step 3, access v0.2.0, ccc/resource v0.3.0, pins ccc/impulse v0.3.0: no code change is needed",
				"Pin github.com/cccteam/access to v0.2.0.\ngithub.com/cccteam/ccc/resource is at v0.4.0 already.\ngithub.com/cccteam/ccc/impulse is at v0.3.0 already.",
				"Committed: upgrade: access v0.2.0.",
			},
			wantCalls: []string{
				"go get github.com/cccteam/access@v0.2.0", "go mod tidy", "go generate ./...", "git add -A",
				"git commit -q -m upgrade: access v0.2.0 -m no code change is needed",
			},
		},
		{
			name:    "pseudo-version pins name the modules alone in the subject; the versions go in the body",
			goMod:   upgradeGoMod("v0.2.0", "v0.1.0", "v0.3.0"),
			steps:   pseudoLedger(),
			running: check.Build{Version: "v0.3.0", FromModule: true},
			verify:  passing,
			wantOut: []string{"Committed: upgrade: access, ccc/resource."},
			wantCalls: []string{
				"go get github.com/cccteam/access@v0.2.1-0.20261008034601-9d76e3c2d0b8", "go get github.com/cccteam/ccc/resource@v0.3.1-0.20261008035330-fb5dde92d6da", "go mod tidy", "go generate ./...", "git add -A",
				"git commit -q -m upgrade: access, ccc/resource -m no code change is needed\n\nPins moved: access v0.2.1-0.20261008034601-9d76e3c2d0b8, ccc/resource v0.3.1-0.20261008035330-fb5dde92d6da.",
			},
		},
		{
			name:         "the walk's --skip-generate reaches the pinned check, and no generate runs",
			goMod:        upgradeGoMod("v0.2.0", "v0.1.0", "v0.2.0"),
			steps:        upgradeLedger(),
			running:      check.Build{Version: "v0.3.1", FromModule: true},
			skipGenerate: true,
			verify:       passing,
			wantCalls: []string{
				"go get github.com/cccteam/access@v0.2.0", "go get github.com/cccteam/ccc/resource@v0.3.0", "go get -tool github.com/cccteam/ccc/impulse@v0.3.0", "go mod tidy",
				"go tool impulse render", "go tool impulse check --fix --skip-generate", "git add -A",
				"git commit -q -m upgrade: access v0.2.0, ccc/resource v0.3.0, ccc/impulse v0.3.0 -m no code change is needed",
				"go get -tool github.com/cccteam/ccc/impulse@v0.3.1", "go mod tidy", "git add -A",
				"git commit -q -m upgrade: impulse v0.3.1 -m " + toolPinNote,
			},
			wantCommits: []string{"step 3, impulse v0.3.0", "step 3, impulse v0.3.1"},
		},
		{
			name:    "a checkout build walks the steps, each pinning its release, and makes no tool-only move",
			goMod:   upgradeGoMod("v0.2.0", "v0.1.0", "v0.1.0"),
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.3.1-0.20261008120000-0702ef849363+dirty"},
			dryRun:  true,
			wantOut: []string{
				"beacon stands at step 2 of 3 and pins impulse v0.1.0; the walk:\n  step 3, access v0.2.0, ccc/resource v0.3.0, pins ccc/impulse v0.3.0: no code change is needed\n",
				"This impulse was built from a checkout (v0.3.1-0.20261008120000-0702ef849363+dirty), so the tool-only move does not apply: the pin ends where the last step leaves it.",
			},
		},
		{
			name:    "a failing check by a pinned release before handoff --staged stops the walk with the brief written from its report and the step staged; the agent is left to the user",
			goMod:   upgradeGoMod("v0.1.0", "v0.1.0", "v0.1.0"),
			marker:  "old\n",
			steps:   upgradeLedger(),
			running: check.Build{Version: "(devel)"},
			agent:   true,
			report:  failingReport,
			wantOut: []string{
				"=== step 2",
				"The pinned impulse v0.2.0 renders the owned files and checks the step.",
				"FAIL  paging  1 offset\n      pkg/x.go:3: Offset",
				"The agent is not launched at a step checked by the pinned impulse v0.2.0, a release before " + handoffStagedSince + "; run it on the brief by hand.",
				"Wrote the brief to .impulse-handoff.md: 1 obligation(s) under 1 failing check(s).",
			},
			wantCalls:   []string{"go get github.com/cccteam/ccc/resource@v0.2.0", "go get -tool github.com/cccteam/ccc/impulse@v0.2.0", "go mod tidy", "go tool impulse render", "go generate ./...", "go tool impulse check --fix", "git add -A"},
			wantMarker:  "new\n",
			wantBrief:   true,
			wantInBrief: "## What changed\n\n" + markerChange + "\nThe change under review is staged in the index, 1 path(s):\n\n- go.mod\n\n## What it means\n\nThe marker names the form the step expects.\n\n## The failing checks\n\nThis is the output of `impulse check`, failing checks only. Each line under a check is one obligation.\n\n```\nFAIL  paging  1 offset\n      pkg/x.go:3: Offset\n```",
			wantErr:     "step 2 (ccc/resource v0.2.0, ccc/impulse v0.2.0) left the check failing; its changes are staged and the brief is at .impulse-handoff.md. When the check is clean, commit and run impulse upgrade again: it resumes from go.mod",
		},
		{
			name:      "a failing check by a pinned release with handoff --staged: the staged step is handed to it with the recipe's change and meaning, and the agent when asked; no brief is written here",
			goMod:     upgradeGoMod("v0.1.0", "v0.1.0", "v0.1.0"),
			marker:    "old\n",
			steps:     stagedLedger(t),
			running:   check.Build{Version: "(devel)"},
			agent:     true,
			agentArgs: []string{"--model", "sonnet"},
			report:    failingReport,
			wantOut: []string{
				"=== step 2",
				"The pinned impulse " + handoffStagedSince + " renders the owned files and checks the step.",
				"FAIL  paging  1 offset\n      pkg/x.go:3: Offset",
				"The pinned impulse " + handoffStagedSince + " writes the brief, runs the agent and verifies its work, through go tool impulse handoff --staged.",
			},
			wantNoOut: []string{"The agent is not launched", "Wrote the brief"},
			wantCalls: []string{
				"go get github.com/cccteam/ccc/resource@v0.2.0", "go get -tool github.com/cccteam/ccc/impulse@" + handoffStagedSince, "go mod tidy", "go tool impulse render", "go generate ./...", "go tool impulse check --fix", "git add -A",
				"go tool impulse handoff --staged --change " + markerChange + " --meaning The marker names the form the step expects. --agent --agent-command claude --agent-arg --model --agent-arg sonnet",
			},
			wantMarker: "new\n",
			wantErr:    "step 2 (ccc/resource v0.2.0, ccc/impulse " + handoffStagedSince + ") left the check failing; its changes are staged and the brief is at .impulse-handoff.md. When the check is clean, commit and run impulse upgrade again: it resumes from go.mod",
		},
		{
			name:         "a failing check at a pin bump by a pinned release with handoff --staged, without the agent; the walk's --skip-generate reaches the handoff",
			goMod:        upgradeGoMod("v0.2.0", "v0.1.0", handoffStagedSince),
			steps:        stagedLedger(t),
			running:      check.Build{Version: "(devel)"},
			skipGenerate: true,
			report:       failingReport,
			wantOut:      []string{"The pinned impulse " + stagedLater(t) + " writes the brief, through go tool impulse handoff --staged."},
			wantCalls: []string{
				"go get github.com/cccteam/access@v0.2.0", "go get github.com/cccteam/ccc/resource@v0.3.0", "go get -tool github.com/cccteam/ccc/impulse@" + stagedLater(t), "go mod tidy",
				"go tool impulse render", "go tool impulse check --fix --skip-generate", "git add -A",
				"go tool impulse handoff --staged --skip-generate",
			},
			wantErr: "step 3 (access v0.2.0, ccc/resource v0.3.0, ccc/impulse " + stagedLater(t) + ") left the check failing; its changes are staged and the brief is at .impulse-handoff.md",
		},
		{
			name:    "a failing check on the tool-only move; the brief lists the staged paths",
			goMod:   upgradeGoMod("v0.3.0", "v0.2.0", "v0.1.0"),
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.3.0", FromModule: true},
			verify: func(context.Context, *check.Env) []check.Result {
				return []check.Result{{Name: "ci-workflow", Status: check.Fail, Summary: "differs"}}
			},
			wantCalls:   []string{"go get -tool github.com/cccteam/ccc/impulse@v0.3.0", "go mod tidy", "go generate ./...", "git add -A"},
			wantBrief:   true,
			wantInBrief: "## What changed\n\nThe change under review is staged in the index, 1 path(s):\n\n- go.mod\n\n## The failing checks",
			wantErr:     "the impulse v0.3.0 tool pin step left the check failing",
		},
		{
			name:    "a dirty tree is refused",
			goMod:   upgradeGoMod("v0.1.0", "v0.1.0", "v0.1.0"),
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.3.0", FromModule: true},
			dirty:   " M pkg/x.go\n",
			wantErr: "the working tree is not clean (1 path(s)): commit or stash first, so each step is one commit",
		},
		{
			name:    "a tool pin newer than the running impulse is refused",
			goMod:   upgradeGoMod("v0.1.0", "v0.1.0", "v0.3.0"),
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.2.0", FromModule: true},
			wantErr: "go.mod pins impulse at v0.3.0, newer than the running v0.2.0: run the pinned one (go tool impulse upgrade)",
		},
		{
			name:      "a pin bump go refuses stops the step",
			goMod:     upgradeGoMod("v0.1.0", "v0.1.0", "v0.1.0"),
			marker:    "new\n",
			steps:     upgradeLedger(),
			running:   check.Build{Version: "(devel)"},
			fail:      "go get github.com/cccteam/ccc/resource@v0.2.0",
			verify:    passing,
			wantCalls: []string{"go get github.com/cccteam/ccc/resource@v0.2.0"},
			wantErr:   "go get github.com/cccteam/ccc/resource@v0.2.0: refused: go get github.com/cccteam/ccc/resource@v0.2.0",
		},
		{
			name:      "a pinned render that could not run stops the step",
			goMod:     upgradeGoMod("v0.1.0", "v0.1.0", "v0.1.0"),
			marker:    "new\n",
			steps:     upgradeLedger(),
			running:   check.Build{Version: "(devel)"},
			fail:      "go tool impulse render",
			wantCalls: []string{"go get github.com/cccteam/ccc/resource@v0.2.0", "go get -tool github.com/cccteam/ccc/impulse@v0.2.0", "go mod tidy", "go tool impulse render"},
			wantErr:   "go tool impulse render: refused: go tool impulse render",
		},
		{
			name:      "a pinned check that could not run stops the step without a brief",
			goMod:     upgradeGoMod("v0.1.0", "v0.1.0", "v0.1.0"),
			marker:    "new\n",
			steps:     upgradeLedger(),
			running:   check.Build{Version: "(devel)"},
			fail:      "go tool impulse check",
			wantCalls: []string{"go get github.com/cccteam/ccc/resource@v0.2.0", "go get -tool github.com/cccteam/ccc/impulse@v0.2.0", "go mod tidy", "go tool impulse render", "go generate ./...", "go tool impulse check --fix"},
			wantErr:   "go tool impulse check: refused: go tool impulse check --fix",
		},
		{
			name:      "a pinned check that could not run, by a release with handoff --staged, stops the step before the handoff: its exit status is not a failing check's",
			goMod:     upgradeGoMod("v0.1.0", "v0.1.0", "v0.1.0"),
			marker:    "new\n",
			steps:     stagedLedger(t),
			running:   check.Build{Version: "(devel)"},
			fail:      "go tool impulse check",
			wantCalls: []string{"go get github.com/cccteam/ccc/resource@v0.2.0", "go get -tool github.com/cccteam/ccc/impulse@" + handoffStagedSince, "go mod tidy", "go tool impulse render", "go generate ./...", "go tool impulse check --fix"},
			wantErr:   "go tool impulse check: refused: go tool impulse check --fix",
		},
		{
			name:    "a pinned handoff that could not run stops the step",
			goMod:   upgradeGoMod("v0.1.0", "v0.1.0", "v0.1.0"),
			marker:  "new\n",
			steps:   stagedLedger(t),
			running: check.Build{Version: "(devel)"},
			fail:    "go tool impulse handoff",
			report:  failingReport,
			wantCalls: []string{
				"go get github.com/cccteam/ccc/resource@v0.2.0", "go get -tool github.com/cccteam/ccc/impulse@" + handoffStagedSince, "go mod tidy", "go tool impulse render", "go generate ./...", "go tool impulse check --fix", "git add -A",
				"go tool impulse handoff --staged",
			},
			wantErr: "go tool impulse handoff: refused: go tool impulse handoff --staged",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			files := map[string]string{"go.mod": tt.goMod}
			if tt.marker != "" {
				files[markerFile] = tt.marker
			}
			for rel, content := range files {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			report := tt.report
			if report == "" {
				report = cleanReport
			}
			exec := &upgradeExec{root: root, steps: tt.steps, drags: upgradeDrags(t), report: report, dirty: tt.dirty, fail: tt.fail}
			var out, errOut strings.Builder
			u := &upgrader{
				exec: exec, steps: tt.steps, running: tt.running, verify: tt.verify,
				owned: func(*app.App) ([]string, error) { return nil, nil },
				out:   &out, err: &errOut,
			}
			err := u.run(t.Context(), &transitionFlags{appDir: root, agent: tt.agent, agentCommand: handoff.DefaultCommand, agentArgs: tt.agentArgs, skipGenerate: tt.skipGenerate}, tt.dryRun)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("run() error = %v, want %q; output:\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("run() error = %v; output:\n%s", err, out.String())
			}
			containsInOrder(t, out.String(), tt.wantOut)
			for _, fragment := range tt.wantNoOut {
				if strings.Contains(out.String(), fragment) {
					t.Errorf("output holds %q; output:\n%s", fragment, out.String())
				}
			}
			var calls []string
			for _, c := range exec.calls {
				if strings.HasPrefix(c, "git status") || strings.HasPrefix(c, "git rev-parse") || strings.HasPrefix(c, "git diff --cached") {
					continue
				}
				calls = append(calls, c)
			}
			if tt.wantCalls != nil {
				if diff := cmp.Diff(tt.wantCalls, calls); diff != "" {
					t.Errorf("commands mismatch (-want +got):\n%s", diff)
				}
			} else if len(calls) > 0 {
				t.Errorf("commands run = %v, want none", calls)
			}
			if tt.wantCommits != nil {
				if diff := cmp.Diff(tt.wantCommits, exec.commits); diff != "" {
					t.Errorf("commits mismatch (-want +got):\n%s", diff)
				}
			}
			if tt.wantMarker != "" {
				data, err := os.ReadFile(filepath.Join(root, markerFile))
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != tt.wantMarker {
					t.Errorf("marker = %q, want %q", data, tt.wantMarker)
				}
			}
			brief, err := os.ReadFile(filepath.Join(root, handoff.File))
			if (err == nil) != tt.wantBrief {
				t.Errorf("brief present = %v, want %v", err == nil, tt.wantBrief)
			}
			if tt.wantInBrief != "" && !strings.Contains(string(brief), tt.wantInBrief) {
				t.Errorf("brief lacks %q:\n%s", tt.wantInBrief, brief)
			}
		})
	}
}

// TestHandoffStaged holds handoffStagedSince to a release, and reads which pinned
// impulse writes its own brief: one at or after it; one before it, or a commit between
// two releases, is left to the walk's report path.
func TestHandoffStaged(t *testing.T) {
	t.Parallel()

	if !ledger.IsRelease(handoffStagedSince) {
		t.Fatalf("handoffStagedSince = %q, not a release", handoffStagedSince)
	}
	tests := []struct {
		name   string
		pinned string
		want   bool
	}{
		{name: "the release that added the flag", pinned: handoffStagedSince, want: true},
		{name: "a later release", pinned: stagedLater(t), want: true},
		{name: "the release before it", pinned: "v0.3.1", want: false},
		{name: "a commit between, as a pseudo-version", pinned: semver.Canonical(handoffStagedSince) + "-0.20261010000000-abcdef123456", want: false},
		{name: "no pin", pinned: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := handoffStaged(tt.pinned); got != tt.want {
				t.Errorf("handoffStaged(%q) = %v, want %v", tt.pinned, got, tt.want)
			}
		})
	}
}

// TestToolPinDrag reads the position the tool directive's requirements leave an
// application at: the running release moved ahead of the steps carries the pins to a step
// the application never walked, which is why the walk moves the pin inside each step,
// where its release's requirements stay within the step's set.
func TestToolPinDrag(t *testing.T) {
	t.Parallel()

	steps := upgradeLedger()
	tests := []struct {
		name    string
		from    map[string]string
		release string
		want    int
	}{
		{name: "v0.3.1 ahead of the steps reads step 2 as reached, its recipe never run", from: steps[0].Pins, release: "v0.3.1", want: 1},
		{name: "v0.2.0 at step 2 reads step 2", from: steps[1].Pins, release: "v0.2.0", want: 1},
		{name: "v0.3.0 at step 3 reads step 3", from: steps[2].Pins, release: "v0.3.0", want: 2},
		{name: "v0.3.1 after the last step stays at it", from: steps[2].Pins, release: "v0.3.1", want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pins := map[string]string{}
			for module, version := range tt.from {
				pins[module] = version
			}
			for module, version := range upgradeDrags(t)[tt.release] {
				if have, ok := pins[module]; !ok || semver.Compare(have, version) < 0 {
					pins[module] = version
				}
			}
			if got := ledger.Position(steps, pins); got != tt.want {
				t.Errorf("Position() = %d, want %d", got, tt.want)
			}
		})
	}
}

// concat joins command lists.
func concat(lists ...[]string) []string {
	var all []string
	for _, l := range lists {
		all = append(all, l...)
	}

	return all
}

// containsInOrder fails unless each fragment appears in the text after the one before it.
func containsInOrder(t *testing.T, text string, fragments []string) {
	t.Helper()

	rest := text
	for _, f := range fragments {
		i := strings.Index(rest, f)
		if i < 0 {
			t.Errorf("output lacks %q (in order); output:\n%s", f, text)

			return
		}
		rest = rest[i+len(f):]
	}
}
