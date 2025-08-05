package parser

import (
	"go/ast"
	"go/types"
	"log"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/go/packages"
)

// Loads and type checks a package. Returns any errors encountered during
// loading or typechecking, otherwise returns the package's data.
// Useful for static type analysis with the [types] package instead of
// manually parsing the AST. A good explainer lives here: https://github.com/golang/example/tree/master/gotypes
func LoadPackages(packagePatterns ...string) (map[string]*packages.Package, error) {
	log.Printf("Loading packages %v...\n", packagePatterns)

	files := []string{}
	directories := []string{}

	for _, pattern := range packagePatterns {
		if strings.HasSuffix(pattern, ".go") {
			files = append(files, pattern)
		} else {
			directories = append(directories, pattern)
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

// Loads a single package
func LoadPackage(packagePattern string) (*packages.Package, error) {
	pkgs, err := loadPackages(packagePattern)
	if err != nil {
		return nil, err
	}

	return pkgs[0], nil
}

func loadPackages(packagePatterns ...string) ([]*packages.Package, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedTypes | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypesInfo}
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

			return nil, errors.Wrap(err, "packages.Load() package error(s):")
		}

		if len(pkg.GoFiles) == 0 || pkg.GoFiles[0] == "" {
			return nil, errors.Newf("no files were loaded for package %q", pkg.Name)
		}
	}

	return pkgs, nil
}

// We can iterate over the declarations at the package level a single time
// to extract all the data necessary for generation. Any new data that needs
// to be added to the struct definitions can be extracted here.
func ParsePackage(pkg *packages.Package) *Package {
	log.Printf("Parsing structs from package %q...", pkg.Types.Name())

	// Gather all type definitions from generic (top-level) declarations
	typeSpecs := make([]*ast.TypeSpec, 0, 256)
	for i := range pkg.Syntax {
		for j := range pkg.Syntax[i].Decls {
			if genDecl, ok := pkg.Syntax[i].Decls[j].(*ast.GenDecl); ok {
				for k := range genDecl.Specs {
					if typeSpec, ok := genDecl.Specs[k].(*ast.TypeSpec); ok {
						typeSpecs = append(typeSpecs, typeSpec)
					}
				}
			}
		}
	}

	interfaces := make([]*Interface, 0, 16)
	parsedStructs := make([]*Struct, 0, 128)
	namedTypes := make([]*NamedType, 0, 16)
	for i := range typeSpecs {
		switch astNode := typeSpecs[i].Type.(type) {
		case *ast.InterfaceType:
			obj := pkg.TypesInfo.ObjectOf(typeSpecs[i].Name)
			iface, _ := decodeToType[*types.Interface](obj.Type())
			interfaces = append(interfaces, &Interface{Name: typeSpecs[i].Name.Name, iface: iface})
		case *ast.Ident:
			namedType := &NamedType{}
			obj := pkg.TypesInfo.ObjectOf(typeSpecs[i].Name) // NamedType's name

			namedType.TypeInfo = TypeInfo{obj}

			if typeSpecs[i].Doc != nil {
				namedType.Comments = typeSpecs[i].Doc.Text()
			}
			if typeSpecs[i].Comment != nil {
				namedType.Comments += typeSpecs[i].Comment.Text()
			}

			namedTypes = append(namedTypes, namedType)
		case *ast.StructType:
			obj := pkg.TypesInfo.ObjectOf(typeSpecs[i].Name)
			pStruct := newStruct(obj)
			if pStruct.TypeInfo.obj == nil { // nil pStruct is anonymous struct
				continue
			}

			if typeSpecs[i].Doc != nil {
				pStruct.comments = typeSpecs[i].Doc.Text()
			}
			if typeSpecs[i].Comment != nil {
				pStruct.comments += typeSpecs[i].Comment.Text()
			}

			for j := range pStruct.fields {
				pStruct.fields[j].astInfo = astNode.Fields.List[j]

				if pStruct.fields[j].astInfo.Doc != nil {
					pStruct.fields[j].comments = pStruct.fields[j].astInfo.Doc.Text()
				}
				if pStruct.fields[j].astInfo.Comment != nil {
					pStruct.fields[j].comments += pStruct.fields[j].astInfo.Comment.Text()
				}
			}

			parsedStructs = append(parsedStructs, pStruct)
		}
	}

	for i := range parsedStructs {
		for j := range interfaces {
			// Necessary to check non-pointer and pointer receivers
			if types.Implements(parsedStructs[i].obj.Type(), interfaces[j].iface) || types.Implements(types.NewPointer(parsedStructs[i].obj.Type()), interfaces[j].iface) {
				parsedStructs[i].SetInterface(interfaces[j].Name)
			}
		}
	}

	compareFn := func(a, b *Struct) int {
		return strings.Compare(a.Name(), b.Name())
	}

	slices.SortFunc(parsedStructs, compareFn)

	return &Package{Structs: slices.Clip(parsedStructs), NamedTypes: slices.Clip(namedTypes)}
}

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
	case T:
		return t, true
	// unwraps pointer types e.g. *ccc.UUID -> ccc.UUID
	case *ast.StarExpr:
		return decodeToExpr[T](t.X)
	// captures the expression immediately following the dot e.g. ccc.UUID -> UUID
	case *ast.SelectorExpr:
		return decodeToExpr[T](t.Sel)
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
