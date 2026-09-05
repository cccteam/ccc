package app

import (
	"go/ast"
	"go/parser"
	"go/token"

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

	var auths []Auth
	ast.Inspect(f, func(n ast.Node) bool {
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
			readAuthArg(&auth, &oidcUsers, arg, pkg, storage)
		}
		if (spec.flavor == FlavorOIDCAzure || spec.flavor == FlavorOIDCGoogle) && !oidcUsers {
			auth.UserTable = ""
		}
		auths = append(auths, auth)

		return true
	})

	return auths, nil
}

// readAuthArg applies one constructor argument to the construction: a session option
// naming a table or cookie, or a storage constructed with impersonation, the OIDC user
// anchor, or custom data tables.
func readAuthArg(auth *Auth, oidcUsers *bool, arg ast.Expr, pkg, storage string) {
	call, ok := arg.(*ast.CallExpr)
	if !ok {
		return
	}
	if name, ok := qualifiedName(call.Fun, pkg); ok && len(call.Args) == 1 {
		if s, isString := stringLit(call.Args[0]); isString {
			switch name {
			case "WithSessionTableName":
				auth.SessionTable = s
			case "WithUserTableName", "WithOIDCUserTableName":
				auth.UserTable = s
			case "WithCookieName":
				auth.CookieName = s
			}
		}

		return
	}
	if storage == "" {
		return
	}
	// A storage constructor: look inside for the options that attach tables.
	ast.Inspect(call, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		name, _ := qualifiedName(c.Fun, storage)
		switch name {
		case "WithImpersonation":
			auth.Impersonation = true
		case "NewImpersonationTable":
			if len(c.Args) == 1 {
				if s, ok := stringLit(c.Args[0]); ok {
					auth.ImpersonationTable = s
				}
			}
		case "WithOIDCUsers":
			*oidcUsers = true
		case "NewSpannerCustomSessionData", "NewPostgresCustomSessionData", "NewSpannerCustomUserData", "NewPostgresCustomUserData":
			if len(c.Args) >= 1 {
				if s, ok := stringLit(c.Args[0]); ok {
					auth.ExtraTables = append(auth.ExtraTables, s)
				}
			}
		}

		return true
	})
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
