package ledger

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/mod/modfile"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
	"github.com/cccteam/ccc/impulse/internal/transition"
)

const (
	resourceModule = "github.com/cccteam/ccc/resource"
	accessModule   = "github.com/cccteam/access"
)

// noopRecipe is a recipe with a name and nothing to do, for the ledger's shape tests.
type noopRecipe struct{ name string }

func (r noopRecipe) Name() string { return r.name }

func (noopRecipe) Detect(context.Context, *app.App) ([]string, error) { return nil, nil }

func (noopRecipe) Apply(context.Context, *app.App, check.Execer) (*transition.Change, error) {
	return &transition.Change{}, nil
}

func (noopRecipe) Meaning() string { return "" }

// testLedger is three steps: the first pins both modules at 0.1.0, the second moves
// resource with a recipe, the third moves both with none.
func testLedger() []Step {
	return []Step{
		{Pins: map[string]string{resourceModule: "v0.1.0", accessModule: "v0.1.0"}, Note: "the first beta"},
		{Pins: map[string]string{resourceModule: "v0.2.0", accessModule: "v0.1.0"}, Recipes: []Recipe{noopRecipe{name: "paging"}}, Note: "pages by cursor"},
		{Pins: map[string]string{resourceModule: "v0.3.0", accessModule: "v0.2.0"}, Note: "no code change is needed"},
	}
}

// TestPosition reads where pins stand: before every step, at one, beyond the last, and a
// pin bumped ahead of the others counting for the steps it reaches alone.
func TestPosition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pins map[string]string
		want int
	}{
		{name: "no framework pin", pins: map[string]string{}, want: -1},
		{name: "before the first step", pins: map[string]string{resourceModule: "v0.0.9", accessModule: "v0.1.0"}, want: -1},
		{name: "at the first", pins: map[string]string{resourceModule: "v0.1.0", accessModule: "v0.1.0"}, want: 0},
		{name: "resource ahead alone reaches the second", pins: map[string]string{resourceModule: "v0.2.5", accessModule: "v0.1.0"}, want: 1},
		{name: "a pseudo-version past the third", pins: map[string]string{resourceModule: "v0.3.1-0.20261005064211-ce3bc37a7307", accessModule: "v0.2.0"}, want: 2},
		{name: "beyond the last", pins: map[string]string{resourceModule: "v1.0.0", accessModule: "v1.0.0"}, want: 2},
		{name: "one module missing", pins: map[string]string{resourceModule: "v0.3.0"}, want: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Position(testLedger(), tt.pins); got != tt.want {
				t.Errorf("Position() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestPending lists the steps to walk: every later one, none when the application is at
// the last.
func TestPending(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pins map[string]string
		want []string
	}{
		{name: "before the first: all three", pins: map[string]string{}, want: []string{"access v0.1.0, ccc/resource v0.1.0", "ccc/resource v0.2.0", "access v0.2.0, ccc/resource v0.3.0"}},
		{name: "at the first: the two after", pins: map[string]string{resourceModule: "v0.1.0", accessModule: "v0.1.0"}, want: []string{"ccc/resource v0.2.0", "access v0.2.0, ccc/resource v0.3.0"}},
		{name: "at the last: none", pins: map[string]string{resourceModule: "v0.3.0", accessModule: "v0.2.0"}, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got []string
			pending := Pending(testLedger(), tt.pins)
			prev := map[string]string{}
			if n := len(testLedger()) - len(pending); n > 0 {
				prev = testLedger()[n-1].Pins
			}
			for i := range pending {
				got = append(got, strings.Join(pending[i].Moves(prev), ", "))
				prev = pending[i].Pins
			}
			if diff := cmp.Diff(tt.want, got, cmp.Transformer("nil", func(s []string) []string {
				if s == nil {
					return []string{}
				}

				return s
			})); diff != "" {
				t.Errorf("Pending() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestMoves lists what a step moves from given pins: a module missing or behind, never
// one at or beyond the step's.
func TestMoves(t *testing.T) {
	t.Parallel()

	step := testLedger()[2]
	tests := []struct {
		name string
		from map[string]string
		want []string
	}{
		{name: "from nothing: every pin", from: nil, want: []string{"access v0.2.0", "ccc/resource v0.3.0"}},
		{name: "from the step before: both move", from: testLedger()[1].Pins, want: []string{"access v0.2.0", "ccc/resource v0.3.0"}},
		{name: "resource bumped by hand ahead: access alone", from: map[string]string{resourceModule: "v0.4.0", accessModule: "v0.1.0"}, want: []string{"access v0.2.0"}},
		{name: "at the step: nothing", from: step.Pins, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, step.Moves(tt.from)); diff != "" {
				t.Errorf("Moves() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestNames labels the steps by what each moves from the one before.
func TestNames(t *testing.T) {
	t.Parallel()

	want := []string{"access v0.1.0, ccc/resource v0.1.0", "ccc/resource v0.2.0", "access v0.2.0, ccc/resource v0.3.0"}
	if diff := cmp.Diff(want, Names(testLedger())); diff != "" {
		t.Errorf("Names() mismatch (-want +got):\n%s", diff)
	}
}

// TestAppPins reads the framework requires of a go.mod and nothing else: not impulse's
// own, whose pin is the tool's.
func TestAppPins(t *testing.T) {
	t.Parallel()

	mod, err := modfile.Parse("go.mod", []byte("module example.com/acme/beacon\n\ngo 1.26\n\nrequire (\n\tgithub.com/cccteam/ccc/resource v0.10.7-0.20261005064211-ce3bc37a7307\n\tgithub.com/cccteam/access v0.9.12\n\tgithub.com/go-chi/chi/v5 v5.3.2\n)\n\nrequire github.com/cccteam/ccc/impulse v0.1.0 // indirect\n\ntool github.com/cccteam/ccc/impulse\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{resourceModule: "v0.10.7-0.20261005064211-ce3bc37a7307", accessModule: "v0.9.12"}
	if diff := cmp.Diff(want, AppPins(mod)); diff != "" {
		t.Errorf("AppPins() mismatch (-want +got):\n%s", diff)
	}
	if got := AppPins(nil); len(got) != 0 {
		t.Errorf("AppPins(nil) = %v, want none", got)
	}
}

// TestValidate holds a ledger to its shape, and the recorded ledger to it.
func TestValidate(t *testing.T) {
	t.Parallel()

	pins := func(resource string) map[string]string { return map[string]string{resourceModule: resource} }
	tests := []struct {
		name    string
		steps   []Step
		wantErr string
	}{
		{name: "the recorded ledger", steps: Steps},
		{name: "the test ledger", steps: testLedger()},
		{name: "no step", steps: []Step{}, wantErr: "the ledger records no step; the first is the first impulse release's pins"},
		{name: "no pins", steps: []Step{{Note: "n"}}, wantErr: "step 1 pins no framework module"},
		{name: "no note", steps: []Step{{Pins: pins("v0.1.0")}}, wantErr: "step 1 has no note"},
		{name: "impulse pinned as a step's", steps: []Step{{Pins: map[string]string{check.ImpulseModule: "v0.1.1"}, Note: "n"}}, wantErr: "step 1 pins github.com/cccteam/ccc/impulse, which is the tool pin the walk moves to the running impulse, not a step's"},
		{name: "a pin outside the framework", steps: []Step{{Pins: map[string]string{"github.com/go-chi/chi/v5": "v5.3.2"}, Note: "n"}}, wantErr: "step 1 pins github.com/go-chi/chi/v5, which is not a framework module (github.com/cccteam/...)"},
		{name: "a pin that is not a version", steps: []Step{{Pins: pins("latest"), Note: "n"}}, wantErr: `step 1 pins github.com/cccteam/ccc/resource at "latest", which is not a semantic version`},
		{name: "a pin moving backward", steps: []Step{{Pins: pins("v0.2.0"), Note: "n"}, {Pins: pins("v0.1.0"), Note: "n"}}, wantErr: "step 2 pins github.com/cccteam/ccc/resource at v0.1.0, behind step 1's v0.2.0; the ledger walks forward only"},
		{name: "a step that moves nothing", steps: []Step{{Pins: pins("v0.1.0"), Note: "n"}, {Pins: pins("v0.1.0"), Note: "n"}}, wantErr: "step 2 moves no pin and names no recipe: it is not a step"},
		{name: "a recipe alone is a step", steps: []Step{{Pins: pins("v0.1.0"), Note: "n"}, {Pins: pins("v0.1.0"), Note: "n", Recipes: []Recipe{noopRecipe{name: "paging"}}}}},
		{name: "a recipe named twice", steps: []Step{{Pins: pins("v0.1.0"), Note: "n", Recipes: []Recipe{noopRecipe{name: "paging"}, noopRecipe{name: "paging"}}}}, wantErr: "step 1 names the recipe paging twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := Validate(tt.steps)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Validate() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

// TestSkeletonPins holds the ledger's last step to the skeleton: every module it pins is
// pinned at the same version by every candidate's go.mod, and every cccteam module a
// candidate requires directly is one it pins, so a pin bump or a new module in the
// skeleton without its step fails here.
func TestSkeletonPins(t *testing.T) {
	t.Parallel()

	candidates, err := skeleton.Candidates()
	if err != nil {
		t.Fatal(err)
	}
	last := &Steps[len(Steps)-1]
	for _, c := range candidates {
		sub, err := skeleton.FS(c.Name)
		if err != nil {
			t.Fatal(err)
		}
		data, err := fs.ReadFile(sub, skeleton.ModFile)
		if err != nil {
			t.Fatal(err)
		}
		mod, err := modfile.Parse(skeleton.ModFile, data, nil)
		if err != nil {
			t.Fatalf("candidate %s: %v", c.Name, err)
		}
		pins := AppPins(mod)
		for _, name := range last.PinNames() {
			if pins[name] != last.Pins[name] {
				t.Errorf("candidate %s pins %s at %q and the ledger's last step at %s: append a step for the skeleton's pins", c.Name, name, pins[name], last.Pins[name])
			}
		}
		for _, r := range mod.Require {
			if r.Indirect || !strings.HasPrefix(r.Mod.Path, FrameworkPrefix) || r.Mod.Path == check.ImpulseModule {
				continue
			}
			if _, ok := last.Pins[r.Mod.Path]; !ok {
				t.Errorf("candidate %s requires %s %s, which the ledger's last step does not pin: append a step for it", c.Name, r.Mod.Path, r.Mod.Version)
			}
		}
	}
}
