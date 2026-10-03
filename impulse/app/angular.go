package app

import (
	"encoding/json"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"
)

// AngularProject is one project of a browser workspace's angular.json.
type AngularProject struct {
	Name string
	// ProjectType is "application" or "library"; an entry naming neither is read as an
	// application.
	ProjectType string
	// Root and SourceRoot are the project's directories relative to the workspace.
	Root       string
	SourceRoot string
	// ProxyConfig is the serve option's proxy configuration file relative to the
	// workspace, or empty.
	ProxyConfig string
	// ServePath is the serve option's path the dev server serves the application
	// under, empty when unset (the dev server then serves at /).
	ServePath string
	// DevPort is the development serve configuration's port, 0 when unset.
	DevPort int
	// TestBuilder is the test target's builder, empty when the project has no test
	// target.
	TestBuilder string
	// TestTsConfig is the test target's tsConfig option relative to the workspace,
	// empty when the target names none (the builder then reads tsconfig.spec.json from
	// the project root).
	TestTsConfig string
	// Index is the build option's entry document relative to the workspace (a path, or
	// the input of the object form), empty when unset.
	Index string
	// AssetDirs are the inputs of the build option's assets entries, the directories the
	// build copies into the bundle root (public in a fresh workspace), relative to the
	// workspace; a string entry is taken as given.
	AssetDirs []string
	// BaseHref is the build option's base href, empty when unset (the build then writes /).
	BaseHref string
	// ServiceWorker is the production configuration's serviceWorker option as a path
	// relative to the workspace: the path it names, ngsw-config.json in the project root
	// when it is true, and empty when it is unset or false.
	ServiceWorker string
}

// ReadAngular reads the projects of the browser workspace at the root-relative
// directory, sorted by name.
func (a *App) ReadAngular(webDir string) ([]AngularProject, error) {
	data, err := os.ReadFile(a.Abs(path.Join(webDir, "angular.json")))
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	var angular struct {
		Projects map[string]*struct {
			ProjectType string `json:"projectType"`
			Root        string `json:"root"`
			SourceRoot  string `json:"sourceRoot"`
			Architect   struct {
				Build struct {
					Options struct {
						Index    json.RawMessage   `json:"index"`
						Assets   []json.RawMessage `json:"assets"`
						BaseHref string            `json:"baseHref"`
					} `json:"options"`
					Configurations struct {
						Production struct {
							ServiceWorker json.RawMessage `json:"serviceWorker"`
						} `json:"production"`
					} `json:"configurations"`
				} `json:"build"`
				Serve struct {
					Options struct {
						ProxyConfig string `json:"proxyConfig"`
						ServePath   string `json:"servePath"`
					} `json:"options"`
					Configurations struct {
						Development struct {
							Port int `json:"port"`
						} `json:"development"`
					} `json:"configurations"`
				} `json:"serve"`
				Test struct {
					Builder string `json:"builder"`
					Options struct {
						TsConfig string `json:"tsConfig"`
					} `json:"options"`
				} `json:"test"`
			} `json:"architect"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &angular); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): %s/angular.json", webDir)
	}

	projects := make([]AngularProject, 0, len(angular.Projects))
	for name, p := range angular.Projects {
		build := p.Architect.Build
		assetDirs := make([]string, 0, len(build.Options.Assets))
		for _, raw := range build.Options.Assets {
			if dir := pathOrInput(raw); dir != "" {
				assetDirs = append(assetDirs, dir)
			}
		}
		projects = append(projects, AngularProject{
			Name:          name,
			ProjectType:   p.ProjectType,
			Root:          path.Clean(p.Root),
			SourceRoot:    path.Clean(p.SourceRoot),
			ProxyConfig:   p.Architect.Serve.Options.ProxyConfig,
			ServePath:     p.Architect.Serve.Options.ServePath,
			DevPort:       p.Architect.Serve.Configurations.Development.Port,
			TestBuilder:   p.Architect.Test.Builder,
			TestTsConfig:  p.Architect.Test.Options.TsConfig,
			Index:         pathOrInput(build.Options.Index),
			AssetDirs:     assetDirs,
			BaseHref:      build.Options.BaseHref,
			ServiceWorker: serviceWorkerPath(build.Configurations.Production.ServiceWorker, p.Root),
		})
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })

	return projects, nil
}

// pathOrInput reads an angular.json value that is a path or an object naming one as its
// input (the index and assets options take both forms); anything else reads empty.
func pathOrInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.Input
	}

	return ""
}

// serviceWorkerPath reads the production configuration's serviceWorker option: the config
// path it names, the project root's ngsw-config.json when it is true, and empty when it
// is unset or false.
func serviceWorkerPath(raw json.RawMessage, root string) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var enabled bool
	if err := json.Unmarshal(raw, &enabled); err == nil && enabled {
		return path.Join(path.Clean(root), "ngsw-config.json")
	}

	return ""
}

// ProjectFor returns the project whose source root (or root) contains the
// workspace-relative path, preferring the deepest match, or false.
func ProjectFor(projects []AngularProject, rel string) (AngularProject, bool) {
	best, found, depth := AngularProject{}, false, -1
	for i := range projects {
		p := &projects[i]
		for _, dir := range []string{p.SourceRoot, p.Root} {
			if dir == "" || dir == "." || !within(dir, rel) {
				continue
			}
			if d := strings.Count(dir, "/"); d > depth {
				best, found, depth = *p, true, d
			}
		}
	}

	return best, found
}
