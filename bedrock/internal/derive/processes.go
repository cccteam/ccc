// processes.go reads the deployed processes: the served site and the migration, the
// configuration levels each constructs, and the schema the migration applies.

package derive

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

const (
	mainFile = "main.go"
	// migrateSchemaFunc is the function the migrate command calls to apply the schema.
	migrateSchemaFunc = "MigrateSchema"
	fileScheme        = "file://"
)

// processes settles the site and the migration.
func (m *Model) processes(a *app.App, cfg *config) error {
	profile := a.Profile()
	if len(profile.Sites) != 1 {
		return errors.Newf("the application has %d sites; the stack serves one", len(profile.Sites))
	}
	siteDir := profile.Sites[0].Dir
	if !slices.Contains(a.MainPackages, siteDir) {
		return errors.Newf("no main package at %s, the site's directory", siteDir)
	}
	site, err := m.process(a, cfg, siteProcess, siteDir)
	if err != nil {
		return err
	}
	m.Site = site

	if slices.Contains(a.MainPackages, migrateDir) {
		migrate, err := m.process(a, cfg, migrateProcess, migrateDir)
		if err != nil {
			return err
		}
		m.Migrate = &migrate
	}

	return nil
}

// process reads one main package for the levels it constructs.
func (m *Model) process(a *app.App, cfg *config, name, dir string) (Process, error) {
	p := Process{Name: name, Dir: dir, Main: dir}
	if dir == "." {
		p.Main = mainFile
	}
	rel := path.Join(dir, mainFile)
	src, err := os.ReadFile(a.Abs(rel))
	if err != nil {
		return Process{}, errors.Wrapf(err, "os.ReadFile(): %s", rel)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return Process{}, errors.Wrap(err, "parser.ParseFile()")
	}
	configPath := a.GoMod.Module.Mod.Path + "/" + m.ConfigDir
	local := importName(f, configPath)
	if local == "" {
		return Process{}, errors.Newf("%s does not import the config package %s", rel, configPath)
	}

	highest := -1
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != local {
			return true
		}
		for i, level := range cfg.levels {
			if sel.Sel.Name == level.Constructor && i > highest {
				highest = i
			}
		}

		return true
	})
	if highest < 0 {
		return Process{}, errors.Newf("%s constructs no configuration level", rel)
	}
	for _, level := range cfg.levels[:highest+1] {
		p.Levels = append(p.Levels, level.Name)
	}

	return p, nil
}

// SeedDir is the seed directory's name beside the schema migrations directory: the
// data migrations the migrate command applies with -seed, tracked apart from the schema.
const SeedDir = "devseed"

// MigrationsDir is the root-relative directory of the schema migrations: the file://
// source the site generator declares.
func MigrationsDir(a *app.App) (string, error) {
	sites := a.SiteGenerators()
	if len(sites) == 0 {
		return "", errors.New("no site generator declares the migration sources")
	}
	for _, source := range sites[0].MigrationSources {
		if dir, ok := strings.CutPrefix(source, fileScheme); ok {
			return path.Clean(dir), nil
		}
	}

	return "", errors.Newf("the generator in %s names no file:// migration source", sites[0].File)
}

// MigrationDirs is the schema migrations directory and the seed directory beside it,
// root-relative: the two directories the migrate command reads, the guard checks and
// the renumber moves files in.
func MigrationDirs(a *app.App) ([]string, error) {
	dir, err := MigrationsDir(a)
	if err != nil {
		return nil, err
	}

	return []string{dir, path.Join(path.Dir(dir), SeedDir)}, nil
}

// schema settles the migrations directory and the call that applies them.
func (m *Model) schema(a *app.App) error {
	dir, err := MigrationsDir(a)
	if err != nil {
		return err
	}
	m.Schema.MigrationsDir = dir
	if m.Migrate == nil {
		return nil
	}

	rel := path.Join(m.Migrate.Dir, mainFile)
	src, err := os.ReadFile(a.Abs(rel))
	if err != nil {
		return errors.Wrapf(err, "os.ReadFile(): %s", rel)
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return errors.Wrap(err, "parser.ParseFile()")
	}
	imports := map[string]string{}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		local := path.Base(p)
		if imp.Name != nil {
			local = imp.Name.Name
		}
		imports[local] = p
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != migrateSchemaFunc {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		if dir, local := a.ModuleDir(imports[pkg.Name]); local {
			m.Schema.MigrateCall = dir + " " + migrateSchemaFunc
		}

		return true
	})
	if m.Schema.MigrateCall == "" {
		return errors.Newf("%s calls no %s of the application's own", rel, migrateSchemaFunc)
	}

	return nil
}

// importName returns the name the file imports the path under, or empty.
func importName(f *ast.File, importPath string) string {
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != importPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}

		return path.Base(p)
	}

	return ""
}
