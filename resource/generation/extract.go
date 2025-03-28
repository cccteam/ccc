package generation

import (
	"fmt"
	"go/types"
	"slices"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/go-playground/errors/v5"
)

func (c *client) structToResource(pStruct *parser.Struct) (*resourceInfo, error) {
	if pStruct == nil {
		return nil, errors.New("parser.Struct cannot be nil")
	}

	table, err := c.lookupTable(pStruct.Name())
	if err != nil {
		return nil, errors.Wrapf(err, "struct %q at %s:%d is not in lookupTable", pStruct.Name(), pStruct.PackageName(), pStruct.Position())
	}

	resource := &resourceInfo{
		TypeInfo:              pStruct.TypeInfo,
		Fields:                make([]*resourceField, len(pStruct.Fields())),
		IsView:                table.IsView,
		searchIndexes:         table.SearchIndexes,
		HasCompoundPrimaryKey: table.PkCount > 1,
		IsConsolidated:        !table.IsView && slices.Contains(c.consolidatedResourceNames, pStruct.Name()) != c.consolidateAll,
	}

	for i, field := range pStruct.Fields() {
		spannerTag, ok := field.LookupTag("spanner")
		if !ok {
			return nil, errors.Newf("field %s.%s in package %s\n%s", resource.Name(), field.Name(), field.PackageName(), pStruct.PrintWithFieldError(i, "missing spanner tag"))
		}
		tableColumn, ok := table.Columns[spannerTag]
		if !ok {
			return nil, errors.Newf("field %s.%s in package %s\n%s", resource.Name(), field.Name(), field.PackageName(), pStruct.PrintWithFieldError(i, fmt.Sprintf("not a valid column in table %q", c.pluralize(pStruct.Name()))))
		}

		resource.Fields[i] = &resourceField{
			Field:              &field,
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
	resourceStructs, err := parser.ParseStructs(pkg)
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

func extractStructsByInterface(pkg *types.Package, interfaceNames ...string) ([]parser.Struct, error) {
	parsedStructs, err := parser.ParseStructs(pkg)
	if err != nil {
		return nil, err
	}

	if len(interfaceNames) == 0 {
		return nil, nil
	}

	var rpcStructs []parser.Struct

	for _, pStruct := range parsedStructs {
		for _, interfaceName := range interfaceNames {
			if parser.HasInterface(pkg, pStruct, interfaceName) {
				rpcStructs = append(rpcStructs, pStruct)
			}
		}
	}

	if len(rpcStructs) == 0 {
		return nil, errors.Newf("package %q has no structs that implement an interface in %v", pkg.Name(), interfaceNames)
	}

	return rpcStructs, nil
}
