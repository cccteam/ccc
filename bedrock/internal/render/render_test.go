package render

import (
	"bytes"
	"flag"
	"fmt"
	goversion "go/version"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/impulse/app"
)

// fixtures is where the fixture applications and the placement live.
const fixtures = "../derive/testdata"

// deriveFixture derives the named fixture application under the named placement.
func deriveFixture(t *testing.T, name, placement string) *derive.Model {
	t.Helper()

	p, err := derive.ReadPlacement(filepath.Join(fixtures, placement))
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

// update rewrites the goldens from the render instead of comparing: go test ./internal/render
// -update, after a template change that is meant, in the same commit.
var update = flag.Bool("update", false, "rewrite the goldens under testdata from the render")

// TestRenderGolden proves the render reproduces the hand-written stack byte for byte:
// every file under testdata/<golden>, comments included, since the comments naming the
// source declarations are the point.
func TestRenderGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		fixture   string
		placement string
		golden    string
	}{
		{name: "harbor, a Google directory auth", fixture: "harbor", placement: "placement.json", golden: "harbor"},
		{name: "beacon, a password auth", fixture: "beacon", placement: "placement-beacon.json", golden: "beacon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files, err := Render(deriveFixture(t, tt.fixture, tt.placement))
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			// The stack's files are the golden directory's; the files for the application
			// root are under its root subdirectory.
			goldenDir := filepath.Join("testdata", tt.golden)
			rendered := map[string]bool{}
			for _, f := range files {
				golden := goldenDir
				if f.Root {
					golden = filepath.Join(goldenDir, "root")
				}
				rendered[filepath.Join(golden, f.Path)] = true
				if *update {
					if err := os.MkdirAll(filepath.Dir(filepath.Join(golden, f.Path)), 0o750); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(golden, f.Path), f.Content, 0o600); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(filepath.Join(golden, f.Path))
				if err != nil {
					t.Errorf("%s: rendered but not in %s: %v", f.Path, golden, err)

					continue
				}
				if !bytes.Equal(f.Content, want) {
					t.Errorf("%s differs from %s:\n%s", f.Path, golden, firstDiff(want, f.Content))
				}
			}
			for _, golden := range []string{goldenDir, filepath.Join(goldenDir, "root")} {
				entries, err := os.ReadDir(golden)
				if err != nil {
					t.Fatalf("os.ReadDir() error = %v", err)
				}
				for _, e := range entries {
					if !e.IsDir() && !rendered[filepath.Join(golden, e.Name())] {
						t.Errorf("%s is in %s but not rendered", e.Name(), golden)
					}
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

func TestFormatted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		file    string
		content string
		want    string
	}{
		{
			name:    "a .tf file's assignments and trailing comments align as tofu fmt writes them",
			file:    "cloud-build.tf",
			content: "locals {\n  a = 1 # one\n  bbb = \"two\"   # two\n}\n",
			want:    "locals {\n  a   = 1     # one\n  bbb = \"two\" # two\n}\n",
		},
		{
			name:    "a .tfvars file too",
			file:    "terraform.tfvars",
			content: "a=1\n",
			want:    "a = 1\n",
		},
		{
			name:    "any other file as it is",
			file:    "cloudbuild.yaml",
			content: "steps:\n  - name:   x\n",
			want:    "steps:\n  - name:   x\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := string(Formatted(tt.file, []byte(tt.content))); got != tt.want {
				t.Errorf("Formatted() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestGoImage holds the Go image a commit pin is built in to bedrock: its Go is at least
// the go line of bedrock's go.mod, since the builds run with GOTOOLCHAIN=local and a newer
// go line would fail every pipeline pinned to that commit, and it is the image the seeded
// Dockerfile builds the application in, so bedrock names one Go digest.
func TestGoImage(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	mod, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatalf("modfile.Parse() error = %v", err)
	}
	dockerfile, err := templates.ReadFile("templates/root/Dockerfile.tmpl")
	if err != nil {
		t.Fatalf("templates.ReadFile() error = %v", err)
	}
	tests := []struct {
		name string
		ok   bool
		why  string
	}{
		{
			name: "the image's Go is at least bedrock's go line",
			ok:   mod.Go != nil && goversion.Compare("go"+goImageGo, "go"+mod.Go.Version) >= 0,
			why:  fmt.Sprintf("goImage carries Go %s and bedrock's go.mod says go %v: pin a digest of an image with that Go and set goImageGo", goImageGo, mod.Go),
		},
		{
			name: "the seeded Dockerfile builds in the same image",
			ok:   bytes.Contains(dockerfile, []byte("FROM "+goImage+" AS build-env")),
			why:  "the seeded Dockerfile's build stage is not FROM " + goImage + ": move both to the same digest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !tt.ok {
				t.Error(tt.why)
			}
		})
	}
}

func TestWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		existing     map[string]string
		existingRoot map[string]string
		wantOwned    int
		wantPipeline []string
		wantSeeded   []string
		wantKept     []string
		wantTfvars   string
		wantDocker   string
	}{
		{
			name:         "empty directories get everything",
			wantOwned:    2,
			wantPipeline: []string{"cloudbuild.yaml"},
			wantSeeded:   []string{"terraform.tfvars", "Dockerfile"},
			wantTfvars:   "seeded\n",
			wantDocker:   "image\n",
		},
		{
			name:         "owned files are rewritten, seeded files kept, in both places",
			existing:     map[string]string{"locals.tf": "old\n", "terraform.tfvars": "mine\n"},
			existingRoot: map[string]string{"cloudbuild.yaml": "old pipeline\n", "Dockerfile": "my image\n"},
			wantOwned:    2,
			wantPipeline: []string{"cloudbuild.yaml"},
			wantKept:     []string{"terraform.tfvars", "Dockerfile"},
			wantTfvars:   "mine\n",
			wantDocker:   "my image\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "stack")
			appDir := t.TempDir()
			for name, content := range tt.existing {
				if err := os.MkdirAll(dir, 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for name, content := range tt.existingRoot {
				if err := os.WriteFile(filepath.Join(appDir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			files := []File{
				{Path: "locals.tf", Tier: Owned, Content: []byte("new\n")},
				{Path: "README.md", Tier: Owned, Content: []byte("readme\n")},
				{Path: "terraform.tfvars", Tier: Seeded, Content: []byte("seeded\n")},
				{Path: "cloudbuild.yaml", Tier: Owned, Root: true, Content: []byte("pipeline\n")},
				{Path: "Dockerfile", Tier: Seeded, Root: true, Content: []byte("image\n")},
			}
			written, err := Write(files, dir, appDir)
			if err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			if written.Owned != tt.wantOwned {
				t.Errorf("Write() owned = %d, want %d", written.Owned, tt.wantOwned)
			}
			if strings.Join(written.Pipeline, ",") != strings.Join(tt.wantPipeline, ",") {
				t.Errorf("Write() pipeline = %v, want %v", written.Pipeline, tt.wantPipeline)
			}
			if strings.Join(written.Seeded, ",") != strings.Join(tt.wantSeeded, ",") {
				t.Errorf("Write() seeded = %v, want %v", written.Seeded, tt.wantSeeded)
			}
			if strings.Join(written.Kept, ",") != strings.Join(tt.wantKept, ",") {
				t.Errorf("Write() kept = %v, want %v", written.Kept, tt.wantKept)
			}
			for path, want := range map[string]string{
				filepath.Join(dir, "locals.tf"):          "new\n",
				filepath.Join(dir, "terraform.tfvars"):   tt.wantTfvars,
				filepath.Join(appDir, "cloudbuild.yaml"): "pipeline\n",
				filepath.Join(appDir, "Dockerfile"):      tt.wantDocker,
			} {
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("os.ReadFile(%s) error = %v", path, err)
				}
				if string(got) != want {
					t.Errorf("%s = %q, want %q", path, got, want)
				}
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

func TestSubstitutionNames(t *testing.T) {
	t.Parallel()

	names, err := SubstitutionNames()
	if err != nil {
		t.Fatalf("SubstitutionNames() error = %v", err)
	}
	for _, want := range []string{"_ENV", "_APP", "_PROJECT", "_SERVICES", "_MIGRATE_JOB", "_SEED", "_DEPLOYER_KEY_SECRET", "_BUILD_SECRETS"} {
		if !slices.Contains(names, want) {
			t.Errorf("SubstitutionNames() = %v, want it to contain %s", names, want)
		}
	}
	if slices.Contains(names, "_PR_NUMBER") {
		t.Errorf("SubstitutionNames() = %v: _PR_NUMBER is the trigger's, not the map's", names)
	}
}
