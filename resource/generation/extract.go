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

func (c *client) structsToResources(structs []*parser.Struct, validators ...structValidator) ([]*resourceInfo, error) {
	resources := make([]*resourceInfo, 0, len(structs))
	var resourceErrors []error
	for _, pStruct := range structs {
		annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(pStruct)
		if err != nil {
			resourceErrors = append(resourceErrors, errors.Wrap(err, "scanner.ScanStruct()"))

			continue
		}

		if !annotations.Struct.Has(resourceKeyword) {
			continue
		}

		if err := validate(pStruct, validators...); err != nil {
			resourceErrors = append(resourceErrors, err)

			continue
		}

		table, err := c.tableMetadataFor(pStruct.Name())
		if err != nil {
			return nil, err
		}

		resource := &resourceInfo{
			TypeInfo:       pStruct.TypeInfo,
			Fields:         make([]*resourceField, 0, len(pStruct.Fields())),
			IsConsolidated: c.IsConsolidated(pStruct.Name()),
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

		if err := resolveResourceAnnotations(resource, annotations); err != nil {
			resourceErrors = append(resourceErrors, err)

			continue
		}

		resources = append(resources, resource)
	}

	if len(resourceErrors) > 0 {
		return nil, errors.Wrapf(errors.Join(resourceErrors...), "encountered %d errors converting structs to resources", len(resourceErrors))
	}

	return resources, nil
}

func resolveResourceAnnotations(res *resourceInfo, annotations genlang.StructAnnotations) error {
	if annotations.Struct.Has(suppressKeyword) {
		for handlerArg := range annotations.Struct.Get(suppressKeyword).Seq() {
			switch HandlerType(handlerArg) {
			case AllHandlers:
				res.SuppressedHandlers = []HandlerType{ListHandler, ReadHandler, PatchHandler}
				res.IsConsolidated = false
			case ListHandler:
				res.SuppressedHandlers = append(res.SuppressedHandlers, ListHandler)
			case ReadHandler:
				res.SuppressedHandlers = append(res.SuppressedHandlers, ReadHandler)
			case PatchHandler:
				res.SuppressedHandlers = append(res.SuppressedHandlers, PatchHandler)
				res.IsConsolidated = false
			default:
				return errors.Newf("unexpected handler type %[1]q in @suppress(%[1]s) on %[2]s, must be one of %v", handlerArg, res.Name(), handlerTypes())
			}
		}
	}

	if annotations.Struct.Has(defaultsCreateTypeKeyword) {
		res.DefaultsCreateType = string(annotations.Struct.Get(defaultsCreateTypeKeyword))
	}
	if annotations.Struct.Has(defaultsUpdateTypeKeyword) {
		res.DefaultsUpdateType = string(annotations.Struct.Get(defaultsUpdateTypeKeyword))
	}
	if annotations.Struct.Has(validateCreateTypeKeyword) {
		res.ValidateCreateType = string(annotations.Struct.Get(validateCreateTypeKeyword))
	}
	if annotations.Struct.Has(validateUpdateTypeKeyword) {
		res.ValidateUpdateType = string(annotations.Struct.Get(validateUpdateTypeKeyword))
	}

	return nil
}

func (c *client) structsToVirtualResources(structs []*parser.Struct, validators ...structValidator) ([]*resourceInfo, error) {
	resources := make([]*resourceInfo, 0, len(structs))
	var errs []error
	for _, pStruct := range structs {
		annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(pStruct)
		if err != nil {
			errs = append(errs, errors.Wrap(err, "scanner.ScanStruct()"))

			continue
		}

		if !annotations.Struct.Has(virtualKeyword) {
			continue
		}

		if err := validate(pStruct, validators...); err != nil {
			errs = append(errs, err)

			continue
		}

		resource := &resourceInfo{
			TypeInfo:  pStruct.TypeInfo,
			IsVirtual: true,
		}

		fields, err := newVirtualFields(resource, pStruct)
		if err != nil {
			errs = append(errs, err)

			continue
		}
		resource.Fields = fields

		nullableFields, err := fieldNullability(pStruct)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		for _, field := range resource.Fields {
			spannerTag, _ := field.LookupTag("spanner")
			nullability, ok := nullableFields[spannerTag]
			if !ok {
				continue
			}

			field.IsNullable = nullability
		}

		if annotations.Struct.Has(suppressKeyword) {
			for handlerArg := range annotations.Struct.Get(suppressKeyword).Seq() {
				switch HandlerType(handlerArg) {
				case AllHandlers:
					resource.SuppressedHandlers = []HandlerType{ListHandler, ReadHandler, PatchHandler}
				case ListHandler, ReadHandler, PatchHandler:
					resource.SuppressedHandlers = append(resource.SuppressedHandlers, HandlerType(handlerArg))
				default:
					errs = append(errs, errors.Newf("unexpected handler type %[1]q in @suppress(%[1]s) on %[2]s, must be one of %v", handlerArg, pStruct.Name(), handlerTypes()))

					continue
				}
			}
		}

		resources = append(resources, resource)
	}

	if len(errs) > 0 {
		return nil, errors.Wrapf(errors.Join(errs...), "encountered %d errors converting structs to resources", len(errs))
	}

	return resources, nil
}

func newResourceFields(parent *resourceInfo, pStruct *parser.Struct, table *tableMetadata) ([]*resourceField, error) {
	if parent.IsVirtual {
		panic("newResourceFields cannot be used with virtual resources")
	}
	fields := make([]*resourceField, 0, len(pStruct.Fields()))
	for _, field := range pStruct.Fields() {
		spannerTag, ok := field.LookupTag("spanner")
		if !ok {
			field.AddError("missing spanner tag")

			continue
		}
		tableColumn, ok := table.Columns[spannerTag]
		if !ok {
			field.AddError("spanner tag does not match any table columns")

			continue
		}
		if field.HasTag("index") {
			field.AddError("cannot use index tag in non-virtual resource")

			continue
		}

		fields = append(fields, &resourceField{
			Field:              field,
			Parent:             parent,
			IsPrimaryKey:       tableColumn.IsPrimaryKey,
			IsForeignKey:       tableColumn.IsForeignKey,
			IsIndex:            tableColumn.IsIndex,
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

func newVirtualFields(parent *resourceInfo, pStruct *parser.Struct) ([]*resourceField, error) {
	if !parent.IsVirtual {
		panic("newVirtualFields cannot be used with concrete resources")
	}
	fields := make([]*resourceField, 0, len(pStruct.Fields()))
	for _, field := range pStruct.Fields() {
		_, ok := field.LookupTag("spanner")
		if !ok {
			field.AddError("missing spanner tag")

			continue
		}

		fields = append(fields, &resourceField{
			Field:         field,
			Parent:        parent,
			IsIndex:       field.HasTag("index") || field.HasTag("uniqueindex"),
			IsUniqueIndex: field.HasTag("uniqueindex"),
		})
	}

	if pStruct.HasErrors() {
		return nil, errors.Newf("struct %s has field errors:\n%s", pStruct.Name(), pStruct.PrintErrors())
	}

	return fields, nil
}

func (c *client) structsToRPCMethods(structs []*parser.Struct, validators ...structValidator) ([]*rpcMethodInfo, error) {
	rpcMethods := make([]*rpcMethodInfo, 0, len(structs))
	var errs []error
	for _, s := range structs {
		annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(s)
		if err != nil {
			errs = append(errs, errors.Wrap(err, "scanner.ScanStruct()"))
		}

		if !annotations.Struct.Has(rpcKeyword) {
			continue
		}

		if err := validate(s, validators...); err != nil {
			errs = append(errs, err)

			continue
		}

		rpcMethod := &rpcMethodInfo{
			Struct: s,
			Fields: make([]*rpcField, 0, len(s.Fields())),
		}

		for _, field := range s.Fields() {
			field := rpcField{Field: field}
			if enumeratedResource, hasEnumeratedTag := field.LookupTag("enumerated"); hasEnumeratedTag {
				if !c.doesResourceExist(enumeratedResource) {
					field.AddError(fmt.Sprintf("referenced resource %q in enumerated tag does not exist", enumeratedResource))

					continue
				}
				field.enumeratedResource = &enumeratedResource
			}

			rpcMethod.Fields = append(rpcMethod.Fields, &field)
		}

		if s.HasErrors() {
			errs = append(errs, errors.Newf("%s has errors:\n%s", s.Name(), s.PrintErrors()))

			continue
		}

		rpcMethod.SuppressHandler = annotations.Struct.Has(suppressKeyword)

		rpcMethods = append(rpcMethods, rpcMethod)
	}

	if len(errs) != 0 {
		return nil, errors.Wrap(errors.Join(errs...), "RPC method errors")
	}

	return rpcMethods, nil
}

func structsToCompResources(structs []*parser.Struct, validators ...structValidator) ([]*computedResource, error) {
	compResources := make([]*computedResource, 0, len(structs))
	var resourceErrors []error
	for _, s := range structs {
		annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(s)
		if err != nil {
			resourceErrors = append(resourceErrors, errors.Wrap(err, "scanner.ScanStruct()"))

			continue
		}

		if !annotations.Struct.Has(computedKeyword) {
			continue
		}

		if err := validate(s, validators...); err != nil {
			resourceErrors = append(resourceErrors, err)

			continue
		}

		res := &computedResource{
			Struct: s,
			Fields: make([]*computedField, 0, len(s.Fields())),
		}

		if annotations.Struct.Has(suppressKeyword) {
			handlerArg := annotations.Struct.Get(suppressKeyword)
			if strings.Contains(string(handlerArg), string(ReadHandler)) {
				res.SuppressReadHandler = true
			}
			if strings.Contains(string(handlerArg), string(ListHandler)) {
				res.SuppressListHandler = true
			}
		}

		var keyCount int
		for i, field := range s.Fields() {
			field := &computedField{
				Field:        field,
				IsPrimaryKey: annotations.Fields[i].Has(primarykeyKeyword),
			}

			if annotations.Fields[i].Has(primarykeyKeyword) {
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
