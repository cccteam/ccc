package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"

	"github.com/go-playground/errors/v5"
)

// ProvesGrantFunc is the harness helper a test case calls to name the conditional grant it
// proves: provesGrant(t, <auth>.Roles(), role, permission, resource, condition). The
// helper parses the auth's embedded role file at test time and fails unless a grant with
// that role, permission, and resource carries exactly that condition text; the
// conditions-proven check reads the call, so the two hold the case and the file together
// from both sides.
const ProvesGrantFunc = "provesGrant"

// provesGrantArity is the helper's argument count: t, the role file, then the four
// coordinates of the grant.
const provesGrantArity = 6

// grantArgNames names the helper's arguments after t, for the problem a call raises.
var grantArgNames = [provesGrantArity]string{"t", "roles", "role", "permission", "resource", "condition"}

// GrantProof is one call to the harness helper in a test file: a case's claim that it
// proves the named conditional grant of a role file.
type GrantProof struct {
	File string
	Line int
	// RolesPackage is the import path of the auth package whose Roles() the call hands
	// the helper: the role file the proof is about.
	RolesPackage string
	Role         string
	Permission   string
	Resource     string
	Condition    string
	// Problem says why the call cannot be read, or is empty: the wrong number of
	// arguments, a role file that is not an imported package's Roles() call, or a
	// coordinate that is neither a literal nor a constant of the file.
	Problem string
}

// parseGrantProofs returns every call to the harness helper in a test file. The role file
// is read as a Roles() call on an imported package; the four coordinates are literals or
// constants of the file.
func parseGrantProofs(rel string, src []byte) ([]GrantProof, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}
	imports := fileImports(f)
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
	proof.RolesPackage = rolesCallPackage(call.Args[1], imports, "")
	if proof.RolesPackage == "" {
		proof.Problem = "argument " + grantArgNames[1] + " is not an imported auth package's " + rolesFunc + "()"

		return proof
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
