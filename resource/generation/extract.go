package generation

import (
	"fmt"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

func (c *client) extractResources(structs []*parser.Struct) ([]resourceInfo, error) {
	resources := make([]resourceInfo, 0, len(structs))
	for _, pStruct := range structs {
		resourceName := pStruct.Name()
		table, err := c.lookupTable(resourceName)
		if err != nil {
			return nil, err
		}

		resource := resourceInfo{
			TypeInfo:      pStruct.TypeInfo,
			Fields:        make([]resourceField, len(pStruct.Fields())),
			IsView:        table.IsView,
			searchIndexes: table.SearchIndexes,
			// Consolidate resource if it is not a view and it is in consolidated list
			IsConsolidated: !table.IsView && c.consolidateConfig.IsConsolidated(resourceName),
			PkCount:        table.PkCount,
		}

		scanner := genlang.NewScanner(keywords())
		result, err := scanner.ScanStruct(pStruct)
		if err != nil {
			return nil, errors.Wrap(err, "scanner.ScanStruct()")
		}

		if result.Struct.Has(suppressKeyword) {
		suppressLoop:
			for i, handlerArg := range result.Struct.Get(suppressKeyword) {
				switch HandlerType(handlerArg.Arg1) {
				case AllHandlers:
					resource.SuppressedHandlers = [3]HandlerType{ListHandler, ReadHandler, PatchHandler}
					resource.IsConsolidated = false

					break suppressLoop
				case ListHandler:
					resource.SuppressedHandlers[i] = ListHandler
				case ReadHandler:
					resource.SuppressedHandlers[i] = ReadHandler
				case PatchHandler:
					resource.SuppressedHandlers[i] = PatchHandler
					resource.IsConsolidated = false
				default:
					return nil, errors.Newf("unexpected handler type %[1]q in @suppress(%[1]s) on %[2]s", handlerArg.Arg1, resourceName)
				}
			}
		}

		if result.Struct.Has(defaultsCreateFuncKeyword) {
			args := result.Struct.Get(defaultsCreateFuncKeyword)
			if len(args) != 1 {
				return nil, errors.Newf("@%s should have exactly one argument, found %d on %s", defaultsCreateFuncKeyword, len(args), resourceName)
			}
			resource.DefaultsCreateFunc = args[0].Arg1
		}

		if result.Struct.Has(defaultsUpdateFuncKeyword) {
			args := result.Struct.Get(defaultsUpdateFuncKeyword)
			if len(args) != 1 {
				return nil, errors.Newf("@%s should have exactly one argument, found %d on %s", defaultsUpdateFuncKeyword, len(args), resourceName)
			}
			resource.DefaultsUpdateFunc = args[0].Arg1
		}

		if result.Struct.Has(validateCreateFuncKeyword) {
			args := result.Struct.Get(validateCreateFuncKeyword)
			if len(args) != 1 {
				return nil, errors.Newf("@%s should have exactly one argument, found %d on %s", validateCreateFuncKeyword, len(args), resourceName)
			}
			resource.ValidateCreateFunc = args[0].Arg1
		}

		if result.Struct.Has(validateUpdateFuncKeyword) {
			args := result.Struct.Get(validateUpdateFuncKeyword)
			if len(args) != 1 {
				return nil, errors.Newf("@%s should have exactly one argument, found %d on %s", validateUpdateFuncKeyword, len(args), resourceName)
			}
			resource.ValidateUpdateFunc = args[0].Arg1
		}

		for i, field := range pStruct.Fields() {
			spannerTag, ok := field.LookupTag("spanner")
			if !ok {
				return nil, errors.Newf("field %s \n%s", field.Name(), pStruct.PrintWithFieldError(i, "missing spanner tag"))
			}
			tableColumn, ok := table.Columns[spannerTag]
			if !ok {
				return nil, errors.Newf("field %s \n%s", field.Name(), pStruct.PrintWithFieldError(i, fmt.Sprintf("not a valid column in table %q", c.pluralize(resourceName))))
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

func (c *client) structsToRPCMethods(structs []*parser.Struct) ([]rpcMethodInfo, error) {
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
