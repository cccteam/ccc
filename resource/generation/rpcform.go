package generation

import (
	"go/types"
	"strings"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/go-playground/errors/v5"
)

// rpcForm is how an RPC method's Execute runs, read off the method's own
// signature. The generator never consults an interface the application
// declares: the signature is the declaration.
type rpcForm int

const (
	rpcFormUnclassified rpcForm = iota
	// rpcFormTxn is Execute(ctx, txn resource.ReadWriteTransaction, client *Client):
	// the body runs inside the generated handler's read-write transaction.
	rpcFormTxn
	// rpcFormClient is Execute(ctx, client resource.Client, rpcClient *Client):
	// the body runs outside any transaction the handler owns, against the
	// resource.Client interface.
	rpcFormClient
)

// resourcePackagePath is the import path of the package whose types name the
// two Execute forms.
const resourcePackagePath = "github.com/cccteam/ccc/resource"

// executeForms is the shape every refusal quotes, so the message says what to
// write rather than only what was wrong.
const executeForms = `an @rpc struct declares Execute in one of two forms:
	Execute(ctx context.Context, txn resource.ReadWriteTransaction, client *Client) error   // runs inside the handler's transaction
	Execute(ctx context.Context, client resource.Client, rpcClient *Client) error          // runs outside one`

// classifyExecute reads the struct's Execute method and returns its form. The
// method may be declared on either receiver. Every departure from the two
// forms is a generation error naming the struct.
func classifyExecute(s *parser.Struct) (rpcForm, error) {
	fn := s.Method("Execute")
	if fn == nil {
		return rpcFormUnclassified, errors.Newf("struct %s has no Execute method; %s", s.Name(), executeForms)
	}

	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return rpcFormUnclassified, errors.Newf("struct %s: Execute is not a method signature (%T)", s.Name(), fn.Type())
	}

	params := sig.Params()
	if params.Len() != 3 || sig.Variadic() {
		return rpcFormUnclassified, errors.Newf("struct %s: Execute takes %s; %s", s.Name(), paramsString(sig), executeForms)
	}

	if !isNamedType(params.At(0).Type(), "context", "Context") {
		return rpcFormUnclassified, errors.Newf("struct %s: Execute's first parameter is %s, not context.Context; %s", s.Name(), typeStringer(params.At(0).Type()), executeForms)
	}

	var form rpcForm
	switch second := params.At(1).Type(); {
	case isNamedType(second, resourcePackagePath, "ReadWriteTransaction"):
		form = rpcFormTxn
	case isNamedType(second, resourcePackagePath, "Client"):
		form = rpcFormClient
	default:
		return rpcFormUnclassified, errors.Newf("struct %s: Execute's second parameter is %s, neither resource.ReadWriteTransaction nor resource.Client; %s", s.Name(), typeStringer(second), executeForms)
	}

	if third, ok := params.At(2).Type().(*types.Pointer); !ok || !isNamed(third.Elem()) {
		return rpcFormUnclassified, errors.Newf("struct %s: Execute's third parameter is %s, not a pointer to the application's RPC client type; %s", s.Name(), typeStringer(params.At(2).Type()), executeForms)
	}

	results := sig.Results()
	if results.Len() != 1 || !types.Identical(results.At(0).Type(), types.Universe.Lookup("error").Type()) {
		return rpcFormUnclassified, errors.Newf("struct %s: Execute returns %s; it returns error; %s", s.Name(), typeTupleString(results), executeForms)
	}

	return form, nil
}

func isNamed(t types.Type) bool {
	_, ok := t.(*types.Named)

	return ok
}

// isNamedType reports whether t is the named type pkgPath.name.
func isNamedType(t types.Type, pkgPath, name string) bool {
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}

	return named.Obj().Pkg().Path() == pkgPath && named.Obj().Name() == name
}

// typeStringer renders a type the way it reads in source: package-qualified by
// package name, never by import path.
func typeStringer(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Name() })
}

// paramsString renders a signature's parameter list as "(a, b, ...c)", or
// "nothing" when empty, for refusal messages.
func paramsString(sig *types.Signature) string {
	params := sig.Params()
	if params.Len() == 0 {
		return "nothing"
	}

	parts := make([]string, params.Len())
	for i := range params.Len() {
		t := params.At(i).Type()
		if slice, ok := t.(*types.Slice); ok && sig.Variadic() && i == params.Len()-1 {
			parts[i] = "..." + typeStringer(slice.Elem())

			continue
		}
		parts[i] = typeStringer(t)
	}

	return "(" + strings.Join(parts, ", ") + ")"
}

// typeTupleString renders a parameter or result list as "(a, b)", or "nothing"
// when empty, for refusal messages.
func typeTupleString(tuple *types.Tuple) string {
	if tuple.Len() == 0 {
		return "nothing"
	}

	return types.TypeString(tuple, func(p *types.Package) string { return p.Name() })
}
