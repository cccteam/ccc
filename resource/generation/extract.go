package generation

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

func (c *client) extractResources(structs []*parser.Struct) ([]*resourceInfo, error) {
	resources := make([]*resourceInfo, 0, len(structs))
	var resourceErrors []error
	for _, pStruct := range structs {
		resourceName := pStruct.Name()
		table, err := c.lookupTable(resourceName)
		if err != nil {
			return nil, err
		}

		resource := &resourceInfo{
			TypeInfo:       pStruct.TypeInfo,
			Fields:         make([]*resourceField, 0, len(pStruct.Fields())),
			IsView:         table.IsView,
			IsConsolidated: !table.IsView && c.IsConsolidated(resourceName),
			PkCount:        table.PkCount,
		}

		fields, err := newResourceFields(resource, pStruct, table)
		if err != nil {
			resourceErrors = append(resourceErrors, err)

			continue
		}
		resource.Fields = fields

		if err := validateNullability(pStruct, table); err != nil {
			resourceErrors = append(resourceErrors, err)

			continue
		}

		result, err := genlang.NewScanner(keywords()).ScanStruct(pStruct)
		if err != nil {
			resourceErrors = append(resourceErrors, errors.Wrap(err, "scanner.ScanStruct()"))

			continue
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
					resourceErrors = append(resourceErrors, errors.Newf("unexpected handler type %[1]q in @suppress(%[1]s) on %[2]s, must be one of %v", handlerArg.Arg1, resourceName, new(HandlerType).enumerate()))

					continue
				}
			}
		}

		if result.Struct.Has(defaultsCreateTypeKeyword) {
			resource.DefaultsCreateType = result.Struct.GetOne(defaultsCreateTypeKeyword).Arg1
		}
		if result.Struct.Has(defaultsUpdateTypeKeyword) {
			resource.DefaultsUpdateType = result.Struct.GetOne(defaultsUpdateTypeKeyword).Arg1
		}
		if result.Struct.Has(validateCreateTypeKeyword) {
			resource.ValidateCreateType = result.Struct.GetOne(validateCreateTypeKeyword).Arg1
		}
		if result.Struct.Has(validateUpdateTypeKeyword) {
			resource.ValidateUpdateType = result.Struct.GetOne(validateUpdateTypeKeyword).Arg1
		}

		resources = append(resources, resource)
	}

	if len(resourceErrors) > 0 {
		return nil, errors.Wrapf(errors.Join(resourceErrors...), "encountered %d errors converting structs to resources", len(resourceErrors))
	}

	return resources, nil
}

func newResourceFields(parent *resourceInfo, pStruct *parser.Struct, table *tableMetadata) ([]*resourceField, error) {
	fields := make([]*resourceField, 0, len(pStruct.Fields()))
	for i, field := range pStruct.Fields() {
		spannerTag, ok := field.LookupTag("spanner")
		if !ok {
			pStruct.AddFieldError(i, "missing spanner tag")

			continue
		}
		tableColumn, ok := table.Columns[spannerTag]
		if !ok {
			pStruct.AddFieldError(i, "spanner tag does not match any table columns")

			continue
		}
		if !table.IsView && field.HasTag("index") {
			pStruct.AddFieldError(i, "cannot use index tag in non-virtual resource")

			continue
		}

		fields = append(fields, &resourceField{
			Field:              field,
			Parent:             parent,
			IsPrimaryKey:       tableColumn.IsPrimaryKey,
			IsForeignKey:       tableColumn.IsForeignKey,
			IsIndex:            tableColumn.IsIndex || field.HasTag("index"),
			IsUniqueIndex:      tableColumn.IsUniqueIndex,
			IsNullable:         tableColumn.IsNullable,
			OrdinalPosition:    tableColumn.OrdinalPosition,
			KeyOrdinalPosition: tableColumn.KeyOrdinalPosition,
			ReferencedResource: tableColumn.ReferencedTable,
			ReferencedField:    tableColumn.ReferencedColumn,
			HasDefault:         tableColumn.HasDefault,
		})
	}

	if pStruct.HasErrors() {
		return nil, errors.Newf("struct %s has field errors:\n%s", pStruct.Name(), pStruct.PrintErrors())
	}

	return fields, nil
}

func (c *client) structsToRPCMethods(structs []*parser.Struct) ([]*rpcMethodInfo, error) {
	rpcMethods := make([]*rpcMethodInfo, 0, len(structs))
	for _, s := range structs {
		rpcMethod := &rpcMethodInfo{
			Struct: s,
			Fields: make([]*rpcField, 0, len(s.Fields())),
		}

		for i, field := range s.Fields() {
			field := rpcField{Field: field}
			if enumeratedResource, hasEnumeratedTag := field.LookupTag("enumerated"); hasEnumeratedTag {
				if !c.doesResourceExist(enumeratedResource) {
					return nil, errors.Newf("field %s \n%s", field.Name(), s.PrintWithFieldError(i, fmt.Sprintf("referenced resource %q in enumerated tag does not exist", enumeratedResource)))
				}
				field.enumeratedResource = &enumeratedResource
			}

			rpcMethod.Fields = append(rpcMethod.Fields, &field)
		}
		rpcMethods = append(rpcMethods, rpcMethod)
	}

	return rpcMethods, nil
}

func structsToCompResources(structs []*parser.Struct) ([]*computedResource, error) {
	compResources := make([]*computedResource, 0, len(structs))
	var resourceErrors []error
	for _, s := range structs {
		res := &computedResource{
			Struct: s,
			Fields: make([]*computedField, 0, len(s.Fields())),
		}

		result, err := genlang.NewScanner(keywords()).ScanStruct(s)
		if err != nil {
			resourceErrors = append(resourceErrors, errors.Wrap(err, "scanner.ScanStruct()"))

			continue
		}

		if result.Struct.Has(suppressKeyword) {
			suppress := result.Struct.GetOne(suppressKeyword).Arg1
			if !strings.Contains(suppress, string(ReadHandler)) {
				res.SuppressReadHandler = true
			}
			if !strings.Contains(suppress, string(ListHandler)) {
				res.SuppressListHandler = true
			}
		}

		var keyCount int
		for i, field := range s.Fields() {
			field := &computedField{
				Field:        field,
				IsPrimaryKey: result.Fields[i].Has(primarykeyKeyword),
			}

			if result.Fields[i].Has(primarykeyKeyword) {
				field.IsPrimaryKey = true
				field.KeyOrdinalPosition = keyCount
				keyCount++
			}

			res.Fields = append(res.Fields, field)
		}
		compResources = append(compResources, res)
	}

	if resourceErrors != nil {
		return nil, errors.Wrap(errors.Join(resourceErrors...), "structsToCompResources()")
	}

	return compResources, nil
}

func validateNullability(pStruct *parser.Struct, table *tableMetadata) error {
	nullableFields, err := fieldNullability(pStruct)
	if err != nil {
		return err
	}

	var errRows []string
	for _, field := range pStruct.Fields() {
		spannerTag, _ := field.LookupTag("spanner")
		if nullableFields[spannerTag] != table.Columns[spannerTag].IsNullable {
			errRow := fmt.Sprintf("| %-32s | %13t | %15t |", spannerTag, nullableFields[spannerTag], table.Columns[spannerTag].IsNullable)
			errRows = append(errRows, errRow)
		}
	}

	if len(errRows) > 0 {
		msg := strings.Builder{}
		msg.WriteString("| ------------------------------------------------------------------ |\n")
		msg.WriteString(fmt.Sprintf("| %*s |\n", -66, fmt.Sprintf("%*s", (66+len(pStruct.Name()))/2, pStruct.Name()))) // string centering voodoo black magic
		msg.WriteString("| ------------------------------------------------------------------ |\n")
		msg.WriteString("|               Name               | Can Nil Field | Can Null Column |\n")
		msg.WriteString("| -------------------------------- | ------------- | --------------- |\n")

		for i := range errRows {
			msg.WriteString(errRows[i])
			msg.WriteString("\n")
		}

		return errors.Newf("found mismatching nullability between the struct fields and columns:\n%s", msg.String())
	}

	return nil
}

func fieldNullability(pStruct *parser.Struct) (map[string]bool, error) {
	nullableFields := make(map[string]bool)
	var missingTags []string
	for _, field := range pStruct.Fields() {
		spannerTag, ok := field.LookupTag("spanner")
		if !ok {
			missingTags = append(missingTags, field.Name())
		}

		if slices.Contains([]string{
			"*string",
			"*bool",
			"*uint", "*uint8", "*uint16", "*uint32", "*uint64",
			"*int", "*int8", "*int16", "*int32", "*int64",
			"*float32", "*float64",
			"*time.Time",
			"*interface {}",
			"ccc.NullUUID",
			"sql.NullBool", "sql.NullByte", "sql.NullFloat64", "sql.NullInt16", "sql.NullInt32", "sql.NullInt64", "sql.NullString", "sql.NullTime",
			"spanner.NullBool", "spanner.NullDate", "spanner.NullFloat32", "spanner.NullFloat64", "spanner.NullInt64", "spanner.NullJSON", "spanner.NullNumeric", "spanner.NullString", "spanner.NullTime",
			"*civil.Date",
		}, field.Type()) {
			nullableFields[spannerTag] = true

			continue
		}

		if field.IsPointer() {
			nullableFields[spannerTag] = true

			continue
		}

		if name := field.DerefUnqualifiedType(); strings.HasPrefix(name, "Null") && unicode.IsUpper(rune(name[4])) {
			nullableFields[spannerTag] = true

			continue
		}
	}

	if len(missingTags) > 0 {
		msg := strings.Builder{}
		for i := range missingTags {
			if i > 0 {
				msg.WriteString(", ")
			}
			msg.WriteString(missingTags[i])
		}

		return nil, errors.Newf("struct %s fields missing spanner tags: [%s]", pStruct.Name(), msg.String())
	}

	return nullableFields, nil
}
