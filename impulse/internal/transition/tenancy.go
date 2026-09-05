package transition

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
	"github.com/cccteam/ccc/impulse/internal/skeleton"
)

// Tenancy makes a flat, untenanted application tenanted: the tenant record becomes a
// table and a global resource, the generator serves tenant-scoped resources under the
// tenant segment with tenant existence concealed, the data level carries the tenant
// roster and the visibility seam, and the app exposes the seam to the generated code.
// Which resources become tenant-scoped, how existing rows are assigned, the bootstrap
// order, the tests, and the tenant picker are the agent's.
type Tenancy struct {
	// Table is the tenant-record table, PascalCase and plural: Tenants. The domain route
	// segment is its kebab-case form (tenants), and the resource struct its singular.
	Table string
}

// TenancyReferenceCandidate is the embedded skeleton with tenancy wired.
const TenancyReferenceCandidate = "tenanted"

// tenantServicePath is the tenant service inside the reference candidate's console,
// relative to the project's source root.
const tenantServicePath = "app/core/tenant/tenant.service.ts"

var tableNameRE = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// Command is the impulse command line for the transition.
func (tn Tenancy) Command() string {
	return fmt.Sprintf("impulse add tenancy --table %s", tn.Table)
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
		return errors.New("making a multi-site application tenanted is not supported yet")
	}
	site := &p.Sites[0]
	if site.Tenanted() {
		return errors.Newf("%s: the application is already tenanted on %q", site.Generator.File, site.DomainRoute)
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
	if err := tn.editConfig(a, ch); err != nil {
		return nil, err
	}
	if err := tn.editApp(a, ch); err != nil {
		return nil, err
	}
	if err := tn.copyTenantService(a, g, ch); err != nil {
		return nil, err
	}

	if out, err := exec.Run(ctx, a.Root, nil, "go", "generate", "./..."); err != nil {
		ch.skipf("go generate ./... failed, so the tenant routes are not generated yet; fix the cause and run it:\n%s", strings.TrimSpace(string(out)))
	} else {
		ch.didf("ran go generate ./..., which emitted the %s resource and the tenant segment pair under /%s/{%sID}", tn.Record(), tn.Segment(), strcase.ToCamel(tn.Record()))
	}

	return ch, nil
}

// editProgram adds the tenancy options after GenerateRoutes.
func (tn Tenancy) editProgram(a *app.App, g *app.Generator, ch *Change) error {
	src, mode, err := readFile(a, g.File)
	if err != nil {
		return err
	}
	options := []string{
		fmt.Sprintf("generation.WithDomainRoute(%q)", tn.Segment()),
		"generation.WithConcealedDomains()",
	}
	edited, err := app.InsertOptions(g.File, src, "GenerateRoutes", options)
	if err != nil {
		return err
	}
	if err := os.WriteFile(a.Abs(g.File), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	ch.didf("%s: added WithDomainRoute(%q) and WithConcealedDomains()", g.File, tn.Segment())

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
	ch.didf("%s/%s.up.sql and .down.sql: the development tenants north and south, a data migration for the bootstrap to apply before the roles", dir, base)

	return nil
}

// writeRecord adds the tenant-record resource struct.
func (tn Tenancy) writeRecord(a *app.App, resourceDir string, ch *Change) error {
	rel := path.Join(resourceDir, strings.ToLower(tn.Table)+".go")
	if _, err := os.Stat(a.Abs(rel)); err == nil {
		ch.skipf("%s already exists, so the %s resource struct was not written; make sure it is a global @resource with a slug Id and a Name", rel, tn.Record())

		return nil
	}
	pkg := path.Base(resourceDir)
	if entries, err := os.ReadDir(a.Abs(resourceDir)); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
				if src, err := os.ReadFile(a.Abs(path.Join(resourceDir, e.Name()))); err == nil {
					if name, err := app.PackageName(e.Name(), src); err == nil {
						pkg = name

						break
					}
				}
			}
		}
	}
	src := fmt.Sprintf(`package %s

type (
	// %[2]s is the tenant record: its route name equals the domain route segment, so
	// /%[3]s lists the tenants while /%[3]s/{%[4]sID}/... serves the tenant-scoped routes.
	// The application derives its domain universe from this table rather than a fixed
	// in-code list: the deployment reads it for MigrateRoles and the DomainVisible seam
	// checks it, so the tenant list is data.
	//
	// The primary key is a human-readable slug, not a UUID: tenant identifiers appear in
	// every tenant-scoped URL and in role provisioning, and the schema enforces the slug
	// shape with a CHECK constraint. Creating a tenant therefore supplies its key.
	//
	// %[2]s itself is a GLOBAL resource: administering the tenant list is a global
	// concern.
	//
	// @resource
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

// editConfig gives the data level the tenant roster and the visibility seam: a new file
// with the methods, a field on DataConfiguration, and the roster load at construction.
func (tn Tenancy) editConfig(a *app.App, ch *Change) error {
	rel, src, mode, err := findDeclaringFile(a, "DataConfiguration")
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no file declares a DataConfiguration struct, so the tenant roster and DomainVisible were not added to the data level; add a roster read from %s at startup, Domains() listing it, and DomainVisible(ctx, user, domain) answering roster membership AND access.UserHasGrants", tn.Table)

		return nil
	}
	spannerField, err := app.StructFieldOfType(rel, src, "DataConfiguration", "cloud.google.com/go/spanner", "Client")
	if err != nil {
		return err
	}
	accessField, err := app.StructFieldOfType(rel, src, "DataConfiguration", "github.com/cccteam/access", "Client")
	if err != nil {
		return err
	}
	if spannerField == "" || accessField == "" {
		ch.skipf("%s: DataConfiguration holds no *spanner.Client and *access.Client fields the roster could read through, so the tenant roster and DomainVisible were not added; add them", rel)

		return nil
	}
	pkg, err := app.PackageName(rel, src)
	if err != nil {
		return err
	}
	edited, err := app.AddStructField(rel, src, "DataConfiguration", "tenants tenantRoster")
	if err != nil {
		return err
	}
	wrapErr := "err"
	if ok, _ := app.HasImport(rel, src, "github.com/go-playground/errors/v5"); ok {
		wrapErr = `errors.Wrap(err, "loadTenants()")`
	}
	edited, err = app.WrapReturn(rel, edited, "NewDataConfiguration", "DataConfiguration", "conf", "conf.loadTenants(ctx)", wrapErr)
	if errors.Is(err, app.ErrNoAnchor) {
		ch.skipf("%s: NewDataConfiguration does not end in \"return &DataConfiguration{...}, nil\", so the roster load was not inserted; call conf.loadTenants(ctx) once the configuration is built", rel)
	} else if err != nil {
		return err
	}
	if err := os.WriteFile(a.Abs(rel), edited, mode); err != nil {
		return errors.Wrap(err, "os.WriteFile()")
	}
	tenancyFile := path.Join(path.Dir(rel), "tenancy.go")
	if err := writeNew(a, tenancyFile, tn.configSource(pkg, spannerField, accessField)); err != nil {
		return err
	}
	ch.didf("%s: the tenant roster (tenantRoster, loaded from %s at startup), Domains(), and DomainVisible() on DataConfiguration; %s gained the field and the load", tenancyFile, tn.Table, rel)

	return nil
}

func (tn Tenancy) configSource(pkg, spannerField, accessField string) string {
	return fmt.Sprintf(`package %[1]s

import (
	"context"
	stderrors "errors"

	cloudspanner "cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/iterator"
)

// tenantRoster is the tenant list read from the %[2]s table once at startup. It is
// cached rather than queried per check: the generated consolidated handler consults
// DomainVisible inside the mutation transaction, where opening another read is illegal
// on the emulator. A process restart picks up new tenants.
type tenantRoster struct {
	domains []accesstypes.Domain
	set     map[accesstypes.Domain]bool
}

// Domains lists the tenants as permission domains, from the roster read at startup.
func (c *DataConfiguration) Domains(_ context.Context) ([]accesstypes.Domain, error) {
	return c.tenants.domains, nil
}

// DomainVisible reports whether the domain is a known tenant AND the user holds at least
// one grant in it: existence from the startup roster, foothold from the permission
// engine's in-memory policy snapshot (no store read, so it is safe inside the
// consolidated handler's mutation transaction). Tenant existence is concealed: a caller
// with no foothold is answered exactly like the tenant does not exist.
func (c *DataConfiguration) DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
	if !c.tenants.set[domain] {
		return false, nil
	}

	visible, err := c.%[4]s.UserHasGrants(ctx, user, accesstypes.DomainScope(domain))
	if err != nil {
		return false, errors.Wrap(err, "access.Client.UserHasGrants()")
	}

	return visible, nil
}

// loadTenants reads the tenant roster from the %[2]s table.
func (c *DataConfiguration) loadTenants(ctx context.Context) error {
	iter := c.%[3]s.Single().Query(ctx, cloudspanner.Statement{SQL: "SELECT Id FROM %[2]s ORDER BY Id"})
	defer iter.Stop()

	c.tenants.set = make(map[accesstypes.Domain]bool)
	for {
		row, err := iter.Next()
		if err != nil {
			if stderrors.Is(err, iterator.Done) {
				return nil
			}

			return errors.Wrap(err, "spanner.RowIterator.Next()")
		}

		var id string
		if err := row.Columns(&id); err != nil {
			return errors.Wrap(err, "spanner.Row.Columns()")
		}
		c.tenants.domains = append(c.tenants.domains, accesstypes.Domain(id))
		c.tenants.set[accesstypes.Domain(id)] = true
	}
}
`, pkg, tn.Table, spannerField, accessField)
}

// editApp exposes the seam to the generated code: the Configurer requires it, the App
// carries it, and a new file declares the types and the method.
func (tn Tenancy) editApp(a *app.App, ch *Change) error {
	rel, src, mode, err := findDeclaringFile(a, "Configurer")
	if err != nil {
		return err
	}
	if rel == "" {
		ch.skipf("no file declares a Configurer interface, so DomainVisible was not exposed on the app; the generated code needs DomainVisible(ctx, user, domain) (bool, error) on the handlers")

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
	edited, err = app.AddStructField(rel, edited, "App", "domainVisible DomainVisibleFunc")
	if errors.Is(err, app.ErrNoAnchor) {
		ch.skipf("%s: no App struct to carry the seam; give the handlers a DomainVisible method that defers to the configurer", rel)
		edited = nil
	} else if err != nil {
		return err
	}
	if edited != nil {
		edited, err = app.AddLiteralElement(rel, edited, "New", "App", "domainVisible: cfg.DomainVisible")
		if errors.Is(err, app.ErrNoAnchor) {
			ch.skipf("%s: New builds no App literal; set domainVisible from the configurer where the App is constructed", rel)
		} else if err != nil {
			return err
		}
	}
	if edited != nil {
		if err := os.WriteFile(a.Abs(rel), edited, mode); err != nil {
			return errors.Wrap(err, "os.WriteFile()")
		}
	}
	tenancyFile := path.Join(path.Dir(rel), "tenancy.go")
	if err := writeNew(a, tenancyFile, tn.appSource(pkg)); err != nil {
		return err
	}
	ch.didf("%s: TenancyConfigurer (embedded in Configurer), DomainVisibleFunc, and App.DomainVisible; %s gained the field and its assignment", tenancyFile, rel)

	return nil
}

func (tn Tenancy) appSource(pkg string) string {
	return fmt.Sprintf(`package %s

import (
	"context"

	"github.com/cccteam/ccc/accesstypes"
)

// TenancyConfigurer is the tenancy seam the configuration provides. Tenant existence is
// concealed (generation.WithConcealedDomains): DomainVisible answers whether the tenant
// exists AND the caller holds at least one grant in it, so a prober cannot confirm a
// tenant exists from the rejection shape.
type TenancyConfigurer interface {
	DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error)
}

// DomainVisibleFunc is the seam as the App carries it.
type DomainVisibleFunc = func(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error)

// DomainVisible reports whether the tenant exists and the user holds at least one grant
// in it; the generated DomainGuard middleware and the consolidated dispatcher answer "no"
// with the same not-found an unknown tenant gets.
func (a *App) DomainVisible(ctx context.Context, user accesstypes.User, domain accesstypes.Domain) (bool, error) {
	return a.domainVisible(ctx, user, domain)
}
`, pkg)
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
	fmt.Fprintf(&b, "Tenancy is data. The %s table is the domain universe: a permission domain per row, read into a roster at startup, and every struct annotated `@permissionScope(domain)` is served under the tenant segment pair `/%s/{%sID}/...` with the tenant as its permission domain. `%s` itself is a global resource, since administering the tenant list is a global concern. Tenant existence is concealed (`WithConcealedDomains`): the generated guard asks `DomainVisible`, which answers whether the tenant exists AND the caller holds at least one grant in it, so a login with no foothold gets the same not-found an unknown tenant gets. A login's tenant list (`user-domains`) is the set of tenants where it holds a grant, and a grant needs a tenant-scoped resource to land on.\n\n", tn.Table, seg, strcase.ToCamel(rec), rec)
	b.WriteString("Left to wire, in this order:\n\n")
	items := []string{
		"The bootstrap and the deployment. Seed the development tenants (`schema/devseed`, a data migration applied with the migrator's data step) BEFORE the roles, then pass the roster (`Domains()`) to `MigrateRoles` in both the bootstrap and the deployment's migrate step: the roles are reconciled per tenant, and `tenancy-wired` requires every `MigrateRoles` call to carry domains. Give the development logins roles in the tenants (the bootstrap identities file): the administrator everywhere, and add a member login that holds a role in one tenant only, so concealment is observable.",
		"Tenant-scoped resources. Decide which existing resources belong to a tenant: give each a `TenantId` column referencing the tenant table (a migration, with a data step assigning existing rows to a tenant), annotate the struct `@permissionScope(domain)` and the column `// @domain`, then run `go generate ./...`. If none of the application's resources is tenant-scoped yet, add a first one so the option is observable from the first sign-in; the reference has one. `tenancy-wired` requires at least one.",
		"The test harnesses. The authorization suite's configurer needs `DomainVisible` recognizing the generated matrix's domain (`testDomain`) when the case carries grants; the integration harness needs `DomainVisible` composed with `UserHasGrants` over the development tenants, the dev seed applied beside the schema, `MigrateRoles` with the tenants, the member's assignments, and a wait for the engine's snapshot to show them.",
		"Integration tests: the administrator lists both tenants and reads a tenant's digest; the member lists one and gets not-found in the other and in an unknown tenant; the tenant list is global.",
		"The browser app: a tenant picker in the header bound to the tenant service (its tenants, current, and select), and the pages reading permissions through it, so the digest follows the selected tenant.",
	}
	for i, item := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, item)
	}
	b.WriteString("\nData consequence: every row of a resource that becomes tenant-scoped needs a tenant. Write the assignment as a migration a person can review, never a default nobody chose.\n")

	return b.String()
}
