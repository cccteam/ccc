package generation

import (
	"go/types"
	"log"
	"reflect"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/go/packages"
)

// Loads and type checks a package. Returns any errors encountered during
// loading or typechecking, otherwise returns the package's data.
// Useful for static type analysis with the [types] package instead of
// manually parsing the AST. A good explainer lives here: https://github.com/golang/example/tree/master/gotypes
func loadPackage(packagePattern string) (*packages.Package, error) {
	log.Printf("Loading file(s) at %q...\n", packagePattern)
	cfg := &packages.Config{Mode: packages.NeedTypes | packages.NeedFiles}
	pkgs, err := packages.Load(cfg, packagePattern)
	if err != nil {
		return nil, errors.Wrap(err, "packages.Load()")
	}

	if len(pkgs) == 0 {
		return nil, errors.New("no packages loaded")
	}

	if len(pkgs[0].GoFiles) == 0 || pkgs[0].GoFiles[0] == "" {
		return nil, errors.New("no files loaded")
	}

	return pkgs[0], nil
}

// We can iterate over the declarations at the package level a single time
// to extract all the data necessary for generation. Any new data that needs
// to be added to the struct definitions can be extracted here.
func extractResourceTypes(pkg *types.Package) ([]*ResourceInfo, error) {
	log.Println("Starting resource extraction...")
	if pkg == nil {
		return nil, errors.New("package is nil")
	}

	scope := pkg.Scope() // The package scope holds all the objects declared at package level (TypeNames, Consts, Vars, and Funcs)
	if scope == nil || len(scope.Names()) == 0 {
		return nil, errors.Newf("package %q has invalid scope", pkg.Name())
	}

	resources := make([]*ResourceInfo, scope.Len())
	for i, name := range scope.Names() {
		object := scope.Lookup(name)
		if object == nil {
			return nil, errors.Newf("package %q in an invalid state: %q from scope.Names() not found in scope.Lookup()", pkg.Name(), name)
		}

		structType := decodeToStructType(object.Type())
		if structType == nil {
			continue
		}

		resource := &ResourceInfo{
			Name:         object.Name(),
			_packageName: pkg.Name(),
			_position:    int(object.Pos()),
		}

		for j := range structType.NumFields() {
			field := structType.Field(j)
			if field == nil || !field.IsField() || field.Embedded() {
				return nil, errors.Newf("invalid field[%d] in struct %q at %s:%v", j, object.Name(), pkg.Name(), object.Pos())
			}

			fieldInfo := &FieldInfo{
				Parent:     resource,
				Name:       field.Name(),
				parsedType: field.Type(),
				_position:  int(field.Pos()),
			}

			structTag := reflect.StructTag(structType.Tag(j))

			fieldInfo.query = structTag.Get("query")
			if structTag.Get("conditions") != "" {
				fieldInfo.Conditions = strings.Split(structTag.Get("conditions"), ",")
			}

			if structTag.Get("perm") != "" {
				fieldInfo.permissions = strings.Split(structTag.Get("perm"), ",")
			}

			var err error
			fieldInfo.GoType, err = decodeToGoType(field.Type())
			if err != nil {
				return nil, errors.Wrapf(err, "could not decode go type for field %q in struct %q at %s:%v", field.Name(), object.Name(), pkg.Name(), object.Pos())
			}

			fieldInfo.SpannerName = structTag.Get("spanner")
			if fieldInfo.SpannerName == "" {
				return nil, errors.Newf("field %q in struct %q at %s:%d must include `spanner:\"<column name>\" struct tag", field.Name(), object.Name(), pkg.Name(), field.Pos())
			}

			resource.Fields = append(resource.Fields, fieldInfo)
		}

		if len(resource.Fields) == 0 {
			return nil, errors.Newf("struct %q has no fields at %s:%v", object.Name(), pkg.Name(), object.Pos())
		}

		resources[i] = resource
	}

	return resources, nil
}

func (c *Client) syncWithSpannerMetadata(extractedResources []*ResourceInfo) ([]*ResourceInfo, error) {
	if len(extractedResources) == 0 {
		return nil, errors.New("no resources to sync with spanner")
	}

	for _, resource := range extractedResources {
		if resource == nil {
			return nil, errors.New("nil resources cannot be synced with spanner metadata")
		}

		spannerTable, ok := c.tableLookup[c.pluralize(resource.Name)]
		if !ok {
			return nil, errors.Newf("struct %q at %s:%d is not in tableMeta", resource.Name, resource._packageName, resource._position)
		}

		resource.IsView = spannerTable.IsView
		resource.HasCompoundPrimaryKey = spannerTable.PkCount > 1
		resource.searchIndexes = spannerTable.SearchIndexes
		resource.IsConsolidated = !spannerTable.IsView && slices.Contains(c.consolidatedResourceNames, resource.Name) != c.consolidateAll

		for _, fieldInfo := range resource.Fields {
			spannerColumn, ok := spannerTable.Columns[fieldInfo.SpannerName]
			if !ok {
				return nil, errors.Newf("field %q in struct %q at %s:%d is not in tableMeta", fieldInfo.Name, resource.Name, resource._packageName, fieldInfo._position)
			}

			fieldInfo.IsPrimaryKey = spannerColumn.IsPrimaryKey
			fieldInfo.IsForeignKey = spannerColumn.IsForeignKey
			fieldInfo.IsNullable = spannerColumn.IsNullable
			fieldInfo.IsIndex = spannerColumn.IsIndex
			fieldInfo.IsUniqueIndex = spannerColumn.IsUniqueIndex
			fieldInfo.OrdinalPosition = spannerColumn.OrdinalPosition
			fieldInfo.KeyOrdinalPosition = spannerColumn.KeyOrdinalPosition
			fieldInfo.ReferencedResource = spannerColumn.ReferencedTable
			fieldInfo.ReferencedField = spannerColumn.ReferencedColumn

		}

	}

	return extractedResources, nil
}

func (t *TypescriptGenerator) addTypescriptTypes() error {
	for _, resource := range t.resources {
		if resource == nil {
			return errors.New("cannot extract typescript types from nil resources")
		}

		for _, fieldInfo := range resource.Fields {
			var err error
			fieldInfo.typescriptType, err = decodeToTypescriptType(fieldInfo.parsedType, t.typescriptOverrides)
			if err != nil {
				return errors.Wrapf(err, "could not decode typescript type for field %q in struct %q at %s:%v", fieldInfo.Name, resource.Name, resource._packageName, fieldInfo._position)
			}

			if fieldInfo.IsPrimaryKey && fieldInfo.typescriptType != "uuid" {
				fieldInfo.Required = true
			}

			if !fieldInfo.IsPrimaryKey && !fieldInfo.IsNullable {
				fieldInfo.Required = true
			}

			if fieldInfo.IsForeignKey && slices.Contains(t.routerResources, accesstypes.Resource(fieldInfo.ReferencedResource)) {
				fieldInfo.IsEnumerated = true
			}
		}
	}

	return nil
}

// The [types.Type] interface can be one of 14 concrete types:
// https://github.com/golang/example/tree/master/gotypes#types
// Types can be safely and deterministically decoded from this interface,
// and support can easily be expanded to other types in our [resources] package
func decodeToStructType(typ types.Type) *types.Struct {
	switch t := typ.(type) {
	case *types.Named:
		return decodeToStructType(t.Underlying())
	case *types.Struct:
		return t
	default:
		return nil
	}
}

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
		qualifiedTypeString := types.TypeString(namedType, _qualifier)

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

// We are reading Go types and converting them to Go types, not much is needed
// in the way of type checking because we can just print the type string and
// the [goimports] package will ensure qualified named types have their dependencies
func decodeToGoType(typ types.Type) (string, error) {
	if typ == nil {
		return "", errors.Newf("received nil type")
	}

	switch t := typ.(type) {
	case *types.Basic:
		return t.String(), nil
	case *types.Named:
		// Qualifies a named type with its package: `package.TypeName`
		return types.TypeString(t, _qualifier), nil
	case *types.Pointer:
		str, err := decodeToGoType(t.Elem())

		return "*" + str, err
	default:
		return "", errors.Newf("%q is an unsupported type", t.String())
	}
}

// Necessary for qualifying type names with the package they're imported from
// e.g. `ccc.UUID`
func _qualifier(p *types.Package) string {
	if p == nil {
		return ""
	}

	return p.Name()
}
