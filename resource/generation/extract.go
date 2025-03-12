package generation

import (
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
		Type:                  pStruct.Typ,
		Fields:                make([]*resourceField, len(pStruct.Fields())),
		IsView:                table.IsView,
		searchIndexes:         table.SearchIndexes,
		HasCompoundPrimaryKey: table.PkCount > 1,
		IsConsolidated:        !table.IsView && slices.Contains(c.consolidatedResourceNames, pStruct.Name()) != c.consolidateAll,
	}

	for i, field := range pStruct.Fields() {
		spannerTag, ok := field.LookupTag("spanner")
		if !ok {
			return nil, errors.Newf("field %q in struct %q[%d] at %s:%d is missing struct tag `spanner`", field.Name(), resource.Name(), i, field.PackageName(), field.Position())
		}
		tableColumn, ok := table.Columns[spannerTag]
		if !ok {
			return nil, errors.Newf("field %q in struct %q[%d] at %s:%d is not in tableMeta", field.Name(), resource.Name(), i, field.PackageName(), field.Position())
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

func extractStructsByMethod(pkg *types.Package, methodNames ...string) ([]parser.Struct, error) {
	parsedStructs, err := parser.ParseStructs(pkg)
	if err != nil {
		return nil, err
	}

	if len(methodNames) == 0 {
		return parsedStructs, nil
	}

	var rpcStructs []parser.Struct

	for _, pStruct := range parsedStructs {
		if parser.HasMethods(pStruct, methodNames...) {
			rpcStructs = append(rpcStructs, pStruct)
		}
	}

	if len(rpcStructs) == 0 {
		return nil, errors.Newf("package %q has no structs that implement methods %v", pkg.Name(), methodNames)
	}

	return rpcStructs, nil
}
