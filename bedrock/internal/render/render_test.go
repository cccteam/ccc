package render

import (
	"bytes"
	"encoding/json"
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

// TestPipelineFlowItems reads the rendered Cloud Build configurations and refuses a bare
// item of a flow sequence that YAML 1.1, which Cloud Build reads, takes for a boolean or
// null: on, off, yes, no, y, n, true, false and null, in any case, and ~. A step's args
// are strings, and "cannot unmarshal bool into Go value of type string" stopped a pipeline
// at its first step once over args: [deploy, maintenance, on]. Such a word is quoted.
func TestPipelineFlowItems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		file string
	}{
		{name: "harbor's pipeline", file: "testdata/harbor/root/cloudbuild.yaml"},
		{name: "harbor's sweep", file: "testdata/harbor/root/cloudbuild-sweep.yaml"},
		{name: "beacon's pipeline", file: "testdata/beacon/root/cloudbuild.yaml"},
		{name: "beacon's sweep", file: "testdata/beacon/root/cloudbuild-sweep.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			for i, line := range strings.Split(string(content), "\n") {
				if word := bareYAMLBool(line); word != "" {
					t.Errorf("%s:%d: %q is a YAML 1.1 boolean or null in a flow sequence; quote it: %s", tt.file, i+1, word, strings.TrimSpace(line))
				}
			}
		})
	}
}

// TestPipelineOrder proves the rendered pipeline keeps the order a window release needs,
// start check, image, the job where the application has a job process, pre-flight, wait,
// maintenance, migrate, revision, traffic, record, carries the release file's directory to
// the release check and the version variable to the migration steps, renders the job steps
// only for an application with a job process, and keeps every step's own timeout under the
// whole-build ceiling of 24 hours.
func TestPipelineOrder(t *testing.T) {
	t.Parallel()

	order := []string{"ValidateRelease", "CheckRelease", "BuildImage", "MaintenanceOnRestore", "PlanEnvironmentStack", "ApplyEnvironmentStack", "CreateJobs", "PreflightMigrations", "WaitForWindow", "MaintenanceOnWindow", "RunMigrations", "DeployServiceNoTraffic", "ShiftTraffic", "MaintenanceOff", "SweepJobs", "WriteDeploymentRecord"}
	jobSteps := []string{"CreateJobs", "SweepJobs"}
	timeouts := map[string]string{"ValidateRelease": "300s", "BuildImage": "1800s", "PlanEnvironmentStack": "7200s", "PreflightMigrations": "1200s", "WaitForWindow": "86400s", "MaintenanceOnWindow": "900s", "RunMigrations": "2400s", "DeployServiceNoTraffic": "1200s", "ShiftTraffic": "600s"}
	tests := []struct {
		name string
		file string
		jobs bool
	}{
		{name: "harbor's pipeline", file: "testdata/harbor/root/cloudbuild.yaml", jobs: true},
		{name: "beacon's pipeline", file: "testdata/beacon/root/cloudbuild.yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			var ids []string
			seen := map[string]string{}
			for _, line := range strings.Split(string(content), "\n") {
				if id, ok := strings.CutPrefix(line, "  - id: "); ok {
					ids = append(ids, id)
				}
				if timeout, ok := strings.CutPrefix(line, "    timeout: "); ok && len(ids) > 0 {
					seen[ids[len(ids)-1]] = timeout
				}
			}
			want := order
			if !tt.jobs {
				want = slices.DeleteFunc(slices.Clone(order), func(id string) bool {
					return slices.Contains(jobSteps, id)
				})
				for _, id := range jobSteps {
					if slices.Contains(ids, id) {
						t.Errorf("step %s is rendered for an application without a job process", id)
					}
				}
			}
			at := 0
			for _, id := range want {
				i := slices.Index(ids[at:], id)
				if i < 0 {
					t.Errorf("step %s is missing or out of order after %v", id, ids[:at])

					continue
				}
				at += i + 1
			}
			for id, want := range timeouts {
				if seen[id] != want {
					t.Errorf("step %s has timeout %q, want %q", id, seen[id], want)
				}
			}
			if !strings.Contains(string(content), "args: [deploy, validate-release, --router-dir, pkg/router]") {
				t.Errorf("the release check is not told the router directory:\n%s", content)
			}
			if !strings.Contains(string(content), "\ntimeout: 86400s\n") {
				t.Errorf("the whole-build timeout is not the 24-hour ceiling:\n%s", content)
			}
		})
	}
}

// bareYAMLBool is the first unquoted item of a flow sequence on the line that YAML 1.1
// reads as a boolean or null, or "".
func bareYAMLBool(line string) string {
	open := strings.Index(line, "[")
	if open < 0 || strings.HasPrefix(strings.TrimSpace(line), "#") {
		return ""
	}
	closing := strings.LastIndex(line, "]")
	if closing < open {
		return ""
	}
	for _, item := range strings.Split(line[open+1:closing], ",") {
		item = strings.TrimSpace(item)
		switch strings.ToLower(item) {
		case "y", "n", "yes", "no", "on", "off", "true", "false", "null", "~":
			return item
		}
	}

	return ""
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

// TestReleaseFiles: render seeds release-please's configuration and manifest at the
// application root, the configuration starting the releases at 0.1.0 (initial-version)
// with a feature on the minor below 1.0, the manifest at 0.0.0, and keeps either once the
// application has it.
func TestReleaseFiles(t *testing.T) {
	t.Parallel()

	files, err := Render(deriveFixture(t, "harbor", "placement.json"))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	rendered := map[string]File{}
	for _, f := range files {
		if f.Root {
			rendered[f.Path] = f
		}
	}
	var config map[string]any
	if err := json.Unmarshal(rendered[ReleasePleaseConfig].Content, &config); err != nil {
		t.Fatalf("%s does not read: %v", ReleasePleaseConfig, err)
	}
	var manifest map[string]string
	if err := json.Unmarshal(rendered[ReleasePleaseManifest].Content, &manifest); err != nil {
		t.Fatalf("%s does not read: %v", ReleasePleaseManifest, err)
	}
	tests := []struct {
		name string
		got  any
		want any
	}{
		{name: "the configuration is seeded at the root", got: rendered[ReleasePleaseConfig].Tier == Seeded && rendered[ReleasePleaseConfig].Root, want: true},
		{name: "the manifest is seeded at the root", got: rendered[ReleasePleaseManifest].Tier == Seeded && rendered[ReleasePleaseManifest].Root, want: true},
		{name: "the first release is 0.1.0", got: config["initial-version"], want: "0.1.0"},
		{name: "a feature advances the minor below 1.0", got: config["bump-patch-for-minor-pre-major"], want: false},
		{name: "a breaking change advances the minor below 1.0", got: config["bump-minor-pre-major"], want: true},
		{name: "the tags are v<version>, as the pipeline reads them", got: config["include-v-in-tag"], want: true},
		{name: "one package, the repository", got: fmt.Sprint(config["packages"]), want: "map[.:map[]]"},
		{name: "the manifest starts at 0.0.0", got: manifest["."], want: "0.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}

	seeding := []struct {
		name       string
		existing   map[string]string
		wantSeeded []string
		wantKept   []string
	}{
		{name: "an application without them gets both", wantSeeded: []string{ReleasePleaseConfig, ReleasePleaseManifest}},
		{
			name:       "a manifest release-please moved is kept",
			existing:   map[string]string{ReleasePleaseManifest: "{\".\": \"0.3.1\"}\n"},
			wantSeeded: []string{ReleasePleaseConfig},
			wantKept:   []string{ReleasePleaseManifest},
		},
	}
	for _, tt := range seeding {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			appDir := t.TempDir()
			for name, content := range tt.existing {
				if err := os.WriteFile(filepath.Join(appDir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			written, err := Write([]File{rendered[ReleasePleaseConfig], rendered[ReleasePleaseManifest]}, filepath.Join(t.TempDir(), "stack"), appDir)
			if err != nil {
				t.Fatalf("Write() error = %v", err)
			}
			if strings.Join(written.Seeded, ",") != strings.Join(tt.wantSeeded, ",") {
				t.Errorf("Write() seeded = %v, want %v", written.Seeded, tt.wantSeeded)
			}
			if strings.Join(written.Kept, ",") != strings.Join(tt.wantKept, ",") {
				t.Errorf("Write() kept = %v, want %v", written.Kept, tt.wantKept)
			}
			for name, content := range tt.existing {
				if got, err := os.ReadFile(filepath.Join(appDir, name)); err != nil || string(got) != content {
					t.Errorf("%s = %q (%v), want %q kept", name, got, err, content)
				}
			}
		})
	}
}

// store is a derived file store at the level, for the file-store tests.
func store(variable, level, name string) derive.FileStore {
	resource, suffix := "files", "files"
	if name != "" {
		resource += "_" + strings.ReplaceAll(name, "-", "_")
		suffix += "-" + name
	}

	return derive.FileStore{Variable: &derive.Variable{Name: variable, Level: level, Struct: "dataConfig", Field: "Store"}, Name: name, Resource: resource, Suffix: suffix}
}

func TestNewFileStore(t *testing.T) {
	t.Parallel()

	data := &derive.Process{Name: "jobs", Dir: "cmd/jobs", Levels: []string{derive.LevelCore, derive.LevelData}}
	tests := []struct {
		name          string
		store         derive.FileStore
		jobs          *derive.Process
		wantValue     string
		wantLabel     string
		wantTitle     string
		wantJobsReads bool
	}{
		{
			name:          "the default store: the bucket's gs:// URL from its name, read by a job process that constructs its level",
			store:         store("APP_FILE_STORE", derive.LevelData, ""),
			jobs:          data,
			wantValue:     `"gs://${google_storage_bucket.files.name}"`,
			wantLabel:     "the default file store",
			wantTitle:     "The default file store",
			wantJobsReads: true,
		},
		{
			name:          "a named store: the resource with underscores in the URL, the name in the label",
			store:         store("APP_FILE_STORE_DOCUMENTS", derive.LevelData, "documents"),
			jobs:          data,
			wantValue:     `"gs://${google_storage_bucket.files_documents.name}"`,
			wantLabel:     "the documents file store",
			wantTitle:     "The documents file store",
			wantJobsReads: true,
		},
		{
			name:      "a two-word name",
			store:     store("APP_FILE_STORE_CLIENT_FILES", derive.LevelData, "client-files"),
			wantValue: `"gs://${google_storage_bucket.files_client_files.name}"`,
			wantLabel: "the client-files file store",
			wantTitle: "The client-files file store",
		},
		{
			name:      "a store at a level the job process does not construct",
			store:     store("APP_FILE_STORE", derive.LevelSite, ""),
			jobs:      data,
			wantValue: `"gs://${google_storage_bucket.files.name}"`,
			wantLabel: "the default file store",
			wantTitle: "The default file store",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := newFileStore(tt.store, tt.jobs)
			if got.Value != tt.wantValue || got.Label != tt.wantLabel || got.Title != tt.wantTitle || got.JobsReads != tt.wantJobsReads {
				t.Errorf("newFileStore() = value %s, label %q, title %q, jobs %t; want %s, %q, %q, %t", got.Value, got.Label, got.Title, got.JobsReads, tt.wantValue, tt.wantLabel, tt.wantTitle, tt.wantJobsReads)
			}
		})
	}
}

func TestFileStores(t *testing.T) {
	t.Parallel()

	data := &derive.Process{Name: "jobs", Dir: "cmd/jobs", Levels: []string{derive.LevelCore, derive.LevelData}}
	tests := []struct {
		name   string
		stores []derive.FileStore
		jobs   *derive.Process
		// wantEnv is the aligned block of every store, wantJobsEnv the job process's own
		// block (empty when it reads every store's level or none), wantJobs the stores it
		// reads, and wantAddresses the buckets for the pipeline's substitution.
		wantEnv       string
		wantJobsEnv   string
		wantJobs      []string
		wantAddresses string
	}{
		{name: "no store", stores: nil, jobs: data},
		{
			name:          "one store the job process reads: one block serves both",
			stores:        []derive.FileStore{store("APP_FILE_STORE", derive.LevelData, "")},
			jobs:          data,
			wantEnv:       `    APP_FILE_STORE = "gs://${google_storage_bucket.files.name}"`,
			wantJobs:      []string{"APP_FILE_STORE"},
			wantAddresses: "google_storage_bucket.files",
		},
		{
			name:          "two stores at the data level, aligned, in declaration order",
			stores:        []derive.FileStore{store("APP_FILE_STORE", derive.LevelData, ""), store("APP_FILE_STORE_DOCUMENTS", derive.LevelData, "documents")},
			jobs:          data,
			wantEnv:       "    APP_FILE_STORE           = \"gs://${google_storage_bucket.files.name}\"\n    APP_FILE_STORE_DOCUMENTS = \"gs://${google_storage_bucket.files_documents.name}\"",
			wantJobs:      []string{"APP_FILE_STORE", "APP_FILE_STORE_DOCUMENTS"},
			wantAddresses: "google_storage_bucket.files,google_storage_bucket.files_documents",
		},
		{
			name:          "a store at a level the job process does not construct gets the job process a block of its own",
			stores:        []derive.FileStore{store("APP_FILE_STORE", derive.LevelData, ""), store("APP_FILE_STORE_DOCUMENTS", derive.LevelSite, "documents")},
			jobs:          data,
			wantEnv:       "    APP_FILE_STORE           = \"gs://${google_storage_bucket.files.name}\"\n    APP_FILE_STORE_DOCUMENTS = \"gs://${google_storage_bucket.files_documents.name}\"",
			wantJobsEnv:   `    APP_FILE_STORE = "gs://${google_storage_bucket.files.name}"`,
			wantJobs:      []string{"APP_FILE_STORE"},
			wantAddresses: "google_storage_bucket.files,google_storage_bucket.files_documents",
		},
		{
			name:          "no job process: every store is the site's alone",
			stores:        []derive.FileStore{store("APP_FILE_STORE", derive.LevelData, "")},
			wantEnv:       `    APP_FILE_STORE = "gs://${google_storage_bucket.files.name}"`,
			wantAddresses: "google_storage_bucket.files",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			v := &view{Model: &derive.Model{FileStores: tt.stores, Jobs: tt.jobs}}
			v.fileStores()
			if len(v.FileStores) != len(tt.stores) {
				t.Errorf("FileStores has %d, want %d", len(v.FileStores), len(tt.stores))
			}
			if v.FileStoreEnv != tt.wantEnv {
				t.Errorf("FileStoreEnv =\n%s\nwant\n%s", v.FileStoreEnv, tt.wantEnv)
			}
			if v.JobsFileStoreEnv != tt.wantJobsEnv {
				t.Errorf("JobsFileStoreEnv =\n%s\nwant\n%s", v.JobsFileStoreEnv, tt.wantJobsEnv)
			}
			var jobs []string
			for i := range v.JobsFileStores {
				jobs = append(jobs, v.JobsFileStores[i].Variable.Name)
			}
			if !slices.Equal(jobs, tt.wantJobs) {
				t.Errorf("JobsFileStores = %v, want %v", jobs, tt.wantJobs)
			}
			if v.FileStoreAddresses != tt.wantAddresses {
				t.Errorf("FileStoreAddresses = %q, want %q", v.FileStoreAddresses, tt.wantAddresses)
			}
		})
	}
}

// TestFileStorePolicy reads a file store's bucket policy as the stack declares it: the
// bucket's whole permission list, objectUser for the site and, when the job process
// constructs the store's level, the job, nobody else, set again with a replaced bucket,
// and no member resource on the bucket beside it.
func TestFileStorePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// level is the store's level: the data level, which harbor's job process
		// constructs, or the site level, which it does not.
		level  string
		want   []string
		absent []string
	}{
		{
			name:  "a store the job process reads: the site and the job",
			level: derive.LevelData,
			want: []string{
				"data \"google_iam_policy\" \"files\" {\n  binding {\n    role    = \"roles/storage.objectUser\"\n    members = [local.app_member, local.jobs_member]\n  }\n}\n",
				"resource \"google_storage_bucket_iam_policy\" \"files\" {\n  bucket      = google_storage_bucket.files.name\n  policy_data = data.google_iam_policy.files.policy_data\n\n  depends_on = [google_service_account.app, google_service_account.jobs]\n",
				"  lifecycle {\n    replace_triggered_by = [google_storage_bucket.files]\n  }\n",
				"the one authoritative IAM resource the stack\n# declares",
				// The member resources the policy replaced leave the state without a
				// destroy, which would take their members out of the live policy.
				"removed {\n  from = google_storage_bucket_iam_member.files_app\n\n  lifecycle {\n    destroy = false\n  }\n}\n",
				"removed {\n  from = google_storage_bucket_iam_member.files_jobs\n\n  lifecycle {\n    destroy = false\n  }\n}\n",
				"# The member resources the policy above replaced, taken out of the state\n# without being destroyed.",
				"Carried for one bedrock release, so a stack that moves to\n# the policy is applied once; dropped in the next release",
			},
			absent: []string{"resource \"google_storage_bucket_iam_member\""},
		},
		{
			name:  "a store at a level the job process does not construct: the site alone",
			level: derive.LevelSite,
			want: []string{
				"    members = [local.app_member]\n",
				"  depends_on = [google_service_account.app]\n",
				"# The member resource the policy above replaced,",
				"removed {\n  from = google_storage_bucket_iam_member.files_app\n\n  lifecycle {\n    destroy = false\n  }\n}\n",
			},
			absent: []string{"resource \"google_storage_bucket_iam_member\"", "local.jobs_member", "files_jobs"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := deriveFixture(t, "harbor", "placement.json")
			if len(m.FileStores) != 1 {
				t.Fatalf("harbor declares %d file stores, want 1", len(m.FileStores))
			}
			m.FileStores[0].Variable.Level = tt.level
			files, err := Render(m)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			var storage string
			for _, f := range files {
				if f.Path == "storage.tf" {
					storage = string(f.Content)
				}
			}
			if storage == "" {
				t.Fatal("storage.tf is not rendered")
			}
			for _, w := range tt.want {
				if !strings.Contains(storage, w) {
					t.Errorf("storage.tf lacks:\n%s", w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(storage, a) {
					t.Errorf("storage.tf still carries %q", a)
				}
			}
		})
	}
}

func TestBuildMachine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// buildMachine is the placement's value; empty leaves the machine to Cloud Build.
		buildMachine string
		wantPipeline []string
		wantReadme   []string
		// absentPipeline and absentReadme are what each file must not carry: the README
		// names options.machineType either way, saying there is none when it is unset.
		absentPipeline []string
		absentReadme   []string
	}{
		{
			name:         "no build machine: the options carry none and the README says so",
			buildMachine: "",
			wantPipeline: []string{"options:\n  logging: CLOUD_LOGGING_ONLY\n  # The bedrock deploy steps"},
			wantReadme: []string{
				"run on Cloud Build's default machine, the placement naming no\n  `buildMachine`",
				"no `options.machineType`,\n  the placement naming no `buildMachine`",
				"`E2_MEDIUM` (1 vCPU), `E2_STANDARD_2` (2 vCPUs), `E2_HIGHCPU_8` (8 vCPUs), and `E2_HIGHCPU_32` (32 vCPUs)",
			},
			absentPipeline: []string{"machineType"},
			absentReadme:   []string{"`options.machineType: "},
		},
		{
			name:         "a 32-vCPU machine: options.machineType and the README's arithmetic",
			buildMachine: "E2_HIGHCPU_32",
			wantPipeline: []string{"options:\n  logging: CLOUD_LOGGING_ONLY\n  # The placement's buildMachine.", "  machineType: E2_HIGHCPU_32\n  # The bedrock deploy steps"},
			wantReadme: []string{
				"run on `E2_HIGHCPU_32` (32 vCPUs), the placement's\n  `buildMachine`",
				"`options.machineType: E2_HIGHCPU_32`,\n  the machine the placement's `buildMachine` names",
				"sixteen builds of 8 vCPUs\n  at once, or four of 32",
			},
			absentReadme: []string{"Cloud Build's default machine"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := deriveFixture(t, "harbor", "placement.json")
			m.Placement.BuildMachine = tt.buildMachine
			files, err := Render(m)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			var pipeline, readme string
			for _, f := range files {
				switch {
				case f.Root && f.Path == "cloudbuild.yaml":
					pipeline = string(f.Content)
				case !f.Root && f.Path == "README.md":
					readme = string(f.Content)
				}
			}
			if pipeline == "" || readme == "" {
				t.Fatal("cloudbuild.yaml or README.md is not rendered")
			}
			for _, w := range tt.wantPipeline {
				if !strings.Contains(pipeline, w) {
					t.Errorf("cloudbuild.yaml lacks:\n%s", w)
				}
			}
			for _, w := range tt.wantReadme {
				if !strings.Contains(readme, w) {
					t.Errorf("README.md lacks:\n%s", w)
				}
			}
			for _, a := range tt.absentPipeline {
				if strings.Contains(pipeline, a) {
					t.Errorf("cloudbuild.yaml still carries %q", a)
				}
			}
			for _, a := range tt.absentReadme {
				if strings.Contains(readme, a) {
					t.Errorf("README.md still carries %q", a)
				}
			}
		})
	}
}

// TestMaxInstances pins the service's scaling for each shape of the placement's caps: no
// cap anywhere (no max_instance_count, no local), the same cap everywhere, and caps in
// some environments, where the others take Cloud Run's default through the lookup's null.
func TestMaxInstances(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		caps       map[string]int
		wantRun    []string
		wantLocals []string
		wantReadme string
		absent     []string
	}{
		{
			name: "no cap: Cloud Run's default, and no local",
			caps: nil,
			wantRun: []string{
				"    # Scale to zero, and to Cloud Run's default maximum instances per region:\n    # the placement caps no environment (maxInstances).\n    scaling {\n      min_instance_count = 0\n    }\n",
			},
			wantReadme: "It scales from zero to Cloud Run's default maximum instances per region, the placement capping no environment (`maxInstances`).",
			absent:     []string{"max_instance_count", "max_instances"},
		},
		{
			name:       "the same cap in every environment",
			caps:       map[string]int{"tst": 2, "stg": 2, "prd": 2},
			wantRun:    []string{"    scaling {\n      min_instance_count = 0\n      max_instance_count = local.max_instances\n    }\n"},
			wantLocals: []string{"  max_instances = lookup({ tst = 2, stg = 2, prd = 2 }, var.environment, null)\n"},
			wantReadme: "It scales from zero to at most 2 instances per region in every environment, the placement's cap (`maxInstances`).",
		},
		{
			name:       "caps in some environments, the rest uncapped",
			caps:       map[string]int{"tst": 1, "prd": 20},
			wantRun:    []string{"      max_instance_count = local.max_instances\n"},
			wantLocals: []string{"  max_instances = lookup({ tst = 1, prd = 20 }, var.environment, null)\n"},
			wantReadme: "It scales from zero to at most 1 instance per region in tst, 20 in prd, the placement's caps (`maxInstances`), and to Cloud Run's default maximum in stg.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := deriveFixture(t, "harbor", "placement.json")
			m.Placement.MaxInstances = tt.caps
			files, err := Render(m)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			text := map[string]string{}
			for _, f := range files {
				if !f.Root {
					text[f.Path] = string(f.Content)
				}
			}
			for _, w := range tt.wantRun {
				if !strings.Contains(text["cloud-run.tf"], w) {
					t.Errorf("cloud-run.tf lacks:\n%s", w)
				}
			}
			for _, w := range tt.wantLocals {
				if !strings.Contains(text["locals.tf"], w) {
					t.Errorf("locals.tf lacks:\n%s", w)
				}
			}
			if !strings.Contains(text["README.md"], tt.wantReadme) {
				t.Errorf("README.md lacks:\n%s", tt.wantReadme)
			}
			for _, a := range tt.absent {
				for _, path := range []string{"cloud-run.tf", "locals.tf"} {
					if strings.Contains(text[path], a) {
						t.Errorf("%s carries %q", path, a)
					}
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
	for _, want := range []string{"_ENV", "_APP", "_PROJECT", "_SERVICES", "_MIGRATE_ENV", "_SEED", "_DEPLOYER_KEY_SECRET", "_BUILD_SECRETS"} {
		if !slices.Contains(names, want) {
			t.Errorf("SubstitutionNames() = %v, want it to contain %s", names, want)
		}
	}
	if slices.Contains(names, "_PR_NUMBER") {
		t.Errorf("SubstitutionNames() = %v: _PR_NUMBER is the trigger's, not the map's", names)
	}
}

// TestMigrationGrants proves the rendered stacks grant the deploy identity, which runs the
// migrate command on the build worker, database admin on the application's own database
// the way they pin its project roles, and the Firestore user role only where the migrate
// command constructs that level; that no migrate identity, template migrate job or job
// output remains and the pipeline's contract carries the command's variables in place of
// the job; that the pipeline hands the migration steps the version variable; and that the
// operations workflow reads the build's own log by step name under static job names.
func TestMigrationGrants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		dir           string
		wantFirestore bool
	}{
		{name: "harbor, whose migrate command reads Firestore", dir: "testdata/harbor", wantFirestore: true},
		{name: "beacon, whose migrate command does not", dir: "testdata/beacon"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := map[string]string{}
			for _, name := range []string{"spanner.tf", "firestore.tf", "service-accounts.tf", "cloud-run.tf", "cloud-build.tf", "outputs.tf", "logging.tf", "locals.tf", "README.md", "root/cloudbuild.yaml", "root/.github/workflows/operations.yml"} {
				content, err := os.ReadFile(filepath.Join(tt.dir, name))
				if err != nil {
					t.Fatalf("ReadFile() error = %v", err)
				}
				files[name] = string(content)
			}
			present := map[string][]string{
				"spanner.tf":                            {`resource "google_spanner_database_iam_member" "deploy_admin" {`, "  role     = \"roles/spanner.databaseAdmin\"\n  member   = local.identities.deploy_identity_member"},
				"service-accounts.tf":                   {`resource "google_project_iam_member" "deploy_metrics" {`, "  count = local.is_pr ? 0 : 1\n", "  role    = \"roles/monitoring.metricWriter\"\n  member  = local.identities.deploy_identity_member"},
				"cloud-build.tf":                        {"_MIGRATE_ENV             = jsonencode(local.migrate_env)", "_MIGRATE_DATABASES       = jsonencode(local.migrate_databases)"},
				"locals.tf":                             {"migrate_env = merge(local.core_env, local.data_env", "migrate_databases = concat(\n    [\"projects/${local.instance.project}/instances/${local.instance.name}/databases/${local.database_name}\"],"},
				"logging.tf":                            {`resource.labels.build_trigger_id`, `"operations_reads_migrate_logs"`},
				"README.md":                             {"`roles/spanner.databaseAdmin` on the database only, for DDL", "`roles/monitoring.metricWriter` on the project for the Spanner\n  client's metrics", "The first apply after a render with this bedrock removes them"},
				"root/cloudbuild.yaml":                  {"args: [deploy, migrate, --preflight, --version-variable, APP_VERSION]", "args: [deploy, migrate, --version-variable, APP_VERSION]"},
				"root/.github/workflows/operations.yml": {`labels.build_step=~\"(Preflight|Run)Migrations\"`, "    name: restore the environment\n", "    name: run the release again\n", "    name: the migrations' operation\n"},
			}
			absent := map[string][]string{
				"spanner.tf":                            {"migrate_admin", "local.migrate_member"},
				"service-accounts.tf":                   {`"google_service_account" "migrate"`, "deploy_uses_migrate", "local.migrate_member"},
				"cloud-run.tf":                          {`"google_cloud_run_v2_job" "migrate"`},
				"cloud-build.tf":                        {"_MIGRATE_JOB", "_MIGRATE_LOGS"},
				"outputs.tf":                            {`output "migrate_job"`, "google_service_account.migrate"},
				"locals.tf":                             {"migrate_account", "job_env"},
				"logging.tf":                            {"deploy_reads_migrate_logs", "cloud_run_job"},
				"README.md":                             {"the migrate job", "Migrate job", "`_MIGRATE_JOB`", "`_MIGRATE_LOGS`"},
				"root/.github/workflows/operations.yml": {"MIGRATE_JOB", "inputs.environment }} to", "cloud_run_job", "migrate job"},
			}
			firestore := []string{`resource "google_project_iam_member" "firestore_deploy" {`, "  member  = local.identities.deploy_identity_member"}
			firestoreDatabase := `["projects/${local.project_id}/databases/${google_firestore_database.firestore.name}"],`
			if tt.wantFirestore {
				present["firestore.tf"] = firestore
				present["locals.tf"] = append(present["locals.tf"], firestoreDatabase)
			} else {
				absent["firestore.tf"] = firestore
				absent["locals.tf"] = []string{firestoreDatabase}
			}
			absent["firestore.tf"] = append(absent["firestore.tf"], "firestore_migrate", "local.migrate_member")
			for name, wants := range present {
				for _, want := range wants {
					if !strings.Contains(files[name], want) {
						t.Errorf("%s lacks %q", name, want)
					}
				}
			}
			for name, wants := range absent {
				for _, want := range wants {
					if strings.Contains(files[name], want) {
						t.Errorf("%s still carries %q", name, want)
					}
				}
			}
		})
	}
}
