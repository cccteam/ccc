package generation

import (
	"fmt"
	"slices"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/go-playground/errors/v5"
)

func (c *client) extractResources(structs []parser.Struct) ([]resourceInfo, error) {
	resources := make([]resourceInfo, 0, len(structs))
	for _, pStruct := range structs {
		table, err := c.lookupTable(pStruct.Name())
		if err != nil {
			return nil, err
		}

		resource := resourceInfo{
			TypeInfo:       pStruct.TypeInfo,
			Fields:         make([]resourceField, len(pStruct.Fields())),
			IsView:         table.IsView,
			searchIndexes:  table.SearchIndexes,
			IsConsolidated: !table.IsView && slices.Contains(c.consolidatedResourceNames, pStruct.Name()) != c.consolidateAll,
			PkCount:        table.PkCount,
		}

		for i, field := range pStruct.Fields() {
			spannerTag, ok := field.LookupTag("spanner")
			if !ok {
				return nil, errors.Newf("field %s \n%s", field.Name(), pStruct.PrintWithFieldError(i, "missing spanner tag"))
			}
			tableColumn, ok := table.Columns[spannerTag]
			if !ok {
				return nil, errors.Newf("field %s \n%s", field.Name(), pStruct.PrintWithFieldError(i, fmt.Sprintf("not a valid column in table %q", c.pluralize(pStruct.Name()))))
			}
			_, hasIndexTag := field.LookupTag("index")
			if !table.IsView && hasIndexTag {
				return nil, errors.Newf("cannot use index tag on field %s because resource %s is not virtual/view", field.Name(), resource.Name())
			}

			resource.Fields[i] = resourceField{
				Field:              field,
				Parent:             &resource,
				IsPrimaryKey:       tableColumn.IsPrimaryKey,
				IsForeignKey:       tableColumn.IsForeignKey,
				IsIndex:            tableColumn.IsIndex || hasIndexTag,
				IsUniqueIndex:      tableColumn.IsUniqueIndex,
				IsNullable:         tableColumn.IsNullable,
				OrdinalPosition:    tableColumn.OrdinalPosition,
				KeyOrdinalPosition: tableColumn.KeyOrdinalPosition,
				ReferencedResource: tableColumn.ReferencedTable,
				ReferencedField:    tableColumn.ReferencedColumn,
				HasDefault:         tableColumn.HasDefault,
			}
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

func (c *client) structsToRPCMethods(structs []parser.Struct) ([]rpcMethodInfo, error) {
	rpcMethods := make([]rpcMethodInfo, 0, len(structs))
	for _, s := range structs {
		rpcMethod := rpcMethodInfo{
			Struct: s,
			Fields: make([]rpcField, len(s.Fields())),
		}

		for i, field := range s.Fields() {
			rpcMethod.Fields[i] = rpcField{Field: field}
		}
		rpcMethods = append(rpcMethods, rpcMethod)
	}

	return rpcMethods, nil
}
