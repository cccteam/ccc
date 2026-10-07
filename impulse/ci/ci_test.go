package ci_test

import (
	"bytes"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

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

// TestRenderEqualsTheCandidates is the golden per candidate: the committed owned
// workflows of every embedded skeleton (ci.yml and ci-cache.yml) are what the code renders
// over the rendered tree, byte for byte. A change to a template or the pins is a change to
// the eight files in the same commit (impulse render in a rendered candidate writes them).
func TestRenderEqualsTheCandidates(t *testing.T) {
	t.Parallel()

	for _, candidate := range candidates {
		t.Run(candidate, func(t *testing.T) {
			t.Parallel()

			sub, err := skeleton.FS(candidate)
			if err != nil {
				t.Fatalf("skeleton.FS() error = %v", err)
			}
			if _, err := fs.Stat(sub, ".github/codeql-config.yml"); err == nil {
				t.Errorf("the %s candidate still carries .github/codeql-config.yml, which nothing reads", candidate)
			}
			a := renderCandidate(t, candidate)
			for _, file := range ci.Files {
				rendered, err := ci.RenderFile(a, file)
				if err != nil {
					t.Fatalf("RenderFile(%s) error = %v", file, err)
				}
				if *update {
					if err := os.WriteFile(filepath.Join(candidatesDir, candidate, filepath.FromSlash(file)), rendered, 0o600); err != nil {
						t.Fatalf("rewriting the %s candidate's %s: %v", candidate, file, err)
					}

					continue
				}
				committed, err := fs.ReadFile(sub, file)
				if err != nil {
					t.Fatalf("the %s candidate carries no %s: %v", candidate, file, err)
				}
				if diff := cmp.Diff(string(committed), string(rendered)); diff != "" {
					t.Errorf("%s: the committed %s differs from the rendering (-committed +rendered); go test ./ci -update rewrites it:\n%s", candidate, file, diff)
				}
			}
			if *update {
				return
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

// TestLargeRunner: the jobs the //impulse:ci line lists under large-runner (the two test
// legs and the image build without a line) run on the runner the CI_LARGE_RUNNER variable
// names and GitHub's standard runner while it is unset, every other job runs on the
// standard runner, the header names them, and a job the workflow does not render, or a
// gate, is refused.
func TestLargeRunner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		ci      *app.CIDirective
		want    []string
		wantErr string
	}{
		{name: "no line: the test legs and the image build", want: []string{"go-test", "go-test-skipauth", "image"}},
		{name: "a line without large-runner keeps the default", ci: &app.CIDirective{TestCache: boolPtr(true)}, want: []string{"go-test", "go-test-skipauth", "image"}},
		{name: "the line's jobs, any rendered job", ci: &app.CIDirective{LargeRunner: []string{"go-build", "angular-web", "secrets"}}, want: []string{"go-build", "angular-web", "secrets"}},
		{name: "none", ci: &app.CIDirective{LargeRunner: []string{}}, want: nil},
		{name: "a job the workflow does not render", ci: &app.CIDirective{File: "main.go", Line: 3, LargeRunner: []string{"go-test", "angular-portal"}}, wantErr: `main.go:3: //impulse:ci large-runner names "angular-portal", which the workflow does not render; the jobs are title, go-build, go-test, go-test-skipauth, go-lint, go-lint-skipauth, go-vuln, go-semgrep, go-check, angular-web, image, secrets, migrations`},
		{name: "a gate", ci: &app.CIDirective{File: "main.go", Line: 3, LargeRunner: []string{"go"}}, wantErr: "main.go:3: //impulse:ci large-runner names go, a gate, which runs nothing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := &app.App{WebApps: []app.WebApp{{Dir: "web"}}, CI: tt.ci}
			settings, err := ci.SettingsOf(a)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("SettingsOf() error = %v, want containing %q", err, tt.wantErr)
				}
				if _, err := ci.Render(a); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Render() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("SettingsOf() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, settings.LargeRunner, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("SettingsOf().LargeRunner mismatch (-want +got):\n%s", diff)
			}
			for _, file := range ci.Files {
				rendered, err := ci.RenderFile(a, file)
				if err != nil {
					t.Fatalf("RenderFile(%s) error = %v", file, err)
				}
				const large = "    runs-on: ${{ vars." + ci.LargeRunnerVariable + " || '" + ci.StandardRunner + "' }}\n"
				onLarge := 0
				for _, id := range jobIDs(t, rendered) {
					j := job(rendered, id)
					if j == "" {
						t.Fatalf("%s: no %s job", file, id)
					}
					if got, want := strings.Contains(j, large), slices.Contains(tt.want, id); got != want {
						t.Errorf("%s: %s runs on the larger runner = %v, want %v:\n%s", file, id, got, want, j)
					}
					if !slices.Contains(tt.want, id) && !strings.Contains(j, "    runs-on: "+ci.StandardRunner+"\n") {
						t.Errorf("%s: %s does not run on the standard runner:\n%s", file, id, j)
					}
					if slices.Contains(tt.want, id) {
						onLarge++
					}
				}
				if strings.Count(string(rendered), large) != onLarge {
					t.Errorf("%s: the larger runner's runs-on appears %d times, want %d", file, strings.Count(string(rendered), large), onLarge)
				}
			}
			rendered, err := ci.Render(a)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			header := header(t, rendered)
			switch {
			case len(tt.want) == 0 && !strings.Contains(header, "Every job runs on GitHub's standard\n# runner (the application's //impulse:ci line says large-runner=none)"):
				t.Errorf("the header does not say every job is on the standard runner:\n%s", header)
			case len(tt.want) > 0 && !strings.Contains(header, ci.List(tt.want)+" run"):
				t.Errorf("the header does not name %s:\n%s", ci.List(tt.want), header)
			case !strings.Contains(header, "variable "+ci.LargeRunnerVariable) && len(tt.want) > 0:
				t.Errorf("the header says nothing of the variable %s", ci.LargeRunnerVariable)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

// header is the comment block before the workflow's name line.
func header(t *testing.T, rendered []byte) string {
	t.Helper()

	before, _, found := bytes.Cut(rendered, []byte("\nname: CI\n"))
	if !found {
		t.Fatal("the rendered workflow has no name line")
	}

	return string(before)
}

// TestTestCache: the test legs run every test (go test -count=1) unless the line says
// test-cache=on, and then they restore the files' modification times from git before
// the run, so Go's cached results can match; the cache-filling workflow's test jobs run
// the same command, with the modification times restored when the results are reused.
// The header says which.
func TestTestCache(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ci   *app.CIDirective
		on   bool
	}{
		{name: "no line: off"},
		{name: "off", ci: &app.CIDirective{TestCache: boolPtr(false)}},
		{name: "on", ci: &app.CIDirective{TestCache: boolPtr(true)}, on: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			a := &app.App{WebApps: []app.WebApp{{Dir: "web"}}, CI: tt.ci}
			settings, err := ci.SettingsOf(a)
			if err != nil {
				t.Fatalf("SettingsOf() error = %v", err)
			}
			if settings.TestCache != tt.on {
				t.Errorf("SettingsOf().TestCache = %v, want %v", settings.TestCache, tt.on)
			}
			pr, err := ci.Render(a)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			cache, err := ci.RenderFile(a, ci.CacheFile)
			if err != nil {
				t.Fatalf("RenderFile(CacheFile) error = %v", err)
			}
			const mtimes = "      - name: Restore the files' modification times from git\n"
			for leg, want := range map[string]string{"go-test": "        run: go test -race -count=1 -timeout 20m ./...\n", "go-test-skipauth": "        run: go test -race -count=1 -timeout 20m -tags skipAuth ./...\n"} {
				if tt.on {
					want = strings.Replace(want, "-count=1 ", "", 1)
				}
				j := job(pr, leg)
				if !strings.Contains(j, want) {
					t.Errorf("%s lacks %q:\n%s", leg, want, j)
				}
				if strings.Contains(j, mtimes) != tt.on {
					t.Errorf("%s restores the modification times = %v, want %v:\n%s", leg, !tt.on, tt.on, j)
				}
				warm := job(cache, leg)
				if warm == "" {
					t.Fatalf("the cache-filling workflow has no %s job", leg)
				}
				if !strings.Contains(warm, want) || !strings.Contains(warm, "TESTCONTAINERS_RYUK_DISABLED") || !strings.Contains(warm, "fetch-depth: 0") {
					t.Errorf("the cache-filling %s does not run the tests as the pull request's does (%q):\n%s", leg, want, warm)
				}
				if strings.Contains(warm, mtimes) != tt.on {
					t.Errorf("the cache-filling %s restores the modification times = %v, want %v:\n%s", leg, !tt.on, tt.on, warm)
				}
			}
			header := header(t, pr)
			if want := "test results are not\n# reused (go test -count=1)"; strings.Contains(header, want) == tt.on {
				t.Errorf("the header says %q = %v, want %v", want, !tt.on, tt.on)
			}
			if want := "(here they are: the line says test-cache=on)"; strings.Contains(header, want) != tt.on {
				t.Errorf("the header says %q = %v, want %v", want, !tt.on, tt.on)
			}
		})
	}
}

// TestCacheSteps: the Go legs restore the Go caches from the lineage CachedJobs names
// (their own for go-build and the test legs, go-build's for go-vuln and go-check), the
// ones that save do so only when their go.sum had no entry, the lint legs and the rest
// restore nothing, setup-go's own cache is off wherever the caches are restored, and the
// cache-filling workflow saves under the commit's key in every Go job.
func TestCacheSteps(t *testing.T) {
	t.Parallel()

	a := &app.App{WebApps: []app.WebApp{{Dir: "web"}}}
	pr, err := ci.Render(a)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	cache, err := ci.RenderFile(a, ci.CacheFile)
	if err != nil {
		t.Fatalf("RenderFile(CacheFile) error = %v", err)
	}
	const restore = "      - name: Restore the Go caches\n"
	const save = "      - name: Save the Go caches\n"
	plan := map[string]ci.CachedJob{}
	for _, c := range ci.CachedJobs {
		plan[c.Job] = c
	}
	for _, id := range jobIDs(t, pr) {
		j := job(pr, id)
		c, cached := plan[id]
		if strings.Contains(j, restore) != cached {
			t.Errorf("%s restores the Go caches = %v, want %v:\n%s", id, !cached, cached, j)
		}
		if strings.Contains(j, "          cache: false\n") != cached {
			t.Errorf("%s turns setup-go's cache off = %v, want %v:\n%s", id, !cached, cached, j)
		}
		if !cached {
			if strings.Contains(j, save) {
				t.Errorf("%s saves the Go caches:\n%s", id, j)
			}

			continue
		}
		key := "          key: " + c.From + "-${{ runner.os }}-${{ hashFiles('go.sum') }}-${{ github.sha }}\n"
		keys := "          restore-keys: |\n            " + c.From + "-${{ runner.os }}-${{ hashFiles('go.sum') }}-\n            " + c.From + "-${{ runner.os }}-\n"
		if !strings.Contains(j, key) || !strings.Contains(j, keys) {
			t.Errorf("%s restores from a lineage other than %s's:\n%s", id, c.From, j)
		}
		if strings.Contains(j, save) != c.Saves {
			t.Errorf("%s saves the Go caches = %v, want %v:\n%s", id, !c.Saves, c.Saves, j)
		}
		if c.Saves {
			cond := "        if: ${{ !cancelled() && !startsWith(steps.go-cache.outputs.cache-matched-key, format('" + id + "-{0}-{1}-', runner.os, hashFiles('go.sum'))) }}\n"
			if !strings.Contains(j, cond) {
				t.Errorf("%s saves without the condition %q:\n%s", id, cond, j)
			}
			if !strings.HasSuffix(strings.TrimRight(j, "\n"), "          key: ${{ steps.go-cache.outputs.cache-primary-key }}") {
				t.Errorf("%s does not end with the save:\n%s", id, j)
			}
		}
	}
	wantWarm := []string{"go-build", "go-test", "go-test-skipauth"}
	if diff := cmp.Diff(wantWarm, jobIDs(t, cache)); diff != "" {
		t.Errorf("the cache-filling workflow's jobs mismatch (-want +got):\n%s", diff)
	}
	for _, id := range wantWarm {
		j := job(cache, id)
		if !strings.Contains(j, restore) || !strings.Contains(j, save) || !strings.Contains(j, "        if: ${{ !cancelled() }}\n") {
			t.Errorf("the cache-filling %s does not restore and save unconditionally:\n%s", id, j)
		}
		if !strings.Contains(j, "          key: "+id+"-${{ runner.os }}-${{ hashFiles('go.sum') }}-${{ github.sha }}\n") {
			t.Errorf("the cache-filling %s saves under another job's key:\n%s", id, j)
		}
	}
	for _, text := range []string{"    branches: [main, master, 'hotfix/**']\n", "  group: ci-cache-${{ github.ref }}\n", "permissions: {}\n"} {
		if !strings.Contains(string(cache), text) {
			t.Errorf("the cache-filling workflow lacks %q", text)
		}
	}
	// The image build is not cached: a plain docker build, no builder of its own.
	prImage := job(pr, "image")
	if !strings.Contains(prImage, `        run: docker build --build-arg VERSION=ci --build-arg COMMIT="$GITHUB_SHA" -t application:ci .`+"\n") || strings.Contains(prImage, "buildx") || strings.Contains(prImage, "cache") {
		t.Errorf("the image job is not the plain build:\n%s", prImage)
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

// lineOf is the line, counting from 1, where the owned file of an application with the
// flat workspace first reads as want, so a case names a line by its content.
func lineOf(t *testing.T, file, want string) int {
	t.Helper()

	rendered, err := ci.RenderFile(&app.App{WebApps: []app.WebApp{{Dir: "web"}}}, file)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(rendered), "\n") {
		if line == want {
			return i + 1
		}
	}
	t.Fatalf("no line %q in %s", want, file)

	return 0
}

// renderedLine is line n, counting from 1, of the owned file of an application with the
// flat workspace.
func renderedLine(t *testing.T, file string, n int) string {
	t.Helper()

	rendered, err := ci.RenderFile(&app.App{WebApps: []app.WebApp{{Dir: "web"}}}, file)
	if err != nil {
		t.Fatal(err)
	}

	return strings.Split(string(rendered), "\n")[n-1]
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
			want: &ci.Difference{File: ci.File, Missing: true}, wantText: ".github/workflows/ci.yml is missing",
		},
		{
			name: "the cache-filling workflow missing",
			app: func(t *testing.T) *app.App {
				t.Helper()
				a := workspaceApp(t, []string{"web"}, true)
				if err := os.Remove(a.Abs(ci.CacheFile)); err != nil {
					t.Fatal(err)
				}

				return a
			},
			want: &ci.Difference{File: ci.CacheFile, Missing: true}, wantText: ".github/workflows/ci-cache.yml is missing",
		},
		{
			name: "a hand edit in the cache-filling workflow",
			app: func(t *testing.T) *app.App {
				t.Helper()
				a := workspaceApp(t, []string{"web"}, true)
				data, err := os.ReadFile(a.Abs(ci.CacheFile))
				if err != nil {
					t.Fatal(err)
				}
				edited := strings.Replace(string(data), "  cancel-in-progress: true", "  cancel-in-progress: false", 1)
				if err := os.WriteFile(a.Abs(ci.CacheFile), []byte(edited), 0o600); err != nil {
					t.Fatal(err)
				}

				return a
			},
			want:     &ci.Difference{File: ci.CacheFile, Line: lineOf(t, ci.CacheFile, "  cancel-in-progress: true"), Want: "  cancel-in-progress: true", Got: "  cancel-in-progress: false"},
			wantText: fmt.Sprintf(`.github/workflows/ci-cache.yml:%d: the code renders "  cancel-in-progress: true"; the file has "  cancel-in-progress: false"`, lineOf(t, ci.CacheFile, "  cancel-in-progress: true")),
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
			want:     &ci.Difference{File: ci.File, Line: lineOf(t, ci.File, "  cancel-in-progress: true"), Want: "  cancel-in-progress: true", Got: "  cancel-in-progress: false"},
			wantText: fmt.Sprintf(`.github/workflows/ci.yml:%d: the code renders "  cancel-in-progress: true"; the file has "  cancel-in-progress: false"`, lineOf(t, ci.File, "  cancel-in-progress: true")),
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
				File: ci.File,
				Line: lineOf(t, ci.File, "  # The browser gate, one fixed name over the per-workspace jobs so a repository rule can require it: it fails when any of them did not succeed, and passes with nothing to check in an application without a browser workspace."),
				Want: "  # The browser workspace at apps/portal/web: bun installs from the lockfile exactly (bun ci), then the package scripts build, lint and test.",
				Got:  "  # The browser gate, one fixed name over the per-workspace jobs so a repository rule can require it: it fails when any of them did not succeed, and passes with nothing to check in an application without a browser workspace.",
			},
			wantText: fmt.Sprintf(`.github/workflows/ci.yml:%d: the code renders "  # The browser workspace at apps/portal/web: bun installs from the lockfile exactly (bun ci), then the package scripts build, lint and test."; the file has "  # The browser gate, one fixed name over the per-workspace jobs so a repository rule can require it: it fails when any of them did not succeed, and passes with nothing to check in an application without a browser workspace."`, lineOf(t, ci.File, "  # The browser gate, one fixed name over the per-workspace jobs so a repository rule can require it: it fails when any of them did not succeed, and passes with nothing to check in an application without a browser workspace.")),
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
			want:     &ci.Difference{File: ci.File, Line: 21, Want: renderedLine(t, ci.File, 21), Got: ""},
			wantText: fmt.Sprintf(`.github/workflows/ci.yml:21: the code renders %q; the file has ""`, renderedLine(t, ci.File, 21)),
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
	if diff := cmp.Diff([]ci.FileOutcome{{File: ci.File, Written: true}, {File: ci.CacheFile, Written: true}}, first.Files); diff != "" {
		t.Errorf("first Write().Files mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(ci.Files, first.WrittenFiles()); diff != "" {
		t.Errorf("first Write().WrittenFiles() mismatch (-want +got):\n%s", diff)
	}
	for _, file := range ci.Files {
		info, err := os.Stat(a.Abs(file))
		if err != nil {
			t.Fatalf("%s was not written: %v", file, err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Errorf("%s: mode = %o, want 644", file, info.Mode().Perm())
		}
	}
	again, err := ci.Write(a)
	if err != nil {
		t.Fatalf("second Write() error = %v", err)
	}
	if again.Written || len(again.WrittenFiles()) != 0 {
		t.Error("second Write() rewrote an unchanged file")
	}
	if diff := cmp.Diff([]ci.FileOutcome{{File: ci.File}, {File: ci.CacheFile}}, again.Files); diff != "" {
		t.Errorf("second Write().Files mismatch (-want +got):\n%s", diff)
	}
	if err := os.Remove(a.Abs(ci.CacheFile)); err != nil {
		t.Fatal(err)
	}
	third, err := ci.Write(a)
	if err != nil {
		t.Fatalf("third Write() error = %v", err)
	}
	if diff := cmp.Diff([]string{ci.CacheFile}, third.WrittenFiles()); diff != "" {
		t.Errorf("third Write().WrittenFiles() mismatch (-want +got):\n%s", diff)
	}
	if d, err := ci.Compare(a); err != nil || d != nil {
		t.Errorf("Compare() after Write() = %v, %v; want nil, nil", d, err)
	}
	if _, err := os.Stat(filepath.Join(a.Root, ".github", "workflows")); err != nil {
		t.Errorf("the workflows directory was not created: %v", err)
	}
}
