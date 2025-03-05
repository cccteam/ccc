package generation

import (
	"go/types"
	"log"
	"reflect"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/go/packages"
)

// Loads and type checks a package. Returns any errors encountered during
// loading or typechecking, otherwise returns the package's data.
// Useful for static type analysis with the [types] package instead of
// manually parsing the AST. A good explainer lives here: https://github.com/golang/example/tree/master/gotypes
func loadPackageMap(packagePatterns ...string) (map[string]*types.Package, error) {
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
func parseStructs(pkg *types.Package) ([]parsedStruct, error) {
	if pkg == nil {
		return nil, errors.New("package is nil")
	}

	log.Printf("Parsing structs from package %q...", pkg.Name())

	scope := pkg.Scope() // The package scope holds all the objects declared at package level (TypeNames, Consts, Vars, and Funcs)
	if scope == nil || len(scope.Names()) == 0 {
		return nil, errors.Newf("package %q has invalid scope", pkg.Name())
	}

	var parsedStructs []parsedStruct
	for _, name := range scope.Names() {
		object := scope.Lookup(name)
		if object == nil {
			return nil, errors.Newf("package %q in an invalid state: %q from scope.Names() not found in scope.Lookup()", pkg.Name(), name)
		}

		structType, ok := decodeToType[*types.Struct](object.Type())
		if !ok {
			continue
		}

		pStruct := parsedStruct{
			parsedType: parsedType{name: object.Name(), tt: object.Type(), packageName: pkg.Name(), position: int(object.Pos())},
			methods:    structMethods(object.Type()),
			localTypes: localTypesFromStruct(pkg.Name(), object.Type(), map[string]struct{}{}),
		}

		for j := range structType.NumFields() {
			field := structType.Field(j)
			if field == nil {
				return nil, errors.Newf("nil field[%d] in struct %q at %s:%v", j, object.Name(), pkg.Name(), object.Pos())
			}

			if field.Embedded() {
				return nil, errors.Newf("embedded fields are not supported yet")
			}

			fieldInfo := structField{
				parsedType:  parsedType{name: field.Name(), tt: field.Type(), packageName: pkg.Name()},
				tags:        reflect.StructTag(structType.Tag(j)),
				isLocalType: isTypeLocalToPackage(field, pkg.Name()),
			}

			pStruct.fields = append(pStruct.fields, fieldInfo)
		}

		parsedStructs = append(parsedStructs, pStruct)
	}

	return parsedStructs, nil
}

func structMethods(s types.Type) []*types.Selection {
	var methods []*types.Selection

	// Need to iterate over the type and its pointer type because
	// a method can use either as a receiver e.g. (a *app) or (a app)
	for _, t := range []types.Type{s, types.NewPointer(s)} {
		methodSet := types.NewMethodSet(t)
		if methodSet.Len() == 0 {
			continue
		}

		for method := range methodSet.Methods() {
			methods = append(methods, method)
		}
	}

	return methods
}

func (c *client) structToResource(pStruct *parsedStruct) (*resourceInfo, error) {
	if pStruct == nil {
		return nil, errors.New("resourceinfo cannot be nil")
	}

	table, err := c.lookupTable(pStruct.name)
	if err != nil {
		return nil, errors.Wrapf(err, "struct %q at %s:%d is not in lookupTable", pStruct.name, pStruct.packageName, pStruct.position)
	}

	resource := &resourceInfo{
		Name:                  pStruct.name,
		Fields:                make([]*resourceField, len(pStruct.fields)),
		IsView:                table.IsView,
		searchIndexes:         table.SearchIndexes,
		HasCompoundPrimaryKey: table.PkCount > 1,
		IsConsolidated:        !table.IsView && slices.Contains(c.consolidatedResourceNames, pStruct.name) != c.consolidateAll,
	}

	for i, field := range pStruct.fields {
		tableColumn, ok := table.Columns[field.tags.Get("spanner")]
		if !ok {
			return nil, errors.Newf("field %q in struct %q[%d] at %s:%d is not in tableMeta", field.Name(), resource.Name, i, field.PackageName(), field.Position())
		}

		resource.Fields[i] = &resourceField{
			structField:        &field,
			Parent:             resource,
			IsPrimaryKey:       tableColumn.IsPrimaryKey,
			IsForeignKey:       tableColumn.IsForeignKey,
			IsIndex:            tableColumn.IsIndex,
			IsUniqueIndex:      tableColumn.IsUniqueIndex,
			IsNullable:         tableColumn.IsNullable,
			OrdinalPosition:    tableColumn.OrdinalPosition,
			KeyOrdinalPosition: tableColumn.KeyOrdinalPosition,
			ReferencedResource: tableColumn.ReferencedTable,
			ReferencedField:    tableColumn.ReferencedColumn,
		}
	}

	return resource, nil
}

func (c *client) extractResources(pkg *types.Package) ([]*resourceInfo, error) {
	resourceStructs, err := parseStructs(pkg)
	if err != nil {
		return nil, err
	}

	resources := make([]*resourceInfo, len(resourceStructs))
	for i, pStruct := range resourceStructs {
		resource, err := c.structToResource(&pStruct)
		if err != nil {
			return nil, err
		}

		resources[i] = resource
	}

	return resources, nil
}

func extractStructsByMethod(pkg *types.Package, methodNames ...string) ([]parsedStruct, error) {
	parsedStructs, err := parseStructs(pkg)
	if err != nil {
		return nil, err
	}

	if len(methodNames) == 0 {
		return parsedStructs, nil
	}

	var rpcStructs []parsedStruct

	for _, pStruct := range parsedStructs {
		if hasMethods(pStruct, methodNames...) {
			rpcStructs = append(rpcStructs, pStruct)
		}
	}

	if len(rpcStructs) == 0 {
		return nil, errors.Newf("package %q has no structs that implement methods %v", pkg.Name(), methodNames)
	}

	return rpcStructs, nil
}

func hasMethods(pStruct parsedStruct, methodNames ...string) bool {
	if len(pStruct.methods) < len(methodNames) {
		return false
	}

	bools := make([]bool, len(methodNames))

methods:
	for i := range methodNames {
		for _, method := range pStruct.methods {
			if method.Obj().Name() == methodNames[i] {
				bools[i] = true
				continue methods
			}
		}
	}

	return !slices.Contains(bools, false)
}

// The [types.Type] interface can be one of 14 concrete types:
// https://github.com/golang/example/tree/master/gotypes#types
// Types can be safely and deterministically decoded from this interface,
// and support can easily be expanded to other types in our [resources] package
func decodeToType[T types.Type](v types.Type) (T, bool) {
	switch t := v.(type) {
	case *types.Slice:
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

// TODO: replace decodeToTypescriptType with typeStringer & overrides check
func decodeToTypescriptType(typ types.Type, typescriptOverrides map[string]string) (string, error) {
	if typ == nil {
		return "", errors.Newf("received nil type")
	}

	switch t := typ.(type) {
	case *types.Basic:
		switch basicInfo := t.Info(); {
		case basicInfo&types.IsBoolean != 0:
			return "boolean", nil
		case basicInfo&types.IsNumeric != 0:
			return "number", nil
		case basicInfo&types.IsString != 0:
			return "string", nil
		case basicInfo&types.IsUntyped != 0:
			return "string", nil
		default:
			return "", errors.Newf("%q is an unknown basic type of info/kind: %v/%v", t.String(), t.Info(), t.Kind())
		}
	case *types.Named:
		if override, ok := typescriptOverrides[typeStringer(t)]; ok {
			return override, nil
		}

		return decodeToTypescriptType(t.Underlying(), typescriptOverrides)
	case *types.Alias:
		if override, ok := typescriptOverrides[typeStringer(t)]; ok {
			return override, nil
		}

		return decodeToTypescriptType(t.Underlying(), typescriptOverrides)
	case *types.Pointer:
		return decodeToTypescriptType(t.Elem(), typescriptOverrides)
	case *types.Slice:
		return decodeToTypescriptType(t.Elem(), typescriptOverrides)
	case *types.Array:
		return decodeToTypescriptType(t.Elem(), typescriptOverrides)
	default:
		return "string", nil
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

func isTypeLocalToPackage(t *types.Var, pkgName string) bool {
	typeName := strings.TrimPrefix(typeStringer(t.Type()), "[]")
	typeName = strings.TrimPrefix(typeName, "*")

	return strings.HasPrefix(typeName, pkgName)
}

func localTypesFromStruct(pkgName string, tt types.Type, typeMap map[string]struct{}) []parsedType {
	var dependencies []parsedType
	typeMap[typeStringer(tt)] = struct{}{}

	s, ok := decodeToType[*types.Struct](tt)
	if !ok {
		return dependencies
	}

	for field := range s.Fields() {
		if _, ok := typeMap[typeStringer(unwrapType(field.Type()))]; ok {
			continue
		}

		if isTypeLocalToPackage(field, pkgName) {
			if _, ok := decodeToType[*types.Struct](field.Type()); ok {
				dependencies = append(dependencies, localTypesFromStruct(pkgName, field.Type(), typeMap)...)
			}

			pt := parsedType{
				name:        field.Name(),
				tt:          field.Type(),
				packageName: pkgName,
			}
			dependencies = append(dependencies, pt)
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
