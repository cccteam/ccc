package check

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// tenancyWired verifies that the tenancy option is wired through the application: a
// tenanted program has a tenant-record table behind its domain route and resources that
// are actually tenant-scoped; an untenanted program has neither half lying around. The
// compiler holds the rest of the seam (the DomainExists and DomainVisible contract), the
// generator refuses a tenant-scoped resource without its @domain binding, and the
// permission engine reaches every tenant with the roles held in every domain, so this
// check covers what none of them sees.
type tenancyWired struct{}

func (tenancyWired) Name() string { return "tenancy-wired" }

func (tenancyWired) Describe() string {
	return "a tenanted application has its tenant-record table and tenant-scoped resources; an untenanted one has neither"
}

// createTableRE finds the table a migration statement creates.
var createTableRE = regexp.MustCompile("(?i)CREATE\\s+TABLE\\s+(?:IF\\s+NOT\\s+EXISTS\\s+)?`?([A-Za-z_][A-Za-z0-9_]*)`?")

// fileScheme prefixes a migration source the check can read.
const fileScheme = "file://"

func (c tenancyWired) Run(_ context.Context, env *Env) Result {
	a := env.App
	p := a.Profile()
	if len(p.Sites) == 0 {
		return skip(c.Name(), "no site generator")
	}

	site := &p.Sites[0]
	if !site.Tenanted() {
		return c.untenanted(a)
	}

	var details []string
	tables, unread, err := tenantTables(a, p, site.DomainRoute)
	if err != nil {
		return fail(c.Name(), err.Error())
	}
	details = append(details, unread...)
	if len(tables) == 0 {
		details = append(details, fmt.Sprintf("WithDomainRoute(%q): no migration creates a table named like it (the tenant-record table)", site.DomainRoute))
	}
	if len(a.DomainResources) == 0 {
		details = append(details, "no struct is annotated @permissionScope(domain): every resource is global, and the tenant segment serves nothing")
	}

	if len(details) > len(unread) {
		return fail(c.Name(), fmt.Sprintf("%d tenancy wiring problem(s)", len(details)-len(unread)), details...)
	}

	summary := fmt.Sprintf("tenant record %s; %d tenant-scoped resource(s)", strings.Join(tables, ", "), len(a.DomainResources))

	return passWithDetails(c.Name(), summary, unread...)
}

// untenanted reports the tenancy half an application without WithDomainRoute must not
// carry: a tenant-scoped resource would be served under the generator's default
// /domain/{...} pair.
func (c tenancyWired) untenanted(a *app.App) Result {
	details := make([]string, 0, len(a.DomainResources))
	for _, r := range a.DomainResources {
		details = append(details, fmt.Sprintf("%s:%d: %s is @permissionScope(domain), but no WithDomainRoute names the tenant segment; it is served under the default /domain/{domain}/ pair", r.File, r.Line, r.Name))
	}
	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d tenancy wiring problem(s) in an untenanted application", len(details)), details...)
	}

	return pass(c.Name(), "not tenanted: no tenant-scoped resources")
}

// tenantTables lists the tables, across every site's file:// migration sources, whose
// names kebab-case to the domain route segment, as "Table (file)". Sources the check
// cannot read are returned as notes.
func tenantTables(a *app.App, p app.Profile, segment string) (tables, unread []string, err error) {
	all, unread, err := migrationTables(a, p)
	if err != nil {
		return nil, nil, err
	}
	for name, file := range all {
		if strcase.ToKebab(name) == segment {
			tables = append(tables, fmt.Sprintf("%s (%s)", name, file))
		}
	}
	sort.Strings(tables)

	return tables, unread, nil
}

// migrationTables reads the up migrations of every site's file:// migration sources and
// returns the created tables by name, each with the migration that creates it. Sources
// the check cannot read are returned as notes.
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
