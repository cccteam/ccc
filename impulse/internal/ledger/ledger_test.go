package ledger

import (
	"context"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/mod/modfile"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
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

// testLedger is three releases: the first pins both modules at 0.1.0, the second moves
// resource with a recipe, the third moves both with none.
func testLedger() []Release {
	return []Release{
		{Version: "v0.1.0", Pins: map[string]string{resourceModule: "v0.1.0", accessModule: "v0.1.0"}, Note: "the first beta"},
		{Version: "v0.2.0", Pins: map[string]string{resourceModule: "v0.2.0", accessModule: "v0.1.0"}, Recipes: []Recipe{noopRecipe{name: "paging"}}, Note: "pages by cursor"},
		{Version: "v0.3.0", Pins: map[string]string{resourceModule: "v0.3.0", accessModule: "v0.2.0"}, Note: "no code change is needed"},
	}
}

// TestPosition reads where pins stand: before every release, at one, beyond the last, and
// a pin bumped ahead of the others counting for the releases it reaches alone.
func TestPosition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		pins map[string]string
		want int
	}{
		{name: "no framework pin", pins: map[string]string{}, want: -1},
		{name: "before the first release", pins: map[string]string{resourceModule: "v0.0.9", accessModule: "v0.1.0"}, want: -1},
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

// TestPending lists the releases to walk: every later one, up to a target, none when the
// application is at the last, and a target outside the ledger or behind the position refused.
func TestPending(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pins    map[string]string
		target  string
		want    []string
		wantErr string
	}{
		{name: "before the first: all three", pins: map[string]string{}, want: []string{"v0.1.0", "v0.2.0", "v0.3.0"}},
		{name: "at the first: the two after", pins: map[string]string{resourceModule: "v0.1.0", accessModule: "v0.1.0"}, want: []string{"v0.2.0", "v0.3.0"}},
		{name: "at the first, up to the second", pins: map[string]string{resourceModule: "v0.1.0", accessModule: "v0.1.0"}, target: "v0.2.0", want: []string{"v0.2.0"}},
		{name: "at the last: none", pins: map[string]string{resourceModule: "v0.3.0", accessModule: "v0.2.0"}, want: []string{}},
		{name: "a target the ledger does not record", pins: map[string]string{}, target: "v0.4.0", wantErr: "v0.4.0 is not an impulse release the ledger records (v0.1.0, v0.2.0, v0.3.0)"},
		{name: "a target at the position: none", pins: map[string]string{resourceModule: "v0.2.0", accessModule: "v0.1.0"}, target: "v0.2.0", want: []string{}},
		{name: "a target behind the position", pins: map[string]string{resourceModule: "v0.2.0", accessModule: "v0.1.0"}, target: "v0.1.0", wantErr: "the application already stands beyond v0.1.0 (its pins reach v0.2.0); an upgrade walks forward only"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Pending(testLedger(), tt.pins, tt.target)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Pending() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Pending() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, Versions(got)); diff != "" {
				t.Errorf("Pending() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestAppPins reads the framework requires of a go.mod and nothing else.
func TestAppPins(t *testing.T) {
	t.Parallel()

	mod, err := modfile.Parse("go.mod", []byte("module example.com/acme/beacon\n\ngo 1.26\n\nrequire (\n\tgithub.com/cccteam/ccc/resource v0.10.7-0.20261005064211-ce3bc37a7307\n\tgithub.com/cccteam/access v0.9.12\n\tgithub.com/go-chi/chi/v5 v5.3.2\n)\n\nrequire github.com/cccteam/ccc/impulse v0.1.0 // indirect\n\ntool github.com/cccteam/ccc/impulse\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{resourceModule: "v0.10.7-0.20261005064211-ce3bc37a7307", accessModule: "v0.9.12", "github.com/cccteam/ccc/impulse": "v0.1.0"}
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

	tests := []struct {
		name     string
		releases []Release
		wantErr  string
	}{
		{name: "the recorded ledger", releases: Releases},
		{name: "the test ledger", releases: testLedger()},
		{name: "a version that is not one", releases: []Release{{Version: "0.1", Pins: map[string]string{resourceModule: "v0.1.0"}, Note: "n"}}, wantErr: `release 0: "0.1" is not a semantic version`},
		{name: "releases out of order", releases: []Release{{Version: "v0.2.0", Pins: map[string]string{resourceModule: "v0.1.0"}, Note: "n"}, {Version: "v0.1.0", Pins: map[string]string{resourceModule: "v0.1.0"}, Note: "n"}}, wantErr: "release v0.1.0 follows v0.2.0; the ledger is oldest first"},
		{name: "the same version twice", releases: []Release{{Version: "v0.1.0", Pins: map[string]string{resourceModule: "v0.1.0"}, Note: "n"}, {Version: "v0.1.0", Pins: map[string]string{resourceModule: "v0.1.0"}, Note: "n"}}, wantErr: "release v0.1.0 follows v0.1.0"},
		{name: "no pins", releases: []Release{{Version: "v0.1.0", Note: "n"}}, wantErr: "release v0.1.0 pins no framework module"},
		{name: "no note", releases: []Release{{Version: "v0.1.0", Pins: map[string]string{resourceModule: "v0.1.0"}}}, wantErr: "release v0.1.0 has no note"},
		{name: "a pin outside the framework", releases: []Release{{Version: "v0.1.0", Pins: map[string]string{"github.com/go-chi/chi/v5": "v5.3.2"}, Note: "n"}}, wantErr: "release v0.1.0 pins github.com/go-chi/chi/v5, which is not a framework module (github.com/cccteam/...)"},
		{name: "a pin that is not a version", releases: []Release{{Version: "v0.1.0", Pins: map[string]string{resourceModule: "latest"}, Note: "n"}}, wantErr: `release v0.1.0 pins github.com/cccteam/ccc/resource at "latest", which is not a semantic version`},
		{name: "a recipe named twice", releases: []Release{{Version: "v0.1.0", Pins: map[string]string{resourceModule: "v0.1.0"}, Note: "n", Recipes: []Recipe{noopRecipe{name: "paging"}, noopRecipe{name: "paging"}}}}, wantErr: "release v0.1.0 names the recipe paging twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := Validate(tt.releases)
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
