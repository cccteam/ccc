package ledger

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"

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
// resource with a recipe and was added by impulse v0.2.0, the third moves both with none
// and was added by v0.3.0.
func testLedger() []Step {
	return []Step{
		{Pins: map[string]string{resourceModule: "v0.1.0", accessModule: "v0.1.0"}, Note: "the first beta"},
		{Pins: map[string]string{resourceModule: "v0.2.0", accessModule: "v0.1.0"}, Recipes: []Recipe{noopRecipe{name: "paging"}}, Note: "pages by cursor", Impulse: "v0.2.0"},
		{Pins: map[string]string{resourceModule: "v0.3.0", accessModule: "v0.2.0"}, Note: "no code change is needed", Impulse: "v0.3.0"},
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

// TestMovesTool reads whether a step moves the tool pin from a pin: a step naming the
// release that added it moves a pin missing or behind it and leaves one at or beyond it;
// a step naming none moves nothing.
func TestMovesTool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		step Step
		pin  string
		want bool
	}{
		{name: "no pin", step: testLedger()[1], pin: "", want: true},
		{name: "a pin behind the release", step: testLedger()[1], pin: "v0.1.2-0.20261008040440-dcb9876c02e8", want: true},
		{name: "a pin at the release", step: testLedger()[1], pin: "v0.2.0", want: false},
		{name: "a pin beyond the release", step: testLedger()[1], pin: "v0.3.1", want: false},
		{name: "a step naming no release", step: testLedger()[0], pin: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.step.MovesTool(tt.pin); got != tt.want {
				t.Errorf("MovesTool(%q) = %v, want %v", tt.pin, got, tt.want)
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
		{name: "impulse pinned among the framework pins", steps: []Step{{Pins: map[string]string{check.ImpulseModule: "v0.1.1"}, Note: "n"}}, wantErr: "step 1 pins github.com/cccteam/ccc/impulse among the framework pins; the step names the impulse release that added it in Impulse, and the walk moves the tool pin to it"},
		{name: "a pin outside the framework", steps: []Step{{Pins: map[string]string{"github.com/go-chi/chi/v5": "v5.3.2"}, Note: "n"}}, wantErr: "step 1 pins github.com/go-chi/chi/v5, which is not a framework module (github.com/cccteam/...)"},
		{name: "a pin that is not a version", steps: []Step{{Pins: pins("latest"), Note: "n"}}, wantErr: `step 1 pins github.com/cccteam/ccc/resource at "latest", which is not a semantic version`},
		{name: "a pin moving backward", steps: []Step{{Pins: pins("v0.2.0"), Note: "n"}, {Pins: pins("v0.1.0"), Note: "n"}}, wantErr: "step 2 pins github.com/cccteam/ccc/resource at v0.1.0, behind step 1's v0.2.0; the ledger walks forward only"},
		{name: "a step that moves nothing", steps: []Step{{Pins: pins("v0.1.0"), Note: "n"}, {Pins: pins("v0.1.0"), Note: "n"}}, wantErr: "step 2 moves no pin and names no recipe: it is not a step"},
		{name: "a recipe alone is a step", steps: []Step{{Pins: pins("v0.1.0"), Note: "n"}, {Pins: pins("v0.1.0"), Note: "n", Recipes: []Recipe{noopRecipe{name: "paging"}}}}},
		{name: "a recipe named twice", steps: []Step{{Pins: pins("v0.1.0"), Note: "n", Recipes: []Recipe{noopRecipe{name: "paging"}, noopRecipe{name: "paging"}}}}, wantErr: "step 1 names the recipe paging twice"},
		{name: "a pin at a pushed commit on a step not marked pending", steps: []Step{{Pins: pins("v0.1.1-0.20261009052330-7bd8478e0ccf"), Note: "n"}}, wantErr: "step 1 pins github.com/cccteam/ccc/resource at v0.1.1-0.20261009052330-7bd8478e0ccf, a pushed commit, and is not marked pending: a step whose pins name commits says so, and the release's repin clears it"},
		{name: "a pending step whose pins are all released", steps: []Step{{Pins: pins("v0.1.0"), Note: "n", Pending: true}}, wantErr: "step 1 is marked pending and pins no pushed commit: the repin that moved it to the tags clears the mark"},
		{name: "a pending step pins a pushed commit", steps: []Step{{Pins: pins("v0.1.1-0.20261009052330-7bd8478e0ccf"), Note: "n", Pending: true}}},
		{name: "a release that is not a version", steps: []Step{{Pins: pins("v0.1.0"), Note: "n", Impulse: "latest"}}, wantErr: `step 1 names impulse "latest", which is not a release: a step names the tagged version of the impulse that added it (v0.2.0), or the version its pull request will take while it is pending`},
		{name: "a pushed commit named as the release", steps: []Step{{Pins: pins("v0.1.0"), Note: "n", Impulse: "v0.3.1-0.20261009052330-7bd8478e0ccf"}}, wantErr: `step 1 names impulse "v0.3.1-0.20261009052330-7bd8478e0ccf", which is not a release`},
		{name: "a pre-release named as the release", steps: []Step{{Pins: pins("v0.1.0"), Note: "n", Impulse: "v0.3.0-rc.1"}}, wantErr: `step 1 names impulse "v0.3.0-rc.1", which is not a release`},
		{name: "a release with build metadata", steps: []Step{{Pins: pins("v0.1.0"), Note: "n", Impulse: "v0.3.0+dirty"}}, wantErr: `step 1 names impulse "v0.3.0+dirty", which is not a release`},
		{name: "a release moving backward", steps: []Step{{Pins: pins("v0.1.0"), Note: "n", Impulse: "v0.3.0"}, {Pins: pins("v0.2.0"), Note: "n", Impulse: "v0.2.0"}}, wantErr: "step 2 names impulse v0.2.0, behind step 1's v0.3.0; the ledger walks forward only"},
		{name: "a step after one naming a release names none", steps: []Step{{Pins: pins("v0.1.0"), Note: "n", Impulse: "v0.2.0"}, {Pins: pins("v0.2.0"), Note: "n"}}, wantErr: "step 2 names no impulse release and step 1 names v0.2.0: once a step names the release that added it, every later step does"},
		{name: "a step naming a release after one naming none", steps: []Step{{Pins: pins("v0.1.0"), Note: "n"}, {Pins: pins("v0.2.0"), Note: "n", Impulse: "v0.2.0"}}},
		{name: "one release adding two steps", steps: []Step{{Pins: pins("v0.1.0"), Note: "n", Impulse: "v0.2.0"}, {Pins: pins("v0.2.0"), Note: "n", Impulse: "v0.2.0"}}},
		{name: "a pending step names the release its pull request takes", steps: []Step{{Pins: pins("v0.1.0"), Note: "n", Impulse: "v0.3.1"}, {Pins: pins("v0.1.1-0.20261009052330-7bd8478e0ccf"), Note: "n", Pending: true, Impulse: "v0.4.0"}}},
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

// TestCurrentAtLastStep reads the recorded ledger as the checks do: go.mod at the last
// step's pins reaches it, one at the step before's does not, and one without framework
// pins does not.
func TestCurrentAtLastStep(t *testing.T) {
	t.Parallel()

	goMod := func(pins map[string]string) string {
		var b strings.Builder
		b.WriteString("module example.com/acme/beacon\n\ngo 1.26\n\nrequire (\n")
		for _, name := range (&Step{Pins: pins}).PinNames() {
			b.WriteString("\t" + name + " " + pins[name] + "\n")
		}
		b.WriteString(")\n")

		return b.String()
	}
	tests := []struct {
		name  string
		gomod string
		want  bool
	}{
		{name: "at the last step", gomod: goMod(Steps[len(Steps)-1].Pins), want: true},
		{name: "at the step before", gomod: goMod(Steps[len(Steps)-2].Pins), want: false},
		{name: "no framework pins", gomod: "module example.com/acme/beacon\n\ngo 1.26\n", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mod, err := modfile.Parse("go.mod", []byte(tt.gomod), nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := (Current{}).AtLastStep(mod); got != tt.want {
				t.Errorf("AtLastStep() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestToolDirectiveDrag holds impulse's own go.mod to the ledger's last step: no framework
// module the step pins is required at a version beyond the pin. The tool directive puts
// impulse's requirements into an application's build list, so this is what the tool pin's
// move at the last step, and the tool-only move to a release that added no step, carry an
// application to; held here, they carry it nowhere the ledger does not record. A release
// whose requirements move past the last step fails here until its step is appended.
func TestToolDirectiveDrag(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	last := &Steps[len(Steps)-1]
	for _, r := range mod.Require {
		pin, ok := last.Pins[r.Mod.Path]
		if !ok {
			continue
		}
		if semver.Compare(r.Mod.Version, pin) > 0 {
			t.Errorf("impulse requires %s at %s and the ledger's last step pins it at %s: the tool pin would carry an application past the ledger; append a step for the release that moves it", r.Mod.Path, r.Mod.Version, pin)
		}
	}
}
