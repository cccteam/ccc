// Package parser is a simplified abstraction over go/parser, tailored for the go:generate resource/generation tool.
package parser

import (
	"fmt"
	"go/ast"
	"go/types"
	"log"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/go/packages"
)

// LoadPackages loads and type checks a list of packages. Returns package names mapped to a *packages.Package.
// Package data contains package name, file names, ast, and types' info. Returns an error if
// a package pattern does not match any packages, no files are loaded in a matched package, or parsing & typechecking
// yield any errors.
// Useful for static type analysis with the [types] package instead of
// manually parsing the AST. A good explainer lives here: https://github.com/golang/example/tree/master/gotypes
func LoadPackages(packagePatterns ...string) (map[string]*packages.Package, error) {
	log.Printf("Loading packages %v...\n", packagePatterns)

	files := []string{}
	directories := []string{}

	for _, pattern := range packagePatterns {
		if strings.HasSuffix(pattern, ".go") {
			files = append(files, filepath.Clean(pattern))
		} else {
			directories = append(directories, "./"+filepath.Clean(pattern))
		}
	}

	packMap := make(map[string]*packages.Package, len(packagePatterns))

	if len(files) > 0 {
		pkgs, err := loadPackages(files...)
		if err != nil {
			return nil, err
		}

		for _, pkg := range pkgs {
			packMap[pkg.Name] = pkg
		}
	}

	if len(directories) > 0 {
		pkgs, err := loadPackages(directories...)
		if err != nil {
			return nil, err
		}

		for _, pkg := range pkgs {
			packMap[pkg.Name] = pkg
		}
	}

	return packMap, nil
}

// LoadPackage loads and type checks a single package.
// Package data contains package name, file names, ast, and types' info. Returns an error if
// a package pattern does not match any packages, no files are loaded in a matched package,
// or parsing & typechecking yield any errors.
// Useful for static type analysis with the [types] package instead of
// manually parsing the AST. A good explainer lives here: https://github.com/golang/example/tree/master/gotypes
func LoadPackage(packagePattern string) (*packages.Package, error) {
	pkgs, err := loadPackages(packagePattern)
	if err != nil {
		return nil, err
	}

	return pkgs[0], nil
}

func loadPackages(packagePatterns ...string) ([]*packages.Package, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedTypes | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypesInfo}
	pkgs, err := packages.Load(cfg, packagePatterns...)
	if err != nil {
		return nil, errors.Wrap(err, "packages.Load()")
	}

	if len(pkgs) == 0 {
		return nil, errors.Newf("no packages loaded for pattern %v", packagePatterns)
	}

	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 || len(pkg.TypeErrors) > 0 {
			var err error
			for _, e := range pkg.Errors {
				err = errors.Join(e)
			}

			for _, e := range pkg.TypeErrors {
				err = errors.Join(e)
			}

			return nil, errors.Wrap(err, "packages.Load() package error(s)")
		}

		if len(pkg.CompiledGoFiles) == 0 || pkg.CompiledGoFiles[0] == "" {
			return nil, errors.Newf("no files were loaded for package %q", pkg.Name)
		}
	}

	return pkgs, nil
}

// ParsePackage parses a package's ast and type info and returns data about structs and named types
// necessary for resource generation. We can iterate over the declarations at the package level
// a single time to extract all the data necessary for generation. Any new data that needs to be
// added to the Struct type can be extracted here.
func ParsePackage(pkg *packages.Package) *Package {
	log.Printf("Parsing structs from package %q...", pkg.Types.Name())

	typeSpecs := packageTypeSpecs(pkg.Syntax)
	interfaces := make([]*Interface, 0, 16)
	parsedStructs := make([]*Struct, 0, 128)
	namedTypes := make([]*NamedType, 0, 16)
	for _, typeSpec := range typeSpecs {
		switch astNode := typeSpec.Type.(type) {
		case *ast.InterfaceType:
			obj := pkg.TypesInfo.ObjectOf(typeSpec.Name)
			named, ok := obj.Type().(*types.Named)
			if !ok {
				panic(fmt.Sprintf("cannot assert %q to *types.Named", typeSpec.Name.Name))
			}

			i := &Interface{Name: typeSpec.Name.Name, named: named}
			if typeSpec.TypeParams != nil {
				i.isGeneric = true
			}

			interfaces = append(interfaces, i)
		case *ast.Ident:
			namedType := &NamedType{}
			obj := pkg.TypesInfo.ObjectOf(typeSpec.Name) // NamedType's name

			namedType.TypeInfo = TypeInfo{obj}

			if typeSpec.Doc != nil {
				namedType.Comments = typeSpec.Doc.Text()
			}
			if typeSpec.Comment != nil {
				namedType.Comments += typeSpec.Comment.Text()
			}

			namedTypes = append(namedTypes, namedType)
		case *ast.StructType:
			obj := pkg.TypesInfo.ObjectOf(typeSpec.Name)
			pStruct := newStruct(obj)
			if pStruct.obj == nil { // nil pStruct is anonymous struct
				continue
			}

			pStruct.astInfo = astNode

			if typeSpec.Doc != nil {
				pStruct.comments = typeSpec.Doc.Text()
			}
			if typeSpec.Comment != nil {
				pStruct.comments += typeSpec.Comment.Text()
			}

			for j, field := range pStruct.fields {
				field.astInfo = astNode.Fields.List[j]

				if field.astInfo.Doc != nil {
					field.comments = field.astInfo.Doc.Text()
				}
				if field.astInfo.Comment != nil {
					field.comments += field.astInfo.Comment.Text()
				}
			}

			parsedStructs = append(parsedStructs, pStruct)
		}
	}

	for _, pStruct := range parsedStructs {
		for _, iface := range interfaces {
			if implementsInterface(pStruct, iface) {
				pStruct.setInterface(iface.Name)
			}
		}
	}

	compareFn := func(a, b *Struct) int {
		return strings.Compare(a.Name(), b.Name())
	}

	slices.SortFunc(parsedStructs, compareFn)

	return &Package{Structs: slices.Clip(parsedStructs), NamedTypes: slices.Clip(namedTypes)}
}

// packageTypeSpecs returns all type definitions from a package's generic (top-level) declarations
func packageTypeSpecs(syntax []*ast.File) []*ast.TypeSpec {
	typeSpecs := make([]*ast.TypeSpec, 0, 256)
	for _, file := range syntax {
		for _, decl := range file.Decls {
			if genDecl, ok := decl.(*ast.GenDecl); ok {
				for _, spec := range genDecl.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						typeSpecs = append(typeSpecs, typeSpec)
					}
				}
			}
		}
	}

	return typeSpecs
}

func implementsInterface(pStruct *Struct, iface *Interface) bool {
	var ifaceType *types.Interface

	if iface.isGeneric && iface.named.TypeParams().Len() == 1 {
		instance, err := types.Instantiate(types.NewContext(), iface.named, []types.Type{pStruct.obj.Type()}, false)
		if err != nil {
			panic(err)
		}

		interfaceInstance, ok := decodeToType[*types.Interface](instance)
		if !ok { // this is impossible but I'm including it to appease the linter
			panic("unreachable: cannot assert instantiated interface's underlying type to *types.Interface")
		}
		ifaceType = interfaceInstance
	} else {
		interfaceInstance, ok := decodeToType[*types.Interface](iface.named)
		if !ok { // this is impossible but I'm including it to appease the linter
			panic("unreachable: cannot assert *types.Named to *types.Interface")
		}
		ifaceType = interfaceInstance
	}

	// It's necessary to check with and without a pointer because method receivers may or may not be pointer types.
	return types.Implements(pStruct.obj.Type(), ifaceType) || types.Implements(types.NewPointer(pStruct.obj.Type()), ifaceType)
}

// FilterStructsByInterface returns a filtered slice of structs that satisfy one or more from the list of interface names.
func FilterStructsByInterface(pStructs []*Struct, interfaceNames []string) []*Struct {
	filteredStructs := make([]*Struct, 0, len(pStructs))
	for _, pStruct := range pStructs {
		for _, iface := range interfaceNames {
			if pStruct.Implements(iface) {
				filteredStructs = append(filteredStructs, pStruct)
			}
		}
	}

	return slices.Clip(filteredStructs)
}

// The [types.Type] interface can be one of 14 concrete types:
// https://github.com/golang/example/tree/master/gotypes#types
// Types can be safely and deterministically decoded from this interface,
// and support can easily be expanded to other types in our [resources] package
func decodeToType[T types.Type](v types.Type) (T, bool) {
	switch t := v.(type) {
	case *types.Slice:
		return decodeToType[T](t.Elem())
	case *types.Pointer:
		return decodeToType[T](t.Elem())
	case *types.Named:
		return decodeToType[T](t.Underlying())
	case *types.Alias:
		return decodeToType[T](t.Rhs())

	case T:
		return t, true
	default:
		var zero T

		return zero, false
	}
}

func decodeToExpr[T ast.Expr](v ast.Expr) (T, bool) {
	if v == nil {
		panic("nil ast.Expr cannot be decoded")
	}

	switch t := v.(type) {
	case *ast.StarExpr: // unwraps pointer types e.g. *ccc.UUID -> ccc.UUID
		return decodeToExpr[T](t.X)
	case *ast.SelectorExpr: // captures the expression immediately following the dot e.g. ccc.UUID -> UUID
		return decodeToExpr[T](t.Sel)
	case T:
		return t, true
	default:
		var zero T

		return zero, false
	}
}

// Necessary for qualifying type names with the package they're imported from
// e.g. `ccc.UUID`
func typeStringer(t types.Type) string {
	qualifier := func(p *types.Package) string {
		if p == nil {
			return ""
		}

		return p.Name()
	}

	return types.TypeString(t, qualifier)
}

func isTypeLocalToPackage(t *types.Var, pkg *types.Package) bool {
	typeName := strings.TrimPrefix(typeStringer(t.Type()), "[]")
	typeName = strings.TrimPrefix(typeName, "*")

	return strings.HasPrefix(typeName, pkg.Name())
}

// Returns a list of types from this struct's package that the struct depends on
func localTypesFromStruct(obj types.Object, typeMap map[string]struct{}) []TypeInfo {
	var dependencies []TypeInfo
	pkg := obj.Pkg()
	tt := obj.Type()

	typeMap[typeStringer(tt)] = struct{}{}

	s, ok := decodeToType[*types.Struct](tt)
	if !ok {
		return dependencies
	}

	for field := range s.Fields() {
		ft := field.Type()
		if _, ok := typeMap[typeStringer(unwrapType(ft))]; ok {
			continue
		}

		if isTypeLocalToPackage(field, pkg) {
			if _, ok := decodeToType[*types.Struct](ft); ok {
				dependencies = append(dependencies, localTypesFromStruct(field, typeMap)...)
			} else {
				typeMap[typeStringer(unwrapType(ft))] = struct{}{}
			}

			dependencies = append(dependencies, TypeInfo{field})
		}
	}

	return dependencies
}

// Returns the underlying element type for slices and pointer,
// or the named type if its underlying type is a struct
func unwrapType(tt types.Type) types.Type {
	switch t := tt.(type) {
	case *types.Slice:
		return unwrapType(t.Elem())
	case *types.Pointer:
		return unwrapType(t.Elem())
	case *types.Named:
		switch u := t.Underlying().(type) {
		case *types.Struct:
			return t
		default:
			return unwrapType(u)
		}
	default:
		return t
	}
}

// Returns the underlying element type for pointer types
func derefType(tt types.Type) types.Type {
	switch t := tt.(type) {
	case *types.Pointer:
		return derefType(t.Elem())
	default:
		return t
	}
}
