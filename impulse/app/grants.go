package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strconv"

	"github.com/go-playground/errors/v5"
)

// ProvesGrantFunc is the harness helper a test case calls to name the conditional grant it
// proves: provesGrant(t, <auth>.RolesPath, role, permission, resource, condition). The
// helper reads the roles file at test time and fails unless a grant with that role,
// permission, and resource carries exactly that condition text; the conditions-proven
// check reads the call, so the two hold the case and the file together from both sides.
const ProvesGrantFunc = "provesGrant"

// provesGrantArity is the helper's argument count: t, the roles path, then the four
// coordinates of the grant.
const provesGrantArity = 6

// grantArgNames names the helper's arguments after t, for the problem a call raises.
var grantArgNames = [provesGrantArity]string{"t", "rolesPath", "role", "permission", "resource", "condition"}

// GrantProof is one call to the harness helper in a test file: a case's claim that it
// proves the named conditional grant of a roles file.
type GrantProof struct {
	File string
	Line int
	// RolesPath is the root-relative roles file the call names as a literal or a constant
	// of the file, or empty when it names a package's RolesPath constant instead.
	RolesPath string
	// RolesPackage is the import path of the package whose RolesPath constant the call
	// names, or empty for a literal path.
	RolesPackage string
	Role         string
	Permission   string
	Resource     string
	Condition    string
	// Problem says why the call cannot be read, or is empty: the wrong number of
	// arguments, or an argument that is neither a literal nor a constant of the file.
	Problem string
}

// parseGrantProofs returns every call to the harness helper in a test file. The roles path
// is read as a literal, a constant of the file, or a selector on an imported package's
// RolesPath; the four coordinates are literals or constants of the file.
func parseGrantProofs(rel string, src []byte) ([]GrantProof, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}
	imports := map[string]string{}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		local := path.Base(p)
		if imp.Name != nil {
			local = imp.Name.Name
		}
		imports[local] = p
	}
	consts := fileConstStrings(f)

	var proofs []GrantProof
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); !ok || id.Name != ProvesGrantFunc {
			return true
		}
		proofs = append(proofs, readGrantProof(rel, fset.Position(call.Pos()).Line, call, imports, consts))

		return true
	})

	return proofs, nil
}

// readGrantProof reads one call's arguments into a proof, or the problem that stops it.
func readGrantProof(rel string, line int, call *ast.CallExpr, imports, consts map[string]string) GrantProof {
	proof := GrantProof{File: rel, Line: line}
	if len(call.Args) != provesGrantArity {
		proof.Problem = "takes " + strconv.Itoa(provesGrantArity) + " arguments, found " + strconv.Itoa(len(call.Args))

		return proof
	}
	if sel, ok := call.Args[1].(*ast.SelectorExpr); ok && sel.Sel.Name == rolesPathConst {
		if id, ok := sel.X.(*ast.Ident); ok {
			if p, imported := imports[id.Name]; imported {
				proof.RolesPackage = p
			}
		}
	}
	if proof.RolesPackage == "" {
		p, ok := constString(call.Args[1], consts)
		if !ok {
			proof.Problem = "argument " + grantArgNames[1] + " is neither a literal, a constant of the file, nor an imported package's " + rolesPathConst

			return proof
		}
		proof.RolesPath = path.Clean(p)
	}
	fields := [...]*string{&proof.Role, &proof.Permission, &proof.Resource, &proof.Condition}
	for i, field := range fields {
		value, ok := constString(call.Args[i+2], consts)
		if !ok {
			proof.Problem = "argument " + grantArgNames[i+2] + " is not a literal or a constant of the file"

			return proof
		}
		*field = value
	}

	return proof
}
