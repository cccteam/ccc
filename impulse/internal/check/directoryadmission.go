package check

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// directoryAdmission verifies that an application whose tenant record declares
// directories seats a stranger the way the framework admits them: the directories
// record (@tenantDirectory) carries a column annotated @directoryDefaultSeat, the role a
// stranger the directory vouches for is seated with at their first sign-in, NULL where
// the tenant admits them by invitation only; every auth whose roles the application
// assigns (one registering no method with RoleSync, whose directory would write them)
// sets the directory admission on its session Auth (SetDirectoryAdmission, over the
// generated directory reader), so a stranger from a declared directory is created seated
// with the row's role or refused with no account; and a role in a role file handed to the permission
// engine grants Update on the directories record with the default seat field, so the
// tenant's administrator chooses the seat on the Sign-in page. The generator holds the
// record's shape and the session library refuses an admission over nothing, but the
// column, the call and the grant are each taken by hand, which nothing compiles
// against, and this check holds them.
type directoryAdmission struct{}

func (directoryAdmission) Name() string {
	return "directory-admission"
}

func (directoryAdmission) Describe() string {
	return "an application whose tenant record declares directories (@tenantDirectory) has a column annotated @directoryDefaultSeat on the directories record, every auth whose roles the application assigns (one registering no method with RoleSync) calls SetDirectoryAdmission, and a role in a role file handed to the permission engine grants Update on the directories record with its default seat field"
}

// The directory admission's shape as the application wires it: the method set on the
// session Auth, the Settings field carrying the generated directory reader, the
// reader's constructor around the tenant record, and the permission the default seat is
// chosen under.
const (
	admissionMethod         = "SetDirectoryAdmission"
	directoriesSetting      = "TenantDirectories"
	directoryReaderPrefix   = "New"
	directoryReaderSuffix   = "DirectoryReader"
	defaultSeatPermission   = "Update"
	directoryDefaultSeatTag = "@directoryDefaultSeat"
	tenantDirectoryTag      = "@tenantDirectory"
)

// directoriesOf is one site's directories record with the site declaring it.
type directoriesOf struct {
	site   *app.Site
	record *app.DirectoryRecord
}

func (c directoryAdmission) Run(_ context.Context, env *Env) Result {
	a := env.App
	p := a.Profile()
	if len(p.Sites) == 0 {
		return skip(c.Name(), "no site generator")
	}
	var declared []directoriesOf
	for i := range p.Sites {
		site := &p.Sites[i]
		if site.Directories != nil && !slices.ContainsFunc(declared, func(d directoriesOf) bool { return d.record == site.Directories }) {
			declared = append(declared, directoriesOf{site: site, record: site.Directories})
		}
	}
	if len(declared) == 0 {
		return skip(c.Name(), "no tenant record declares directories")
	}

	var details, records, grants []string
	for _, d := range declared {
		if d.record.DefaultSeatField == "" {
			details = append(details, fmt.Sprintf("%s:%d: %s declares no column annotated %s; the tenant directory record carries a nullable DefaultSeat column (STRING(64), annotated %s; a migration adds it to the directories table), what a stranger the directory vouches for is seated with at their first sign-in, chosen by the tenant's administrator on the Sign-in page", d.record.File, d.record.Line, d.record.Name, directoryDefaultSeatTag, directoryDefaultSeatTag))

			continue
		}
		records = append(records, fmt.Sprintf("%s (default seat %s)", d.record.Name, d.record.DefaultSeatField))
	}
	set, found, err := admissionFindings(a, declared[0].site)
	if err != nil {
		return fail(c.Name(), err.Error())
	}
	details = append(details, found...)
	for _, d := range declared {
		if d.record.DefaultSeatField == "" {
			continue
		}
		granted, found := grantFindings(a, &d)
		details = append(details, found...)
		grants = append(grants, granted...)
	}

	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d directory admission wiring problem(s)", len(details)), details...)
	}
	admission := "admission set by no auth (every auth's roles are its directory's)"
	if len(set) > 0 {
		admission = "admission set by " + strings.Join(set, ", ")
	}

	return pass(c.Name(), fmt.Sprintf("directories record %s; %s; %s", strings.Join(records, ", "), admission, strings.Join(grants, ", ")))
}

// admissionFindings holds every auth whose roles the application assigns to the
// directory admission: set lists the auths calling SetDirectoryAdmission, details the
// ones calling it nowhere. The reader the finding names is the generated one around the
// site's tenant record.
func admissionFindings(a *app.App, site *app.Site) (set, details []string, err error) {
	reader := directoryReaderPrefix + "<Record>" + directoryReaderSuffix
	if site.TenantRecord != nil {
		reader = directoryReaderPrefix + site.TenantRecord.Name + directoryReaderSuffix
	}
	for i := range a.AuthPackages {
		p := &a.AuthPackages[i]
		if !assignsRoles(a, p) {
			continue
		}
		calls, err := callsInPackage(a, p.Dir, admissionMethod)
		if err != nil {
			return nil, nil, err
		}
		if calls {
			set = append(set, p.Name)

			continue
		}
		details = append(details, fmt.Sprintf("%s: nothing in the %s auth calls %s, and the application assigns its roles (no method is registered with RoleSync); the auth carries the generated directory reader in its Settings (%s, %s) and sets %s(settings.%s, resource.SignInRoles(engine.UserManager()), resource.SeatDeclared(engine.UserManager())) beside its sign-in policy, so a stranger from a declared directory is created seated with the row's role or refused by invitation only (invitation_only) with no account, before the registration's account rule", p.Dir, p.Name, admissionMethod, directoriesSetting, reader, admissionMethod, directoriesSetting))
	}

	return set, details, nil
}

// assignsRoles reports an auth whose roles the application assigns: one registering no
// method of directory authority (RoleSync, as session-tables classifies it), since a
// registration whose directory writes the roles stands alone on its auth. The password
// method and a directory method registered with DisableRoleSync are the application's.
// An auth the scan reads no construction of is not held.
func assignsRoles(a *app.App, p *app.AuthPackage) bool {
	auth := authOf(a, p)
	if auth == nil {
		return false
	}
	for i := range auth.Methods {
		if auth.Methods[i].Authority == app.AuthorityDirectory {
			return false
		}
	}

	return true
}

// callsInPackage reports whether a hand-written file of the package calls the named
// method on anything.
func callsInPackage(a *app.App, dir, method string) (bool, error) {
	entries, err := os.ReadDir(a.Abs(dir))
	if err != nil {
		return false, errors.Wrap(err, "os.ReadDir()")
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || isTestFile(name) || strings.HasPrefix(name, "zz_gen_") {
			continue
		}
		rel := path.Join(dir, name)
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return false, errors.Wrap(err, "os.ReadFile()")
		}
		f, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.SkipObjectResolution)
		if err != nil {
			return false, errors.Wrap(err, "parser.ParseFile()")
		}
		if callsMethod(f, method) {
			return true, nil
		}
	}

	return false, nil
}

// callsMethod reports whether the file calls the named method on any receiver.
func callsMethod(f *ast.File, method string) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if found {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == method {
				found = true
			}
		}

		return !found
	})

	return found
}

// grantFindings holds the directories record to its grant: a role in a role file handed
// to the permission engine grants Update on the record with its default seat field.
// granted names the grant for the summary; details carries the finding when no role
// file grants it, or one cannot be read.
func grantFindings(a *app.App, d *directoriesOf) (granted, details []string) {
	resource := resourceOf(d.site, d.record.Name)
	field := d.record.DefaultSeatName
	var read []string
	for _, f := range engineRolesFiles(a) {
		read = append(read, f.Path)
		ok, err := grantsField(a.Abs(f.Path), defaultSeatPermission, resource, field)
		if err != nil {
			details = append(details, fmt.Sprintf("%s: cannot be read: %v", f.Path, errors.Cause(err)))

			continue
		}
		if ok {
			return []string{fmt.Sprintf("%s on %s.%s granted in %s", defaultSeatPermission, resource, field, f.Path)}, details
		}
	}
	where := "no role in " + strings.Join(read, ", ")
	if len(read) == 0 {
		where = "no role file reaches access.WithDefaultRoles (see auths-wired), so no role"
	}
	details = append(details, fmt.Sprintf("%s:%d: %s grants %s on %s with the field %s; the administrator role's grants on the directory record carry %s on Create, List and Read and gain %s on it, so the tenant's administrator chooses the default seat on the Sign-in page", d.record.File, d.record.Line, where, defaultSeatPermission, resource, field, field, defaultSeatPermission))

	return nil, details
}

// grantsField reports whether a role of the file grants the permission on the resource
// with the field.
func grantsField(abs, permission, resource, field string) (bool, error) {
	doc, err := readRolesFile(abs)
	if err != nil {
		return false, err
	}
	for _, role := range slices.Concat(doc.Roles.Global, doc.Roles.Domain) {
		for _, g := range role.Permissions[permission] {
			if g.Resource == resource && slices.Contains(g.Fields, field) {
				return true, nil
			}
		}
	}

	return false, nil
}

// pluralOverridesOption is the generator option naming the plurals the English rules
// get wrong.
const pluralOverridesOption = "WithPluralOverrides"

// resourceOf is a record's resource name as the role files grant it: the plural the
// generator derives, from the site's WithPluralOverrides when the option names the
// record, else by the generator's English suffix rules (a consonant and y become ies, a
// vowel and z double the z, a sibilant ending takes es, and everything else takes s).
func resourceOf(site *app.Site, name string) string {
	if c, ok := site.Generator.Option(pluralOverridesOption); ok && len(c.Args) == 1 && c.Args[0].Kind == app.ArgStringMap {
		if plural, ok := c.Args[0].Map[name]; ok {
			return plural
		}
	}
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "y") && len(lower) > 1 && !isVowel(lower[len(lower)-2]):
		return name[:len(name)-1] + "ies"
	case strings.HasSuffix(lower, "z") && len(lower) > 1 && isVowel(lower[len(lower)-2]):
		return name + "zes"
	case strings.HasSuffix(lower, "s"), strings.HasSuffix(lower, "x"), strings.HasSuffix(lower, "z"), strings.HasSuffix(lower, "ch"), strings.HasSuffix(lower, "sh"):
		return name + "es"
	default:
		return name + "s"
	}
}

// isVowel reports a vowel, for the plural rules.
func isVowel(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	default:
		return false
	}
}

// Meaning explains the invariant for the brief.
func (directoryAdmission) Meaning() string {
	return "A tenant that declares a directory decides what a stranger the directory vouches for gets at their first sign-in: the directories record (the struct annotated `@tenantDirectory`, the skeletons' `TenantDirectory` in `pkg/resources/tenant_directories.go`) carries a nullable default seat column (`DefaultSeat *string`, annotated `@directoryDefaultSeat`, STRING(64) in the directories table, which a migration adds), the tenant's role the stranger is seated with, or NULL, and the tenant admits them by invitation only. Every auth whose roles the application assigns (the password method, and a directory method registered with `DisableRoleSync`; an auth whose one registration carries `RoleSync` is the directory's) carries the generated directory reader in its Settings (`TenantDirectories`, built with `New<Record>DirectoryReader` on the data level) and sets the admission on its session Auth beside its sign-in policy, `authSession.SetDirectoryAdmission(settings.TenantDirectories, resource.SignInRoles(engine.UserManager()), resource.SeatDeclared(engine.UserManager()))`, so a stranger from a declared directory is created seated with the row's role, or refused with no account (`invitation_only`), before the registration's account rule; an auth whose directory writes the roles sets none. The tenant's administrator chooses the seat on the Sign-in page, so the administrator role's grants on the directories record carry the default seat field on Create, List and Read and gain Update on it (`pkg/auth/<auth>/roles.json`). Each piece is taken by hand, which nothing compiles against, and this check holds them."
}
