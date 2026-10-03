package check

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// tenancyWired verifies that tenancy is wired through the application: a site whose
// resource package declares a tenant record (@tenant) has resources that are actually
// tenant-scoped and a tenant roster built with the record's generated constructor, handed
// the tenants signal, and started outside tests; a site declaring no record has no
// tenant-scoped resource lying around. The generator refuses the rest (a tenant-scoped
// resource in a package with no record, a record that is not a global, table-backed
// resource with one string key, a second record), the compiler holds the application to
// the roster accessor the generated contract names (TenantRoster), and the permission
// engine reaches every tenant with the roles held in every domain, so this check covers
// what none of them sees: the record's annotation behind the tenant-scoped resources, and
// the roster that keeps the application's tenants current without a restart.
type tenancyWired struct{}

func (tenancyWired) Name() string { return "tenancy-wired" }

func (tenancyWired) Describe() string {
	return "a tenanted application has its tenant record (@tenant), tenant-scoped resources, and a tenant roster built with the generated constructor, handed the tenants signal, and started; an untenanted one has no tenant-scoped resources"
}

// The tenant roster's shape as the application wires it: the generated constructor's
// name around the record, the option handing it the tenants signal, and the method that
// loads it and keeps it current.
const (
	rosterConstructorPrefix = "New"
	rosterConstructorSuffix = "Roster"
	rosterSignalsOption     = "resource.WithTenantSignals"
	rosterStartMethod       = "Start"
)

// rosterConstructor is the generated constructor's name for a record.
func rosterConstructor(record string) string {
	return rosterConstructorPrefix + record + rosterConstructorSuffix
}

func (c tenancyWired) Run(_ context.Context, env *Env) Result {
	a := env.App
	p := a.Profile()
	if len(p.Sites) == 0 {
		return skip(c.Name(), "no site generator")
	}

	var details, notes, records, rosters []string
	scoped, tenanted := 0, 0
	for i := range p.Sites {
		site := &p.Sites[i]
		resources := siteResources(a, p, site)
		if !site.Tenanted() {
			details = append(details, untenantedFindings(site, resources)...)

			continue
		}
		tenanted++
		if !contains(records, site.TenantRecord.Name) {
			records = append(records, site.TenantRecord.Name)
		}
		scoped += len(resources)
		if len(resources) == 0 {
			details = append(details, fmt.Sprintf("%s: no struct in %s is annotated @permissionScope(domain): every resource is global, and the tenant segment serves nothing", site.Generator.File, site.Generator.ResourcePackageDir))
		}
		found, built, noted, err := rosterFindings(a, site.TenantRecord)
		if err != nil {
			return fail(c.Name(), err.Error())
		}
		details = append(details, found...)
		notes = append(notes, noted...)
		for _, b := range built {
			if !contains(rosters, b) {
				rosters = append(rosters, b)
			}
		}
	}

	if len(details) > 0 {
		if tenanted == 0 {
			return fail(c.Name(), fmt.Sprintf("%d tenancy wiring problem(s) in an untenanted application", len(details)), append(details, notes...)...)
		}

		return fail(c.Name(), fmt.Sprintf("%d tenancy wiring problem(s)", len(details)), append(details, notes...)...)
	}
	if tenanted == 0 {
		return pass(c.Name(), "not tenanted: no tenant-scoped resources")
	}

	summary := fmt.Sprintf("tenant record %s; %d tenant-scoped resource(s); roster built by %s", strings.Join(records, ", "), scoped, strings.Join(rosters, ", "))

	return passWithDetails(c.Name(), summary, notes...)
}

// untenantedFindings are the tenant-scoped structs a site declaring no tenant record must
// not carry: the generator refuses each, since its routes would be served under a segment
// no record names.
func untenantedFindings(site *app.Site, resources []app.DomainResource) []string {
	details := make([]string, 0, len(resources))
	for _, r := range resources {
		details = append(details, fmt.Sprintf("%s:%d: %s is @permissionScope(domain), but no struct in %s is annotated @tenant; a tenant-scoped resource needs a tenant record, the global resource whose rows are the tenants", r.File, r.Line, r.Name, site.Generator.ResourcePackageDir))
	}

	return details
}

// siteResources lists the tenant-scoped structs of a site's resource package. A struct
// declared in a package no site generator reads belongs to the first site, so a flat
// application's every declaration is seen.
func siteResources(a *app.App, p app.Profile, site *app.Site) []app.DomainResource {
	owned := map[string]bool{}
	for i := range p.Sites {
		owned[p.Sites[i].Generator.ResourcePackageDir] = true
	}
	var resources []app.DomainResource
	for _, r := range a.DomainResources {
		dir := path.Dir(r.File)
		if dir == site.Generator.ResourcePackageDir || (!owned[dir] && site == &p.Sites[0]) {
			resources = append(resources, r)
		}
	}

	return resources
}

// rosterFindings are the findings about the roster over a tenant record: no construction
// outside tests with the record's generated constructor, a construction handed no tenants
// signal, and a construction nothing starts. built lists the constructions that pass, as
// "<constructor> at <position>"; notes carries a construction whose start cannot be
// followed.
func rosterFindings(a *app.App, record *app.TenantRecord) (details, built, notes []string, err error) {
	constructor := rosterConstructor(record.Name)
	var constructions []app.RosterConstruction
	for _, r := range a.RosterConstructions {
		if r.Constructor == constructor {
			constructions = append(constructions, r)
		}
	}
	if len(constructions) == 0 {
		details = append(details, fmt.Sprintf("%s:%d: no file outside tests calls %s, the generated constructor of the tenant roster over %s; build the roster in the data level (%s(client, %s(<live service>))) and start it (%s) before serving, so the generated DomainGuard knows the tenants and a tenant created on one instance reaches the others", record.File, record.Line, constructor, record.Name, constructor, rosterSignalsOption, rosterStartMethod))

		return details, nil, nil, nil
	}
	for _, r := range constructions {
		pos := fmt.Sprintf("%s:%d", r.File, r.Line)
		ok := true
		if !r.Signals {
			ok = false
			details = append(details, fmt.Sprintf("%s: %s is handed no %s; pass the live service the data level opens, so a tenant created on another instance reaches this one at once rather than at the roster's backstop", pos, constructor, rosterSignalsOption))
		}
		switch started, followed, err := rosterStarted(a, &r); {
		case err != nil:
			return nil, nil, nil, err
		case !followed:
			notes = append(notes, fmt.Sprintf("%s: %s's result is not bound to a variable or a field here, so whether %s is called on it was not read", pos, constructor, rosterStartMethod))
		case !started:
			ok = false
			details = append(details, fmt.Sprintf("%s: no file in %s calls %s on the roster %s builds (%s), so it is never loaded and the generated DomainGuard knows no tenant; start it where the data level is built and fail the start on its error", pos, r.Package, rosterStartMethod, constructor, r.Holder))
		}
		if ok {
			built = append(built, constructor+" at "+pos)
		}
	}

	return details, built, notes, nil
}

// rosterStarted reports whether a hand-written file of the construction's package calls
// Start on the name the construction's result is bound to; followed is false when the
// result is bound to no name.
func rosterStarted(a *app.App, r *app.RosterConstruction) (started, followed bool, err error) {
	if r.Holder == "" {
		return false, false, nil
	}
	entries, err := os.ReadDir(a.Abs(r.Package))
	if err != nil {
		return false, true, errors.Wrap(err, "os.ReadDir()")
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, "zz_gen_") {
			continue
		}
		rel := path.Join(r.Package, name)
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return false, true, errors.Wrap(err, "os.ReadFile()")
		}
		f, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.SkipObjectResolution)
		if err != nil {
			return false, true, errors.Wrap(err, "parser.ParseFile()")
		}
		if callsMethodOn(f, r.Holder, rosterStartMethod) {
			return true, true, nil
		}
	}

	return false, true, nil
}

// callsMethodOn reports whether the file calls the named method on an expression whose
// final name is holder (conf.tenants.Start for the holder tenants, roster.Start for the
// holder roster).
func callsMethodOn(f *ast.File, holder, method string) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != method {
			return true
		}
		if receiver := finalName(sel.X); receiver == holder {
			found = true
		}

		return !found
	})

	return found
}

// finalName is the last name of an identifier or selector expression, or empty.
func finalName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.SelectorExpr:
		return expr.Sel.Name
	default:
		return ""
	}
}

// contains reports whether the list holds the string.
func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}

	return false
}

// createTableRE finds the table a migration statement creates.
var createTableRE = regexp.MustCompile("(?i)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?`?([A-Za-z_][A-Za-z0-9_]*)`?")

// fileScheme prefixes a migration source the checks can read.
const fileScheme = "file://"

// migrationTables reads the up migrations of every site's file:// migration sources and
// returns the created tables by name, each with the migration that creates it. Sources
// the checks cannot read are returned as notes.
func migrationTables(a *app.App, p app.Profile) (tables map[string]string, unread []string, err error) {
	tables = map[string]string{}
	seen := map[string]bool{}
	for i := range p.Sites {
		s := &p.Sites[i]
		for _, src := range s.Generator.MigrationSources {
			if seen[src] {
				continue
			}
			seen[src] = true
			if !strings.HasPrefix(src, fileScheme) {
				unread = append(unread, fmt.Sprintf("%s: migration source %s is not a file:// path; its tables were not read", s.Generator.File, src))

				continue
			}
			if err := readTables(a, strings.TrimPrefix(src, fileScheme), tables); err != nil {
				return nil, nil, err
			}
		}
	}

	return tables, unread, nil
}

// readTables adds the tables the up migrations under a root-relative directory create.
func readTables(a *app.App, dir string, tables map[string]string) error {
	entries, err := os.ReadDir(a.Abs(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.Wrap(err, "os.ReadDir()")
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(dir, e.Name()))
		data, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return errors.Wrap(err, "os.ReadFile()")
		}
		for _, m := range createTableRE.FindAllSubmatch(data, -1) {
			if name := string(m[1]); tables[name] == "" {
				tables[name] = rel
			}
		}
	}

	return nil
}
