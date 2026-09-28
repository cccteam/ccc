package check

import (
	"context"
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"
)

// ciWorkflow verifies that the application runs the shared CI on every pull request:
// .github/workflows/ci.yml wires the workflows of cccteam/github-workflows (the Go and
// Angular checks, the image scan, the semantic titles, code and secret scanning, schema
// protection), every uses: line pinned at the version the skeleton carries, and each
// browser workspace has an Angular job of its own. A missing file is a failure: the
// pull requests run no checks at all. An older pin warns: the checks run, at a version
// behind the skeleton's.
type ciWorkflow struct{}

func (ciWorkflow) Name() string { return "ci-workflow" }

func (ciWorkflow) Describe() string {
	return "the shared CI workflows run on every pull request from .github/workflows/ci.yml, pinned at the skeleton's version, with an Angular job per browser workspace (an older pin WARNs)"
}

// CIWorkflowFile is where the skeleton keeps the CI workflow.
const CIWorkflowFile = ".github/workflows/ci.yml"

// WorkflowsPin is the commit of cccteam/github-workflows the skeleton's workflow pins
// (its tag in the comment), and the version the check compares an application's with.
const (
	WorkflowsPin     = "7f103553e4cb3524d127299a6ac70d85156d36b3"
	WorkflowsVersion = "v7.0.0"
)

// usesRE matches a uses: line calling one of the shared workflows, capturing the
// workflow's name and the ref.
var usesRE = regexp.MustCompile(`(?m)^\s*uses:\s*cccteam/github-workflows/\.github/workflows/([a-z-]+)\.yml@(\S+)`)

// workingDirRE matches an Angular job's working directory.
var workingDirRE = regexp.MustCompile(`(?m)^\s*working-directory:\s*"?\.?/?([^"\s]+)"?`)

// requiredWorkflows are the shared workflows every application wires.
var requiredWorkflows = []string{"semantic-pull-request-title", "golang-ci", "angular-ci", "codeql", "secrets-scanning", "schema-protection"}

func (c ciWorkflow) Run(_ context.Context, env *Env) Result {
	a := env.App
	data, err := os.ReadFile(a.Abs(CIWorkflowFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fail(c.Name(), fmt.Sprintf("%s is missing: the pull requests run no Go or Angular checks, no image scan, no code or secret scanning and no schema protection", CIWorkflowFile), "copy the skeleton's workflow (impulse render <candidate> and take .github/workflows/ci.yml and .github/codeql-config.yml), with an angular-ci job per browser workspace")
		}

		return fail(c.Name(), fmt.Sprintf("%s: %v", CIWorkflowFile, err))
	}
	text := string(data)
	wired := map[string]bool{}
	var details, warnings []string
	for _, m := range usesRE.FindAllStringSubmatch(text, -1) {
		wired[m[1]] = true
		if m[2] != WorkflowsPin {
			warnings = append(warnings, fmt.Sprintf("%s: %s.yml is pinned at %s; the skeleton pins %s (%s)", CIWorkflowFile, m[1], m[2], WorkflowsPin, WorkflowsVersion))
		}
	}
	for _, name := range requiredWorkflows {
		if !wired[name] {
			details = append(details, fmt.Sprintf("%s: no job uses cccteam/github-workflows/.github/workflows/%s.yml", CIWorkflowFile, name))
		}
	}
	dirs := map[string]bool{}
	for _, m := range workingDirRE.FindAllStringSubmatch(text, -1) {
		dirs[path.Clean(m[1])] = true
	}
	for _, w := range a.WebApps {
		if !dirs[path.Clean(w.Dir)] {
			details = append(details, fmt.Sprintf("%s: no angular-ci job has working-directory ./%s; the %s workspace is never built, linted or tested in CI", CIWorkflowFile, w.Dir, w.Dir))
		}
	}
	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d CI wiring problem(s)", len(details)), append(details, warnings...)...)
	}
	summary := fmt.Sprintf("%d shared workflow(s) wired at %s, %d browser workspace(s) with an Angular job", len(wired), WorkflowsVersion, len(a.WebApps))
	if len(warnings) > 0 {
		return warn(c.Name(), strings.Replace(summary, "wired at "+WorkflowsVersion, "wired, some behind the skeleton's "+WorkflowsVersion, 1), warnings...)
	}

	return pass(c.Name(), summary)
}
