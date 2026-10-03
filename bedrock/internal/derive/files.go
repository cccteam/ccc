// files.go reads the two non-Go files the model depends on: the Dockerfile, for the
// variables the image sets and the release reaching its browser builds, and the
// development environment template, for the values it sets that the stack restates as
// defaults.

package derive

import (
	"os"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

const dockerfile = "Dockerfile"

var (
	// envAssignRE matches NAME=value or NAME="value" in a Dockerfile ENV instruction.
	envAssignRE = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)=`)
	// exportRE matches an active export line of the environment template.
	exportRE = regexp.MustCompile(`(?m)^\s*export\s+([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
	// stageRE matches a FROM instruction and captures the stage's name when it has one.
	stageRE = regexp.MustCompile(`(?m)^\s*FROM\s+\S+(?:\s+AS\s+(\S+))?`)
	// browserBuildRE matches the browser workspace build the image runs in a stage.
	browserBuildRE = regexp.MustCompile(`\bbun run build\b`)
	// versionArgRE matches the ARG instruction that brings VERSION into a stage.
	versionArgRE = regexp.MustCompile(`(?m)^\s*ARG\s+VERSION(?:=\S*)?\s*$`)
)

// versionArg is the build argument the release reaches the image through.
const versionArg = "VERSION"

// dockerfileEnv lists the variables the Dockerfile's ENV instructions set, or nil
// without a Dockerfile.
func dockerfileEnv(a *app.App) (map[string]bool, error) {
	data, err := os.ReadFile(a.Abs(dockerfile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "os.ReadFile()")
	}

	set := map[string]bool{}
	// An ENV instruction continues over lines ending in a backslash.
	for _, instruction := range strings.Split(strings.ReplaceAll(string(data), "\\\n", " "), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(instruction), "ENV ")
		if !ok {
			continue
		}
		for _, m := range envAssignRE.FindAllStringSubmatch(rest, -1) {
			set[m[1]] = true
		}
	}

	return set, nil
}

// dockerfileBrowserStages refuses a Dockerfile whose browser build stage does not declare
// the VERSION argument: a build argument declared before the first FROM is not visible
// inside a stage until the stage declares it again, and the browser bundle is stamped
// with the release in that stage, so without the declaration every bundle is built as
// dev and never sends its release. Without a Dockerfile there is nothing to refuse.
func dockerfileBrowserStages(a *app.App) error {
	data, err := os.ReadFile(a.Abs(dockerfile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return errors.Wrap(err, "os.ReadFile()")
	}

	return checkBrowserStages(string(data))
}

// checkBrowserStages is dockerfileBrowserStages over the Dockerfile's text.
func checkBrowserStages(text string) error {
	// Comments never run a build and never declare an argument.
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		lines = append(lines, line)
	}
	body := strings.Join(lines, "\n")
	starts := stageRE.FindAllStringSubmatchIndex(body, -1)
	for i, start := range starts {
		end := len(body)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		stage := body[start[0]:end]
		if !browserBuildRE.MatchString(stage) {
			continue
		}
		if versionArgRE.MatchString(stage) {
			continue
		}
		name := "the browser build stage"
		if start[2] >= 0 {
			name = "stage " + body[start[2]:start[3]]
		}

		return errors.Newf("%s: %s runs the browser build and does not declare ARG %s; a build argument is visible inside a stage only after the stage declares it, and the release is stamped into each bundle there, so declare it (ARG %s) before the build", dockerfile, name, versionArg, versionArg)
	}

	return nil
}

// envTemplateValues reads the values the development environment template exports.
func envTemplateValues(a *app.App) (map[string]string, error) {
	values := map[string]string{}
	if a.EnvTemplate == "" {
		return values, nil
	}
	data, err := os.ReadFile(a.Abs(a.EnvTemplate))
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	for _, m := range exportRE.FindAllStringSubmatch(string(data), -1) {
		values[m[1]] = strings.TrimSpace(m[2])
	}

	return values, nil
}
