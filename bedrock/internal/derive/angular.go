// angular.go reads where a browser workspace's build writes its application's bundle: the
// application project's build target in angular.json and its outputPath, in either of
// the forms Angular takes it in. The sites layout serves each site's bundle from that
// directory when the Procfile names none, and bedrock check holds the Procfile's to it.

package derive

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// The workspace file and the words of its build target the output is read from.
const (
	angularJSONName    = "angular.json"
	angularApplication = "application"
	angularBuildTarget = "build"
	// angularDist is the directory under the workspace Angular writes to without an
	// outputPath, under the project's name; angularBrowserDir the subdirectory the
	// application builder writes the browser bundle to unless the project sets another
	// or none.
	angularDist       = "dist"
	angularBrowserDir = "browser"
)

// angularJSON is the workspace file as far as the output is read: its projects, each with
// its type and its targets under either key Angular takes them by.
type angularJSON struct {
	Projects map[string]angularProject `json:"projects"`
}

// angularProject is one project: an application unless its type says otherwise, with its
// targets under architect or targets.
type angularProject struct {
	ProjectType string                   `json:"projectType"`
	Architect   map[string]angularTarget `json:"architect"`
	Targets     map[string]angularTarget `json:"targets"`
}

// angularTarget is one target: its builder and the one build option read, outputPath, a
// string or an object, kept raw until it is read.
type angularTarget struct {
	Builder string `json:"builder"`
	Options struct {
		OutputPath json.RawMessage `json:"outputPath"`
	} `json:"options"`
}

// angularOutputPath is outputPath's object form: the base directory and the browser
// bundle's subdirectory under it, which the application builder defaults to browser and
// an empty string turns off.
type angularOutputPath struct {
	Base    string  `json:"base"`
	Browser *string `json:"browser"`
}

// bundleOutput is the root-relative directory the workspace's build writes its
// application's browser bundle to, read from angular.json: the application project's
// build target's outputPath, as a string (the directory, under which the application
// builder writes the browser bundle in browser/, and the browser builder of old wrote it
// directly) or an object (base, with browser the subdirectory under it: browser unless
// the project sets it, none when set empty), or dist/<project> without one, the default.
// Empty when the workspace has no application project or several, since then there is
// no one bundle to read.
func bundleOutput(a *app.App, workspace string) (string, error) {
	rel := path.Join(workspace, angularJSONName)
	src, err := os.ReadFile(a.Abs(rel))
	if err != nil {
		return "", errors.Wrapf(err, "os.ReadFile(): %s", rel)
	}
	var ws angularJSON
	if err := json.Unmarshal(src, &ws); err != nil {
		return "", errors.Wrapf(err, "json.Unmarshal(): %s", rel)
	}
	var names []string
	for name, p := range ws.Projects {
		if p.ProjectType == "" || p.ProjectType == angularApplication {
			names = append(names, name)
		}
	}
	if len(names) != 1 {
		return "", nil
	}
	name := names[0]
	project := ws.Projects[name]
	build := project.target(angularBuildTarget)
	dir, err := build.outputDir(name, rel+": project "+name)
	if err != nil {
		return "", err
	}

	return path.Join(workspace, dir), nil
}

// target is the project's named target, under whichever key it is declared; an empty
// target when neither declares it, whose output is the default's.
func (p *angularProject) target(name string) angularTarget {
	if t, ok := p.Architect[name]; ok {
		return t
	}

	return p.Targets[name]
}

// outputDir is the directory the target writes the browser bundle to, relative to the
// workspace, from its outputPath in either form or the default without one; where names
// the file and the project in a refusal.
func (t *angularTarget) outputDir(project, where string) (string, error) {
	base, browser := path.Join(angularDist, project), angularBrowserDir
	if t.browserBuilder() {
		browser = ""
	}
	raw := bytes.TrimSpace(t.Options.OutputPath)
	switch {
	case len(raw) == 0 || bytes.Equal(raw, []byte("null")):
	case raw[0] == '"':
		if err := json.Unmarshal(raw, &base); err != nil {
			return "", errors.Wrapf(err, "json.Unmarshal(): %s: outputPath", where)
		}
	default:
		var out angularOutputPath
		if err := json.Unmarshal(raw, &out); err != nil {
			return "", errors.Wrapf(err, "json.Unmarshal(): %s: outputPath", where)
		}
		if out.Base == "" {
			return "", errors.Newf("%s: outputPath sets no base", where)
		}
		base = out.Base
		if out.Browser != nil {
			browser = *out.Browser
		}
	}
	if base == "" {
		return "", errors.Newf("%s: outputPath is empty", where)
	}

	return path.Join(base, browser), nil
}

// browserBuilder reports the browser builder of old (@angular-devkit/build-angular:browser
// and its esbuild variant), which wrote the bundle at outputPath itself; the application
// builder writes it under a subdirectory.
func (t *angularTarget) browserBuilder() bool {
	_, kind, _ := strings.Cut(t.Builder, ":")

	return kind == "browser" || kind == "browser-esbuild"
}
