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
	// Root and SourceRoot are the project's directories relative to the workspace.
	Root       string
	SourceRoot string
	// ProxyConfig is the serve option's proxy configuration file relative to the
	// workspace, or empty.
	ProxyConfig string
	// DevPort is the development serve configuration's port, 0 when unset.
	DevPort int
}

// ReadAngular reads the projects of the browser workspace at the root-relative
// directory, sorted by name.
func (a *App) ReadAngular(webDir string) ([]AngularProject, error) {
	data, err := os.ReadFile(a.Abs(path.Join(webDir, "angular.json")))
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	var angular struct {
		Projects map[string]struct {
			Root       string `json:"root"`
			SourceRoot string `json:"sourceRoot"`
			Architect  struct {
				Serve struct {
					Options struct {
						ProxyConfig string `json:"proxyConfig"`
					} `json:"options"`
					Configurations struct {
						Development struct {
							Port int `json:"port"`
						} `json:"development"`
					} `json:"configurations"`
				} `json:"serve"`
			} `json:"architect"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &angular); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): %s/angular.json", webDir)
	}

	projects := make([]AngularProject, 0, len(angular.Projects))
	for name, p := range angular.Projects {
		projects = append(projects, AngularProject{
			Name:        name,
			Root:        path.Clean(p.Root),
			SourceRoot:  path.Clean(p.SourceRoot),
			ProxyConfig: p.Architect.Serve.Options.ProxyConfig,
			DevPort:     p.Architect.Serve.Configurations.Development.Port,
		})
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })

	return projects, nil
}

// ProjectFor returns the project whose source root (or root) contains the
// workspace-relative path, preferring the deepest match, or false.
func ProjectFor(projects []AngularProject, rel string) (AngularProject, bool) {
	best, found, depth := AngularProject{}, false, -1
	for _, p := range projects {
		for _, dir := range []string{p.SourceRoot, p.Root} {
			if dir == "" || dir == "." || !within(dir, rel) {
				continue
			}
			if d := strings.Count(dir, "/"); d > depth {
				best, found, depth = p, true, d
			}
		}
	}

	return best, found
}
