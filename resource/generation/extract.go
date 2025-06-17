package generation

import (
	"fmt"
	"slices"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/go/packages"
)

func (c *client) structToResource(pStruct *parser.Struct) (*resourceInfo, error) {
	if pStruct == nil {
		return nil, errors.New("parser.Struct cannot be nil")
	}

	table, err := c.lookupTable(pStruct.Name())
	if err != nil {
		return nil, errors.Wrapf(err, "struct %s is not in lookupTable", pStruct.Error())
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
			return nil, errors.Newf("field %s \n%s", field.Error(), pStruct.PrintWithFieldError(i, "missing spanner tag"))
		}
		tableColumn, ok := table.Columns[spannerTag]
		if !ok {
			return nil, errors.Newf("field %s \n%s", field.Error(), pStruct.PrintWithFieldError(i, fmt.Sprintf("not a valid column in table %q", c.pluralize(pStruct.Name()))))
		}
		_, hasIndexTag := field.LookupTag("index")
		if !table.IsView && hasIndexTag {
			return nil, errors.Newf("cannot use index tag on field %s because resource %s is not virtual/view", field.Name(), resource.Name())
		}

		resource.Fields[i] = &resourceField{
			Field:              field,
			Parent:             resource,
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

	return resource, nil
}

func (c *client) extractResources(pkg *packages.Package) ([]*resourceInfo, error) {
	resourceStructs := parser.ParseStructs(pkg)

	resources := make([]*resourceInfo, 0, len(resourceStructs))
	for _, pStruct := range resourceStructs {
		resource, err := c.structToResource(pStruct)
		if err != nil {
			return nil, err
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

func (c *client) structToRPCMethod(pStruct *parser.Struct) (*rpcMethodInfo, error) {
	if pStruct == nil {
		return nil, errors.New("parser.Struct cannot be nil")
	}

	method := &rpcMethodInfo{
		Struct: *pStruct,
		Fields: make([]*rpcField, len(pStruct.Fields())),
	}

	for i, field := range pStruct.Fields() {
		method.Fields[i] = &rpcField{
			Field: field,
		}
	}

	return method, nil
}
