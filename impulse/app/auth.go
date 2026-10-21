package app

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"sort"
	"strconv"

	"github.com/go-playground/errors/v5"
)

// The session library and the packages of it the scan reads: the Auth and the password
// method (the root package), the storage, and the redirect methods, each its own package.
const (
	sessionImportPath        = "github.com/cccteam/session"
	sessionStorageImportPath = "github.com/cccteam/session/sessionstorage"
	sessionAzureImportPath   = "github.com/cccteam/session/azure"
	sessionGoogleImportPath  = "github.com/cccteam/session/google"
	sessionWorkOSImportPath  = "github.com/cccteam/session/workos"
	sessionSignInImportPath  = "github.com/cccteam/session/signin"
)

// The sign-in methods, as the session library names them: the password method of the
// root package and the redirect methods of the azure, google and workos packages.
const (
	MethodPassword = "password"
	MethodAzure    = "azure"
	MethodGoogle   = "google"
	MethodWorkOS   = "workos"
)

// PasswordRegistration is the name the password method is always registered under.
const PasswordRegistration = "password"

// The authorities for a directory method's role membership, read from the registration's
// RoleSync slot: signin.RoleSync or google.RoleSync makes the directory's role claims or
// groups the authority, DisableRoleSync leaves membership to the application. The password
// method has no slot and is the application's by nature.
const (
	AuthorityDirectory   = "directory"
	AuthorityApplication = "application"
)

// The session library's default table names for an Auth, which the library's schema sets
// create under those names (schema/spanner/migrations and schema/spanner/auth/migrations).
const (
	DefaultSessionTable    = "Sessions"
	DefaultUserTable       = "SessionUsers"
	DefaultIdentitiesTable = "SessionIdentities"
	DefaultPendingTable    = "SessionPendingSignIns"
	DefaultAttemptsTable   = "SessionSignInAttempts"
	// DefaultImpersonationTable is the session library's impersonation table name.
	DefaultImpersonationTable = "SessionImpersonations"
	// DefaultAuthEventsTable is the name the library's schema creates the auth events table
	// under; the storage takes the name explicitly (NewAuthEventsTable).
	DefaultAuthEventsTable = "SessionAuthEvents"
)

// authConstructor is the Auth's constructor in the session package.
const authConstructor = "NewAuth"

// methodConstructors maps each sign-in method's registration function, by the package
// that exports it, to the method it registers. The password method is registered by the
// session package itself; each redirect method by its own package.
var methodConstructors = map[string]map[string]string{
	sessionImportPath:       {"PasswordSignIn": MethodPassword},
	sessionAzureImportPath:  {signInConstructor: MethodAzure},
	sessionGoogleImportPath: {signInConstructor: MethodGoogle},
	sessionWorkOSImportPath: {signInConstructor: MethodWorkOS},
}

// signInConstructor is the name every directory method's package registers through.
const signInConstructor = "SignIn"

// parseAuths returns every session Auth construction in the file with the methods the
// file registers on it, and the methods registered on an Auth the file does not
// construct (attached at the package level).
func parseAuths(rel string, src []byte) (auths []Auth, unattached []Method, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, errors.Wrap(err, "parser.ParseFile()")
	}
	pkg := localImportName(f, sessionImportPath)
	storage := localImportName(f, sessionStorageImportPath)
	consts := fileConstStrings(f)
	methodPkgs := map[string]string{} // local import name -> method constructors' package path
	for importPath := range methodConstructors {
		if local := localImportName(f, importPath); local != "" {
			methodPkgs[local] = importPath
		}
	}
	if pkg == "" && len(methodPkgs) == 0 {
		return nil, nil, nil
	}

	var methods []Method
	for _, decl := range f.Decls {
		// The enclosing function's body is where an identifier handed to a storage option
		// or a registration is defined; a package-level construction has none.
		var body *ast.BlockStmt
		if fn, ok := decl.(*ast.FuncDecl); ok {
			body = fn.Body
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := selectorOf(call.Fun)
			if !ok {
				return true
			}
			pkgIdent, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			local, name := pkgIdent.Name, sel.Sel.Name
			switch {
			case pkg != "" && local == pkg && name == authConstructor:
				auth := Auth{
					File: rel, Line: fset.Position(call.Pos()).Line, Var: assignedName(call, body),
					SessionTable: DefaultSessionTable, UserTable: DefaultUserTable,
					IdentitiesTable: DefaultIdentitiesTable, PendingTable: DefaultPendingTable,
					AttemptsTable: DefaultAttemptsTable, OptionsForwarded: call.Ellipsis.IsValid(),
				}
				for _, arg := range call.Args {
					readAuthArg(&auth, arg, pkg, storage, consts, body)
				}
				auths = append(auths, auth)
			case methodPkgs[local] != "" && methodConstructors[methodPkgs[local]][name] != "":
				methods = append(methods, readMethodCall(call, methodConstructors[methodPkgs[local]][name], local, rel, fset, consts, body, f))
			}

			return true
		})
	}
	unattached = attachMethods(auths, methods)

	return auths, unattached, nil
}

// readMethodCall reads a registration: the method's kind, the Auth it is registered on
// (the first argument's identifier), its registration name (the second argument, a
// literal or a constant of the file; the password method is always "password") and, for
// a directory method, its authority from the configuration (the fourth argument).
func readMethodCall(call *ast.CallExpr, kind, local, rel string, fset *token.FileSet, consts map[string]string, body *ast.BlockStmt, f *ast.File) Method {
	m := Method{File: rel, Line: fset.Position(call.Pos()).Line, Kind: kind, Name: PasswordRegistration}
	if len(call.Args) > 0 {
		if id, ok := call.Args[0].(*ast.Ident); ok {
			m.AuthVar = id.Name
		}
	}
	if kind == MethodPassword {
		return m
	}
	if len(call.Args) > 1 {
		m.Name = ""
		if s, ok := constString(call.Args[1], consts); ok {
			m.Name = s
		}
	}
	if len(call.Args) > 3 {
		m.Authority = readAuthority(call.Args[3], body, local, f)
	}

	return m
}

// attachMethods gives each method to the Auth its first argument names, and to the only
// Auth of the set when the identifier does not resolve; it returns the methods left over.
func attachMethods(auths []Auth, methods []Method) (unattached []Method) {
	for _, m := range methods {
		placed := false
		for i := range auths {
			if auths[i].Var != "" && auths[i].Var == m.AuthVar {
				auths[i].Methods = append(auths[i].Methods, m)
				placed = true

				break
			}
		}
		if !placed && len(auths) == 1 {
			auths[0].Methods = append(auths[0].Methods, m)
			placed = true
		}
		if !placed {
			unattached = append(unattached, m)
		}
	}

	return unattached
}

// attachPackageMethods gives the methods registered outside the file constructing their
// Auth to the only Auth of their package directory; what is left over stays in
// UnattachedMethods for the session-tables check to report.
func (a *App) attachPackageMethods() {
	var left []Method
	for _, m := range a.unattached {
		var same []int
		for i := range a.Auths {
			if path.Dir(a.Auths[i].File) == path.Dir(m.File) {
				same = append(same, i)
			}
		}
		if len(same) == 1 {
			a.Auths[same[0]].Methods = append(a.Auths[same[0]].Methods, m)

			continue
		}
		left = append(left, m)
	}
	a.UnattachedMethods = left
}

// selectorOf returns the pkg.Name selector a call is made through, looking through a
// generic instantiation such as pkg.Name[T, U].
func selectorOf(fun ast.Expr) (*ast.SelectorExpr, bool) {
	switch e := fun.(type) {
	case *ast.IndexExpr:
		return selectorOf(e.X)
	case *ast.IndexListExpr:
		return selectorOf(e.X)
	case *ast.SelectorExpr:
		if _, ok := e.X.(*ast.Ident); ok {
			return e, true
		}
	}

	return nil, false
}

// assignedName returns the identifier a call's result is assigned to in the enclosing
// body (x, err := pkg.New(...) or x = pkg.New(...)), or empty.
func assignedName(call *ast.CallExpr, body *ast.BlockStmt) string {
	if body == nil {
		return ""
	}
	name := ""
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Rhs) != 1 || assign.Rhs[0] != call {
			return true
		}
		if id, ok := assign.Lhs[0].(*ast.Ident); ok {
			name = id.Name
		}

		return false
	})

	return name
}

// readAuthority reads the RoleSync slot of a registration's configuration: the Config
// literal's RoleSync field, in the call itself (&azure.Config{...}) or in the nearest
// assignment of the identifier passed, as the application writes it to keep the call
// short. signin.RoleSync or google.RoleSync is the directory's authority, DisableRoleSync
// the application's; anything else is not read.
func readAuthority(cfg ast.Expr, body *ast.BlockStmt, methodPkg string, f *ast.File) string {
	lit := compositeOf(cfg, body)
	if lit == nil {
		return ""
	}
	signinPkg := localImportName(f, sessionSignInImportPath)
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "RoleSync" {
			continue
		}
		call, ok := kv.Value.(*ast.CallExpr)
		if !ok {
			return ""
		}
		sel, ok := selectorOf(call.Fun)
		if !ok {
			return ""
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return ""
		}
		local := pkgIdent.Name
		if local != methodPkg && (signinPkg == "" || local != signinPkg) {
			return ""
		}
		switch sel.Sel.Name {
		case "RoleSync":
			return AuthorityDirectory
		case "DisableRoleSync":
			return AuthorityApplication
		}

		return ""
	}

	return ""
}

// compositeOf returns the composite literal an expression is, or the one the identifier
// it names was last assigned before its use in the body (cfg := &azure.Config{...}), or
// nil.
func compositeOf(expr ast.Expr, body *ast.BlockStmt) *ast.CompositeLit {
	switch e := expr.(type) {
	case *ast.UnaryExpr:
		return compositeOf(e.X, body)
	case *ast.CompositeLit:
		return e
	case *ast.Ident:
		value := assignedValue(e, body)
		if value == nil {
			return nil
		}

		return compositeOf(value, body)
	default:
		return nil
	}
}

// assignedValue returns the value the identifier was last assigned before its use in the
// body: x := <value> or x = <value>, with x the only name on the left or the first of
// two (x, err := ...), or nil.
func assignedValue(id *ast.Ident, body *ast.BlockStmt) ast.Expr {
	if body == nil {
		return nil
	}
	var value ast.Expr
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || assign.Pos() >= id.Pos() || len(assign.Rhs) != 1 || (assign.Tok != token.DEFINE && assign.Tok != token.ASSIGN) {
			return true
		}
		if l, ok := assign.Lhs[0].(*ast.Ident); ok && l.Name == id.Name {
			// Statements are visited in source order, so the last one seen is nearest.
			value = assign.Rhs[0]
		}

		return true
	})

	return value
}

// readAuthArg applies one constructor argument to the construction: a session option
// naming a table or cookie, or a storage constructed with impersonation, auth events, or
// custom data tables. body is the enclosing function's body, where an identifier the
// storage's options take is defined.
func readAuthArg(auth *Auth, arg ast.Expr, pkg, storage string, consts map[string]string, body *ast.BlockStmt) {
	call, ok := arg.(*ast.CallExpr)
	if !ok {
		return
	}
	if name, ok := qualifiedName(call.Fun, pkg); ok && len(call.Args) == 1 {
		if s, isString := constString(call.Args[0], consts); isString {
			switch name {
			case "WithSessionTableName":
				auth.SessionTable = s
			case "WithUserTableName":
				auth.UserTable = s
			case "WithIdentitiesTableName":
				auth.IdentitiesTable = s
			case "WithPendingSignInsTableName":
				auth.PendingTable = s
			case "WithSignInAttemptsTableName":
				auth.AttemptsTable = s
			case "WithCookieName":
				auth.CookieName = s
			case "WithXSRFCookieName":
				auth.XSRFCookieName = s
			}
		}

		return
	}
	if storage == "" {
		return
	}
	readStorageArg(auth, call, storage, consts, body)
}

// readStorageArg looks inside a storage constructor for the options that attach tables.
func readStorageArg(auth *Auth, call *ast.CallExpr, storage string, consts map[string]string, body *ast.BlockStmt) {
	ast.Inspect(call, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, _ := qualifiedName(c.Fun, storage)
		switch name {
		case "WithImpersonation":
			auth.Impersonation = true
			if s, ok := assignedTable(c, body, storage, "NewImpersonationTable", consts); ok {
				auth.ImpersonationTable = s
			}
		case "NewImpersonationTable":
			if len(c.Args) == 1 {
				if s, ok := constString(c.Args[0], consts); ok {
					auth.ImpersonationTable = s
				}
			}
		case "WithAuthEvents":
			auth.AuthEvents = true
			if s, ok := assignedTable(c, body, storage, "NewAuthEventsTable", consts); ok {
				auth.AuthEventsTable = s
			}
		case "NewAuthEventsTable":
			if len(c.Args) == 1 {
				if s, ok := constString(c.Args[0], consts); ok {
					auth.AuthEventsTable = s
				}
			}
		case "NewCustomSessionData", "NewCustomUserData", "NewSpannerCustomSessionData", "NewPostgresCustomSessionData", "NewSpannerCustomUserData", "NewPostgresCustomUserData":
			if len(c.Args) >= 1 {
				if s, ok := constString(c.Args[0], consts); ok {
					auth.ExtraTables = append(auth.ExtraTables, s)
				}
			}
		}

		return true
	})
}

// assignedTable reads the table name behind an option taking an identifier
// (WithImpersonation(x), WithAuthEvents(x)) rather than the table constructor itself,
// the way a caller writes it to check the constructor's error: the definition is the
// nearest assignment before the use in the enclosing function body,
// x := NewImpersonationTable(...) or x, err := NewImpersonationTable(...), and the name
// is its argument when that is a literal or a constant of the file. A name defined any
// other way does not read.
func assignedTable(with *ast.CallExpr, body *ast.BlockStmt, storage, constructor string, consts map[string]string) (string, bool) {
	if len(with.Args) != 1 {
		return "", false
	}
	id, ok := with.Args[0].(*ast.Ident)
	if !ok {
		return "", false
	}
	call, ok := assignedValue(id, body).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return "", false
	}
	if name, _ := qualifiedName(call.Fun, storage); name != constructor {
		return "", false
	}

	return constString(call.Args[0], consts)
}

// qualifiedName returns the selector name of a pkg.Name call, looking through a generic
// instantiation such as pkg.Name[T, U].
func qualifiedName(fun ast.Expr, pkg string) (string, bool) {
	sel, ok := selectorOf(fun)
	if !ok || !isQualified(sel, pkg, sel.Sel.Name) {
		return "", false
	}

	return sel.Sel.Name, true
}

// fileConstStrings evaluates the file's package-level string constants: literals, other
// constants of the file, and concatenations of them, so a table name written as
// TablePrefix + "Sessions" reads as the name it is.
func fileConstStrings(f *ast.File) map[string]string {
	specs := map[string]ast.Expr{}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, sp := range gd.Specs {
			vs, ok := sp.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i < len(vs.Values) {
					specs[name.Name] = vs.Values[i]
				}
			}
		}
	}
	consts := map[string]string{}
	for name := range specs {
		if s, ok := evalConst(name, specs, map[string]bool{}); ok {
			consts[name] = s
		}
	}

	return consts
}

// evalConst folds one constant; visiting guards against a cycle.
func evalConst(name string, specs map[string]ast.Expr, visiting map[string]bool) (string, bool) {
	expr, ok := specs[name]
	if !ok || visiting[name] {
		return "", false
	}
	visiting[name] = true
	defer delete(visiting, name)

	return foldString(expr, specs, visiting)
}

func foldString(expr ast.Expr, specs map[string]ast.Expr, visiting map[string]bool) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return stringLit(e)
	case *ast.Ident:
		return evalConst(e.Name, specs, visiting)
	case *ast.ParenExpr:
		return foldString(e.X, specs, visiting)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		left, ok := foldString(e.X, specs, visiting)
		if !ok {
			return "", false
		}
		right, ok := foldString(e.Y, specs, visiting)
		if !ok {
			return "", false
		}

		return left + right, true
	default:
		return "", false
	}
}

// constString reads a string argument that is a literal, a constant of the file, or a
// concatenation of them.
func constString(expr ast.Expr, consts map[string]string) (string, bool) {
	if s, ok := stringLit(expr); ok {
		return s, true
	}
	specs := map[string]ast.Expr{}
	for name, value := range consts {
		specs[name] = &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(value)}
	}

	return foldString(expr, specs, map[string]bool{})
}

// authDir is the directory the auth packages live under: pkg/auth/<name>, one per
// population that signs in one way and holds roles in one store.
const authDir = "auth"

// RolesFileName is the role file an auth package embeds beside its source: the
// release's default roles for that auth, which the package exports through Roles().
const RolesFileName = "roles.json"

// AuthPackageName returns the auth an authenticator construction belongs to, from its
// file's place in the tree (a package directly under an auth directory), or empty.
func AuthPackageName(file string) string {
	dir := path.Dir(file)
	if path.Base(path.Dir(dir)) != authDir {
		return ""
	}

	return path.Base(dir)
}

// AuthPackage is one auth: a package under an auth directory constructing a session
// authenticator, with every reference to it from the rest of the application.
type AuthPackage struct {
	// Name is the auth's name, the package's directory name.
	Name string
	// Dir is the package's root-relative directory.
	Dir string
	// Path is the package's import path.
	Path string
	// Refs are the files referencing the package, with the identifiers they take from it.
	Refs []AuthRef
}

// AuthRef is one file's references to an auth package.
type AuthRef struct {
	File string
	// Names are the package-level identifiers the file uses, sorted and without repeats.
	Names []string
}

// References reports whether some file outside the excluded directories uses the
// identifier.
func (p *AuthPackage) References(name string, excluding func(file string) bool) []string {
	var files []string
	for _, r := range p.Refs {
		if excluding != nil && excluding(r.File) {
			continue
		}
		for _, n := range r.Names {
			if n == name {
				files = append(files, r.File)

				break
			}
		}
	}

	return files
}

// authPackages collects the auth packages and their references, after the walk.
func (a *App) authPackages() error {
	byDir := map[string]*AuthPackage{}
	for i := range a.Auths {
		file := a.Auths[i].File
		name := AuthPackageName(file)
		if name == "" {
			continue
		}
		dir := path.Dir(file)
		if _, ok := byDir[dir]; !ok {
			byDir[dir] = &AuthPackage{Name: name, Dir: dir, Path: a.packagePath(dir)}
		}
	}
	if len(byDir) == 0 {
		return nil
	}
	for _, rel := range a.goFiles {
		data, err := os.ReadFile(a.Abs(rel))
		if err != nil {
			return errors.Wrap(err, "os.ReadFile()")
		}
		for _, pkg := range byDir {
			if pkg.Path == "" || path.Dir(rel) == pkg.Dir || !bytes.Contains(data, []byte(pkg.Path)) {
				continue
			}
			names, err := selectorsOf(rel, data, pkg.Path)
			if err != nil {
				return err
			}
			if len(names) > 0 {
				pkg.Refs = append(pkg.Refs, AuthRef{File: rel, Names: names})
			}
		}
	}
	for _, pkg := range byDir {
		a.AuthPackages = append(a.AuthPackages, *pkg)
	}
	sort.Slice(a.AuthPackages, func(i, j int) bool { return a.AuthPackages[i].Name < a.AuthPackages[j].Name })

	return nil
}

// selectorsOf lists the identifiers a file takes from the package at importPath.
func selectorsOf(rel string, src []byte, importPath string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}
	local := localImportName(f, importPath)
	if local == "" {
		return nil, nil
	}
	seen := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == local {
			seen[sel.Sel.Name] = true
		}

		return true
	})
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)

	return names, nil
}
