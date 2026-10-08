package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"

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
// go get moving the pins in go.mod as the real one would (the tool pin included), so a
// step's rediscovery reads them.
type upgradeExec struct {
	root  string
	calls []string
	dirty string
	fail  string
}

func (f *upgradeExec) Run(_ context.Context, _ string, _ []string, name string, args ...string) ([]byte, error) {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	if f.fail != "" && strings.HasPrefix(line, f.fail) {
		return []byte("refused: " + line + "\n"), errors.New(line)
	}
	switch {
	case line == "git status --porcelain --untracked-files=all":
		return []byte(f.dirty), nil
	case strings.HasPrefix(line, "go get "):
		spec := args[len(args)-1]
		module, version, _ := strings.Cut(spec, "@")
		p := filepath.Join(f.root, "go.mod")
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		var lines []string
		for l := range strings.Lines(string(data)) {
			switch {
			case strings.HasPrefix(strings.TrimSpace(l), module+" "):
				l = "\t" + module + " " + version + "\n"
			case strings.HasPrefix(l, "require "+module+" "):
				l = "require " + module + " " + version + " // indirect\n"
			}
			lines = append(lines, l)
		}

		if err := os.WriteFile(p, []byte(strings.Join(lines, "")), 0o600); err != nil {
			return nil, errors.Wrap(err, "os.WriteFile()")
		}

		return nil, nil
	case line == "go generate ./...":
		return []byte("Warning: Sortie resolves its tenant through MissionId\n"), nil
	}

	return nil, nil
}

// upgradeLedger is three steps: the first the application stands at, the second with the
// marker recipe, the third a pin bump alone.
func upgradeLedger() []ledger.Step {
	return []ledger.Step{
		{Pins: map[string]string{upgradeResource: "v0.1.0", upgradeAccess: "v0.1.0"}, Note: "the first beta"},
		{Pins: map[string]string{upgradeResource: "v0.2.0", upgradeAccess: "v0.1.0"}, Recipes: []ledger.Recipe{markerRecipe{}}, Note: "the marker moves to its new form"},
		{Pins: map[string]string{upgradeResource: "v0.3.0", upgradeAccess: "v0.2.0"}, Note: "no code change is needed"},
	}
}

func passing(context.Context, *check.Env) []check.Result {
	return []check.Result{{Name: "pins", Status: check.Pass, Summary: "ok"}}
}

// The commands a walk runs, by step.
var (
	toolStepCalls = []string{
		"go get -tool github.com/cccteam/ccc/impulse@v0.3.0", "go mod tidy", "go generate ./...", "git add -A",
		"git commit -q -m upgrade: impulse v0.3.0 -m " + toolPinNote,
	}
	markerStepCalls = []string{
		"go get github.com/cccteam/ccc/resource@v0.2.0", "go mod tidy", "go generate ./...", "git add -A",
		"git commit -q -m upgrade: ccc/resource v0.2.0 (recipe marker) -m the marker moves to its new form\n\nRecipes applied by impulse upgrade: marker.",
	}
	lastStepCalls = []string{
		"go get github.com/cccteam/access@v0.2.0", "go get github.com/cccteam/ccc/resource@v0.3.0", "go mod tidy", "go generate ./...", "git add -A",
		"git commit -q -m upgrade: access v0.2.0, ccc/resource v0.3.0 -m no code change is needed",
	}
)

// TestUpgrade walks an application through the ledger: the plan, the dry run, the tool
// pin's move first, each step's recipes, pin bump, owned files, regeneration, check and
// commit in order, a pin already ahead left alone, the recipe idempotent, a checkout build
// leaving the tool pin, a failing check stopping with the brief and the step staged, and
// the refusals (a dirty tree, a tool pin newer than the running impulse, a pin bump go
// refuses).
func TestUpgrade(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		goMod   string
		marker  string
		steps   []ledger.Step
		running check.Build
		dryRun  bool
		dirty   string
		fail    string
		verify  func(context.Context, *check.Env) []check.Result
		// wantOut are fragments of the output in order; wantCalls the commands run (the
		// git status and rev-parse reads left out); wantMarker the marker after the walk;
		// wantBrief whether the handoff brief exists after; wantErr the error.
		wantOut    []string
		wantCalls  []string
		wantMarker string
		wantBrief  bool
		wantErr    string
	}{
		{
			name:    "at the last step and the running impulse: nothing to do",
			goMod:   upgradeGoMod("v0.3.0", "v0.2.0", "v0.3.0"),
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.3.0", FromModule: true},
			wantOut: []string{"beacon stands at step 3 of 3, the ledger's last, and pins impulse v0.3.0, the running impulse: nothing to do."},
		},
		{
			name:    "at the last step with the tool pin behind: the tool pin step alone",
			goMod:   upgradeGoMod("v0.3.0", "v0.2.0", "v0.1.0"),
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.3.0", FromModule: true},
			verify:  passing,
			wantOut: []string{
				"beacon stands at step 3 of 3 and pins impulse v0.1.0; the walk:\n  impulse v0.3.0: " + toolPinNote + "\n\n",
				"=== impulse v0.3.0: " + toolPinNote + " ===\nPin the impulse tool to v0.3.0.",
				"Committed: upgrade: impulse v0.3.0.",
				"beacon stands at step 3 of 3 and pins impulse v0.3.0. Review the commits and open the pull request.",
			},
			wantCalls: toolStepCalls,
		},
		{
			name:    "a dry run prints the plan and changes nothing",
			goMod:   upgradeGoMod("v0.1.0", "v0.1.0", "v0.1.0"),
			marker:  "old\n",
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.3.0", FromModule: true},
			dryRun:  true,
			wantOut: []string{
				"beacon stands at step 1 of 3 and pins impulse v0.1.0; the walk:",
				"  impulse v0.3.0: " + toolPinNote,
				"  step 2, ccc/resource v0.2.0 (recipe marker): the marker moves to its new form",
				"  step 3, access v0.2.0, ccc/resource v0.3.0: no code change is needed",
			},
			wantMarker: "old\n",
		},
		{
			name:    "the walk: the tool pin, then each step's recipes, pins, owned files, generate, check, commit",
			goMod:   upgradeGoMod("v0.1.0", "v0.1.0", "v0.1.0"),
			marker:  "old\n",
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.3.0", FromModule: true},
			verify:  passing,
			wantOut: []string{
				"=== impulse v0.3.0: " + toolPinNote + " ===",
				"Committed: upgrade: impulse v0.3.0.",
				"=== step 2: the marker moves to its new form ===",
				"Recipe marker finds 1 place(s) in the old form:\n  - pkg/marker.txt:1: the marker is in the old form",
				"moved the marker to its new form",
				"github.com/cccteam/access is at v0.1.0 already.\nPin github.com/cccteam/ccc/resource to v0.2.0.",
				"Running go generate ./...\n  Warning: Sortie resolves its tenant through MissionId",
				"Committed: upgrade: ccc/resource v0.2.0 (recipe marker).",
				"=== step 3: no code change is needed ===",
				"Pin github.com/cccteam/access to v0.2.0.\nPin github.com/cccteam/ccc/resource to v0.3.0.",
				"Committed: upgrade: access v0.2.0, ccc/resource v0.3.0.",
				"beacon stands at step 3 of 3 and pins impulse v0.3.0. Review the commits and open the pull request.",
			},
			wantCalls:  concat(toolStepCalls, markerStepCalls, lastStepCalls),
			wantMarker: "new\n",
		},
		{
			name:       "a recipe finds nothing in an application already in the new form",
			goMod:      upgradeGoMod("v0.1.0", "v0.1.0", "v0.3.0"),
			marker:     "new\n",
			steps:      upgradeLedger()[:2],
			running:    check.Build{Version: "v0.3.0", FromModule: true},
			verify:     passing,
			wantOut:    []string{"Recipe marker: nothing in the old form.", "Committed: upgrade: ccc/resource v0.2.0 (recipe marker)."},
			wantCalls:  markerStepCalls,
			wantMarker: "new\n",
		},
		{
			name:    "a pin bumped by hand ahead of the step stays",
			goMod:   upgradeGoMod("v0.4.0", "v0.1.0", "v0.3.0"),
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.3.0", FromModule: true},
			verify:  passing,
			wantOut: []string{
				"beacon stands at step 2 of 3 and pins impulse v0.3.0; the walk:\n  step 3, access v0.2.0, ccc/resource v0.3.0: no code change is needed",
				"Pin github.com/cccteam/access to v0.2.0.\ngithub.com/cccteam/ccc/resource is at v0.4.0 already.",
				"Committed: upgrade: access v0.2.0.",
			},
			wantCalls: []string{
				"go get github.com/cccteam/access@v0.2.0", "go mod tidy", "go generate ./...", "git add -A",
				"git commit -q -m upgrade: access v0.2.0 -m no code change is needed",
			},
		},
		{
			name:    "a checkout build walks the steps and leaves the tool pin",
			goMod:   upgradeGoMod("v0.2.0", "v0.1.0", "v0.1.0"),
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.3.1-0.20261008120000-0702ef849363+dirty"},
			dryRun:  true,
			wantOut: []string{
				"beacon stands at step 2 of 3 and pins impulse v0.1.0; the walk:\n  step 3, access v0.2.0, ccc/resource v0.3.0: no code change is needed\n",
				"This impulse was built from a checkout (v0.3.1-0.20261008120000-0702ef849363+dirty), so the tool pin stays where it is.",
			},
		},
		{
			name:    "a failing check stops the walk with the brief written and the step staged",
			goMod:   upgradeGoMod("v0.1.0", "v0.1.0", "v0.1.0"),
			marker:  "old\n",
			steps:   upgradeLedger(),
			running: check.Build{Version: "(devel)"},
			verify: func(context.Context, *check.Env) []check.Result {
				return []check.Result{{Name: "paging", Status: check.Fail, Summary: "1 offset", Details: []string{"pkg/x.go:3: Offset"}}}
			},
			wantOut:    []string{"=== step 2", "FAIL"},
			wantCalls:  []string{"go get github.com/cccteam/ccc/resource@v0.2.0", "go mod tidy", "go generate ./...", "git add -A"},
			wantMarker: "new\n",
			wantBrief:  true,
			wantErr:    "step 2 (ccc/resource v0.2.0) left the check failing; its changes are staged and the brief is at .impulse-handoff.md. When the check is clean, commit and run impulse upgrade again: it resumes from go.mod",
		},
		{
			name:    "a failing check on the tool pin step",
			goMod:   upgradeGoMod("v0.3.0", "v0.2.0", "v0.1.0"),
			steps:   upgradeLedger(),
			running: check.Build{Version: "v0.3.0", FromModule: true},
			verify: func(context.Context, *check.Env) []check.Result {
				return []check.Result{{Name: "ci-workflow", Status: check.Fail, Summary: "differs"}}
			},
			wantCalls: []string{"go get -tool github.com/cccteam/ccc/impulse@v0.3.0", "go mod tidy", "go generate ./...", "git add -A"},
			wantBrief: true,
			wantErr:   "the impulse v0.3.0 tool pin step left the check failing",
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
			exec := &upgradeExec{root: root, dirty: tt.dirty, fail: tt.fail}
			var out, errOut strings.Builder
			u := &upgrader{
				exec: exec, steps: tt.steps, running: tt.running, verify: tt.verify,
				owned: func(*app.App) ([]string, error) { return nil, nil },
				out:   &out, err: &errOut,
			}
			err := u.run(t.Context(), &transitionFlags{appDir: root, agentCommand: handoff.DefaultCommand}, tt.dryRun)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("run() error = %v, want %q; output:\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("run() error = %v; output:\n%s", err, out.String())
			}
			containsInOrder(t, out.String(), tt.wantOut)
			var calls []string
			for _, c := range exec.calls {
				if strings.HasPrefix(c, "git status") || strings.HasPrefix(c, "git rev-parse") {
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
			if tt.wantMarker != "" {
				data, err := os.ReadFile(filepath.Join(root, markerFile))
				if err != nil {
					t.Fatal(err)
				}
				if string(data) != tt.wantMarker {
					t.Errorf("marker = %q, want %q", data, tt.wantMarker)
				}
			}
			_, err = os.Stat(filepath.Join(root, handoff.File))
			if (err == nil) != tt.wantBrief {
				t.Errorf("brief present = %v, want %v", err == nil, tt.wantBrief)
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
