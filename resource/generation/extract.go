package generation

import (
	"go/types"
	"log"
	"reflect"
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

	parsedStructs := make([]parsedStruct, scope.Len())
	for i, name := range scope.Names() {
		object := scope.Lookup(name)
		if object == nil {
			return nil, errors.Newf("package %q in an invalid state: %q from scope.Names() not found in scope.Lookup()", pkg.Name(), name)
		}

		structType, ok := decodeToType[*types.Struct](object.Type())
		if !ok {
			continue
		}

		pStruct := parsedStruct{
			name:        object.Name(),
			packageName: pkg.Name(),
			position:    int(object.Pos()),
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
				Name:         field.Name(),
				Type:         typeStringer(field.Type()),
				parsedType:   field.Type(),
				tags:         reflect.StructTag(structType.Tag(j)),
				_packageName: field.Pkg().Name(),
				_position:    int(field.Pos()),
			}

			pStruct.fields = append(pStruct.fields, fieldInfo)
		}

		parsedStructs[i] = pStruct
	}

	return parsedStructs, nil
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
		HasCompoundPrimaryKey: table.PkCount > 1,
		searchIndexes:         table.SearchIndexes,
	}

	for i, field := range pStruct.fields {
		tableColumn, ok := table.Columns[field.tags.Get("spanner")]
		if !ok {
			return nil, errors.Newf("field %q in struct %q[%d] at %s:%d is not in tableMeta", field.Name, resource.Name, i, field._packageName, field._position)
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

// The [types.Type] interface can be one of 14 concrete types:
// https://github.com/golang/example/tree/master/gotypes#types
// Types can be safely and deterministically decoded from this interface,
// and support can easily be expanded to other types in our [resources] package
func decodeToType[T types.Type](v types.Type) (T, bool) {
	switch t := v.(type) {
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

	// `types.BasicInfo` is a set of bit flags that describe properties of a basic type.
	// Using bitwise-AND we can check if any basic type has a given property.
	// Defined as a closure because it returns TypeScript types
	decodeBasicType := func(basicType *types.Basic) (string, error) {
		switch basicInfo := basicType.Info(); {
		case basicInfo&types.IsBoolean != 0:
			return "boolean", nil
		case basicInfo&types.IsNumeric != 0:
			return "number", nil
		case basicInfo&types.IsString != 0:
			return "string", nil
		default:
			return "", errors.Newf("%q is an unsupported basic type of info/kind: %v/%v", basicType.String(), basicType.Info(), basicType.Kind())
		}
	}

	decodeNamedType := func(namedType *types.Named) (string, error) {
		// Qualifies a named type with its package: `package.TypeName`
		qualifiedTypeString := typeStringer(namedType)

		overrideTypeString, ok := typescriptOverrides[qualifiedTypeString]
		if !ok {
			return "", errors.Newf("%q is an unsupported type not present in typescriptOverrides", qualifiedTypeString)
		}

		return overrideTypeString, nil
	}

	switch t := typ.(type) {
	case *types.Basic:
		return decodeBasicType(t)
	case *types.Named:
		return decodeNamedType(t)
	case *types.Pointer:
		return decodeToTypescriptType(t.Elem(), typescriptOverrides)
	default:
		return "", errors.Newf("%q is an unsupported type", t.String())
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
