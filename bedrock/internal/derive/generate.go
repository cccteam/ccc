// generate.go settles where the application's generate pass runs, so the stack can put
// bedrock's generate-time step beside it.

package derive

import (
	"go/parser"
	"go/token"
	"path"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// generateStep finds the //go:generate directive that runs the site generator and
// records its directory and package. bedrock renders its own generate-time step there
// (bedrock.go, the migration renumber), in a file that sorts before the application's,
// so `go generate ./...` renumbers the branch's migrations before the generators read
// them. No directive, no step: impulse check reports a generator nothing runs.
func (m *Model) generateStep(a *app.App) error {
	sites := a.SiteGenerators()
	if len(sites) == 0 {
		return nil
	}
	d, ok := a.RunsGenerator(sites[0])
	if !ok {
		return nil
	}
	f, err := parser.ParseFile(token.NewFileSet(), a.Abs(d.File), nil, parser.PackageClauseOnly)
	if err != nil {
		return errors.Wrapf(err, "parser.ParseFile(): %s", d.File)
	}
	m.Schema.GenerateDir, m.Schema.GeneratePackage = path.Dir(d.File), f.Name.Name

	return nil
}

// router settles the generated router's package directory: the one the site generator
// writes its routes into, and the resource generator its release file beside them.
func (m *Model) router(a *app.App) {
	if sites := a.SiteGenerators(); len(sites) > 0 {
		m.RouterDir = sites[0].RoutesDir()
	}
}
