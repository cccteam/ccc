package check

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
)

// testRunner verifies that every browser application project runs its component specs
// on Angular's unit-test builder, the runner ng new scaffolds: a test target on
// @angular/build:unit-test (Vitest under jsdom in Node, no browser), the spec tsconfig the
// target reads, and a package script running ng test for the project, so `bun run test`
// is the single-run form the Checks section and CI call. A project with no spec under its
// source root warns: the runner is wired and nothing runs on it yet.
type testRunner struct{}

func (testRunner) Name() string { return "test-runner" }

func (testRunner) Describe() string {
	return "each browser application project runs its specs on @angular/build:unit-test, with a spec tsconfig and a test script"
}

// unitTestBuilder is the builder every application project's test target names.
const unitTestBuilder = "@angular/build:unit-test"

// specTsConfig is the spec tsconfig the builder reads from the project root when the
// target names none.
const specTsConfig = "tsconfig.spec.json"

func (c testRunner) Run(_ context.Context, env *Env) Result {
	a := env.App
	if len(a.WebApps) == 0 {
		return skip(c.Name(), "no browser apps")
	}

	var details []string
	checked, problems, specless := 0, 0, 0
	for _, w := range a.WebApps {
		projects, err := a.ReadAngular(w.Dir)
		if err != nil {
			return fail(c.Name(), err.Error())
		}
		scripts, err := packageScripts(a.Abs(path.Join(w.Dir, "package.json")))
		if err != nil {
			return fail(c.Name(), err.Error())
		}
		for i := range projects {
			p := &projects[i]
			if p.ProjectType == "library" {
				continue
			}
			checked++
			findings, note, err := c.project(a, w.Dir, p, scripts)
			if err != nil {
				return fail(c.Name(), err.Error())
			}
			problems += len(findings)
			details = append(details, findings...)
			if note != "" {
				specless++
				details = append(details, note)
			}
		}
	}

	switch {
	case checked == 0:
		return skip(c.Name(), "no browser application project")
	case problems > 0:
		return fail(c.Name(), fmt.Sprintf("%d test wiring problem(s)", problems), details...)
	case specless > 0:
		return warn(c.Name(), fmt.Sprintf("%d browser project(s) run their specs on %s; %d without a spec yet", checked, unitTestBuilder, specless), details...)
	default:
		return pass(c.Name(), fmt.Sprintf("%d browser project(s) run their specs on %s", checked, unitTestBuilder))
	}
}

// project verifies one application project's wiring: the findings that fail the check,
// and the note that warns when the project has no spec yet.
func (testRunner) project(a *app.App, webDir string, p *app.AngularProject, scripts map[string]string) (findings []string, note string, err error) {
	workspace := path.Join(webDir, "angular.json")
	switch {
	case p.TestBuilder == "":
		findings = append(findings, fmt.Sprintf("%s: project %s has no test target (add one on %s)", workspace, p.Name, unitTestBuilder))
	case p.TestBuilder != unitTestBuilder:
		findings = append(findings, fmt.Sprintf("%s: project %s tests on %s, not %s", workspace, p.Name, p.TestBuilder, unitTestBuilder))
	}

	if p.TestTsConfig != "" {
		if _, err := os.Stat(a.Abs(path.Join(webDir, p.TestTsConfig))); err != nil {
			findings = append(findings, fmt.Sprintf("%s: project %s names tsConfig %s, which does not exist", workspace, p.Name, p.TestTsConfig))
		}
	} else if rel := path.Join(webDir, p.Root, specTsConfig); !exists(a.Abs(rel)) {
		findings = append(findings, fmt.Sprintf("%s: project %s's spec tsconfig does not exist (the test target names none, so the builder reads it from the project root)", rel, p.Name))
	}

	if !runsNgTest(scripts, p.Name) {
		findings = append(findings, fmt.Sprintf("%s: no script runs ng test %s (bun run test is the single-run form: ng test %s --watch=false)", path.Join(webDir, "package.json"), p.Name, p.Name))
	}

	source := path.Join(webDir, p.SourceRoot)
	found, err := hasSpec(a.Abs(source))
	if err != nil {
		return nil, "", err
	}
	if !found {
		note = fmt.Sprintf("%s: project %s has no *.spec.ts under %s yet; the runner is wired and nothing runs on it", webDir, p.Name, source)
	}

	return findings, note, nil
}

// exists reports whether a file is there.
func exists(abs string) bool {
	_, err := os.Stat(abs)

	return err == nil
}

// packageScripts returns the scripts of the package.json at abs by name; an absent
// manifest has none.
func packageScripts(abs string) (map[string]string, error) {
	data, err := os.ReadFile(abs)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): %s", abs)
	}

	return manifest.Scripts, nil
}

// runsNgTest reports whether any script runs ng test for the project, whatever runs ng
// (bun ng test console, ng test console --watch=false) and whatever follows.
func runsNgTest(scripts map[string]string, project string) bool {
	re := regexp.MustCompile(`(?:^|[\s"&;|(])ng test ` + regexp.QuoteMeta(project) + `(?:\s|$)`)
	for _, command := range scripts {
		if re.MatchString(command) {
			return true
		}
	}

	return false
}

// hasSpec reports whether a *.spec.ts file sits under the directory, dependencies and
// build products left out; a directory that is not there holds none.
func hasSpec(abs string) (bool, error) {
	found := false
	err := filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && p == abs {
				return fs.SkipAll
			}

			return errors.Wrap(err, "filepath.WalkDir()")
		}
		if d.IsDir() {
			if p != abs && skippedDirs[d.Name()] {
				return filepath.SkipDir
			}

			return nil
		}
		if strings.HasSuffix(d.Name(), ".spec.ts") {
			found = true

			return fs.SkipAll
		}

		return nil
	})
	if err != nil {
		return false, errors.Wrap(err, "filepath.WalkDir()")
	}

	return found, nil
}
