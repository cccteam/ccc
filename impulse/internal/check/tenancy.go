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

	"github.com/cccteam/ccc/impulse/internal/app"
)

// tenancyWired verifies that the tenancy option is wired through the application: a
// tenanted program has a tenant-record table behind its domain route, resources that are
// actually tenant-scoped, and roles provisioned into the tenants; an untenanted program
// has none of those halves lying around. The compiler holds the rest of the seam (the
// DomainExists and DomainVisible contract), and the generator refuses a tenant-scoped
// resource without its @domain binding, so this check covers what neither of them sees.
type tenancyWired struct{}

func (tenancyWired) Name() string { return "tenancy-wired" }

func (tenancyWired) Describe() string {
	return "a tenanted application has its tenant-record table, tenant-scoped resources, and per-tenant roles; an untenanted one has none"
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
	if len(a.RoleMigrations) == 0 {
		details = append(details, "no access.MigrateRoles call outside tests: roles are never provisioned")
	}
	for _, m := range a.RoleMigrations {
		if !m.WithDomains() {
			details = append(details, fmt.Sprintf("%s:%d: access.MigrateRoles is called without domains; roles never reach the tenants", m.File, m.Line))
		}
	}

	if len(details) > len(unread) {
		return fail(c.Name(), fmt.Sprintf("%d tenancy wiring problem(s)", len(details)-len(unread)), details...)
	}

	summary := fmt.Sprintf("tenant record %s; %d tenant-scoped resource(s); roles provisioned per tenant in %d place(s)",
		strings.Join(tables, ", "), len(a.DomainResources), len(a.RoleMigrations))

	return passWithDetails(c.Name(), summary, unread...)
}

// untenanted reports the tenancy halves an application without WithDomainRoute must not
// carry: a tenant-scoped resource would be served under the generator's default
// /domain/{...} pair, and roles passed domains have no tenants to land in. A spread
// argument (domains...) is not a finding: the skeleton's wrapper passes its own, empty,
// variadic through so that adding tenancy later changes the callers and not the wrapper.
func (c tenancyWired) untenanted(a *app.App) Result {
	var details []string
	for _, r := range a.DomainResources {
		details = append(details, fmt.Sprintf("%s:%d: %s is @permissionScope(domain), but no WithDomainRoute names the tenant segment; it is served under the default /domain/{domain}/ pair", r.File, r.Line, r.Name))
	}
	for _, m := range a.RoleMigrations {
		if m.Domains > 0 {
			details = append(details, fmt.Sprintf("%s:%d: access.MigrateRoles receives %d domain(s), but the application is not tenanted", m.File, m.Line, m.Domains))
		}
	}
	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d tenancy wiring problem(s) in an untenanted application", len(details)), details...)
	}

	return pass(c.Name(), "not tenanted: no tenant-scoped resources, roles provisioned globally")
}

// tenantTables lists the tables, across every site's file:// migration sources, whose
// names kebab-case to the domain route segment, as "Table (file)". Sources the check
// cannot read are returned as notes.
func tenantTables(a *app.App, p app.Profile, segment string) (tables, unread []string, err error) {
	seen := map[string]bool{}
	for _, s := range p.Sites {
		for _, src := range s.Generator.MigrationSources {
			if seen[src] {
				continue
			}
			seen[src] = true
			if !strings.HasPrefix(src, fileScheme) {
				unread = append(unread, fmt.Sprintf("%s: migration source %s is not a file:// path; the tenant-record table was not checked there", s.Generator.File, src))

				continue
			}
			dir := strings.TrimPrefix(src, fileScheme)
			found, err := tablesNamed(a, dir, segment)
			if err != nil {
				return nil, nil, err
			}
			tables = append(tables, found...)
		}
	}
	sort.Strings(tables)

	return tables, unread, nil
}

// tablesNamed reads the up migrations under a root-relative directory and returns the
// created tables whose kebab-cased names equal segment.
func tablesNamed(a *app.App, dir, segment string) ([]string, error) {
	entries, err := os.ReadDir(a.Abs(dir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadDir()")
	}

	var tables []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(dir, e.Name()))
		data, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return nil, errors.Wrap(err, "os.ReadFile()")
		}
		for _, m := range createTableRE.FindAllSubmatch(data, -1) {
			if name := string(m[1]); strcase.ToKebab(name) == segment {
				tables = append(tables, fmt.Sprintf("%s (%s)", name, rel))
			}
		}
	}

	return tables, nil
}
