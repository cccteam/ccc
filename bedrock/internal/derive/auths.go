// auths.go reads the auths: each auth package's login flavor and, for a directory
// sign-in, its registration by role and the callback route the generated router
// registers for it.

package derive

import (
	"os"
	"path"
	"regexp"
	"sort"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// callbackRE matches the generated router's registration of the OIDC callback:
// r.Get("/api/user/callback", h.CallbackOIDC()).
var callbackRE = regexp.MustCompile(`r\.(Get|Post)\("([^"]+)",\s*h\.CallbackOIDC\(\)\)`)

// auths settles the auths and, for the directory-flavored ones, their callbacks.
func (m *Model) auths(a *app.App, cfg *config) error {
	if len(a.AuthPackages) == 0 {
		return errors.New("the application constructs no auth")
	}
	routesDir := a.SiteGenerators()[0].RoutesDir()
	for i := range a.AuthPackages {
		pkg := &a.AuthPackages[i]
		auth := Auth{Name: pkg.Name, Dir: pkg.Dir, Directory: map[Role]*Variable{}}
		for j := range a.Auths {
			if path.Dir(a.Auths[j].File) == pkg.Dir {
				auth.Flavor = a.Auths[j].Flavor
			}
		}
		if auth.OIDC() {
			for _, role := range directoryRoles {
				if v := m.byRole(role); v != nil {
					auth.Directory[role] = v
				}
			}
			if prefix := auth.Variable(RoleGroupPrefix); prefix != nil {
				auth.GroupPrefixDefault = cfg.envTemplate[prefix.Name]
			}
			route, err := callbackRoute(a, routesDir)
			if err != nil {
				return err
			}
			auth.Callback = route
		}
		m.Auths = append(m.Auths, auth)
	}
	sort.Slice(m.Auths, func(i, j int) bool {
		return m.Auths[i].Name < m.Auths[j].Name
	})

	return nil
}

// callbackRoute finds the OIDC callback the generated router under dir registers.
func callbackRoute(a *app.App, dir string) (Route, error) {
	if dir == "" {
		return Route{}, errors.New("the site generator declares no routes directory (GenerateRoutes)")
	}
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return Route{}, errors.Wrapf(err, "os.ReadDir(): %s", dir)
	}
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".go" {
			continue
		}
		rel := path.Join(dir, entry.Name())
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return Route{}, errors.Wrapf(err, "os.ReadFile(): %s", rel)
		}
		if match := callbackRE.FindSubmatch(src); match != nil {
			return Route{Method: routeMethod(string(match[1])), Path: string(match[2]), File: rel}, nil
		}
	}

	return Route{}, errors.Newf("no file under %s registers the OIDC callback (h.CallbackOIDC)", dir)
}

// routeMethod is the HTTP method a chi registration name stands for.
func routeMethod(name string) string {
	switch name {
	case "Get":
		return "GET"
	case "Post":
		return "POST"
	default:
		return name
	}
}
