package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// roleWriterMethods are the access.UserManager methods that change role membership.
var roleWriterMethods = map[string]bool{
	"AddUserRoles": true, "AddRoleUsers": true, "DeleteUserRoles": true, "DeleteRoleUsers": true,
}

// RoleWriters returns the lines of the file that assign or remove roles in the named
// auth's store: a role-writer call whose receiver reaches the auth's user manager through
// its accessor (<Name>()) or its variable (<name>Auth), and a call handing that user
// manager to a function of the same file whose body calls a role writer. A file that
// cannot be parsed has none.
func RoleWriters(rel string, src []byte, authName string) []int {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	accessor := strings.ToUpper(authName[:1]) + authName[1:] + "(...)"
	variable := authName + "Auth"
	reaches := func(e ast.Expr) bool {
		text := exprText(e)

		return strings.Contains(text, accessor) || strings.Contains(text, variable+".")
	}

	// Functions of the file whose bodies call a role writer, by name.
	writerFuncs := map[string]bool{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Recv != nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && roleWriterMethods[sel.Sel.Name] {
					writerFuncs[fn.Name.Name] = true
				}
			}

			return true
		})
	}

	var lines []int
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			if roleWriterMethods[fun.Sel.Name] && reaches(fun.X) {
				lines = append(lines, fset.Position(call.Pos()).Line)
			}
		case *ast.Ident:
			if !writerFuncs[fun.Name] {
				return true
			}
			for _, arg := range call.Args {
				if reaches(arg) {
					lines = append(lines, fset.Position(call.Pos()).Line)

					break
				}
			}
		}

		return true
	})

	return lines
}
