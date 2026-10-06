package ci_test

import (
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/ci"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

// candidates are the embedded skeletons, each rendered once per test that needs the tree.
var candidates = []string{"solo", "tenanted", "outlets", "sites"}

// update rewrites the candidates' committed workflows from the render instead of
// comparing: go test ./ci -update. The candidates are embedded, so the rewritten files
// are read by the next run.
var update = flag.Bool("update", false, "rewrite the candidates' .github/workflows/ci.yml under internal/skeleton/_candidates from the render")

// candidatesDir is where the embedded candidates live in the source tree, relative to
// this package, for -update.
const candidatesDir = "../internal/skeleton/_candidates"

// renderCandidate renders one embedded skeleton under its own placeholder module path and
// discovers it.
func renderCandidate(t *testing.T, name string) *app.App {
	t.Helper()

	all, err := skeleton.Candidates()
	if err != nil {
		t.Fatalf("skeleton.Candidates() error = %v", err)
	}
	modulePath := ""
	for _, c := range all {
		if c.Name == name {
			modulePath = c.ModulePath
		}
	}
	if modulePath == "" {
		t.Fatalf("no candidate %q", name)
	}
	dir := t.TempDir()
	if _, err := skeleton.Render(&skeleton.Options{Candidate: name, Dir: dir, ModulePath: modulePath}); err != nil {
		t.Fatalf("skeleton.Render(%s) error = %v", name, err)
	}
	a, err := app.Discover(dir)
	if err != nil {
		t.Fatalf("app.Discover() error = %v", err)
	}

	return a
}

// jobLine is a job id at the workflow's job indentation: two spaces, the id, a colon.
var jobLine = regexp.MustCompile(`^ {2}([A-Za-z_][A-Za-z0-9_-]*):\s*$`)

// jobIDs reads the top-level job ids of a rendered workflow: every two-space-indented key
// after the jobs: line.
func jobIDs(t *testing.T, workflow []byte) []string {
	t.Helper()

	var ids []string
	inJobs := false
	for _, line := range strings.Split(string(workflow), "\n") {
		if line == "jobs:" {
			inJobs = true

			continue
		}
		if !inJobs {
			continue
		}
		if m := jobLine.FindStringSubmatch(line); m != nil {
			ids = append(ids, m[1])
		}
	}
	if len(ids) == 0 {
		t.Fatal("no job ids read from the rendered workflow")
	}

	return ids
}

// TestChecksAreTheRenderedJobs pins the one place for names: Checks lists exactly the job
// ids Render writes, in order, and every fixed check is among them.
func TestChecksAreTheRenderedJobs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		candidate string
		want      []string
	}{
		{name: "solo", candidate: "solo", want: []string{"title", "go-build", "go-test", "go-test-skipauth", "go-lint", "go-lint-skipauth", "go-vuln", "go-semgrep", "go-check", "go", "angular-web", "web", "image", "secrets", "migrations"}},
		{name: "tenanted", candidate: "tenanted", want: []string{"title", "go-build", "go-test", "go-test-skipauth", "go-lint", "go-lint-skipauth", "go-vuln", "go-semgrep", "go-check", "go", "angular-web", "web", "image", "secrets", "migrations"}},
		{name: "outlets", candidate: "outlets", want: []string{"title", "go-build", "go-test", "go-test-skipauth", "go-lint", "go-lint-skipauth", "go-vuln", "go-semgrep", "go-check", "go", "angular-web", "web", "image", "secrets", "migrations"}},
		{name: "sites", candidate: "sites", want: []string{"title", "go-build", "go-test", "go-test-skipauth", "go-lint", "go-lint-skipauth", "go-vuln", "go-semgrep", "go-check", "go", "angular-console", "angular-portal", "web", "image", "secrets", "migrations"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := renderCandidate(t, tt.candidate)
			rendered, err := ci.Render(a)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			got := ci.Checks(a)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Checks() mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(got, jobIDs(t, rendered)); diff != "" {
				t.Errorf("Checks() and the rendered jobs disagree (-checks +jobs):\n%s", diff)
			}
			for _, fixed := range ci.FixedChecks {
				found := false
				for _, c := range got {
					if c == fixed {
						found = true
					}
				}
				if !found {
					t.Errorf("Checks() lacks the fixed check %q", fixed)
				}
			}
		})
	}
}

// TestRenderEqualsTheCandidates is the golden per candidate: the committed
// .github/workflows/ci.yml of every embedded skeleton is what Render produces over the
// rendered tree, byte for byte. A change to the template or the pins is a change to the
// four files in the same commit (impulse render in a rendered candidate writes them).
func TestRenderEqualsTheCandidates(t *testing.T) {
	t.Parallel()

	for _, candidate := range candidates {
		t.Run(candidate, func(t *testing.T) {
			t.Parallel()

			sub, err := skeleton.FS(candidate)
			if err != nil {
				t.Fatalf("skeleton.FS() error = %v", err)
			}
			committed, err := fs.ReadFile(sub, ci.File)
			if err != nil {
				t.Fatalf("the %s candidate carries no %s: %v", candidate, ci.File, err)
			}
			if _, err := fs.Stat(sub, ".github/codeql-config.yml"); err == nil {
				t.Errorf("the %s candidate still carries .github/codeql-config.yml, which nothing reads", candidate)
			}
			a := renderCandidate(t, candidate)
			rendered, err := ci.Render(a)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if *update {
				if err := os.WriteFile(filepath.Join(candidatesDir, candidate, filepath.FromSlash(ci.File)), rendered, 0o600); err != nil {
					t.Fatalf("rewriting the %s candidate's %s: %v", candidate, ci.File, err)
				}

				return
			}
			if diff := cmp.Diff(string(committed), string(rendered)); diff != "" {
				t.Errorf("%s: the committed %s differs from the rendering (-committed +rendered); go test ./ci -update rewrites it:\n%s", candidate, ci.File, diff)
			}
			d, err := ci.Compare(a)
			if err != nil {
				t.Fatalf("Compare() error = %v", err)
			}
			if d != nil {
				t.Errorf("Compare() over the rendered candidate = %s, want nil", d)
			}
		})
	}
}

// job returns one top-level job of a rendered workflow, from its id line to the line
// before the next job's comment, or an empty string when the workflow has no such job.
func job(workflow []byte, id string) string {
	lines := strings.Split(string(workflow), "\n")
	start := -1
	for i, line := range lines {
		if m := jobLine.FindStringSubmatch(line); len(m) > 1 && m[1] == id {
			start = i

			continue
		}
		if start >= 0 && (strings.HasPrefix(line, "  # ") || i == len(lines)-1) {
			return strings.Join(lines[start:i], "\n")
		}
	}

	return ""
}

// TestWebGate: the workflow carries one web job whatever the workspaces, fixed in name so
// a repository rule can require it: it needs every browser workspace job, in the
// workspaces' order, runs whether they passed or not, and fails on any result but
// success; without a workspace it needs nothing and passes with nothing to check.
func TestWebGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		dirs      []string
		wantNeeds string
	}{
		{name: "no browser workspace needs nothing", dirs: nil, wantNeeds: ""},
		{name: "the flat workspace", dirs: []string{"web"}, wantNeeds: "    needs:\n      - angular-web\n"},
		{name: "two sites, in the workspaces' order", dirs: []string{"apps/portal/web", "apps/console/web"}, wantNeeds: "    needs:\n      - angular-console\n      - angular-portal\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := &app.App{}
			for _, d := range tt.dirs {
				a.WebApps = append(a.WebApps, app.WebApp{Dir: d})
			}
			rendered, err := ci.Render(a)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			got := job(rendered, "web")
			if got == "" {
				t.Fatal("the rendered workflow has no web job")
			}
			head := "  web:\n    runs-on: ubuntu-latest\n    timeout-minutes: 5\n    if: ${{ always() }}\n" + tt.wantNeeds + "    steps:\n"
			if !strings.HasPrefix(got, head) {
				t.Errorf("the web job opens with\n%s\nwant\n%s", got, head)
			}
			for _, want := range []string{
				"FAILED: ${{ contains(needs.*.result, 'failure') || contains(needs.*.result, 'cancelled') || contains(needs.*.result, 'skipped') }}",
				`if [ "$FAILED" = "true" ]; then`,
				"exit 1",
			} {
				if !strings.Contains(got, want) {
					t.Errorf("the web job lacks %q:\n%s", want, got)
				}
			}
			if strings.Count(got, "needs:") != min(len(tt.dirs), 1) {
				t.Errorf("needs: appears %d times in the web job, want %d:\n%s", strings.Count(got, "needs:"), min(len(tt.dirs), 1), got)
			}
			if !strings.Contains(string(rendered), "\n  # The browser gate, one fixed name over the per-workspace jobs") {
				t.Error("the web job carries no comment saying what it gates")
			}
		})
	}
}

// TestGoGate: the workflow carries one go job, fixed in name so a repository rule can
// require it, over the Go legs: it needs every leg in GoJobs' order, runs whether they
// passed or not, and fails on any result but success; the legs carry no needs of their
// own, so they run at once.
func TestGoGate(t *testing.T) {
	t.Parallel()

	rendered, err := ci.Render(&app.App{})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := job(rendered, "go")
	if got == "" {
		t.Fatal("the rendered workflow has no go job")
	}
	needs := "    needs:\n"
	for _, leg := range ci.GoJobs {
		needs += "      - " + leg + "\n"
	}
	head := "  go:\n    runs-on: ubuntu-latest\n    timeout-minutes: 5\n    if: ${{ always() }}\n" + needs + "    steps:\n"
	if !strings.HasPrefix(got, head) {
		t.Errorf("the go job opens with\n%s\nwant\n%s", got, head)
	}
	for _, want := range []string{
		"FAILED: ${{ contains(needs.*.result, 'failure') || contains(needs.*.result, 'cancelled') || contains(needs.*.result, 'skipped') }}",
		`if [ "$FAILED" = "true" ]; then`,
		"exit 1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the go job lacks %q:\n%s", want, got)
		}
	}
	if !strings.Contains(string(rendered), "\n  # The Go gate, one fixed name over the go-<leg> jobs") {
		t.Error("the go job carries no comment saying what it gates")
	}
	for _, leg := range ci.GoJobs {
		legJob := job(rendered, leg)
		if legJob == "" {
			t.Errorf("the rendered workflow has no %s job", leg)

			continue
		}
		if strings.Contains(legJob, "needs:") {
			t.Errorf("the %s leg waits on another job; the legs run at once:\n%s", leg, legJob)
		}
	}
}

// TestLargeRunner: the two test legs and the image build run on the runner the
// CI_LARGE_RUNNER variable names, GitHub's standard runner while it is unset, and no
// other job reads the variable.
func TestLargeRunner(t *testing.T) {
	t.Parallel()

	rendered, err := ci.Render(&app.App{WebApps: []app.WebApp{{Dir: "web"}}})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	const large = "    runs-on: ${{ vars." + ci.LargeRunnerVariable + " || 'ubuntu-latest' }}\n"
	onLarge := map[string]bool{"go-test": true, "go-test-skipauth": true, "image": true}
	for _, id := range jobIDs(t, rendered) {
		j := job(rendered, id)
		if j == "" {
			t.Fatalf("no %s job", id)
		}
		if got, want := strings.Contains(j, large), onLarge[id]; got != want {
			t.Errorf("%s runs on the larger runner = %v, want %v:\n%s", id, got, want, j)
		}
		if !onLarge[id] && !strings.Contains(j, "    runs-on: ubuntu-latest\n") {
			t.Errorf("%s does not run on the standard runner:\n%s", id, j)
		}
	}
	if strings.Count(string(rendered), large) != len(onLarge) {
		t.Errorf("the larger runner's runs-on appears %d times, want %d", strings.Count(string(rendered), large), len(onLarge))
	}
	if !strings.Contains(string(rendered), "variable "+ci.LargeRunnerVariable) {
		t.Errorf("the header says nothing of the variable %s", ci.LargeRunnerVariable)
	}
}

// TestTitleTypes: the title job accepts exactly TitleTypes, in their order, so the list
// bedrock reads for release-please's sections is the list the check enforces.
func TestTitleTypes(t *testing.T) {
	t.Parallel()

	rendered, err := ci.Render(&app.App{})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	title := job(rendered, "title")
	_, after, found := strings.Cut(title, "          types: |\n")
	if !found {
		t.Fatalf("the title job lists no types:\n%s", title)
	}
	var got []string
	for _, line := range strings.Split(after, "\n") {
		if !strings.HasPrefix(line, "            ") {
			break
		}
		got = append(got, strings.TrimSpace(line))
	}
	if diff := cmp.Diff(ci.TitleTypes, got); diff != "" {
		t.Errorf("the title job's types and TitleTypes disagree (-TitleTypes +job):\n%s", diff)
	}
	for _, want := range []string{"feat", "feature", "fix", "upgrade", "infra", "config", "cleanup", "chore"} {
		if !slices.Contains(ci.TitleTypes, want) {
			t.Errorf("TitleTypes lacks %q", want)
		}
	}
}

func TestWorkspaces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dirs []string
		want []ci.Workspace
	}{
		{name: "none", dirs: nil, want: []ci.Workspace{}},
		{name: "the flat workspace", dirs: []string{"web"}, want: []ci.Workspace{{Dir: "web", Job: "angular-web"}}},
		{
			name: "sites, sorted by directory",
			dirs: []string{"apps/portal/web", "apps/console/web"},
			want: []ci.Workspace{{Dir: "apps/console/web", Job: "angular-console"}, {Dir: "apps/portal/web", Job: "angular-portal"}},
		},
		{
			name: "other layouts join the segments and drop a trailing web",
			dirs: []string{"web/console", "apps/pilots/gui", "gui", "services/kiosk/web"},
			want: []ci.Workspace{
				{Dir: "apps/pilots/gui", Job: "angular-apps-pilots-gui"},
				{Dir: "gui", Job: "angular-gui"},
				{Dir: "services/kiosk/web", Job: "angular-services-kiosk"},
				{Dir: "web/console", Job: "angular-web-console"},
			},
		},
		{name: "the application root", dirs: []string{"."}, want: []ci.Workspace{{Dir: ".", Job: "angular-root"}}},
		{name: "a character a job id cannot carry", dirs: []string{"apps/my.site/web"}, want: []ci.Workspace{{Dir: "apps/my.site/web", Job: "angular-my-site"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := &app.App{}
			for _, d := range tt.dirs {
				a.WebApps = append(a.WebApps, app.WebApp{Dir: d})
			}
			if diff := cmp.Diff(tt.want, ci.Workspaces(a)); diff != "" {
				t.Errorf("Workspaces() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// workspaceApp is an application at a temporary root with the given browser workspaces,
// its workflow written from the code when written is set.
func workspaceApp(t *testing.T, dirs []string, written bool) *app.App {
	t.Helper()

	a := &app.App{Root: t.TempDir()}
	for _, d := range dirs {
		a.WebApps = append(a.WebApps, app.WebApp{Dir: d})
	}
	if written {
		if _, err := ci.Write(a); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	return a
}

func TestCompare(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// app builds the application and its committed file.
		app  func(t *testing.T) *app.App
		want *ci.Difference
		// wantText is the difference's String when a difference is expected.
		wantText string
	}{
		{
			name: "equal",
			app:  func(t *testing.T) *app.App { t.Helper(); return workspaceApp(t, []string{"web"}, true) },
		},
		{
			name: "missing",
			app:  func(t *testing.T) *app.App { t.Helper(); return workspaceApp(t, []string{"web"}, false) },
			want: &ci.Difference{Missing: true}, wantText: ".github/workflows/ci.yml is missing",
		},
		{
			name: "a hand edit",
			app: func(t *testing.T) *app.App {
				t.Helper()
				a := workspaceApp(t, []string{"web"}, true)
				data, err := os.ReadFile(a.Abs(ci.File))
				if err != nil {
					t.Fatal(err)
				}
				edited := strings.Replace(string(data), "  cancel-in-progress: true", "  cancel-in-progress: false", 1)
				if err := os.WriteFile(a.Abs(ci.File), []byte(edited), 0o600); err != nil {
					t.Fatal(err)
				}

				return a
			},
			want:     &ci.Difference{Line: 27, Want: "  cancel-in-progress: true", Got: "  cancel-in-progress: false"},
			wantText: `.github/workflows/ci.yml:27: the code renders "  cancel-in-progress: true"; the file has "  cancel-in-progress: false"`,
		},
		{
			name: "a workspace without its job",
			app: func(t *testing.T) *app.App {
				t.Helper()
				a := workspaceApp(t, []string{"apps/console/web"}, true)
				// A second workspace appears after the file was written.
				a.WebApps = append(a.WebApps, app.WebApp{Dir: "apps/portal/web"})

				return a
			},
			want: &ci.Difference{
				Line: 268,
				Want: "  # The browser workspace at apps/portal/web: bun installs from the lockfile exactly (bun ci), then the package scripts build, lint and test.",
				Got:  "  # The browser gate, one fixed name over the per-workspace jobs so a repository rule can require it: it fails when any of them did not succeed, and passes with nothing to check in an application without a browser workspace.",
			},
			wantText: `.github/workflows/ci.yml:268: the code renders "  # The browser workspace at apps/portal/web: bun installs from the lockfile exactly (bun ci), then the package scripts build, lint and test."; the file has "  # The browser gate, one fixed name over the per-workspace jobs so a repository rule can require it: it fails when any of them did not succeed, and passes with nothing to check in an application without a browser workspace."`,
		},
		{
			name: "a file that ends early",
			app: func(t *testing.T) *app.App {
				t.Helper()
				a := workspaceApp(t, []string{"web"}, true)
				data, err := os.ReadFile(a.Abs(ci.File))
				if err != nil {
					t.Fatal(err)
				}
				lines := strings.Split(string(data), "\n")
				if err := os.WriteFile(a.Abs(ci.File), []byte(strings.Join(lines[:20], "\n")+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}

				return a
			},
			want:     &ci.Difference{Line: 21, Want: "on:", Got: ""},
			wantText: `.github/workflows/ci.yml:21: the code renders "on:"; the file has ""`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ci.Compare(tt.app(t))
			if err != nil {
				t.Fatalf("Compare() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Compare() mismatch (-want +got):\n%s", diff)
			}
			if got != nil && got.String() != tt.wantText {
				t.Errorf("String() = %q, want %q", got.String(), tt.wantText)
			}
		})
	}
}

func TestWrite(t *testing.T) {
	t.Parallel()

	a := workspaceApp(t, []string{"web"}, false)
	first, err := ci.Write(a)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !first.Written {
		t.Error("first Write() reported nothing written")
	}
	info, err := os.Stat(a.Abs(ci.File))
	if err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %o, want 644", info.Mode().Perm())
	}
	again, err := ci.Write(a)
	if err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	if again.Written {
		t.Error("second Write() rewrote an unchanged file")
	}
	if d, err := ci.Compare(a); err != nil || d != nil {
		t.Errorf("Compare() after Write() = %v, %v; want nil, nil", d, err)
	}
	if _, err := os.Stat(filepath.Join(a.Root, ".github", "workflows")); err != nil {
		t.Errorf("the workflows directory was not created: %v", err)
	}
}
