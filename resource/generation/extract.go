package generation

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/go/packages"
)

func parseResourceFile(filePath string) (*ast.File, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filePath, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, errors.Wrap(err, "parser.ParseFile()")
	}

	return file, nil
}

// Loads and type checks a package. Returns any errors encountered during
// loading or typechecking, otherwise returns the package's type data.
// Useful for static type analysis with the [types] package instead of
// manually parsing the AST. A good explainer lives here: https://github.com/golang/example/tree/master/gotypes
func loadPackageTypes(directoryPath string) (*types.Package, error) {
	cfg := &packages.Config{Mode: packages.NeedTypes}
	pkgs, err := packages.Load(cfg, directoryPath)
	if err != nil {
		return nil, errors.Wrap(err, "packages.Load()")
	}

	if len(pkgs) == 0 {
		return nil, errors.New("no packages loaded")
	}

	return pkgs[0].Types, nil
}

// We can iterate over the declarations at the package level a single time
// to extract all the data necessary for generation. Any new data that needs
// to be added to the struct definitions can be extracted here.
func (c *Client) extractResourceTypes(pkg *types.Package) ([]*ResourceInfo, error) {
	if pkg == nil {
		return nil, errors.New("package is nil")
	}

	scope := pkg.Scope() // The package scope holds all the objects declared at package level (TypeNames, Consts, Vars, and Funcs)
	if scope == nil || len(scope.Names()) == 0 {
		return nil, errors.Newf("package `%s` has invalid scope", pkg.Name())
	}

	var routerResources []accesstypes.Resource
	if c.rc != nil {
		routerResources = c.rc.Resources()
	}

	var resources []*ResourceInfo

	for _, name := range scope.Names() {
		object := scope.Lookup(name)
		if object == nil {
			return nil, errors.Newf("package `%s` in an invalid state: `%s` from scope.Names() not found in scope.Lookup()", pkg.Name(), name)
		}

		structType := decodeToStructType(object.Type())
		if structType == nil {
			continue
		}

		resource := ResourceInfo{Name: object.Name()}

		spannerTable, ok := c.tableLookup[c.pluralize(object.Name())]
		if !ok {
			return nil, errors.Newf("struct `%s` at %s:%d is not in tableMeta", object.Name(), pkg.Name(), object.Pos())
		}

		if spannerTable.IsView {
			resource.IsView = true
		}

		if spannerTable.PkCount > 1 {
			resource.HasCompoundPrimaryKey = true
		}

		for i := range structType.NumFields() {
			field := structType.Field(i)
			if field == nil || !field.IsField() || field.Embedded() {
				return nil, errors.Newf("invalid field[%d] in struct `%s` at %s:%v", i, object.Name(), pkg.Name(), object.Pos())
			}

			structTag := reflect.StructTag(structType.Tag(i))

			spannerColumnName := structTag.Get("spanner")
			if spannerColumnName == "" {
				return nil, errors.Newf("field `%s` in struct `%s` at %s:%d must include `spanner:\"<column name>\" struct tag", field.Name(), object.Name(), pkg.Name(), field.Pos())
			}

			query := structTag.Get("query")
			conditions := strings.Split(structTag.Get("conditions"), ",")
			permissions := strings.Split(structTag.Get("perm"), ",")

			typescriptType, err := decodeToTypescriptType(field.Type(), c.typescriptOverrides)
			if err != nil {
				return nil, err
			}

			goType, err := decodeToGoType(field.Type())
			if err != nil {
				return nil, err
			}

			// BEGIN spanner stuff
			spannerColumn, ok := spannerTable.Columns[spannerColumnName]
			if !ok {
				return nil, errors.Newf("field `%s` in struct `%s` at %s:%d is not in tableMeta", field.Name(), object.Name(), pkg.Name(), field.Pos())
			}

			var isRequiredForCreate bool
			if spannerColumn.IsPrimaryKey {
				if typescriptType != "uuid" {
					isRequiredForCreate = true
				}
			} else if !spannerColumn.IsNullable {
				isRequiredForCreate = true
			}

			var isEnumerated bool
			if spannerColumn.IsForeignKey && slices.Contains(routerResources, accesstypes.Resource(spannerColumn.ReferencedTable)) {
				isEnumerated = true
			}

			// END spanner stuff

			fieldInfo := FieldInfo{
				Parent:             &resource,
				Name:               field.Name(),
				SpannerName:        spannerColumnName,
				GoType:             goType,
				typescriptType:     typescriptType,
				query:              query,
				Conditions:         conditions,
				permissions:        permissions,
				Required:           isRequiredForCreate,
				IsPrimaryKey:       spannerColumn.IsPrimaryKey,
				IsForeignKey:       spannerColumn.IsForeignKey,
				IsIndex:            spannerColumn.IsIndex,
				IsUniqueIndex:      spannerColumn.IsUniqueIndex,
				OrdinalPosition:    spannerColumn.OrdinalPosition,
				KeyOrdinalPosition: spannerColumn.KeyOrdinalPosition,
				IsEnumerated:       isEnumerated,
				ReferencedResource: spannerColumn.ReferencedTable,
				ReferencedField:    spannerColumn.ReferencedColumn,
			}

			resource.Fields = append(resource.Fields, fieldInfo)
		}

		if len(resource.Fields) == 0 {
			return nil, errors.Newf("struct `%s` has no fields at %s:%v", object.Name(), pkg.Name(), object.Pos())
		}

		resources = append(resources, &resource)
	}

	return resources, nil
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

	// `types.BasicInfo` is a set of bit flags that describe properies of a basic type.
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
			return "", errors.Newf("`%s` is an unsupported basic type of info/kind: %v/%v", basicType.String(), basicType.Info(), basicType.Kind())
		}
	}

	decodeNamedType := func(namedType *types.Named) (string, error) {
		// Qualifies a named type with its package: `package.TypeName`
		qualifiedTypeString := types.TypeString(namedType, _qualifier)

		overrideTypeString, ok := typescriptOverrides[qualifiedTypeString]
		if !ok {
			return "", errors.Newf("`%s` is an unsupported type not present in typescriptOverrides", qualifiedTypeString)
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
		return "", errors.Newf("`%s` is an unsupported type", t.String())
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
		return "", errors.Newf("`%s` is an unsupported type", t.String())
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
