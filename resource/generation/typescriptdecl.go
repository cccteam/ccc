package generation

import (
	"go/types"
	"regexp"
	"slices"
	"strings"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/go/packages"
)

// The type-scope @typescript: a type whose TypeScript shape lives outside Go declares
// it once, on its declaration, and the generated files import it. Go stays the single
// source of truth for what crosses the wire: a plain struct needs no declaration (its
// interface is derived from its fields), and a type that writes its own JSON must
// declare, since its fields say nothing about the wire. `@typescript(Name, from:
// "module")` imports Name from the module, spelled verbatim; `@typescript(Name)` alone
// names a TypeScript built-in (string, number, boolean, unknown). A slice comes from
// the field ([]Position is Position[]), never from the declaration. The declaration is
// read on every path — a table column, a computed field, an RPC field — so one type is
// one TypeScript type wherever it appears.

// typescriptArgSpec is the declaration's argument shape: the TypeScript name, and the
// module it comes from.
var typescriptArgSpec = &genlang.ArgSpec{Positional: 1, Keys: []string{"from"}}

// typescriptIdentifier is what a TypeScript identifier may look like.
var typescriptIdentifier = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

// typescriptDecls reads what a type's declaration says: its @typescript annotation,
// and the type it is declared over (its right-hand side), from the packages the
// generator loaded and, for a type declared elsewhere, from that package loaded on
// first sight, with its type information. Every declaration read in one run is checked
// against the others: one imported name comes from one module.
type typescriptDecls struct {
	// loaded are the packages the generator loaded, by import path.
	loaded map[string]*packages.Package
	// fetched are the packages loaded on first sight, by import path.
	fetched map[string]*packages.Package
	// docs caches each package's type docs by import path once read.
	docs map[string]map[string]string
	// decls caches parsed declarations by import path and type name; a nil entry
	// records a type that declares nothing.
	decls map[string]*tsImport
	// rhs caches each type's right-hand side by import path and type name; a nil
	// entry records a type whose declaration the reader could not see.
	rhs map[string]types.Type
	// modules records the module each imported name comes from, and the type that
	// declared it, for the collision refusal.
	modules map[string]declaredBy
}

// declaredBy is where an imported name was first declared.
type declaredBy struct {
	from     string
	typeName string
}

// newTypescriptDecls builds a reader over the loaded packages, by import path.
func newTypescriptDecls(loaded map[string]*packages.Package) *typescriptDecls {
	byPath := make(map[string]*packages.Package, len(loaded))
	for _, pkg := range loaded {
		byPath[pkg.PkgPath] = pkg
	}

	return &typescriptDecls{
		loaded:  byPath,
		fetched: make(map[string]*packages.Package),
		docs:    make(map[string]map[string]string),
		decls:   make(map[string]*tsImport),
		rhs:     make(map[string]types.Type),
		modules: make(map[string]declaredBy),
	}
}

// typeKey is a type's cache key: its import path and name, the origin's for a generic
// instance.
func typeKey(named *types.Named) (key string, obj *types.TypeName) {
	obj = named.Origin().Obj()
	if obj.Pkg() == nil {
		return "", obj
	}

	return obj.Pkg().Path() + "." + obj.Name(), obj
}

// declFor reads the @typescript declaration on the named type's declaration, nil when
// it carries none. A generic instance reads its origin's declaration.
func (d *typescriptDecls) declFor(named *types.Named) (*tsImport, error) {
	key, obj := typeKey(named)
	pkg := obj.Pkg()
	if pkg == nil {
		return nil, nil
	}
	if decl, seen := d.decls[key]; seen {
		return decl, nil
	}

	docs, err := d.typeDocs(pkg.Path())
	if err != nil {
		return nil, err
	}
	typeName := pkg.Name() + "." + obj.Name()
	decl, err := parseTypescriptDecl(typeName, docs[obj.Name()])
	if err != nil {
		return nil, err
	}
	if decl != nil && !decl.IsBuiltin() {
		prior, taken := d.modules[decl.Name]
		switch {
		case taken && prior.from != decl.From:
			return nil, errors.Newf("%s: @%s(%s, from: %q) and %s's @%s(%s, from: %q) import the same name from different modules; rename one", typeName, typescriptKeyword, decl.Name, decl.From, prior.typeName, typescriptKeyword, decl.Name, prior.from)
		case !taken:
			d.modules[decl.Name] = declaredBy{from: decl.From, typeName: typeName}
		}
	}
	d.decls[key] = decl

	return decl, nil
}

// rhsFor reads the type the named type is declared over, its declaration's right-hand
// side as the type checker resolved it (an alias in between is read through by the
// caller), nil when the declaration is not in reach: a type with no package, or one
// whose package carries no syntax for it. A generic instance reads its origin's
// declaration. go/types keeps only the underlying type, so `type Position
// json.RawMessage` is read off the type spec's type expression.
func (d *typescriptDecls) rhsFor(named *types.Named) (types.Type, error) {
	key, obj := typeKey(named)
	pkg := obj.Pkg()
	if pkg == nil {
		return nil, nil
	}
	if rhs, seen := d.rhs[key]; seen {
		return rhs, nil
	}

	loaded, err := d.packageFor(pkg.Path())
	if err != nil {
		return nil, err
	}
	var rhs types.Type
	if spec := parser.TypeSpecOf(loaded, obj.Name()); spec != nil && loaded.TypesInfo != nil {
		rhs = loaded.TypesInfo.TypeOf(spec.Type)
	}
	d.rhs[key] = rhs

	return rhs, nil
}

// typeDocs returns the package's type docs, loading the package when the generator did
// not load it.
func (d *typescriptDecls) typeDocs(path string) (map[string]string, error) {
	if docs, ok := d.docs[path]; ok {
		return docs, nil
	}

	pkg, err := d.packageFor(path)
	if err != nil {
		return nil, err
	}
	docs := parser.TypeDocs(pkg)
	d.docs[path] = docs

	return docs, nil
}

// packageFor returns the package at the import path: the one the generator loaded, or
// the one loaded here on first sight, with its syntax and type information, so a
// declaration's annotation and right-hand side are both in reach.
func (d *typescriptDecls) packageFor(path string) (*packages.Package, error) {
	if pkg := d.loaded[path]; pkg != nil {
		return pkg, nil
	}
	if pkg := d.fetched[path]; pkg != nil {
		return pkg, nil
	}

	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedTypes | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypesInfo}, path)
	if err != nil {
		return nil, errors.Wrapf(err, "packages.Load(%q)", path)
	}
	if len(pkgs) != 1 {
		return nil, errors.Newf("packages.Load(%q): %d packages loaded, want one", path, len(pkgs))
	}
	d.fetched[path] = pkgs[0]

	return pkgs[0], nil
}

// parseTypescriptDecl reads the @typescript declaration out of a type's doc comment.
// Only the annotation's own lines are scanned, so a type declared in a package the
// generator does not own is read for this one annotation and nothing else in its
// documentation is judged.
func parseTypescriptDecl(typeName, doc string) (*tsImport, error) {
	var lines []string
	for line := range strings.SplitSeq(doc, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "@"+typescriptKeyword) {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	if len(lines) == 0 {
		return nil, nil
	}

	annotations, err := genlang.NewScanner(resourceKeywords()).ScanNamedType(&parser.NamedType{Comments: strings.Join(lines, "\n")})
	if err != nil {
		return nil, errors.Wrapf(err, "%s: @%s", typeName, typescriptKeyword)
	}
	invocations, err := annotations.Named.Get(typescriptKeyword).ParseInvocations(typescriptArgSpec)
	if err != nil {
		return nil, errors.Wrapf(err, "%s: @%s", typeName, typescriptKeyword)
	}
	if len(invocations) != 1 {
		return nil, errors.Newf("%s: @%s declared %d times; a type has one TypeScript type", typeName, typescriptKeyword, len(invocations))
	}

	name := invocations[0].Positional[0]
	from, hasFrom := invocations[0].Named("from")
	switch {
	case !typescriptIdentifier.MatchString(name):
		return nil, errors.Newf("%s: @%s(%s): %q is not a TypeScript identifier", typeName, typescriptKeyword, name, name)
	case slices.Contains(builtinTypescriptNames, name) && hasFrom:
		return nil, errors.Newf("%s: @%s(%s, from: %q): %s is a TypeScript built-in, which no module exports; drop from:", typeName, typescriptKeyword, name, from, name)
	case !slices.Contains(builtinTypescriptNames, name) && !hasFrom:
		return nil, errors.Newf("%s: @%s(%s): %s is not a TypeScript built-in (%s); add from: %q naming the module that exports it", typeName, typescriptKeyword, name, name, strings.Join(builtinTypescriptNames, ", "), "module")
	}

	return &tsImport{Name: name, From: from}, nil
}

// rejectTypescriptAnnotation refuses @typescript on a struct the generator extracts as
// a resource, a view, a computed resource, or a method: those are rows and requests,
// typed field by field, and only a type used as a field declares a TypeScript type.
func rejectTypescriptAnnotation(pStruct *parser.Struct, annotations genlang.StructAnnotations, kind string) error {
	if !annotations.Struct.Has(typescriptKeyword) {
		return nil
	}

	return errors.Newf("struct %s: @%s declares the TypeScript type of a type used as a field; this struct is a %s, typed field by field", pStruct.Name(), typescriptKeyword, kind)
}
