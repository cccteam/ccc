// Package ci renders the application's CI workflows from the code, and owns them:
// .github/workflows/ci.yml, the checks on every pull request, and
// .github/workflows/ci-cache.yml, the run after a push to the default branch or a hotfix
// branch that fills the caches the checks restore. impulse render writes them (impulse
// new, add site and remove site write them as part of their change), impulse check
// compares the committed files with what the code renders and fails a hand edit, and
// bedrock reads the job ids as the check names its repository rule requires. The
// workflows are plain jobs with fixed ids; no job calls a reusable workflow of another
// repository, every action is pinned by commit with its tag in a comment, and every tool
// version is a constant here, so a change to a check is an impulse release and the pins
// move with it.
//
// The caches never serve a stale result. The Go module cache holds downloads go.sum names,
// each verified against it when read, and the Go build cache is addressed by the hash of
// an action's inputs (the toolchain, the flags, the sources, the dependencies' outputs),
// so an entry from an older commit is either exactly what the current one would compute
// or unused; the jobs restore the nearest entry they find for that reason. The image
// build's two download stages are cached by BuildKit, which serves a layer only for the
// same lockfile. Go's cached test results are the one reuse whose inputs Go cannot see
// in full (a test that reads a running emulator, say), so they are off unless the
// application's //impulse:ci line turns them on. Nothing else is cached: the vulnerability
// databases, Semgrep's rules and the emulator images are fetched when the job runs.
package ci

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"text/template"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// File is where the pull request workflow lives in an application, root-relative.
const File = ".github/workflows/ci.yml"

// CacheFile is where the cache-filling workflow lives: the run after a push to the
// default branch (main or master) or a hotfix branch that builds and tests at the
// branch's head and saves the caches under the same keys the pull request jobs restore,
// so a pull request's first run starts from the branch it targets.
const CacheFile = ".github/workflows/ci-cache.yml"

// Files are the workflows impulse owns, in the order Write writes and Compare reads them.
var Files = []string{File, CacheFile}

// The tool versions the workflow pins. Each is written here once and read into the
// template, so a bump is one edit and an impulse release.
const (
	// GolangciLint is the golangci-lint release the skeleton's .golangci.yml is written for.
	GolangciLint = "v2.12.2"
	// Bun is the Bun that wrote the skeleton's bun.lock, so CI installs with the same one.
	Bun = "1.4.0"
	// Govulncheck is the govulncheck release run over the module.
	Govulncheck = "v1.8.0"
	// Semgrep is the Semgrep release, and SemgrepDigest the digest of the semgrep/semgrep
	// image at that tag (the manifest list, so the runner's platform resolves), since an
	// image tag can move and a digest cannot.
	Semgrep       = "1.179.0"
	SemgrepDigest = "sha256:93963d9295a366f59e4850127b1550400ee7b388f04fe144e4a1f6325d96e01b"
	// TruffleHog is the TruffleHog release: the action is pinned at it, and the action's
	// version input runs the scanner at it too, since the action alone would run latest.
	TruffleHog = "3.95.7"
)

// FixedChecks are the job ids every application's workflow carries, in the file's order
// with the Go legs and the browser jobs left out (the legs precede go, the gate over them;
// the browser jobs follow go and are per application, and web, the gate over them,
// follows them and is fixed). A required-checks rule names these.
var FixedChecks = []string{"title", goGate, webGate, image, "secrets", "migrations"}

// The gates: goGate over the Go legs, webGate over the browser jobs.
const (
	goGate  = "go"
	webGate = "web"
)

// The Go legs and the image build this file names more than once.
const (
	goBuild        = "go-build"
	goTest         = "go-test"
	goTestSkipAuth = "go-test-skipauth"
	goVuln         = "go-vuln"
	goCheck        = "go-check"
	image          = "image"
)

// GoJobs are the Go legs, in the file's order: the jobs the go gate needs, each running at
// once with the others.
var GoJobs = []string{goBuild, goTest, goTestSkipAuth, "go-lint", "go-lint-skipauth", goVuln, "go-semgrep", goCheck}

// LargeRunnerVariable is the GitHub Actions variable (on the repository or the
// organization) naming the larger runner: a runner label or a runner group. The jobs the
// application's //impulse:ci line lists under large-runner (DefaultLargeRunner without
// one) run on it while it is set, and on GitHub's standard runner while it is unset, as
// every other job does.
const LargeRunnerVariable = "CI_LARGE_RUNNER"

// DefaultLargeRunner are the jobs on the larger runner when the application's
// //impulse:ci line does not say: the two test legs and the image build, the jobs that
// take the most machine.
var DefaultLargeRunner = []string{goTest, goTestSkipAuth, image}

// StandardRunner is the runner every job runs on while LargeRunnerVariable is unset, and
// the jobs not on the larger runner always.
const StandardRunner = "ubuntu-latest"

// Settings are what the application's //impulse:ci line decides, with impulse's defaults
// where the line, or the application, says nothing.
type Settings struct {
	// LargeRunner are the jobs that run on the runner LargeRunnerVariable names, by job
	// id: DefaultLargeRunner, or the line's large-runner (none is an empty list).
	LargeRunner []string
	// TestCache reports that the test legs reuse Go's cached test results, which carry
	// over between runs in the saved build cache: off (go test -count=1) unless the line
	// says test-cache=on. Go reuses a passed test's result when the test binary, the
	// files it read and the environment it read are unchanged; it cannot see a running
	// emulator's image, so an application turns this on when its tests' inputs are all
	// in the tree or pinned by digest.
	TestCache bool
}

// SettingsOf reads the application's //impulse:ci line into Settings, filling the
// defaults, and refuses a large-runner job the workflow does not render: a job id must
// be one of Checks(a) and not a gate (go and web run nothing).
func SettingsOf(a *app.App) (Settings, error) {
	s := Settings{LargeRunner: DefaultLargeRunner}
	if a.CI == nil {
		return s, nil
	}
	if a.CI.TestCache != nil {
		s.TestCache = *a.CI.TestCache
	}
	if a.CI.LargeRunner == nil {
		return s, nil
	}
	rendered := Checks(a)
	for _, job := range a.CI.LargeRunner {
		switch {
		case job == goGate || job == webGate:
			return Settings{}, errors.Newf("%s:%d: //impulse:ci large-runner names %s, a gate, which runs nothing; the jobs are %s", a.CI.File, a.CI.Line, job, strings.Join(selectable(rendered), ", "))
		case !slices.Contains(rendered, job):
			return Settings{}, errors.Newf("%s:%d: //impulse:ci large-runner names %q, which the workflow does not render; the jobs are %s", a.CI.File, a.CI.Line, job, strings.Join(selectable(rendered), ", "))
		}
	}
	s.LargeRunner = a.CI.LargeRunner

	return s, nil
}

// selectable lists the jobs large-runner may name: the rendered jobs less the gates.
func selectable(rendered []string) []string {
	jobs := make([]string, 0, len(rendered))
	for _, job := range rendered {
		if job != goGate && job != webGate {
			jobs = append(jobs, job)
		}
	}

	return jobs
}

// runsOn maps every rendered job to its runs-on value: the expression reading
// LargeRunnerVariable for the jobs on the larger runner, StandardRunner for the rest.
func runsOn(rendered []string, s Settings) map[string]string {
	on := make(map[string]string, len(rendered))
	for _, job := range rendered {
		on[job] = StandardRunner
		if slices.Contains(s.LargeRunner, job) {
			on[job] = "${{ vars." + LargeRunnerVariable + " || '" + StandardRunner + "' }}"
		}
	}

	return on
}

// TitleTypes are the conventional-commit types the title check accepts, in the order the
// workflow lists them: first the types a merge of which releases (feat and feature a
// minor version, the rest a patch), then the types release-please hides, a merge of
// which alone opens no release pull request. bedrock seeds a changelog section per type
// into release-please-config.json and its check refuses a configuration missing one,
// since release-please drops a merge whose type has no section exactly as it drops a
// hidden one, so a merge of only such titles would never deploy.
var TitleTypes = []string{"feat", "feature", "fix", "perf", "revert", "docs", "deps", "upgrade", "infra", "config", "style", "chore", "refactor", "cleanup", "test", "build", "ci"}

// Workspace is one browser workspace with the id of the job that installs, builds, lints
// and tests it.
type Workspace struct {
	// Dir is the workspace directory, root-relative (where angular.json is).
	Dir string
	// Job is the id of its job in the workflow, and so the check name the pull request
	// reports.
	Job string
}

// jobIDChar is what a job id may not contain: GitHub allows letters, digits, - and _.
var jobIDChar = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// webDir is the flat layout's browser workspace, and the directory name a site's
// workspace ends in.
const webDir = "web"

// Workspaces lists the application's browser workspaces, sorted by directory, each with
// its job id: angular-web for the workspace at web, angular-<site> for apps/<site>/web
// (the directory impulse add site gives a site's workspace), and for any other directory
// angular- followed by the path's segments joined with -, a trailing web dropped
// (web/console is angular-web-console, apps/pilots/gui is angular-apps-pilots-gui); a
// workspace at the application root is angular-root.
func Workspaces(a *app.App) []Workspace {
	workspaces := make([]Workspace, 0, len(a.WebApps))
	for _, w := range a.WebApps {
		workspaces = append(workspaces, Workspace{Dir: w.Dir, Job: jobID(w.Dir)})
	}
	sort.Slice(workspaces, func(i, j int) bool { return workspaces[i].Dir < workspaces[j].Dir })

	return workspaces
}

// jobID names the job of a workspace directory.
func jobID(dir string) string {
	segments := strings.Split(strings.Trim(dir, "/"), "/")
	switch {
	case dir == "." || dir == "":
		return "angular-root"
	case len(segments) == 1 && segments[0] == webDir:
		return "angular-web"
	case len(segments) == 3 && segments[0] == "apps" && segments[2] == webDir:
		return "angular-" + jobIDChar.ReplaceAllString(segments[1], "-")
	}
	if len(segments) > 1 && segments[len(segments)-1] == webDir {
		segments = segments[:len(segments)-1]
	}

	return "angular-" + jobIDChar.ReplaceAllString(strings.Join(segments, "-"), "-")
}

// Checks lists the job ids of the workflow Render produces for the application, in the
// file's order: title, the Go legs, go, one job per browser workspace, web, image,
// secrets, migrations. These are the check names the pull request reports.
func Checks(a *app.App) []string {
	workspaces := Workspaces(a)
	checks := make([]string, 0, len(FixedChecks)+len(GoJobs)+len(workspaces))
	for _, c := range FixedChecks {
		if c == goGate {
			checks = append(checks, GoJobs...)
		}
		checks = append(checks, c)
		if c == goGate {
			for _, w := range workspaces {
				checks = append(checks, w.Job)
			}
		}
	}

	return checks
}

//go:embed ci.yml.tmpl ci-cache.yml.tmpl steps.tmpl
var sources embed.FS

// workflows are the templates, one per owned file, over the application's workspaces,
// its settings and the tool versions, with steps.tmpl's definitions of the steps both
// files carry (the restore and the save of the Go caches, the files' modification times,
// the image build's cached stages). The delimiters are [[ and ]] so GitHub's ${{ }}
// expressions read as themselves.
var workflows = template.Must(template.New("workflows").Delims("[[", "]]").Funcs(template.FuncMap{"list": List}).ParseFS(sources, "ci.yml.tmpl", "ci-cache.yml.tmpl", "steps.tmpl"))

// List writes names as prose: a; a and b; a, b and c.
func List(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}

	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// data is what the templates read.
type data struct {
	Workspaces []Workspace
	GoJobs     []string
	TitleTypes []string
	// RunsOn is each rendered job's runs-on value, by job id.
	RunsOn map[string]string
	// LargeJobs are the jobs on the larger runner, for the header's sentence.
	LargeJobs []string
	// TestCache is the test-cache setting; TestFlags is what it adds to go test's
	// flags: -count=1 (then a space) while the cached results are off, nothing while on.
	TestCache bool
	TestFlags string
	// Cache is each Go leg's use of the saved Go caches, by job id (CachedJobs).
	Cache         map[string]CachedJob
	GolangciLint  string
	Bun           string
	Govulncheck   string
	Semgrep       string
	SemgrepDigest string
	TruffleHog    string
}

// CachedJob is one Go leg's use of the saved Go caches.
type CachedJob struct {
	// Job is the leg; From is the job whose entries it restores (itself, or go-build);
	// Saves reports that the pull request run saves an entry of its own when it found
	// none for its go.sum.
	Job, From string
	Saves     bool
}

// CachedJobs lists the Go legs that restore the saved Go caches (the module cache and
// the build cache), in the file's order: go-build, go-test and go-test-skipauth each
// restore the nearest entry of their own lineage and save one when no entry for their
// go.sum existed, since their builds differ (the race detector, the tags); go-vuln and
// go-check restore go-build's, which holds what their compiles share with it, and save
// nothing. The lint legs keep setup-go's cache and golangci-lint's own.
var CachedJobs = []CachedJob{
	{Job: goBuild, From: goBuild, Saves: true},
	{Job: goTest, From: goTest, Saves: true},
	{Job: goTestSkipAuth, From: goTestSkipAuth, Saves: true},
	{Job: goVuln, From: goBuild},
	{Job: goCheck, From: goBuild},
}

// Render produces the pull request workflow (File) for the application: deterministic
// for one application and one impulse.
func Render(a *app.App) ([]byte, error) {
	return render(a, File)
}

// RenderFile produces one owned workflow, File or CacheFile.
func RenderFile(a *app.App, file string) ([]byte, error) {
	return render(a, file)
}

// render executes the template of the named file over the application.
func render(a *app.App, file string) ([]byte, error) {
	settings, err := SettingsOf(a)
	if err != nil {
		return nil, err
	}
	rendered := Checks(a)
	d := data{
		Workspaces:    Workspaces(a),
		GoJobs:        GoJobs,
		TitleTypes:    TitleTypes,
		RunsOn:        runsOn(rendered, settings),
		LargeJobs:     settings.LargeRunner,
		TestCache:     settings.TestCache,
		Cache:         make(map[string]CachedJob, len(CachedJobs)),
		GolangciLint:  GolangciLint,
		Bun:           Bun,
		Govulncheck:   Govulncheck,
		Semgrep:       Semgrep,
		SemgrepDigest: SemgrepDigest,
		TruffleHog:    TruffleHog,
	}
	for _, c := range CachedJobs {
		d.Cache[c.Job] = c
	}
	if !settings.TestCache {
		d.TestFlags = "-count=1 "
	}
	var b bytes.Buffer
	if err := workflows.ExecuteTemplate(&b, path.Base(file)+".tmpl", d); err != nil {
		return nil, errors.Wrap(err, "template.Template.ExecuteTemplate()")
	}

	return b.Bytes(), nil
}

// Outcome reports what Write did.
type Outcome struct {
	// Written is true when a file was created or changed, false when every file
	// already read as the code renders.
	Written bool
	// Files says it per file, in Files' order.
	Files []FileOutcome
}

// FileOutcome is Write's outcome for one owned file.
type FileOutcome struct {
	File    string
	Written bool
}

// WrittenFiles lists the files Write created or changed, in Files' order.
func (o Outcome) WrittenFiles() []string {
	var files []string
	for _, f := range o.Files {
		if f.Written {
			files = append(files, f.File)
		}
	}

	return files
}

// Write renders the workflows and writes each at its path when the committed file
// differs or is missing, creating .github/workflows. The files are project files a
// person reads and commits, so they take the conventional 0644.
func Write(a *app.App) (Outcome, error) {
	var outcome Outcome
	for _, file := range Files {
		want, err := render(a, file)
		if err != nil {
			return Outcome{}, err
		}
		target := a.Abs(file)
		if have, err := os.ReadFile(target); err == nil && bytes.Equal(have, want) {
			outcome.Files = append(outcome.Files, FileOutcome{File: file})

			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return Outcome{}, errors.Wrap(err, "os.MkdirAll()")
		}
		if err := os.WriteFile(target, want, 0o644); err != nil {
			return Outcome{}, errors.Wrap(err, "os.WriteFile()")
		}
		outcome.Written = true
		outcome.Files = append(outcome.Files, FileOutcome{File: file, Written: true})
	}

	return outcome, nil
}

// Difference is how a committed workflow departs from what the code renders.
type Difference struct {
	// File is the owned file that differs.
	File string
	// Missing reports that the application has no such file at all.
	Missing bool
	// Line is the first differing line, counting from 1 (0 when Missing). Want is that
	// line as the code renders it and Got as the file has it; either is empty when that
	// side ended first.
	Line int
	Want string
	Got  string
}

// String says what differs in one line, for a check to print: the file and line, what
// the code renders there and what the file has (an empty string where either side has
// ended).
func (d *Difference) String() string {
	if d.Missing {
		return d.File + " is missing"
	}

	return fmt.Sprintf("%s:%d: the code renders %q; the file has %q", d.File, d.Line, d.Want, d.Got)
}

// Compare reads the committed workflows and compares each with what the code renders:
// nil when all are equal, otherwise the first difference, in Files' order.
func Compare(a *app.App) (*Difference, error) {
	for _, file := range Files {
		want, err := render(a, file)
		if err != nil {
			return nil, err
		}
		got, err := os.ReadFile(a.Abs(file))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return &Difference{File: file, Missing: true}, nil
			}

			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		if bytes.Equal(got, want) {
			continue
		}
		d := firstDifference(want, got)
		d.File = file

		return d, nil
	}

	return nil, nil
}

// firstDifference finds the first line two unequal texts disagree on; a side that has
// ended reads as empty lines from there on.
func firstDifference(want, got []byte) *Difference {
	wantLines := strings.Split(string(want), "\n")
	gotLines := strings.Split(string(got), "\n")
	for i := range max(len(wantLines), len(gotLines)) {
		w, g := "", ""
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			return &Difference{Line: i + 1, Want: w, Got: g}
		}
	}

	return nil
}
