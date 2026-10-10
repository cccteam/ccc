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
	cloudrundeclaration "github.com/cccteam/ccc/resource/jobs/cloudrun/declaration"
)

const (
	mainFile = "main.go"
	// ServerPackage is the framework's HTTP server, which speaks HTTP/1.1 and unencrypted
	// HTTP/2 (h2c) on one listener; a site's main package importing it is the
	// application's declaration that its image speaks h2c, from which the stack names the
	// service's port h2c and Cloud Run lifts the 32 MiB bounds of HTTP/1 bodies.
	ServerPackage = "github.com/cccteam/ccc/resource/server"
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
	for _, o := range profile.Sites[0].AllOutlets() {
		m.Outlets = append(m.Outlets, Outlet{Name: o.Name, Prefix: o.Prefix})
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
	if slices.Contains(a.MainPackages, jobsDir) {
		jobs, err := m.process(a, cfg, jobsProcess, jobsDir)
		if err != nil {
			return err
		}
		m.Jobs = &jobs
	}
	if err := m.jobsJob(); err != nil {
		return err
	}

	return m.jobsTemplate()
}

// JobsTemplateVariable is the variable the stack sets on the service to the job process's
// template job, as the Cloud Run API names it (projects/<p>/locations/<l>/jobs/<j>): the
// framework's job driver (resource/jobs/cloudrun) reads it, with the version the image
// bakes in, and names the job of this build from the two, the template's name with the
// version's key, which the pipeline made on this build's image. The served site declares
// it by embedding the driver's settings in a configuration level, and the name is read
// off the driver's declaration here, so the stack sets the variable the driver reads
// however either side is spelled; jobsTemplate holds the declaration to the job process.
var JobsTemplateVariable = jobsTemplateVariable()

// jobsTemplateField is the field of the job driver's settings struct that carries the
// template job, by the name the declaration gives it.
const jobsTemplateField = "Template"

// jobsTemplateVariable reads the template variable off the job driver's declaration: the
// variable the tag of its template field names. Empty when the declaration has no field
// of that name, which the derive tests hold it against.
func jobsTemplateVariable() string {
	for _, f := range cloudrundeclaration.Settings().Fields {
		if f.Name == jobsTemplateField {
			variable, _ := tagOptions(f.Tag)

			return variable
		}
	}

	return ""
}

// jobsTemplate holds the two sides of the job process's template to one fact: the stack
// sets JobsTemplateVariable on the service to the job process's template job, and the
// served site reads it through the job driver's settings, which a configuration level
// declares by embedding them (RoleJobsTemplate). A configuration with one side and not
// the other is refused here, at check and render time, rather than at the first start:
// the stack would have no job to name, or the service would read no template and the
// driver refuse every start.
func (m *Model) jobsTemplate() error {
	v := m.byRole(RoleJobsTemplate)
	switch {
	case v != nil && m.Jobs == nil:
		return errors.Newf("%s:%d: %s (%s) reads the job process's template job, and the application has no job process (no main package at %s): the stack has no job to name; add the job process, or drop the job driver's settings from the configuration", v.File, v.Line, v.Name, v.Declaration(), jobsDir)
	case v == nil && m.Jobs != nil:
		return errors.Newf("%s is the job process, and no configuration level declares %s, which the stack sets on the service to the job process's template job: the service reads no template, so the framework's job driver (resource/jobs/cloudrun) would refuse every start; embed cloudrun.Settings in the served site's configuration", m.Jobs.Dir, JobsTemplateVariable)
	default:
		return nil
	}
}

// retiredJobsJob is the variable a site once declared for the job of its build, which the
// pipeline baked into the image from the trigger's substitutions: the first release that
// added a job process built an image naming no job, since the trigger carries the last
// apply's. The framework names the job itself now (JobsTemplateVariable).
const retiredJobsJob = "APP_JOBS_JOB"

// jobsJob refuses a configuration that still declares the retired variable, naming the
// field to delete.
func (m *Model) jobsJob() error {
	for i := range m.Variables {
		v := &m.Variables[i]
		if v.Name == retiredJobsJob {
			return errors.Newf("%s (%s) is retired: the framework (resource/jobs) names the job of the build itself, from %s, which the stack sets on the service, and the version the image bakes in; delete the field", v.Name, v.Declaration(), JobsTemplateVariable)
		}
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
	p.H2C = importName(f, ServerPackage) != ""

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
