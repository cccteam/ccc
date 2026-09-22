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

// The session library and its storage package, whose constructors the scan reads.
const (
	sessionImportPath        = "github.com/cccteam/session"
	sessionStorageImportPath = "github.com/cccteam/session/sessionstorage"
)

// The login flavors.
const (
	FlavorPassword   = "password"
	FlavorOIDCAzure  = "oidc-azure"
	FlavorOIDCGoogle = "oidc-google"
	FlavorPreauth    = "preauth"
)

// The authorities for an OIDC auth's role membership, read from the constructor's
// role-synchronization slot: RoleSync (or GoogleRoleSync) makes the directory's role
// claims the authority, DisableRoleSync leaves membership to the application. A password
// or preauth auth has no slot and is the application's by nature.
const (
	AuthorityDirectory   = "directory"
	AuthorityApplication = "application"
)

// defaultSessionTable is the session library's sessions table name for every flavor.
const defaultSessionTable = "Sessions"

// authConstructors maps each session constructor to its flavor and the flavor's default
// tables. The session library's schemas live under schema/spanner/<flavor>/migrations.
var authConstructors = map[string]struct {
	flavor       string
	sessionTable string
	userTable    string
}{
	"NewPasswordAuth": {FlavorPassword, defaultSessionTable, "SessionUsers"},
	// The OIDC flavors' user anchor is opt-in (sessionstorage.WithOIDCUsers); the
	// default names apply once it is enabled.
	"NewOIDCAzure":  {FlavorOIDCAzure, defaultSessionTable, "OIDCUsers"},
	"NewOIDCGoogle": {FlavorOIDCGoogle, defaultSessionTable, "GoogleOIDCUsers"},
	"NewPreauth":    {FlavorPreauth, defaultSessionTable, ""},
}

// DefaultImpersonationTable is the session library's impersonation table name.
const DefaultImpersonationTable = "SessionImpersonations"

// parseAuths returns every session authenticator construction in the file.
func parseAuths(rel string, src []byte) ([]Auth, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}
	pkg := localImportName(f, sessionImportPath)
	if pkg == "" {
		return nil, nil
	}
	storage := localImportName(f, sessionStorageImportPath)
	consts := fileConstStrings(f)

	var auths []Auth
	for _, decl := range f.Decls {
		// The enclosing function's body is where an identifier handed to a storage option
		// is defined; a package-level construction has none.
		var body *ast.BlockStmt
		if fn, ok := decl.(*ast.FuncDecl); ok {
			body = fn.Body
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := qualifiedName(call.Fun, pkg)
			if !ok {
				return true
			}
			spec, ok := authConstructors[name]
			if !ok {
				return true
			}
			auth := Auth{
				File: rel, Line: fset.Position(call.Pos()).Line, Flavor: spec.flavor,
				SessionTable: spec.sessionTable, UserTable: spec.userTable,
				OptionsForwarded: call.Ellipsis.IsValid(),
			}
			oidcUsers := false
			for _, arg := range call.Args {
				readAuthArg(&auth, &oidcUsers, arg, pkg, storage, consts, body)
			}
			if (spec.flavor == FlavorOIDCAzure || spec.flavor == FlavorOIDCGoogle) && !oidcUsers {
				auth.UserTable = ""
			}
			auths = append(auths, auth)

			return true
		})
	}

	return auths, nil
}

// readAuthArg applies one constructor argument to the construction: a session option
// naming a table or cookie, or a storage constructed with impersonation, the OIDC user
// anchor, or custom data tables. body is the enclosing function's body, where an
// identifier the storage's options take is defined.
func readAuthArg(auth *Auth, oidcUsers *bool, arg ast.Expr, pkg, storage string, consts map[string]string, body *ast.BlockStmt) {
	call, ok := arg.(*ast.CallExpr)
	if !ok {
		return
	}
	switch name, _ := qualifiedName(call.Fun, pkg); name {
	case "RoleSync", "GoogleRoleSync":
		auth.Authority = AuthorityDirectory

		return
	case "DisableRoleSync":
		auth.Authority = AuthorityApplication

		return
	}
	if name, ok := qualifiedName(call.Fun, pkg); ok && len(call.Args) == 1 {
		if s, isString := constString(call.Args[0], consts); isString {
			switch name {
			case "WithSessionTableName":
				auth.SessionTable = s
			case "WithUserTableName", "WithOIDCUserTableName":
				auth.UserTable = s
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
	readStorageArg(auth, oidcUsers, call, storage, consts, body)
}

// readStorageArg looks inside a storage constructor for the options that attach tables.
func readStorageArg(auth *Auth, oidcUsers *bool, call *ast.CallExpr, storage string, consts map[string]string, body *ast.BlockStmt) {
	ast.Inspect(call, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, _ := qualifiedName(c.Fun, storage)
		switch name {
		case "WithImpersonation":
			auth.Impersonation = true
			if s, ok := assignedImpersonationTable(c, body, storage, consts); ok {
				auth.ImpersonationTable = s
			}
		case "NewImpersonationTable":
			if len(c.Args) == 1 {
				if s, ok := constString(c.Args[0], consts); ok {
					auth.ImpersonationTable = s
				}
			}
		case "WithOIDCUsers":
			*oidcUsers = true
		case "NewSpannerCustomSessionData", "NewPostgresCustomSessionData", "NewSpannerCustomUserData", "NewPostgresCustomUserData":
			if len(c.Args) >= 1 {
				if s, ok := constString(c.Args[0], consts); ok {
					auth.ExtraTables = append(auth.ExtraTables, s)
				}
			}
		}

		return true
	})
}

// assignedImpersonationTable reads the table name behind WithImpersonation(x) when x is an
// identifier rather than the NewImpersonationTable call itself, the way a caller writes it
// to check the constructor's error. The definition is the nearest assignment before the
// use in the enclosing function body, x := NewImpersonationTable(...) or
// x, err := NewImpersonationTable(...), and the name is its argument when that is a
// literal or a constant of the file. A name defined any other way does not read.
func assignedImpersonationTable(with *ast.CallExpr, body *ast.BlockStmt, storage string, consts map[string]string) (string, bool) {
	if body == nil || len(with.Args) != 1 {
		return "", false
	}
	id, ok := with.Args[0].(*ast.Ident)
	if !ok {
		return "", false
	}
	var value ast.Expr
	ast.Inspect(body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || assign.Pos() >= id.Pos() || len(assign.Rhs) != 1 || (assign.Tok != token.DEFINE && assign.Tok != token.ASSIGN) {
			return true
		}
		for _, lhs := range assign.Lhs {
			if l, ok := lhs.(*ast.Ident); ok && l.Name == id.Name {
				// Statements are visited in source order, so the last one seen is nearest.
				value = assign.Rhs[0]
			}
		}

		return true
	})
	call, ok := value.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return "", false
	}
	if name, _ := qualifiedName(call.Fun, storage); name != "NewImpersonationTable" {
		return "", false
	}

	return constString(call.Args[0], consts)
}

// qualifiedName returns the selector name of a pkg.Name call, looking through a generic
// instantiation such as pkg.Name[T, U].
func qualifiedName(fun ast.Expr, pkg string) (string, bool) {
	switch e := fun.(type) {
	case *ast.IndexExpr:
		return qualifiedName(e.X, pkg)
	case *ast.IndexListExpr:
		return qualifiedName(e.X, pkg)
	case *ast.SelectorExpr:
		if isQualified(e, pkg, e.Sel.Name) {
			return e.Sel.Name, true
		}
	}

	return "", false
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

// ConstString evaluates the named package-level string constant of a file.
func ConstString(rel string, src []byte, name string) (string, bool) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return "", false
	}
	value, ok := fileConstStrings(f)[name]

	return value, ok
}
