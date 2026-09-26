package render

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/impulse/app"
)

// fixtures is where the fixture applications and the placement live.
const fixtures = "../derive/testdata"

// deriveFixture derives the named fixture application under the test placement.
func deriveFixture(t *testing.T, name string) *derive.Model {
	t.Helper()

	p, err := derive.ReadPlacement(filepath.Join(fixtures, "placement.json"))
	if err != nil {
		t.Fatalf("derive.ReadPlacement() error = %v", err)
	}
	a, err := app.Discover(filepath.Join(fixtures, name))
	if err != nil {
		t.Fatalf("app.Discover() error = %v", err)
	}
	m, err := derive.Derive(a, p)
	if err != nil {
		t.Fatalf("derive.Derive() error = %v", err)
	}

	return m
}

// TestRenderGolden proves the render reproduces the hand-written stack byte for byte:
// every file under testdata/<golden>, comments included, since the comments naming the
// source declarations are the point.
func TestRenderGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		golden  string
	}{
		{name: "harbor", fixture: "harbor", golden: "harbor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files, err := Render(deriveFixture(t, tt.fixture))
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			goldenDir := filepath.Join("testdata", tt.golden)
			rendered := map[string]bool{}
			for _, f := range files {
				rendered[f.Path] = true
				want, err := os.ReadFile(filepath.Join(goldenDir, f.Path))
				if err != nil {
					t.Errorf("%s: rendered but not in %s: %v", f.Path, goldenDir, err)

					continue
				}
				if !bytes.Equal(f.Content, want) {
					t.Errorf("%s differs from %s:\n%s", f.Path, goldenDir, firstDiff(want, f.Content))
				}
			}
			entries, err := os.ReadDir(goldenDir)
			if err != nil {
				t.Fatalf("os.ReadDir() error = %v", err)
			}
			for _, e := range entries {
				if !e.IsDir() && !rendered[e.Name()] {
					t.Errorf("%s is in %s but not rendered", e.Name(), goldenDir)
				}
			}
		})
	}
}

// firstDiff shows the first differing line of two texts, with a little context.
func firstDiff(want, got []byte) string {
	w := strings.Split(string(want), "\n")
	g := strings.Split(string(got), "\n")
	for i := range max(len(w), len(g)) {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			return fmt.Sprintf("line %d\n  want: %q\n  got:  %q", i+1, wl, gl)
		}
	}

	return "(same lines, different bytes)"
}

func TestWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		existing   map[string]string
		wantOwned  int
		wantSeeded []string
		wantKept   []string
		wantTfvars string
	}{
		{
			name:       "an empty directory gets everything",
			wantOwned:  2,
			wantSeeded: []string{"terraform.tfvars"},
			wantTfvars: "seeded\n",
		},
		{
			name:       "an owned file is rewritten, a seeded file is kept",
			existing:   map[string]string{"locals.tf": "old\n", "terraform.tfvars": "mine\n"},
			wantOwned:  2,
			wantKept:   []string{"terraform.tfvars"},
			wantTfvars: "mine\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "stack")
			if len(tt.existing) > 0 {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				for name, content := range tt.existing {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			}
			files := []File{
				{Path: "locals.tf", Tier: Owned, Content: []byte("new\n")},
				{Path: "README.md", Tier: Owned, Content: []byte("readme\n")},
				{Path: "terraform.tfvars", Tier: Seeded, Content: []byte("seeded\n")},
			}
			written, err := Write(files, dir)
			if err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			if written.Owned != tt.wantOwned {
				t.Errorf("Write() owned = %d, want %d", written.Owned, tt.wantOwned)
			}
			if strings.Join(written.Seeded, ",") != strings.Join(tt.wantSeeded, ",") {
				t.Errorf("Write() seeded = %v, want %v", written.Seeded, tt.wantSeeded)
			}
			if strings.Join(written.Kept, ",") != strings.Join(tt.wantKept, ",") {
				t.Errorf("Write() kept = %v, want %v", written.Kept, tt.wantKept)
			}
			got, err := os.ReadFile(filepath.Join(dir, "terraform.tfvars"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.wantTfvars {
				t.Errorf("terraform.tfvars = %q, want %q", got, tt.wantTfvars)
			}
			locals, err := os.ReadFile(filepath.Join(dir, "locals.tf"))
			if err != nil {
				t.Fatal(err)
			}
			if string(locals) != "new\n" {
				t.Errorf("locals.tf = %q, want the rewritten content", locals)
			}
		})
	}
}

func TestAligned(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		pairs [][2]string
		sep   []string
		want  string
	}{
		{
			name:  "values in one column",
			pairs: [][2]string{{"a", "1"}, {"long", "2"}},
			want:  "  a    = 1\n  long = 2",
		},
		{
			name:  "a separator of its own",
			pairs: [][2]string{{"name-app", "site"}, {"name-migrate", "job"}},
			sep:   []string{"  "},
			want:  "  name-app      site\n  name-migrate  job",
		},
		{name: "nothing", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := aligned("  ", tt.pairs, tt.sep...); got != tt.want {
				t.Errorf("aligned() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestJoinWith(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		items []string
		word  string
		want  string
	}{
		{name: "none", word: "or", want: ""},
		{name: "one", items: []string{"tst"}, word: "or", want: "tst"},
		{name: "two", items: []string{"stg", "prd"}, word: "and", want: "stg and prd"},
		{name: "three", items: []string{"tst", "stg", "prd"}, word: "or", want: "tst, stg, or prd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := joinWith(tt.items, tt.word); got != tt.want {
				t.Errorf("joinWith() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCommonPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		names []string
		want  string
	}{
		{name: "cut at the underscore", names: []string{"GOOGLE_CLOUD_SPANNER_PROJECT", "GOOGLE_CLOUD_SPANNER_INSTANCE_ID"}, want: "GOOGLE_CLOUD_SPANNER"},
		{name: "nothing shared", names: []string{"A_B", "C_D"}, want: ""},
		{name: "one name", names: []string{"APP_PORT"}, want: "APP"},
		{name: "no names", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := commonPrefix(tt.names...); got != tt.want {
				t.Errorf("commonPrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}
