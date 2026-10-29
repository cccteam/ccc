package check

import (
	"context"
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

	"github.com/cccteam/ccc/impulse/app"
)

// authsWired verifies that every auth package is wired through the application: the data
// level constructs it, the release hands its embedded role file to the permission engine
// (access.WithDefaultRoles), and a surface binds to it, either an outlet declaring it
// (Auth, under the generated router) with the sign-in methods the package registers, or
// a hand-written surface taking its type, so a population the application declares can
// sign in somewhere and holds roles. session-tables holds each Auth to its tables; this
// holds each auth to the application around it.
type authsWired struct{}

// authsWiredName is the check's name.
const authsWiredName = "auths-wired"

func (authsWired) Name() string { return authsWiredName }

func (authsWired) Describe() string {
	return "every auth package is constructed by the data level, embeds its role file and hands it to the permission engine (access.WithDefaultRoles), and is bound by a surface (an outlet's Auth declaration naming the methods the package registers, or a hand-written surface taking its type); no two auths share a session, XSRF or trusted-device cookie; an auth with a directory-run method has no role writers in the application; and a role file handed to the engine is validated by a test (WARN)"
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
			return skip(c.Name(), "no session Auth is constructed")
		}

		return warn(c.Name(), "no auth package: the Auths are constructed outside pkg/auth/<name> packages, so the auths cannot be told apart", authFiles(a)...)
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
	if auth := authOf(a, p); auth != nil {
		for i := range declared {
			details = append(details, methodMismatches(&declared[i], p, auth)...)
		}
		for j := range auth.Methods {
			m := &auth.Methods[j]
			if m.Authority != app.AuthorityDirectory {
				continue
			}
			details = append(details, directoryWriters(a, p, m)...)
			details = append(details, standsAlone(p, auth, m)...)
		}
	}

	return details, warnings
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
	defaultSessionCookie       = "auth"
	defaultXSRFCookie          = "XSRF-TOKEN"
	defaultTrustedDeviceCookie = "auth-trusted"
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
		session, xsrf, trusted := auth.CookieName, auth.XSRFCookieName, auth.TrustedDeviceCookieName
		if session == "" {
			session = defaultSessionCookie
		}
		if xsrf == "" {
			xsrf = defaultXSRFCookie
		}
		if trusted == "" {
			trusted = defaultTrustedDeviceCookie
		}
		for _, c := range []cookie{{"session", session}, {"xsrf", xsrf}, {"trusted", trusted}} {
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
		case "trusted":
			details = append(details, fmt.Sprintf("%s: both remember a browser in the cookie %s, so a browser one auth remembers after a sign-in code forgets the other's remembered browsers; name each auth's cookie (session.WithTrustedDeviceCookieName, the skeletons' <name>-trusted)", strings.Join(pkgs, ", "), c.name))
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

// authOf returns the Auth the package constructs, or nil when it constructs none the scan
// reads.
func authOf(a *app.App, p *app.AuthPackage) *app.Auth {
	for i := range a.Auths {
		if path.Dir(a.Auths[i].File) == p.Dir {
			return &a.Auths[i]
		}
	}

	return nil
}

// methodMismatches compares an outlet's declared methods with the methods the package
// registers: a declaration the package does not register would mount routes to handlers
// the App cannot supply, and a registration the outlet does not declare has routes
// nowhere. The two must agree in name and method, and in the options the users surface
// and the router read from the declaration: for the password method, a declaration
// chaining .OpenSignUp() mounts the sign-up routes, which the registration serves only
// with session.OpenSignUp(), and one chaining .UsernameIsEmail() has the users surface
// derive the address from the username, which the registration does only with
// session.UsernameIsEmail(); for a directory method, a declaration chaining .SyncsRoles()
// has the users surface leave the person's roles to the directory, which is right only
// where the registration's RoleSync writes them.
func methodMismatches(o *app.Outlet, p *app.AuthPackage, auth *app.Auth) []string {
	var details []string
	registered := map[string]*app.Method{}
	for i := range auth.Methods {
		registered[auth.Methods[i].Name] = &auth.Methods[i]
	}
	declared := map[string]bool{}
	for i := range o.Auth.Methods {
		m := &o.Auth.Methods[i]
		declared[m.Name] = true
		reg, ok := registered[m.Name]
		switch {
		case !ok:
			details = append(details, fmt.Sprintf("%s: outlet %s declares the %s method %s on the %s auth, but %s registers no such method; the generated router would mount routes to handlers the App cannot supply (register it, or drop the declaration)", o.Pos, o.Name, m.Kind, m.Name, p.Name, p.Dir))
		case reg.Kind != m.Kind:
			details = append(details, fmt.Sprintf("%s: outlet %s declares %s as a %s method on the %s auth, but %s registers it as %s; the generated router would mount the wrong routes", o.Pos, o.Name, m.Name, m.Kind, p.Name, p.Dir, reg.Kind))
		case m.Kind == app.MethodPassword:
			details = append(details, passwordMismatches(o, p, m, reg)...)
		default:
			details = append(details, roleSyncMismatches(o, p, m, reg)...)
		}
	}
	for i := range auth.Methods {
		m := &auth.Methods[i]
		if !declared[m.Name] {
			details = append(details, fmt.Sprintf("%s:%d: the %s auth registers the %s method %s, but outlet %s (%s) does not declare it, so its login route is mounted nowhere; declare generation.%s on the outlet's Auth option", m.File, m.Line, p.Name, m.Kind, m.Name, o.Name, o.Pos, strings.TrimPrefix(app.MethodDeclaration(m.Kind, m.Name, ""), "generation.")))
		}
	}

	return details
}

// passwordMismatches compares the password method's declaration with its registration:
// open sign-up and the username as the address are each said in both places or neither.
func passwordMismatches(o *app.Outlet, p *app.AuthPackage, declared *app.AuthMethodArg, registered *app.Method) []string {
	var details []string
	switch {
	case declared.OpenSignUp && !registered.OpenSignUp:
		details = append(details, fmt.Sprintf("%s: outlet %s declares the password method with .OpenSignUp() on the %s auth, but %s:%d registers it without session.OpenSignUp(); the generated router would mount sign-up routes the method refuses (register it with the option, or drop .OpenSignUp())", o.Pos, o.Name, p.Name, registered.File, registered.Line))
	case !declared.OpenSignUp && registered.OpenSignUp:
		details = append(details, fmt.Sprintf("%s:%d: the %s auth registers the password method with session.OpenSignUp(), but outlet %s (%s) does not declare .OpenSignUp(), so the sign-up routes are mounted nowhere and the login page offers no sign-up; chain .OpenSignUp() on the declaration", registered.File, registered.Line, p.Name, o.Name, o.Pos))
	}
	switch {
	case declared.UsernameIsEmail && !registered.UsernameIsEmail:
		details = append(details, fmt.Sprintf("%s: outlet %s declares the password method with .UsernameIsEmail() on the %s auth, but %s:%d registers it without session.UsernameIsEmail(); the users surface would derive the address from a username the method does not hold to one (register it with the option, or drop .UsernameIsEmail())", o.Pos, o.Name, p.Name, registered.File, registered.Line))
	case !declared.UsernameIsEmail && registered.UsernameIsEmail:
		details = append(details, fmt.Sprintf("%s:%d: the %s auth registers the password method with session.UsernameIsEmail(), but outlet %s (%s) does not declare .UsernameIsEmail(), so the users surface would hold an address apart from the username that is the address; chain .UsernameIsEmail() on the declaration", registered.File, registered.Line, p.Name, o.Name, o.Pos))
	}

	return details
}

// roleSyncMismatches compares a directory method's declaration with its registration:
// the directory writing the person's roles is said in both places or neither. The
// declaration's .SyncsRoles() has the generated users surface show the roles the
// registration's RoleSync writes as the directory's and never write them; without it the
// surface offers those roles as seats an administrator may remove, which the directory
// puts back at the next sign-in. A registration whose RoleSync the scan does not read is
// not compared.
func roleSyncMismatches(o *app.Outlet, p *app.AuthPackage, declared *app.AuthMethodArg, registered *app.Method) []string {
	switch {
	case declared.SyncsRoles && registered.Authority == app.AuthorityApplication:
		return []string{fmt.Sprintf("%s: outlet %s declares the %s method %s with .SyncsRoles() on the %s auth, but %s:%d registers it with DisableRoleSync; the users surface would leave these people's roles to a directory that writes none (register it with RoleSync, or drop .SyncsRoles())", o.Pos, o.Name, declared.Kind, declared.Name, p.Name, registered.File, registered.Line)}
	case !declared.SyncsRoles && registered.Authority == app.AuthorityDirectory:
		return []string{fmt.Sprintf("%s:%d: the %s auth registers the %s method %s with RoleSync, but outlet %s (%s) does not declare .SyncsRoles(), so the users surface would show the roles the directory writes as seats an administrator may remove, which the directory puts back at the next sign-in; chain .SyncsRoles() on the declaration", registered.File, registered.Line, p.Name, registered.Kind, registered.Name, o.Name, o.Pos)}
	default:
		return nil
	}
}

// RoleSyncUndeclared is a directory method registered with RoleSync whose declaration on
// an outlet's Auth option does not chain .SyncsRoles().
type RoleSyncUndeclared struct {
	// Outlet is the outlet declaring the method without the flag.
	Outlet *app.Outlet
	// Package is the auth package registering the method.
	Package *app.AuthPackage
	// Method is the registration, with the file and line of its register call.
	Method *app.Method
}

// RoleSyncUndeclaredMethods lists, outlet by outlet, the directory methods registered with
// RoleSync whose declaration on the outlet's Auth option does not chain .SyncsRoles():
// what auths-wired reports, and what impulse upgrade's syncs-roles recipe chains the
// flag on. A declaration of another kind than the registration is left to auths-wired.
func RoleSyncUndeclaredMethods(a *app.App) []RoleSyncUndeclared {
	profile := a.Profile()
	var found []RoleSyncUndeclared
	for i := range a.AuthPackages {
		p := &a.AuthPackages[i]
		auth := authOf(a, p)
		if auth == nil {
			continue
		}
		outlets := outletBindings(profile, p.Path)
		for k := range outlets {
			o := &outlets[k]
			for j := range auth.Methods {
				m := &auth.Methods[j]
				if m.Authority != app.AuthorityDirectory {
					continue
				}
				declared := o.Auth.Method(m.Name)
				if declared == nil || declared.Kind != m.Kind || declared.SyncsRoles {
					continue
				}
				found = append(found, RoleSyncUndeclared{Outlet: o, Package: p, Method: m})
			}
		}
	}

	return found
}

// standsAlone holds the rule that a registration whose directory writes the roles stands
// alone on its auth: the directory is then the authority for everyone the auth holds,
// and a second way in is a second authority over the same people (a person it
// provisions holds nothing, and the users surface refuses to seat them because the
// directory will not). The session library refuses the registration at construction;
// this names it before the application starts.
func standsAlone(p *app.AuthPackage, auth *app.Auth, m *app.Method) []string {
	if len(auth.Methods) < 2 {
		return nil
	}
	others := make([]string, 0, len(auth.Methods)-1)
	for i := range auth.Methods {
		if o := &auth.Methods[i]; o.Name != m.Name {
			others = append(others, o.Name)
		}
	}

	return []string{fmt.Sprintf("%s:%d: the %s auth registers the %s method %s with RoleSync beside %s: a registration whose directory writes the roles stands alone on its auth, since the directory is then the authority for everyone the auth holds, and session.NewAuth refuses the second registration; give %s an auth of its own, or register it with DisableRoleSync and declare it without .SyncsRoles() so the application assigns the roles", m.File, m.Line, p.Name, m.Kind, m.Name, strings.Join(others, ", "), m.Name)}
}

// directoryWriters finds role assignments in the store of an auth whose membership is the
// directory's: a role assigned in the application is removed at the person's next login,
// so the two cannot coexist. It reads every non-test file for a role-writer call
// (AddUserRoles, AddRoleUsers, DeleteUserRoles, DeleteRoleUsers) whose receiver is the
// auth's user manager, reached through the auth's accessor (<Name>()) or its variable
// (<name>Auth), and for the user manager handed to a function of the same file that
// calls a role writer.
func directoryWriters(a *app.App, p *app.AuthPackage, m *app.Method) []string {
	var details []string
	for _, rel := range a.GoFiles() {
		src, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			continue
		}
		for _, line := range app.RoleWriters(rel, src, p.Name) {
			details = append(details, fmt.Sprintf("%s:%d: the %s auth's %s method hands role membership to the directory (RoleSync), but this assigns roles in its store; the directory removes them at the next sign-in. Assign the roles in the directory, or hand membership to the application (DisableRoleSync)", rel, line, p.Name, m.Name))
		}
	}

	return details
}

func isTestFile(file string) bool { return strings.HasSuffix(file, "_test.go") }

// isDataLevel reports a file of a config package: the level that constructs the auths
// rather than a surface that binds to one.
func isDataLevel(file string) bool { return path.Base(path.Dir(file)) == "config" }

// authFiles lists where Auths are constructed, for the warning.
func authFiles(a *app.App) []string {
	files := make([]string, 0, len(a.Auths))
	for i := range a.Auths {
		auth := &a.Auths[i]
		files = append(files, fmt.Sprintf("%s:%d: %s", auth.File, auth.Line, methodSummary(auth)))
	}

	return files
}
