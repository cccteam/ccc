// releasesections.go checks release-please's configuration for the title types the
// application's CI accepts: release-please drops a commit whose type has no changelog
// section exactly as it drops a hidden one, so a pull request whose squash title carries
// a type without a section opens no release pull request, and a merge of only such titles
// never reaches an environment. An upgrade (new pins, new pipeline) or an infrastructure
// change (applied only in a tag build) must not wait for a later releasing merge, so every
// type impulse's title check accepts has a section; whether it is hidden is the
// application's choice.

package check

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/ci"
)

// ReleaseSectionFinding is a release-please changelog-sections list that lacks a type
// the title check accepts.
type ReleaseSectionFinding struct {
	// Path is the configuration file, relative to the application root.
	Path string
	// Package is the package whose own changelog-sections lacks the types, empty for the
	// top level.
	Package string
	// Missing are the accepted types without a section, in the title check's order.
	Missing []string
}

// changelogSections is release-please's key for the sections, at the top level or for a
// package (a package's own list replaces the top level's for it).
const changelogSections = "changelog-sections"

// defaultSections are the types release-please's default changelog sections cover when a
// configuration carries no changelog-sections at all (DEFAULT_CHANGELOG_SECTIONS: feat,
// feature, fix, perf and revert shown; docs, style, chore, refactor, test, build and ci
// hidden). A type outside them is dropped under the defaults.
var defaultSections = strings.Fields("feat feature fix perf revert docs style chore refactor test build ci")

// scanReleaseSections reads release-please's configuration at the application root and
// reports, at the top level and for each package with a changelog-sections of its own,
// the types impulse's title check accepts that have no section. An application without
// the file has nothing to check here: Run refuses its absence on its own (ReleaseFiles).
func scanReleaseSections(appDir string) ([]ReleaseSectionFinding, error) {
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
	var findings []ReleaseSectionFinding
	if missing := missingSections(config, true); len(missing) > 0 {
		findings = append(findings, ReleaseSectionFinding{Path: releasePleaseConfig, Missing: missing})
	}
	packages, _ := config["packages"].(map[string]any)
	names := make([]string, 0, len(packages))
	for name := range packages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		settings, ok := packages[name].(map[string]any)
		if !ok {
			continue
		}
		if missing := missingSections(settings, false); len(missing) > 0 {
			findings = append(findings, ReleaseSectionFinding{Path: releasePleaseConfig, Package: name, Missing: missing})
		}
	}

	return findings, nil
}

// missingSections lists the accepted types one level of the configuration gives no
// section. A level without the key covers release-please's defaults when it is the top
// level, and inherits the top level's list otherwise (nothing missing of its own).
func missingSections(settings map[string]any, top bool) []string {
	sections, ok := settings[changelogSections].([]any)
	covered := map[string]bool{}
	switch {
	case ok:
		for _, s := range sections {
			if section, ok := s.(map[string]any); ok {
				if typ, ok := section["type"].(string); ok {
					covered[typ] = true
				}
			}
		}
	case top:
		for _, typ := range defaultSections {
			covered[typ] = true
		}
	default:
		return nil
	}
	var missing []string
	for _, typ := range ci.TitleTypes {
		if !covered[typ] {
			missing = append(missing, typ)
		}
	}

	return missing
}

// Problem says what is wrong, the way a person reads it: the level, the types and the fix.
func (f ReleaseSectionFinding) Problem() string {
	where := changelogSections
	if f.Package != "" {
		where = "package " + f.Package + "'s " + changelogSections
	}

	return where + " has no entry for " + joinTypes(f.Missing) + ": release-please drops a merge whose type has no section exactly as it drops a hidden one, so a pull request of only such titles opens no release pull request and never reaches an environment; add an entry per type (hidden or not is the application's choice; the seeded configuration, bedrock render into an empty directory, lists every type the CI's title check accepts)"
}

// joinTypes writes types the way a sentence lists them, each backticked.
func joinTypes(types []string) string {
	quoted := make([]string, len(types))
	for i, t := range types {
		quoted[i] = "`" + t + "`"
	}

	return joinEnvironments(quoted)
}
