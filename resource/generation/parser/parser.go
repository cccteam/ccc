package parser

import (
	"go/types"
	"log"
	"strings"

	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/go/packages"
)

// Loads and type checks a package. Returns any errors encountered during
// loading or typechecking, otherwise returns the package's data.
// Useful for static type analysis with the [types] package instead of
// manually parsing the AST. A good explainer lives here: https://github.com/golang/example/tree/master/gotypes
func LoadPackages(packagePatterns ...string) (map[string]*types.Package, error) {
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

	packMap := make(map[string]*types.Package, len(packagePatterns))

	if len(files) > 0 {
		pkgs, err := loadPackages(files...)
		if err != nil {
			return nil, err
		}

		for _, pkg := range pkgs {
			packMap[pkg.Name] = pkg.Types
		}
	}

	if len(directories) > 0 {
		pkgs, err := loadPackages(directories...)
		if err != nil {
			return nil, err
		}

		for _, pkg := range pkgs {
			packMap[pkg.Name] = pkg.Types
		}
	}

	return packMap, nil
}

func loadPackages(packagePatterns ...string) ([]*packages.Package, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedTypes | packages.NeedFiles}
	pkgs, err := packages.Load(cfg, packagePatterns...)
	if err != nil {
		return nil, errors.Wrap(err, "packages.Load()")
	}

	if len(pkgs) == 0 {
		return nil, errors.New("no packages loaded")
	}

	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return nil, errors.Wrap(pkg.Errors[0], "packages.Load() package error:")
		}
		if len(pkg.TypeErrors) > 0 {
			return nil, errors.Wrap(pkg.TypeErrors[0], "packages.Load() type error:")
		}

		if len(pkg.GoFiles) == 0 || pkg.GoFiles[0] == "" {
			return nil, errors.Newf("package %q: no files loaded", pkg.Name)
		}

		if pkg.Types == nil {
			return nil, errors.Newf("package %q: types not loaded", pkg.Name)
		}
	}

	return pkgs, nil
}

// We can iterate over the declarations at the package level a single time
// to extract all the data necessary for generation. Any new data that needs
// to be added to the struct definitions can be extracted here.
func ParseStructs(pkg *types.Package) ([]Struct, error) {
	if pkg == nil {
		return nil, errors.New("package is nil")
	}

	log.Printf("Parsing structs from package %q...", pkg.Name())

	scope := pkg.Scope() // The package scope holds all the objects declared at package level (TypeNames, Consts, Vars, and Funcs)
	if scope == nil || len(scope.Names()) == 0 {
		return nil, errors.Newf("package %q has invalid scope", pkg.Name())
	}

	var parsedStructs []Struct
	for _, name := range scope.Names() {
		pStruct, ok := newStruct(scope.Lookup(name), false)
		if !ok {
			continue
		}

		parsedStructs = append(parsedStructs, pStruct)
	}

	return parsedStructs, nil
}

func HasInterface(pkg *types.Package, s Struct, methodName string) bool {
	ifaceObject := pkg.Scope().Lookup(methodName)
	if ifaceObject == nil {
		return false
	}

	ifaceTypeName, ok := ifaceObject.(*types.TypeName)
	if !ok {
		return false
	}

	iface, ok := ifaceTypeName.Type().Underlying().(*types.Interface)
	if !ok {
		return false
	}

	structTypeName, ok := s.obj.(*types.TypeName)
	if !ok {
		return false
	}
	structType := structTypeName.Type()

	for _, t := range []types.Type{structType, types.NewPointer(structType)} {
		if types.Implements(t, iface) {
			return true
		}
	}

	return false
}

// The [types.Type] interface can be one of 14 concrete types:
// https://github.com/golang/example/tree/master/gotypes#types
// Types can be safely and deterministically decoded from this interface,
// and support can easily be expanded to other types in our [resources] package
func decodeToType[T types.Type](v types.Type) (T, bool) {
	switch t := v.(type) {
	case *types.Pointer:
		return decodeToType[T](t.Elem())
	case *types.Named:
		return decodeToType[T](t.Underlying())
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

			dependencies = append(dependencies, newType(field, true))
		}
	}

	return dependencies
}

func isUnderlyingTypeStruct(tt types.Type) bool {
	switch t := tt.(type) {
	case *types.Slice:
		return isUnderlyingTypeStruct(t.Elem())
	case *types.Pointer:
		return isUnderlyingTypeStruct(t.Elem())
	case *types.Named:
		return isUnderlyingTypeStruct(tt.Underlying())
	case *types.Struct:
		return true
	default:
		return false
	}
}

// Returns the underlying element type for slice and pointer types
func unwrapType(tt types.Type) types.Type {
	switch t := tt.(type) {
	case *types.Slice:
		return unwrapType(t.Elem())
	case *types.Pointer:
		return unwrapType(t.Elem())
	default:
		return t
	}
}
