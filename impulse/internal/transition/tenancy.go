package transition

import (
	"context"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

// Tenancy makes a flat, untenanted application tenanted: the tenant record becomes a
// table and a global resource annotated @tenant, from which the generator derives the
// tenant segment, serves the tenant-scoped resources under it with tenant existence
// concealed, and emits the constructor of the tenant roster; the data level builds the
// roster and starts it, and the app exposes it to the generated code. Which resources
// become tenant-scoped, how existing rows are assigned, the bootstrap order, the tests,
// and the tenant picker are the agent's.
type Tenancy struct {
	// Table is the tenant-record table, PascalCase and plural: Tenants. The domain route
	// segment is its kebab-case form (tenants), and the resource struct its singular.
	Table string
}

// The tenant roster as the application wires it: the accessor the generated contract
// (domainScopedApp) asks the App for, the data level's method that builds the roster and
// starts it, and the packages the wiring draws on.
const (
	rosterAccessor      = "TenantRoster"
	rosterStart         = "startTenants"
	resourceImportPath  = "github.com/cccteam/ccc/resource"
	liveFirestoreImport = "github.com/cccteam/ccc/resource/live/firestore"
	spannerImportPath   = "cloud.google.com/go/spanner"
	errorsImportPath    = "github.com/go-playground/errors/v5"
)

// TenancyReferenceCandidate is the embedded skeleton with tenancy wired.
const TenancyReferenceCandidate = "tenanted"

// tenantServicePath is the tenant service inside the reference candidate's console,
// relative to the project's source root.
const tenantServicePath = "app/core/tenant/tenant.service.ts"

var tableNameRE = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// Command is the impulse command line for the transition.
func (tn Tenancy) Command() string {
	return fmt.Sprintf("impulse add tenancy --tenant-table %s", tn.Table)
}

// Segment is the domain route segment: the table's kebab-case form.
func (tn Tenancy) Segment() string { return strcase.ToKebab(tn.Table) }

// Record is the tenant-record struct: the table's singular.
func (tn Tenancy) Record() string { return singular(tn.Table) }

// Validate checks the transition against the application before anything is changed.
func (tn Tenancy) Validate(a *app.App) error {
	if !tableNameRE.MatchString(tn.Table) || singular(tn.Table) == tn.Table {
		return errors.Newf("table %q: name the tenant-record table in PascalCase and plural, such as Tenants", tn.Table)
	}
	p := a.Profile()
	switch {
	case len(p.Sites) == 0:
		return errors.New("no generator program emits handlers; the application has no site to make tenanted")
	case len(p.Sites) > 1:
		return errors.New("making an application in the sites layout tenanted is not supported yet")
	}
	site := &p.Sites[0]
	if site.Tenanted() {
		return errors.Newf("%s:%d: the application is already tenanted; %s is its tenant record", site.TenantRecord.File, site.TenantRecord.Line, site.TenantRecord.Name)
	}
	if len(site.Generator.MigrationSources) == 0 || !strings.HasPrefix(site.Generator.MigrationSources[0], "file://") {
		return errors.Newf("%s: the tenant table migration needs a file:// migration source", site.Generator.File)
	}

	return nil
}

// Apply makes the deterministic half of the transition.
func (tn Tenancy) Apply(ctx context.Context, a *app.App, exec check.Execer) (*Change, error) {
	if err := tn.Validate(a); err != nil {
		return nil, err
	}
	ch := &Change{Command: tn.Command()}
	site := &a.Profile().Sites[0]
	g := site.Generator

	if err := tn.editProgram(a, g, ch); err != nil {
		return nil, err
	}
	migrations := strings.TrimPrefix(g.MigrationSources[0], "file://")
	if err := tn.writeMigration(a, migrations, ch); err != nil {
		return nil, err
	}
	if err := tn.writeSeed(a, path.Join(path.Dir(migrations), "devseed"), ch); err != nil {
		return nil, err
	}
	if err := tn.writeRecord(a, g.ResourcePackageDir, ch); err != nil {
		return nil, err
	}
	if err := tn.editConfig(a, g, ch); err != nil {
		return nil, err
	}
	if err := tn.editApp(a, ch); err != nil {
		return nil, err
	}
	if err := tn.copyTenantService(a, g, ch); err != nil {
		return nil, err
	}

	generate(ctx, a, exec, ch, "go generate ./... failed, so the tenant routes are not generated yet; fix the cause and run it:", fmt.Sprintf("ran go generate ./..., which emitted the %s resource, the tenant segment pair under /%s/{%sID}, and the roster constructor New%sRoster", tn.Record(), tn.Segment(), strcase.ToCamel(tn.Record()), tn.Record()))

	return ch, nil
}

// editProgram adds WithConcealedDomains after GenerateRoutes; the tenant segment itself
// derives from the record, so the program names none.
func (tn Tenancy) editProgram(a *app.App, g *app.Generator, ch *Change) error {
	src, mode, err := readFile(a, g.File)
	if err != nil {
		return err
	}
	edited, err := app.InsertOptions(g.File, src, "GenerateRoutes", []string{"generation.WithConcealedDomains()"})
	if err != nil {
		return err
	}
	if err := os.WriteFile(a.Abs(g.File), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: added WithConcealedDomains(); the tenant segment /%s derives from the %s record", g.File, tn.Segment(), tn.Record())

	return nil
}

// writeMigration adds the tenant table as the next migration.
func (tn Tenancy) writeMigration(a *app.App, dir string, ch *Change) error {
	n, err := nextMigration(a.Abs(dir))
	if err != nil {
		return err
	}
	base := fmt.Sprintf("%06d_%s", n, tn.Table)
	up := fmt.Sprintf(`CREATE TABLE %[1]s (
  Id STRING(64) NOT NULL,
  Name STRING(MAX) NOT NULL,

  CONSTRAINT CK_%[1]s_Id CHECK (REGEXP_CONTAINS(Id, r'^[a-z][a-z0-9-]{1,62}$')),
) PRIMARY KEY (Id);

CREATE UNIQUE INDEX %[1]sByName ON %[1]s(Name);
`, tn.Table)
	down := fmt.Sprintf("DROP INDEX %[1]sByName;\nDROP TABLE %[1]s;\n", tn.Table)
	if err := writeNew(a, path.Join(dir, base+".up.sql"), up); err != nil {
		return err
	}
	if err := writeNew(a, path.Join(dir, base+".down.sql"), down); err != nil {
		return err
	}
	ch.didf("%s/%s.up.sql and .down.sql: the %s table (a slug primary key and a unique Name)", dir, base, tn.Table)

	return nil
}

// writeSeed adds the development tenants as a data migration beside the schema.
func (tn Tenancy) writeSeed(a *app.App, dir string, ch *Change) error {
	if _, err := os.Stat(a.Abs(dir)); err == nil {
		ch.skipf("%s already exists, so no development tenants were seeded; add two to it", dir)

		return nil
	}
	up := fmt.Sprintf("INSERT INTO %[1]s (Id, Name) VALUES ('north', 'North');\nINSERT INTO %[1]s (Id, Name) VALUES ('south', 'South');\n", tn.Table)
	down := fmt.Sprintf("DELETE FROM %s WHERE Id IN ('north', 'south');\n", tn.Table)
	base := "000001_dev_" + tn.Segment()
	if err := writeNew(a, path.Join(dir, base+".up.sql"), up); err != nil {
		return err
	}
	if err := writeNew(a, path.Join(dir, base+".down.sql"), down); err != nil {
		return err
	}
	ch.didf("%s/%s.up.sql and .down.sql: the development tenants north and south, a data migration for the bootstrap to apply before the logins", dir, base)

	return nil
}

// writeRecord adds the tenant-record resource struct.
func (tn Tenancy) writeRecord(a *app.App, resourceDir string, ch *Change) error {
	rel := path.Join(resourceDir, strings.ToLower(tn.Table)+".go")
	if _, err := os.Stat(a.Abs(rel)); err == nil {
		ch.skipf("%s already exists, so the %s resource struct was not written; make sure it is a global @resource annotated @tenant, with a slug Id and a Name", rel, tn.Record())

		return nil
	}
	pkg := packageNameOf(a, resourceDir)
	src := fmt.Sprintf(`package %s

type (
	// %[2]s is the tenant record (@tenant): the global resource whose rows are the
	// tenants. Its route name is the segment the tenant-scoped routes are served under,
	// so /%[3]s lists the tenants while /%[3]s/{%[4]sID}/... serves them, and its key is
	// the domain in every tenant-scoped URL. The generator derives the segment from it
	// and emits New%[2]sRoster, the constructor of the tenant roster the data level builds
	// and starts: the tenants as the generated guard knows them, kept current on every
	// instance by this record's generated write paths, so a tenant created here is
	// usable at once, without a restart.
	//
	// The primary key is a human-readable slug, not a UUID: tenant identifiers appear in
	// every tenant-scoped URL and in role provisioning, and the schema enforces the slug
	// shape with a CHECK constraint. Creating a tenant therefore supplies its key.
	//
	// %[2]s itself is a GLOBAL resource: administering the tenant list is a global
	// concern.
	//
	// @resource
	// @tenant
	%[2]s struct {
		ID   string `+"`spanner:\"Id\"`"+`
		Name string `+"`spanner:\"Name\"`"+`
	}
)
`, pkg, tn.Record(), tn.Segment(), strcase.ToCamel(tn.Record()))
	if err := writeNew(a, rel, src); err != nil {
		return err
	}
	ch.didf("%s: the %s resource struct, global, keyed by slug", rel, tn.Record())

	return nil
}

// editConfig gives the data level the tenant roster: a field on DataConfiguration, a new
// file whose startTenants builds the roster with the record's generated constructor and
// starts it and whose TenantRoster accessor hands it to the app, and the start inserted
// where the configuration is built.
func (tn Tenancy) editConfig(a *app.App, g *app.Generator, ch *Change) error {
	handlersPath := a.GoMod.Module.Mod.Path + "/" + g.HandlersDir()
	handlersPkg := packageNameOf(a, g.HandlersDir())
	constructor := handlersPkg + ".New" + tn.Record() + "Roster"
	rel, src, mode, err := findDeclaringFile(a, "DataConfiguration")
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no file declares a DataConfiguration struct, so the tenant roster was not added to the data level; build it where the database is opened with %s(client, resource.WithTenantSignals(<the live service>)), start it (Start) and fail the start on its error, and expose it as %s()", constructor, rosterAccessor)

		return nil
	}
	client, err := clientExpression(rel, src)
	if err != nil {
		return err
	}
	if client == "" {
		ch.skipf("%s: DataConfiguration holds no *resource.SpannerClient and no *spanner.Client field the roster could read through, so the tenant roster was not added; build it with %s over the database client, start it, and expose it as %s()", rel, constructor, rosterAccessor)

		return nil
	}
	liveField, err := app.StructFieldOfType(rel, src, "DataConfiguration", liveFirestoreImport, "Service")
	if err != nil {
		return err
	}
	pkg, err := app.PackageName(rel, src)
	if err != nil {
		return err
	}
	gained, err := addRosterField(a, rel, src, mode, ch)
	if err != nil {
		return err
	}
	tenancyFile := path.Join(path.Dir(rel), "tenancy.go")
	source, err := tn.configSource(pkg, handlersPath, handlersPkg, client, liveField)
	if err != nil {
		return err
	}
	if err := writeNew(a, tenancyFile, source); err != nil {
		return err
	}
	if liveField == "" {
		ch.skipf("%s: DataConfiguration holds no *firestore.Service field (resource/live/firestore), so the roster is built without resource.WithTenantSignals and reloads at its backstop alone; pass the live service the level opens, so a tenant created on another instance reaches this one at once", tenancyFile)
	}
	ch.didf("%s: the tenant roster (%s builds it with %s and starts it) and %s() on DataConfiguration; %s gained %s", tenancyFile, rosterStart, constructor, rosterAccessor, rel, gained)

	return nil
}

// addRosterField gives DataConfiguration the roster field (and the resource import its
// type needs) and inserts the roster's start where the configuration is built, writing
// the file; gained says which of the two the file took. An anchor miss for the start
// leaves the file as edited so far: the working copy is only replaced by a result the
// editor produced.
func addRosterField(a *app.App, rel string, src []byte, mode os.FileMode, ch *Change) (gained string, err error) {
	edited, err := app.AddStructField(rel, src, "DataConfiguration", "tenants *resource.TenantRoster")
	if err != nil {
		return "", err
	}
	edited, err = app.AddImport(rel, edited, resourceImportPath)
	if err != nil {
		return "", err
	}
	wrapErr := plainErr
	if ok, _ := app.HasImport(rel, src, errorsImportPath); ok {
		wrapErr = `errors.Wrap(err, "` + rosterStart + `()")`
	}
	gained = "the field and the start"
	wrapped, err := app.WrapReturn(rel, edited, "NewDataConfiguration", "DataConfiguration", "conf", "conf."+rosterStart+"(ctx)", wrapErr)
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		gained = "the field"
		ch.skipf("%s: NewDataConfiguration does not end in \"return &DataConfiguration{...}, nil\", so the roster's start was not inserted; call conf.%s(ctx) once the configuration is built, and fail the start on its error", rel, rosterStart)
	case err != nil:
		return "", err
	default:
		edited = wrapped
	}
	if err := os.WriteFile(a.Abs(rel), edited, mode); err != nil {
		return "", errors.Wrap(err, "os.WriteFile()")
	}

	return gained, nil
}

// clientExpression is the expression, on a DataConfiguration method's receiver c, of the
// client the roster reads through: the database driver's resource client, a
// *resource.SpannerClient field as it is, or a *spanner.Client field wrapped as a
// resource client; empty when the level holds none of them.
func clientExpression(rel string, src []byte) (string, error) {
	field, err := app.StructFieldOfType(rel, src, "DataConfiguration", databaseImportPath, "Driver")
	if err != nil {
		return "", err
	}
	if field != "" {
		return "c." + field + ".ResourceClient", nil
	}
	field, err = app.StructFieldOfType(rel, src, "DataConfiguration", resourceImportPath, "SpannerClient")
	if err != nil {
		return "", err
	}
	if field != "" {
		return "c." + field, nil
	}
	field, err = app.StructFieldOfType(rel, src, "DataConfiguration", spannerImportPath, "Client")
	if err != nil {
		return "", err
	}
	if field != "" {
		return "resource.NewSpannerClient(c." + field + ")", nil
	}

	return "", nil
}

// configSource is the data level's tenancy file: startTenants building the roster with
// the generated constructor over the client and the live service's tenants signal (a
// comment stands where the level has no live service field) and starting it, and the
// accessor the app's Configurer asks for. The source is formatted, so the imports sort
// whatever the handlers package's path is.
func (tn Tenancy) configSource(pkg, handlersPath, handlersPkg, client, liveField string) (string, error) {
	alias := ""
	if handlersPkg != path.Base(handlersPath) {
		alias = handlersPkg + " "
	}
	construction := fmt.Sprintf("c.tenants = %s.New%sRoster(%s, resource.WithTenantSignals(c.%s))", handlersPkg, tn.Record(), client, liveField)
	if liveField == "" {
		construction = fmt.Sprintf("// No live service field was found on DataConfiguration, so the roster reloads at\n"+
			"\t// its backstop alone; pass resource.WithTenantSignals(<the live service>) so a tenant\n"+
			"\t// created on another instance reaches this one at once.\n"+
			"\tc.tenants = %s.New%sRoster(%s)", handlersPkg, tn.Record(), client)
	}
	src := fmt.Sprintf(`package %[1]s

import (
	"context"

	%[2]s"%[3]s"
	"github.com/cccteam/ccc/resource"
	"github.com/go-playground/errors/v5"
)

// startTenants builds the tenant roster over the %[4]s record with the generated
// constructor and starts it: the roster reads the %[5]s table once, fails the start when
// it cannot, and keeps the set current until ctx ends, rereading on the tenants signal
// (a tenant created or deleted on any instance, published by the record's generated
// write paths) and at its backstop. The generated DomainGuard asks it before a
// tenant-scoped request runs, so a new tenant is usable at once, without a restart.
func (c *DataConfiguration) startTenants(ctx context.Context) error {
	%[6]s
	if err := c.tenants.Start(ctx); err != nil {
		return errors.Wrap(err, "resource.TenantRoster.Start()")
	}

	return nil
}

// TenantRoster returns the application's tenant roster: the tenants as the generated
// guard knows them, and the roster a session's tenant list is filtered from.
func (c *DataConfiguration) TenantRoster() *resource.TenantRoster {
	return c.tenants
}
`, pkg, alias, handlersPath, tn.Record(), tn.Table, construction)
	formatted, err := format.Source([]byte(src))
	if err != nil {
		return "", errors.Wrap(err, "format.Source()")
	}

	return string(formatted), nil
}

// editApp exposes the roster to the generated code, whose contract (domainScopedApp) asks
// the App for TenantRoster(): the Configurer requires it, the App carries it and hands its
// Domains to the session permissions, and a new file declares the interface and the
// accessor.
func (tn Tenancy) editApp(a *app.App, ch *Change) error {
	rel, src, mode, err := findDeclaringFile(a, "Configurer")
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no file declares a Configurer interface, so the tenant roster was not exposed on the app; the generated code needs %s() *resource.TenantRoster on the handlers, answering the roster the data level built", rosterAccessor)

		return nil
	}
	pkg, err := app.PackageName(rel, src)
	if err != nil {
		return err
	}
	edited, err := app.AddInterfaceLine(rel, src, "Configurer", "TenancyConfigurer")
	if err != nil {
		return err
	}
	edited, err = app.AddStructField(rel, edited, "App", "tenants *resource.TenantRoster")
	if errors.Is(err, app.ErrNoAnchor) {
		ch.skipf("%s: no App struct to carry the roster; give the handlers a %s method answering the configurer's", rel, rosterAccessor)
		edited = nil
	} else if err != nil {
		return err
	}
	if edited != nil {
		edited, err = app.AddLiteralElement(rel, edited, "New", "App", "tenants: cfg."+rosterAccessor+"()")
		if errors.Is(err, app.ErrNoAnchor) {
			ch.skipf("%s: New builds no App literal; set tenants from the configurer's %s() where the App is constructed", rel, rosterAccessor)
		} else if err != nil {
			return err
		}
	}
	if edited != nil {
		edited, err = app.AddImport(rel, edited, resourceImportPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(a.Abs(rel), edited, mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
	}
	tenancyFile := path.Join(path.Dir(rel), "tenancy.go")
	if err := writeNew(a, tenancyFile, tn.appSource(pkg)); err != nil {
		return err
	}
	ch.didf("%s: TenancyConfigurer (embedded in Configurer) and App.%s(); %s gained the field and its assignment", tenancyFile, rosterAccessor, rel)
	if edited != nil {
		if err := tn.editPermissions(a, rel, ch); err != nil {
			return err
		}
	}

	return nil
}

// editPermissions hands the roster's Domains to the session permissions: the
// resource.SessionPermissions call of the App's UserPermissions passed nil where an
// untenanted application lists no domain, and a login's tenant list is the roster
// filtered by the principal's footholds.
func (Tenancy) editPermissions(a *app.App, rel string, ch *Change) error {
	src, mode, err := readFile(a, rel)
	if err != nil {
		return err
	}
	edited, err := app.ReplaceNilArgument(rel, src, "UserPermissions", "SessionPermissions", "a.tenants.Domains")
	switch {
	case errors.Is(err, app.ErrNoAnchor):
		ch.skipf("%s: UserPermissions passes no nil roster to resource.SessionPermissions, so the roster's Domains were not handed to it; pass a.tenants.Domains where the session permissions are composed, so a login's tenant list is the roster filtered by its footholds", rel)

		return nil
	case err != nil:
		return err
	}
	if err := os.WriteFile(a.Abs(rel), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: UserPermissions passes the roster's Domains to resource.SessionPermissions", rel)

	return nil
}

func (tn Tenancy) appSource(pkg string) string {
	return fmt.Sprintf(`package %s

import "github.com/cccteam/ccc/resource"

// TenancyConfigurer is what the configuration provides for tenancy: the tenant roster the
// data level built with the generated constructor and started. The generated DomainGuard
// and the consolidated dispatcher ask it whether a domain is a tenant, and since tenant
// existence is concealed (generation.WithConcealedDomains) they ask the caller's foothold
// next, so a prober cannot confirm a tenant exists from the rejection shape.
type TenancyConfigurer interface {
	%[2]s() *resource.TenantRoster
}

// %[2]s returns the application's tenant roster, the generated contract's accessor
// (domainScopedApp); UserPermissions hands its Domains to resource.SessionPermissions, so
// a session's tenant list is the roster filtered by the principal's footholds.
func (a *App) %[2]s() *resource.TenantRoster {
	return a.tenants
}
`, pkg, rosterAccessor)
}

// packageNameOf is the package clause of the non-test Go files under a root-relative
// directory, or the directory's name when none can be read.
func packageNameOf(a *app.App, dir string) string {
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return path.Base(dir)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		src, err := os.ReadFile(a.Abs(path.Join(dir, e.Name())))
		if err != nil {
			continue
		}
		if name, err := app.PackageName(e.Name(), src); err == nil {
			return name
		}
	}

	return path.Base(dir)
}

// copyTenantService lays the reference candidate's tenant service into the default
// browser project, beside the generated client.
func (tn Tenancy) copyTenantService(a *app.App, g *app.Generator, ch *Change) error {
	target := defaultTarget(g)
	if target == nil {
		return nil
	}
	w, ok := a.WebAppFor(target.Dir)
	if !ok {
		return nil
	}
	projects, err := a.ReadAngular(w.Dir)
	if err != nil {
		return err
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(target.Dir, w.Dir), "/")
	project, ok := app.ProjectFor(projects, rel)
	if !ok || project.SourceRoot == "" {
		ch.skipf("%s/angular.json has no project rooted over %s, so the tenant service was not copied in; a browser app needs a selected tenant to scope its requests and digest by", w.Dir, rel)

		return nil
	}
	dst := path.Join(w.Dir, project.SourceRoot, tenantServicePath)
	if _, err := os.Stat(a.Abs(dst)); err == nil {
		ch.skipf("%s already exists and was left alone", dst)

		return nil
	}
	reference, err := skeleton.FS(TenancyReferenceCandidate)
	if err != nil {
		return err
	}
	src, err := fs.ReadFile(reference, "web/console/src/"+tenantServicePath)
	if err != nil {
		return errors.Wrap(err, "fs.ReadFile()")
	}
	if err := writeNew(a, dst, string(src)); err != nil {
		return err
	}
	ch.didf("%s: the tenant service (the selected tenant, the session's tenant list, and the digest scoped to it) from the reference; the header's tenant picker and the pages that read it are yours", dst)

	return nil
}

// findDeclaringFile finds the non-test Go file under the module that declares the named
// type, skipping generated files and browser workspaces.
func findDeclaringFile(a *app.App, typeName string) (rel string, src []byte, mode os.FileMode, err error) {
	root, err := os.OpenRoot(a.Root)
	if err != nil {
		return "", nil, 0, errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()
	webDirs := map[string]bool{}
	for _, w := range a.WebApps {
		webDirs[w.Dir] = true
	}
	err = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errors.Wrap(walkErr, "fs.WalkDir()")
		}
		if d.IsDir() {
			if p != "." && (webDirs[p] || skippedNames[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}

			return nil
		}
		name := d.Name()
		if rel != "" || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "zz_gen_") {
			return nil
		}
		data, readErr := root.ReadFile(filepath.FromSlash(p))
		if readErr != nil {
			return errors.Wrap(readErr, "os.Root.ReadFile()")
		}
		// A file the tool cannot parse is not the one.
		if declares, parseErr := app.DeclaresType(p, data, typeName); parseErr == nil && declares {
			info, statErr := d.Info()
			if statErr != nil {
				return errors.Wrap(statErr, "fs.DirEntry.Info()")
			}
			rel, src, mode = p, data, info.Mode().Perm()
		}

		return nil
	})
	if err != nil {
		return "", nil, 0, errors.Wrap(err, "fs.WalkDir()")
	}

	return rel, src, mode, nil
}

// nextMigration returns the number after the highest migration in the directory.
func nextMigration(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, errors.Wrapf(err, "os.ReadDir(): migrations at %s", dir)
	}
	highest := 0
	for _, e := range entries {
		num, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(num); err == nil {
			highest = max(highest, n)
		}
	}

	return highest + 1, nil
}

// writeNew writes a root-relative file that must not exist yet, creating its directory.
// The write goes through a root scoped to the application, so no path lands outside it.
func writeNew(a *app.App, rel, content string) error {
	root, err := os.OpenRoot(a.Root)
	if err != nil {
		return errors.Wrap(err, "os.OpenRoot()")
	}
	defer root.Close()
	local := filepath.FromSlash(rel)
	if _, err := root.Stat(local); err == nil {
		return errors.Newf("%s already exists", rel)
	}
	if dir := filepath.Dir(local); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return errors.Wrap(err, "os.Root.MkdirAll()")
		}
	}
	if err := root.WriteFile(local, []byte(content), 0o644); err != nil {
		return errors.Wrap(err, "os.Root.WriteFile()")
	}

	return nil
}

// singular is the tenant-record struct name for a plural table name.
func singular(table string) string {
	switch {
	case strings.HasSuffix(table, "ies"):
		return strings.TrimSuffix(table, "ies") + "y"
	case strings.HasSuffix(table, "sses"):
		return strings.TrimSuffix(table, "es")
	case strings.HasSuffix(table, "ches"), strings.HasSuffix(table, "shes"), strings.HasSuffix(table, "xes"), strings.HasSuffix(table, "zes"):
		return strings.TrimSuffix(table, "es")
	case strings.HasSuffix(table, "s"):
		return strings.TrimSuffix(table, "s")
	default:
		return table
	}
}

// Meaning explains tenancy in this framework and names the wiring left to do.
func (tn Tenancy) Meaning() string {
	var b strings.Builder
	seg, rec := tn.Segment(), tn.Record()
	fmt.Fprintf(&b, "Tenancy is data. The %[1]s table is the domain universe, a permission domain per row, and `%[2]s`, the global resource over it, is the tenant record (`@tenant`): the generator derives the tenant segment from it, so every struct annotated `@permissionScope(domain)` is served under `/%[3]s/{%[4]sID}/...` with the tenant as its permission domain, and emits `New%[2]sRoster`, the constructor of the tenant roster. The data level builds the roster with it over the database client and the live service's tenants signal and starts it; the roster reads the table once, rereads when the record's generated write paths publish the tenants signal (a tenant created or deleted on any instance) and at its backstop, so a new tenant is usable at once, without a restart. The generated DomainGuard asks the roster whether a domain is a tenant and, since tenant existence is concealed (`WithConcealedDomains`), the caller's foothold next, so a login with no foothold gets the same not-found an unknown tenant gets. A login's tenant list (`user-domains`) is the roster filtered by the tenants where it holds a grant, and a grant needs a tenant-scoped resource to land on.\n\n", tn.Table, rec, seg, strcase.ToCamel(rec))
	b.WriteString("Left to wire, in this order:\n\n")
	items := []string{
		"The bootstrap and the deployment. Seed the development tenants (`schema/devseed`, a data migration applied with the migrator's data step) BEFORE the logins and before the data level opens, since the roster's start reads the table; the App's `UserPermissions` passes the roster's `Domains` (`a.tenants.Domains`) to `resource.SessionPermissions` so a login's tenant list is read from it. The roles need no provisioning per tenant, since a membership held in every domain (`EveryDomainPolicyScope`) reaches every tenant the roster names. Give the development logins roles in the tenants (the bootstrap identities file): the administrator under `everyDomain`, and add a member login that holds a role in one tenant only under `domains`, so concealment is observable.",
		"Tenant-scoped resources. Decide which existing resources belong to a tenant: give each a `TenantId` column referencing the tenant table (a migration, with a data step assigning existing rows to a tenant), annotate the struct `@permissionScope(domain)` and the column `// @domain`, then run `go generate ./...`. If none of the application's resources is tenant-scoped yet, add a first one so the option is observable from the first sign-in; the reference has one. `tenancy-wired` requires at least one.",
		fmt.Sprintf("The test harnesses. The authorization suite's configurer builds the roster with `New%[1]sRoster` over the test client and adds the generated matrix's domain to it (`Add(\"testDomain\")`; the empty test schema holds no tenant row, and `Start` is not needed), and answers it as `%[2]s()`; the integration harness builds it over the test database with `live.NewFake()` as its signals and starts it once the dev seed is applied beside the schema, gives the member its assignments in its tenant (`DomainPolicyScope`), and waits for the engine's snapshot to show them.", rec, rosterAccessor),
		"Integration tests: the administrator lists both tenants and reads a tenant's digest; the member lists one and gets not-found in the other and in an unknown tenant; the tenant list is global; a tenant created through the API is readable at once, without a restart.",
		"The browser app: a tenant picker in the header bound to the tenant service (its tenants, current, and select), and the pages reading permissions through it, so the digest follows the selected tenant.",
	}
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}
	b.WriteString("\nData consequence: every row of a resource that becomes tenant-scoped needs a tenant. Write the assignment as a migration a person can review, never a default nobody chose.\n")

	return b.String()
}
