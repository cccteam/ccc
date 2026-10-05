// Package ci renders the application's CI workflow, .github/workflows/ci.yml, from the
// code, and owns it: impulse render writes it (impulse new, add site and remove site
// write it as part of their change), impulse check compares the committed file with what
// the code renders and fails a hand edit, and bedrock reads the job ids as the check
// names its repository rule requires. The workflow is one file of plain jobs with fixed
// ids; no job calls a reusable workflow of another repository, every action is pinned by
// commit with its tag in a comment, and every tool version is a constant here, so a
// change to a check is an impulse release and the pins move with it.
package ci

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// File is where the workflow lives in an application, root-relative.
const File = ".github/workflows/ci.yml"

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
// with the browser jobs left out (they follow go and are per application; web, the gate
// over them, follows them and is fixed). A required-checks rule names these.
var FixedChecks = []string{"title", "go", "web", "image", "secrets", "migrations"}

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
// file's order: title, go, one job per browser workspace, web, image, secrets,
// migrations. These are the check names the pull request reports.
func Checks(a *app.App) []string {
	workspaces := Workspaces(a)
	checks := make([]string, 0, len(FixedChecks)+len(workspaces))
	for _, c := range FixedChecks {
		checks = append(checks, c)
		if c == "go" {
			for _, w := range workspaces {
				checks = append(checks, w.Job)
			}
		}
	}

	return checks
}

//go:embed ci.yml.tmpl
var source string

// workflow is the template over the application's workspaces and the tool versions. The
// delimiters are [[ and ]] so GitHub's ${{ }} expressions read as themselves.
var workflow = template.Must(template.New("ci.yml").Delims("[[", "]]").Parse(source))

// data is what the template reads.
type data struct {
	Workspaces    []Workspace
	TitleTypes    []string
	GolangciLint  string
	Bun           string
	Govulncheck   string
	Semgrep       string
	SemgrepDigest string
	TruffleHog    string
}

// Render produces the workflow for the application: deterministic for one application
// and one impulse.
func Render(a *app.App) ([]byte, error) {
	var b bytes.Buffer
	d := data{
		Workspaces:    Workspaces(a),
		TitleTypes:    TitleTypes,
		GolangciLint:  GolangciLint,
		Bun:           Bun,
		Govulncheck:   Govulncheck,
		Semgrep:       Semgrep,
		SemgrepDigest: SemgrepDigest,
		TruffleHog:    TruffleHog,
	}
	if err := workflow.Execute(&b, d); err != nil {
		return nil, errors.Wrap(err, "template.Template.Execute()")
	}

	return b.Bytes(), nil
}

// Outcome reports what Write did.
type Outcome struct {
	// Written is true when the file was created or changed, false when it already read
	// as the code renders.
	Written bool
}

// Write renders the workflow and writes it at File when the committed file differs or is
// missing, creating .github/workflows. The file is a project file a person reads and
// commits, so it takes the conventional 0644.
func Write(a *app.App) (Outcome, error) {
	want, err := Render(a)
	if err != nil {
		return Outcome{}, err
	}
	target := a.Abs(File)
	if have, err := os.ReadFile(target); err == nil && bytes.Equal(have, want) {
		return Outcome{}, nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return Outcome{}, errors.Wrap(err, "os.MkdirAll()")
	}
	if err := os.WriteFile(target, want, 0o644); err != nil {
		return Outcome{}, errors.Wrap(err, "os.WriteFile()")
	}

	return Outcome{Written: true}, nil
}

// Difference is how the committed workflow departs from what the code renders.
type Difference struct {
	// Missing reports that the application has no workflow file at all.
	Missing bool
	// Line is the first differing line, counting from 1 (0 when Missing). Want is that
	// line as the code renders it and Got as the file has it; either is empty when that
	// side ended first.
	Line int
	Want string
	Got  string
}

// String says what differs in one line, for a check to print: the line, what the code
// renders there and what the file has (an empty string where either side has ended).
func (d *Difference) String() string {
	if d.Missing {
		return File + " is missing"
	}

	return fmt.Sprintf("%s:%d: the code renders %q; the file has %q", File, d.Line, d.Want, d.Got)
}

// Compare reads the committed workflow and compares it with what the code renders: nil
// when they are equal, otherwise the first difference.
func Compare(a *app.App) (*Difference, error) {
	want, err := Render(a)
	if err != nil {
		return nil, err
	}
	got, err := os.ReadFile(a.Abs(File))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Difference{Missing: true}, nil
		}

		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	if bytes.Equal(got, want) {
		return nil, nil
	}

	return firstDifference(want, got), nil
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
