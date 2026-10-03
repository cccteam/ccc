// releaselines.go checks release-please's configuration for the hotfix model: a hotfix
// line is v<major>.<minor>.x, the line of the release production runs, so a feature
// release must advance the minor, below 1.0 as above it. release-please's
// bump-patch-for-minor-pre-major makes a feature bump the patch below 1.0, which keeps
// every feature on production's line and leaves a hotfix of production's release no
// number of its own.

package check

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/go-playground/errors/v5"
)

// ReleaseLineFinding is a release-please setting under which a feature release does not
// open a new hotfix line.
type ReleaseLineFinding struct {
	// Path is the configuration file, relative to the application root.
	Path string
	// Problem says what is wrong, the way a person reads it.
	Problem string
}

// releasePleaseConfig is release-please's configuration file at the application root,
// the one the rendered release workflow names.
const releasePleaseConfig = "release-please-config.json"

// patchForFeature is the setting that puts a feature on the patch below 1.0.
const patchForFeature = "bump-patch-for-minor-pre-major"

// patchForFeatureProblem is the finding's text.
const patchForFeatureProblem = patchForFeature + " is true: below 1.0 a feature release bumps the patch and stays on the line production runs (v<major>.<minor>.x), so a hotfix of production's release cannot be numbered (the line's next patch is taken) nor pass the hotfix check (the feature's migrations are what the environment would be restored to); set it to false, so a feature opens a new line, a fix bumps the patch and a breaking change the minor"

// scanReleaseLines reads release-please's configuration at the application root and
// reports bump-patch-for-minor-pre-major set to true, at the top level or for a package
// (a package's setting overrides the top level's for it). The file is read by key, as
// release-please names them. An application without the file has nothing to check here:
// the release workflow needs the file and says so itself.
func scanReleaseLines(appDir string) ([]ReleaseLineFinding, error) {
	src, err := os.ReadFile(filepath.Join(appDir, releasePleaseConfig))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, errors.Wrapf(err, "os.ReadFile(): %s", releasePleaseConfig)
	}
	var config map[string]any
	if err := json.Unmarshal(src, &config); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): %s", releasePleaseConfig)
	}
	var findings []ReleaseLineFinding
	if patchForFeatureOn(config) {
		findings = append(findings, ReleaseLineFinding{Path: releasePleaseConfig, Problem: patchForFeatureProblem})
	}
	packages, _ := config["packages"].(map[string]any)
	names := make([]string, 0, len(packages))
	for name := range packages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if settings, ok := packages[name].(map[string]any); ok && patchForFeatureOn(settings) {
			findings = append(findings, ReleaseLineFinding{Path: releasePleaseConfig, Problem: "package " + name + ": " + patchForFeatureProblem})
		}
	}

	return findings, nil
}

// patchForFeatureOn reads the setting from one level of the configuration.
func patchForFeatureOn(settings map[string]any) bool {
	on, ok := settings[patchForFeature].(bool)

	return ok && on
}
