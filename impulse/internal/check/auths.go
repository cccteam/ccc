package check

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/app"
)

// authsWired verifies that every auth package is wired through the application: the data
// level constructs it, the release hands its embedded role file to the permission engine
// (access.WithDefaultRoles), and a surface binds to it, either an outlet declaring it
// (Auth, under the generated router) in the flavor the package constructs, or a
// hand-written surface taking its type, so a population the application declares can
// sign in somewhere and holds roles. session-tables holds each authenticator to its
// tables; this holds each auth to the application around it.
type authsWired struct{}

// authsWiredName is the check's name.
const authsWiredName = "auths-wired"

func (authsWired) Name() string { return authsWiredName }

func (authsWired) Describe() string {
	return "every auth package is constructed by the data level, embeds its role file and hands it to the permission engine (access.WithDefaultRoles), and is bound by a surface (an outlet's Auth declaration in the package's flavor, or a hand-written surface taking its type); no two auths share a session or XSRF cookie; a directory-run auth has no role writers in the application; and a role file handed to the engine is validated by a test (WARN)"
}

// The identifiers an auth package exports that the wiring is read from.
const (
	authNew   = "New"
	authRoles = "Roles"
	authType  = "Auth"
)

func (c authsWired) Run(_ context.Context, env *Env) Result {
	a := env.App
	if len(a.AuthPackages) == 0 {
		if len(a.Auths) == 0 {
			return skip(c.Name(), "no session authenticator is constructed")
		}

		return warn(c.Name(), "no auth package: the authenticators are constructed outside pkg/auth/<name> packages, so the auths cannot be told apart", authFiles(a)...)
	}

	profile := a.Profile()
	var details, warnings, summaries []string
	for i := range a.AuthPackages {
		p := &a.AuthPackages[i]
		found, warned := c.packageFindings(a, profile, p)
		details = append(details, found...)
		warnings = append(warnings, warned...)
		summaries = append(summaries, p.Name)
	}
	details = append(details, unknownAuthBindings(a, profile)...)
	details = append(details, cookieCollisions(a.Auths)...)
	if len(details) > 0 {
		return fail(c.Name(), fmt.Sprintf("%d auth wiring problem(s)", len(details)), append(details, warnings...)...)
	}
	wired := fmt.Sprintf("%d auth(s) constructed, roles handed to the engine, and bound: %s", len(summaries), strings.Join(summaries, ", "))
	if len(warnings) > 0 {
		return warn(c.Name(), fmt.Sprintf("%s; %d role file(s) without a validation test", wired, len(warnings)), warnings...)
	}

	return pass(c.Name(), wired)
}

// packageFindings checks one auth against the application: the findings that fail the
// check, and the warnings that do not (a role file in the engine that no test validates).
func (authsWired) packageFindings(a *app.App, profile app.Profile, p *app.AuthPackage) (details, warnings []string) {
	if len(p.References(authNew, isTestFile)) == 0 {
		details = append(details, fmt.Sprintf("%s: nothing outside tests calls %s.New; the %s auth is never constructed", p.Dir, p.Name, p.Name))
	}

	exported, embedded := rolesExport(a, p)
	switch {
	case !exported:
		details = append(details, fmt.Sprintf("%s: the package exports no %s(); the %s auth's role file reaches the permission engine as %s.%s() handed to access.WithDefaultRoles", p.Dir, authRoles, p.Name, p.Name, authRoles))
	case !embedded:
		details = append(details, fmt.Sprintf("%s: no //go:embed %s directive in the package; the %s auth's role file does not travel with the release", p.Dir, app.RolesFileName, p.Name))
	case len(defaultRolesOf(a, p)) == 0:
		details = append(details, fmt.Sprintf("%s: nothing outside tests hands %s.%s() to access.WithDefaultRoles; the %s auth's default roles never reach its permission engine", p.Dir, p.Name, authRoles, p.Name))
	case !validated(a, p):
		warnings = append(warnings, fmt.Sprintf("%s: no test validates the %s auth's role file (access.ValidateRoles over the collection, parsing %s.%s()), so a warning the deploy prints is accepted nowhere in code; add the %s row to %s with its expected warnings empty", p.Dir, p.Name, p.Name, authRoles, p.Name, ValidationTestFile(a)))
	}
	if rolesFile := rolesFileOf(p); exported {
		if _, err := os.Stat(a.Abs(rolesFile)); err != nil {
			details = append(details, fmt.Sprintf("%s: %s does not exist; the %s auth embeds its role file from there", p.Dir, rolesFile, p.Name))
		}
	}

	bound := p.References(authType, func(file string) bool {
		return isTestFile(file) || isDataLevel(file) || strings.HasPrefix(file, "cmd/")
	})
	declared := outletBindings(profile, p.Path)
	if len(bound) == 0 && len(declared) == 0 {
		details = append(details, fmt.Sprintf("%s: no outlet declares Auth(%q, ...) and no surface takes *%s.Auth; nothing binds to the %s auth, so its people can sign in nowhere", p.Dir, p.Path, p.Name, p.Name))
	}
	if constructed := flavorOf(a, p); constructed != "" {
		for i := range declared {
			o := &declared[i]
			if o.Auth.LoginFlavor() != constructed {
				details = append(details, fmt.Sprintf("%s: outlet %s binds to the %s auth as %s, but %s constructs a %s authenticator; the generated router would mount the wrong login routes", o.Pos, o.Name, p.Name, o.Auth.Flavor, p.Dir, constructed))
			}
		}
	}

	if authorityOf(a, p) == app.AuthorityDirectory {
		details = append(details, directoryWriters(a, p)...)
		if flavorOf(a, p) == app.FlavorOIDCGoogle {
			details = append(details, uppercaseRoles(a, p)...)
		}
	}

	return details, warnings
}

// uppercaseRoles finds the roles of a Google directory-run auth that no login can ever
// hold: the directory's groups assign roles by name, a group email is lowercase by
// nature, so session.GoogleRoleSync lowercases every derived name and the store compares
// them verbatim; a role defined with an uppercase letter is therefore never assigned,
// and a login in no role group is refused with no_roles.
func uppercaseRoles(a *app.App, p *app.AuthPackage) []string {
	rolesFile := rolesFileOf(p)
	if _, err := os.Stat(a.Abs(rolesFile)); err != nil {
		return nil // reported as missing already
	}
	names, err := roleNames(a.Abs(rolesFile))
	if err != nil {
		return []string{fmt.Sprintf("%s: %s: %v", p.Dir, rolesFile, err)}
	}
	var details []string
	for _, name := range names {
		if name != strings.ToLower(name) {
			details = append(details, fmt.Sprintf("%s: role %s in %s can never be held: the directory's groups assign roles by lowercase name (session.GoogleRoleSync), so rename it %s in the role file and everywhere it is named (the bootstrap identities, APP_ROLES in the environment template, the tests)", p.Dir, name, rolesFile, strings.ToLower(name)))
		}
	}

	return details
}

// roleNames reads the role names a role file defines, global and domain alike, in
// file order.
func roleNames(file string) ([]string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, errors.Wrap(err, "os.ReadFile()")
	}
	var doc struct {
		Roles map[string][]struct {
			Name string `json:"name"`
		} `json:"roles"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): not a role file")
	}
	var names []string
	for _, scope := range []string{"global", "domain"} {
		for _, role := range doc.Roles[scope] {
			names = append(names, role.Name)
		}
	}

	return names, nil
}

// validated reports whether a test calls access.ValidateRoles while parsing the auth
// package's Roles(): the roles validation test with the auth's row.
func validated(a *app.App, p *app.AuthPackage) bool {
	for i := range a.RoleValidations {
		if slices.Contains(a.RoleValidations[i].RolesPackages, p.Path) {
			return true
		}
	}

	return false
}

// skeletonValidationTest is where the skeletons keep the roles validation test.
const skeletonValidationTest = "pkg/deploy/deploy_test.go"

// ValidationTestFile is where the roles validation test lives: beside the library file
// that calls CheckPolicy on the engine (the deploy package), named after its package, or
// the skeletons' file when only a main package does.
func ValidationTestFile(a *app.App) string {
	for _, c := range a.PolicyChecks {
		dir := path.Dir(c.File)
		if slices.Contains(a.MainPackages, dir) {
			continue
		}

		return path.Join(dir, path.Base(dir)+"_test.go")
	}

	return skeletonValidationTest
}

// defaultRolesOf lists the access.WithDefaultRoles calls handing the auth package's
// Roles() to the engine.
func defaultRolesOf(a *app.App, p *app.AuthPackage) []app.DefaultRoles {
	var calls []app.DefaultRoles
	for _, d := range a.DefaultRoles {
		if d.RolesPackage == p.Path {
			calls = append(calls, d)
		}
	}

	return calls
}

// rolesFileOf is the auth package's role file, root-relative: the file beside its source
// that Roles() embeds.
func rolesFileOf(p *app.AuthPackage) string {
	return path.Join(p.Dir, app.RolesFileName)
}

// rolesExport reads the auth package's source for the two halves of its role file: a
// package-level Roles() function, and a //go:embed directive naming the file.
func rolesExport(a *app.App, p *app.AuthPackage) (exported, embedded bool) {
	entries, err := os.ReadDir(a.Abs(p.Dir))
	if err != nil {
		return false, false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || isTestFile(e.Name()) {
			continue
		}
		rel := path.Join(p.Dir, e.Name())
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), rel, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, decl := range f.Decls {
			if fd, ok := decl.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == authRoles {
				exported = true
			}
		}
		for _, group := range f.Comments {
			for _, c := range group.List {
				if strings.TrimSpace(c.Text) == "//go:embed "+app.RolesFileName {
					embedded = true
				}
			}
		}
	}

	return exported, embedded
}

// The session library's cookie names when a construction leaves them unset.
const (
	defaultSessionCookie = "auth"
	defaultXSRFCookie    = "XSRF-TOKEN"
)

// cookieCollisions finds two auth packages issuing the same cookie: the session cookie or
// the XSRF token cookie. The browser holds one cookie of a name per host, so a login to
// one auth then overwrites the other's, and a name left unset is the library default,
// which every other unset auth shares. A construction that forwards its callers' options
// is not judged, since its names are not visible.
func cookieCollisions(auths []app.Auth) []string {
	type cookie struct{ kind, name string }
	holders := map[cookie][]string{}
	for i := range auths {
		auth := &auths[i]
		if app.AuthPackageName(auth.File) == "" || auth.OptionsForwarded {
			continue
		}
		pkg := path.Dir(auth.File)
		session, xsrf := auth.CookieName, auth.XSRFCookieName
		if session == "" {
			session = defaultSessionCookie
		}
		if xsrf == "" {
			xsrf = defaultXSRFCookie
		}
		for _, c := range []cookie{{"session", session}, {"xsrf", xsrf}} {
			if !slices.Contains(holders[c], pkg) {
				holders[c] = append(holders[c], pkg)
			}
		}
	}

	var details []string
	for c, pkgs := range holders {
		if len(pkgs) < 2 {
			continue
		}
		sort.Strings(pkgs)
		switch c.kind {
		case "session":
			details = append(details, fmt.Sprintf("%s: both ride their sessions in the cookie %s, so a login to one ends the other's session in the browser; name each auth's cookie (session.WithCookieName)", strings.Join(pkgs, ", "), c.name))
		default:
			details = append(details, fmt.Sprintf("%s: both issue their XSRF token in the cookie %s, so a login to one overwrites the other's token in the browser; name each auth's cookie (session.WithXSRFCookieName) and the same name in the web app that binds to it (withXsrfConfiguration)", strings.Join(pkgs, ", "), c.name))
		}
	}
	sort.Strings(details)

	return details
}

// outletBindings lists the outlets whose Auth declaration names the package.
func outletBindings(profile app.Profile, importPath string) []app.Outlet {
	var outlets []app.Outlet
	for i := range profile.Sites {
		all := profile.Sites[i].AllOutlets()
		for j := range all {
			o := &all[j]
			if o.Auth != nil && o.Auth.ImportPath == importPath {
				outlets = append(outlets, *o)
			}
		}
	}

	return outlets
}

// unknownAuthBindings finds outlets bound by Auth to a package that is not one of the
// application's auth packages: the generated router would compile against a package
// exporting no session handlers the App can hand it.
func unknownAuthBindings(a *app.App, profile app.Profile) []string {
	known := map[string]bool{}
	for i := range a.AuthPackages {
		known[a.AuthPackages[i].Path] = true
	}
	var details []string
	for i := range profile.Sites {
		all := profile.Sites[i].AllOutlets()
		for j := range all {
			o := &all[j]
			if o.Auth != nil && !known[o.Auth.ImportPath] {
				details = append(details, fmt.Sprintf("%s: outlet %s binds to Auth(%q, ...), which is not one of the application's auth packages (%s)", o.Pos, o.Name, o.Auth.ImportPath, strings.Join(slices.Sorted(maps.Keys(known)), ", ")))
			}
		}
	}

	return details
}

// flavorOf returns the login flavor of the auth package's constructor, or empty when the
// package constructs none the scan reads.
func flavorOf(a *app.App, p *app.AuthPackage) string {
	for i := range a.Auths {
		if path.Dir(a.Auths[i].File) == p.Dir {
			return a.Auths[i].Flavor
		}
	}

	return ""
}

// authorityOf returns the role-membership authority of the auth package's constructor.
func authorityOf(a *app.App, p *app.AuthPackage) string {
	for i := range a.Auths {
		if path.Dir(a.Auths[i].File) == p.Dir {
			return a.Auths[i].Authority
		}
	}

	return ""
}

// directoryWriters finds role assignments in the store of an auth whose membership is the
// directory's: a role assigned in the application is removed at the person's next login,
// so the two cannot coexist. It reads every non-test file for a role-writer call
// (AddUserRoles, AddRoleUsers, DeleteUserRoles, DeleteRoleUsers) whose receiver is the
// auth's user manager, reached through the auth's accessor (<Name>()) or its variable
// (<name>Auth), and for the user manager handed to a function of the same file that
// calls a role writer.
func directoryWriters(a *app.App, p *app.AuthPackage) []string {
	var details []string
	for _, rel := range a.GoFiles() {
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			continue
		}
		for _, line := range app.RoleWriters(rel, src, p.Name) {
			details = append(details, fmt.Sprintf("%s:%d: the %s auth hands role membership to the directory (session.RoleSync), but this assigns roles in its store; the directory removes them at the next login. Assign the roles in the directory, or hand membership to the application (session.DisableRoleSync)", rel, line, p.Name))
		}
	}

	return details
}

func isTestFile(file string) bool { return strings.HasSuffix(file, "_test.go") }

// isDataLevel reports a file of a config package: the level that constructs the auths
// rather than a surface that binds to one.
func isDataLevel(file string) bool { return path.Base(path.Dir(file)) == "config" }

// authFiles lists where authenticators are constructed, for the warning.
func authFiles(a *app.App) []string {
	files := make([]string, 0, len(a.Auths))
	for i := range a.Auths {
		auth := &a.Auths[i]
		files = append(files, fmt.Sprintf("%s:%d: %s", auth.File, auth.Line, auth.Flavor))
	}

	return files
}
