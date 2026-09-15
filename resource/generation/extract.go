package generation

import (
	"fmt"
	"iter"
	"slices"
	"strings"
	"unicode"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

func (c *client) structsToResources(structs []*parser.Struct, validators ...structValidator) ([]*resourceInfo, error) {
	// structsByTable resolves a binding path's remote hops: each hop names Go
	// fields on the struct backing the table the hop lands on.
	structsByTable := make(map[string]*parser.Struct, len(structs))
	for _, s := range structs {
		structsByTable[c.pluralize(s.Name())] = s
	}

	resources := make([]*resourceInfo, 0, len(structs))
	var resourceErrors []error
	for _, pStruct := range structs {
		annotations, err := scanStruct(pStruct)
		if err != nil {
			resourceErrors = append(resourceErrors, err)

			continue
		}

		if !annotations.Struct.Has(resourceKeyword) {
			continue
		}

		if err := validate(pStruct, validators...); err != nil {
			resourceErrors = append(resourceErrors, err)

			continue
		}

		if fieldAnnotationErr := rejectPrimaryKeyAnnotations(pStruct, annotations); fieldAnnotationErr != nil {
			resourceErrors = append(resourceErrors, fieldAnnotationErr)

			continue
		}

		// The annotations other kinds own: a method's frame, a view's table, and a
		// field type's TypeScript type.
		if err := errors.Join(rejectRPCOnlyAnnotations(pStruct, annotations, "resource"), rejectRowsOf(pStruct, annotations, "table-backed resource"), rejectTypescriptAnnotation(pStruct, annotations, "table-backed resource")); err != nil {
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
			IsInterleaved:  table.IsInterleaved,
		}

		fields, err := newResourceFields(resource, pStruct, table)
		if err != nil {
			resourceErrors = append(resourceErrors, err)

			continue
		}
		resource.Fields = fields
		declareFieldEnumerations(pStruct, fields, annotations)

		if err := c.resolveResource(resource, pStruct, annotations, structsByTable, table); err != nil {
			resourceErrors = append(resourceErrors, err)

			continue
		}

		resources = append(resources, resource)
	}

	if len(resourceErrors) > 0 {
		return nil, errors.Wrapf(errors.Join(resourceErrors...), "encountered %d errors converting structs to resources", len(resourceErrors))
	}

	// Workflow chains cross resources, so they resolve — and the uniform
	// state bindings synthesize — only once every resource is extracted.
	if err := c.resolveWorkflows(resources); err != nil {
		return nil, err
	}

	return resources, nil
}

// resolveResource applies everything a table-backed resource declares beyond its
// fields, in dependency order: nullability against the table, the struct annotations,
// the bindings, the per-field index flags the tenant anchor decides, the positional
// masking check that reads those flags, and the state annotations.
func (c *client) resolveResource(resource *resourceInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations, structsByTable map[string]*parser.Struct, table *tableMetadata) error {
	if err := validateNullability(pStruct, table); err != nil {
		return err
	}

	if err := c.resolveStructAnnotations(resource, pStruct, annotations); err != nil {
		return err
	}

	if err := c.resolveBindingAnnotations(resource, pStruct, annotations, structsByTable); err != nil {
		return err
	}

	// The tenant anchor is known only now, and the field flag for the column after it
	// in an index key depends on it.
	resource.deriveTenantIndexFlags(table)

	// With the order and the index flags known, a positional declaration can be
	// checked against what a list of the resource sorts and filters by.
	if err := checkMaskingDeclarations(resource); err != nil {
		return err
	}

	return c.resolveStateAnnotations(resource, pStruct, annotations)
}

// resolveVirtualAnnotations applies a virtual resource's struct- and
// field-level annotations: suppression, manual Sets, permission scope,
// outlets, and — with the scope resolved — the @domain tenancy binding.
func resolveVirtualAnnotations(resource *resourceInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	if annotations.Struct.Has(suppressKeyword) {
		if err := applySuppressDirectives(resource, annotations.Struct.Get(suppressKeyword).Seq()); err != nil {
			return errors.Wrapf(err, "@suppress on %s", pStruct.Name())
		}
	}

	if annotations.Struct.Has(manualAddResourceSetKeyword) {
		if err := applyManualAddResourceSetDirectives(resource, annotations.Struct.Get(manualAddResourceSetKeyword).Seq()); err != nil {
			return errors.Wrapf(err, "@%s on %s", manualAddResourceSetKeyword, pStruct.Name())
		}
	}

	if err := resolvePermissionScope(annotations, &resource.PermissionScope); err != nil {
		return errors.Wrapf(err, "on %s", pStruct.Name())
	}

	if err := resolveOutlets(annotations.Struct, &resource.outletMembership); err != nil {
		return errors.Wrapf(err, "on %s", pStruct.Name())
	}

	// Scope resolves above; the tenancy pairing (domain-scoped ⇔ @domain)
	// validates against it.
	return resolveVirtualDomain(resource, pStruct, annotations)
}

// parsePermissionScopeAnnotation resolves a @permissionScope argument to one of the two
// valid scopes.
func parsePermissionScopeAnnotation(arg genlang.Arg) (accesstypes.PermissionScope, error) {
	scope := accesstypes.PermissionScope(strings.TrimSpace(string(arg)))
	if scope != accesstypes.GlobalPermissionScope && scope != accesstypes.DomainPermissionScope {
		return "", errors.Newf("@%s must be %q or %q, got %q", permissionScopeKeyword, accesstypes.GlobalPermissionScope, accesstypes.DomainPermissionScope, scope)
	}

	return scope, nil
}

// resolvePermissionScope applies a @permissionScope annotation to dest if present, leaving
// it unset (the generator's global default) when absent. It is shared by every resource
// kind (resources, virtual resources, RPC methods, computed resources) that carries a
// PermissionScope field, since the annotation means the same thing on each.
func resolvePermissionScope(annotations genlang.StructAnnotations, dest *accesstypes.PermissionScope) error {
	if !annotations.Struct.Has(permissionScopeKeyword) {
		return nil
	}

	scope, err := parsePermissionScopeAnnotation(annotations.Struct.Get(permissionScopeKeyword))
	if err != nil {
		return err
	}

	*dest = scope

	return nil
}

func resolveResourceAnnotations(res *resourceInfo, annotations genlang.StructAnnotations) error {
	if annotations.Struct.Has(suppressKeyword) {
		if err := applySuppressDirectives(res, annotations.Struct.Get(suppressKeyword).Seq()); err != nil {
			return errors.Wrapf(err, "@suppress on %s", res.Name())
		}
	}

	if annotations.Struct.Has(manualAddResourceSetKeyword) {
		if err := applyManualAddResourceSetDirectives(res, annotations.Struct.Get(manualAddResourceSetKeyword).Seq()); err != nil {
			return errors.Wrapf(err, "@%s on %s", manualAddResourceSetKeyword, res.Name())
		}
	}

	if err := resolvePermissionScope(annotations, &res.PermissionScope); err != nil {
		return errors.Wrapf(err, "on %s", res.Name())
	}

	if err := resolveOutlets(annotations.Struct, &res.outletMembership); err != nil {
		return errors.Wrapf(err, "on %s", res.Name())
	}

	if err := resolveResourcePaging(res, annotations); err != nil {
		return err
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

// resolveStructAnnotations applies a table-backed struct's annotations and then the
// derivation the schema imposes on it: a struct backing an @enumerate table is
// read-only (deriveEnumerationResource).
func (c *client) resolveStructAnnotations(res *resourceInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	if err := resolveResourceAnnotations(res, annotations); err != nil {
		return err
	}
	if typeName, ok := c.enumerationOf(c.pluralize(pStruct.Name())); ok {
		return deriveEnumerationResource(res, typeName)
	}

	return nil
}

// deriveEnumerationResource makes a struct that backs an @enumerate table read-only:
// the table's rows are the program's constants (generated as Go constants and a
// TypeScript enum), so a mutation handler would let the rows drift from the code
// generated from them. The patch handler is suppressed as if @suppress(PatchHandler)
// were written, which also drops Create, Update, and Delete from the collection and
// the consolidated handler. Anything on the struct that only a mutable table needs is
// a contradiction and fails generation, rather than being dropped quietly.
func deriveEnumerationResource(res *resourceInfo, typeName string) error {
	var conflicts []string
	for _, c := range []struct{ keyword, value string }{
		{defaultsCreateTypeKeyword, res.DefaultsCreateType},
		{defaultsUpdateTypeKeyword, res.DefaultsUpdateType},
		{validateCreateTypeKeyword, res.ValidateCreateType},
		{validateUpdateTypeKeyword, res.ValidateUpdateType},
	} {
		if c.value != "" {
			conflicts = append(conflicts, "@"+c.keyword)
		}
	}
	if slices.Contains(res.ManualAddResourceSets, PatchHandler) {
		conflicts = append(conflicts, fmt.Sprintf("@%s(%s)", manualAddResourceSetKeyword, PatchHandler))
	}
	if len(conflicts) > 0 {
		return errors.Newf("struct %s backs the enumeration table %s (@%s on type %s), so it is read-only: its rows are the program's constants; remove %s", res.Name(), typeName, enumerateKeyword, typeName, strings.Join(conflicts, ", "))
	}

	res.EnumerationType = typeName
	if !slices.Contains(res.SuppressedHandlers, PatchHandler) {
		res.SuppressedHandlers = append(res.SuppressedHandlers, PatchHandler)
	}
	res.IsConsolidated = false

	return nil
}

// resolveOutlets applies an @outlet annotation to dest if present; both comma lists
// and repeated annotations are accepted. The annotations are a struct's or, for a
// manual registration, an accesstypes.Resource constant's. Names are validated
// against the declared outlets after everything is extracted
// (validateAnnotatedOutlets); here only empty and duplicate names are rejected.
func resolveOutlets(annotations genlang.ArgMap, dest *outletMembership) error {
	if !annotations.Has(outletKeyword) {
		return nil
	}

	for arg := range annotations.Get(outletKeyword).Seq() {
		for part := range strings.SplitSeq(arg, ",") {
			name := strings.TrimSpace(part)
			if name == "" {
				return errors.Newf("@%s(%s) contains an empty outlet name", outletKeyword, arg)
			}
			if slices.Contains(dest.OutletNames, name) {
				return errors.Newf("@%s names outlet %q twice", outletKeyword, name)
			}
			dest.OutletNames = append(dest.OutletNames, name)
		}
	}

	return nil
}

func applySuppressDirectives(res *resourceInfo, suppressArgs iter.Seq[string]) error {
	for arg := range suppressArgs {
		if RouteType(arg) == AllRoutes {
			res.SuppressedRoutes = append(res.SuppressedRoutes, AllRoutes)

			continue
		}
		switch HandlerType(arg) {
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
			return errors.Newf("unexpected argument %[1]q in @suppress(%[1]s), must be one of %v", arg, validSuppressArgs())
		}
	}

	if hasConsolidatedHandler(res) && res.RoutingDisabled() {
		return errors.Newf("@suppress(%[1]s) is not supported because the patch handler is part of the consolidated handlers and is served by a shared route that cannot be suppressed per resource; exclude this resource from the consolidated handlers or add @suppress(%[2]s)", AllRoutes, PatchHandler)
	}

	return nil
}

func applyComputedSuppressDirectives(res *computedResource, suppressArgs iter.Seq[string]) error {
	for arg := range suppressArgs {
		if RouteType(arg) == AllRoutes {
			res.SuppressedRoutes = append(res.SuppressedRoutes, AllRoutes)

			continue
		}
		switch HandlerType(arg) {
		case AllHandlers:
			res.SuppressListHandler = true
			res.SuppressReadHandler = true
		case ListHandler:
			res.SuppressListHandler = true
		case ReadHandler:
			res.SuppressReadHandler = true
		default:
			return errors.Newf("unexpected argument %[1]q in @suppress(%[1]s), must be one of %v", arg, validComputedSuppressArgs())
		}
	}

	return nil
}

func (c *client) structsToVirtualResources(structs []*parser.Struct, validators ...structValidator) ([]*resourceInfo, error) {
	resources := make([]*resourceInfo, 0, len(structs))
	var errs []error
	for _, pStruct := range structs {
		annotations, err := scanStruct(pStruct)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		if !annotations.Struct.Has(virtualKeyword) {
			continue
		}

		if err := rejectBindingAnnotations(pStruct, annotations, "virtual resource", domainKeyword); err != nil {
			errs = append(errs, err)

			continue
		}

		if err := errors.Join(rejectRPCOnlyAnnotations(pStruct, annotations, "virtual resource"), rejectTypescriptAnnotation(pStruct, annotations, "virtual resource")); err != nil {
			errs = append(errs, err)

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

		fields, err := newVirtualFields(resource, pStruct, annotations)
		if err != nil {
			errs = append(errs, err)

			continue
		}
		resource.Fields = fields
		declareFieldEnumerations(pStruct, fields, annotations)
		declareRowsOf(annotations, &resource.rowsOfDecl)

		// A view declares its order and page sizes as a table does, and its list handler
		// and descriptor carry them the same way.
		if err := resolveResourcePaging(resource, annotations); err != nil {
			errs = append(errs, err)

			continue
		}

		if err := checkMaskingDeclarations(resource); err != nil {
			errs = append(errs, err)

			continue
		}

		nullableFields, err := fieldNullability(pStruct)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		for _, field := range resource.Fields {
			spannerTag, _ := field.LookupTag(spannerTagKey)
			nullability, ok := nullableFields[spannerTag]
			if !ok {
				continue
			}

			field.IsNullable = nullability
		}

		if err := resolveVirtualAnnotations(resource, pStruct, annotations); err != nil {
			errs = append(errs, err)

			continue
		}

		resources = append(resources, resource)
	}

	if len(errs) > 0 {
		return nil, errors.Wrapf(errors.Join(errs...), "encountered %d errors converting structs to resources", len(errs))
	}

	return resources, nil
}

// scanStruct scans a struct's annotations and refuses the struct when it claims more
// than one kind, before any extractor can claim it.
func scanStruct(pStruct *parser.Struct) (genlang.StructAnnotations, error) {
	annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(pStruct)
	if err != nil {
		return genlang.StructAnnotations{}, errors.Wrap(err, "scanner.ScanStruct()")
	}
	if err := rejectMultipleKinds(pStruct, annotations); err != nil {
		return genlang.StructAnnotations{}, err
	}

	return annotations, nil
}

// structKindKeywords decide what a struct is to the generator. Exactly one may appear
// on a struct (README: "Exactly one of @resource, @virtual, @computed, or @rpc may
// appear on a struct"). The scanner's Exclusive flag only stops one keyword from
// repeating, and every extractor scans every struct in its package and claims the ones
// carrying its own keyword, so without this check a struct carrying two kinds would be
// extracted twice, once as each, instead of refused.
var structKindKeywords = []string{resourceKeyword, virtualKeyword, computedKeyword, rpcKeyword}

// rejectMultipleKinds fails a struct that carries more than one kind keyword.
func rejectMultipleKinds(pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	var kinds []string
	for _, keyword := range structKindKeywords {
		if annotations.Struct.Has(keyword) {
			kinds = append(kinds, "@"+keyword)
		}
	}
	if len(kinds) > 1 {
		return errors.Newf("struct %s carries %s: exactly one of @%s, @%s, @%s, or @%s may appear on a struct", pStruct.Name(), strings.Join(kinds, " and "), resourceKeyword, virtualKeyword, computedKeyword, rpcKeyword)
	}

	return nil
}

// rejectPrimaryKeyAnnotations errors when a table-backed @resource struct carries
// @primarykey field annotations: table-backed primary keys come from the schema, and
// the annotation is only valid on @computed and @virtual structs.
func rejectPrimaryKeyAnnotations(pStruct *parser.Struct, annotations genlang.StructAnnotations) error {
	var errs []error
	for i, field := range pStruct.Fields() {
		if annotations.Fields[i].Has(primarykeyKeyword) {
			errs = append(errs, errors.Newf("struct %s field %s: @primarykey is only valid on @computed and @virtual structs; table-backed primary keys come from the schema", pStruct.Name(), field.Name()))
		}
	}

	if len(errs) != 0 {
		return errors.Wrap(errors.Join(errs...), "@primarykey annotation error")
	}

	return nil
}

func newResourceFields(parent *resourceInfo, pStruct *parser.Struct, table *tableMetadata) ([]*resourceField, error) {
	if parent.IsVirtual {
		panic("newResourceFields cannot be used with virtual resources")
	}
	fields := make([]*resourceField, 0, len(pStruct.Fields()))
	for _, field := range pStruct.Fields() {
		spannerTag, ok := field.LookupTag(spannerTagKey)
		if !ok {
			field.AddError("missing spanner tag")

			continue
		}
		if reserved, collides := reservedRowName(spannerTag, field.Name()); collides {
			// The read statements' reserved output columns and the reserved
			// per-row wire property: a colliding resource column would be
			// indistinguishable from the envelope metadata.
			field.AddError(fmt.Sprintf("column name %q is reserved for the row envelope", reserved))

			continue
		}
		tableColumn, ok := table.Columns[spannerTag]
		if !ok {
			field.AddError("spanner tag does not match any table columns")

			continue
		}
		if field.HasTag(indexTagKey) {
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
			SpannerType:        tableColumn.SpannerType,
		})
	}

	if pStruct.HasErrors() {
		return nil, errors.Newf("struct %s has field errors:\n%s", pStruct.Name(), pStruct.PrintErrors())
	}

	return fields, nil
}

// deriveTenantIndexFlags marks the fields a filter seeks with the tenant bound. The
// generated list of a resource with a bare @domain column always binds that column by
// equality (the partition filter), so the column directly after it in any index key
// has a seek path of its own: an equality on the prefix, then a range on the column.
// The table-level flag (deriveIndexFlags) knows no tenant and marks leading columns
// only; this pass adds the tenant-second columns for exactly the resources whose lists
// bind the tenant on the row. A join-path resource compares its foreign key to the
// parent row, not to a parameter, and a global resource binds nothing, so both keep the
// table flags alone. Direction does not matter for a seek, and null filtering leaves
// the reading as it is for a leading column. A global request carries no partition
// predicate, so for it the seek is an index scan, the same cost class as its ordered
// list, which already sorts the whole table.
func (r *resourceInfo) deriveTenantIndexFlags(table *tableMetadata) {
	if r.DomainBinding == nil || len(r.DomainBinding.Path) > 0 {
		return
	}
	anchor := fieldColumn(r.DomainBinding.Anchor)

	byColumn := make(map[string]*resourceField, len(r.Fields))
	for _, field := range r.Fields {
		byColumn[fieldColumn(field)] = field
	}

	for _, index := range table.Indexes {
		if len(index.Key) < 2 || index.Key[0].Column != anchor {
			continue
		}
		if field, ok := byColumn[index.Key[1].Column]; ok {
			field.IsIndex = true
		}
	}
}

func newVirtualFields(parent *resourceInfo, pStruct *parser.Struct, annotations genlang.StructAnnotations) ([]*resourceField, error) {
	if !parent.IsVirtual {
		panic("newVirtualFields cannot be used with concrete resources")
	}
	fields := make([]*resourceField, 0, len(pStruct.Fields()))
	var keyCount int64
	for i, field := range pStruct.Fields() {
		_, ok := field.LookupTag(spannerTagKey)
		if !ok {
			field.AddError("missing spanner tag")

			continue
		}

		rField := &resourceField{
			Field:         field,
			Parent:        parent,
			IsIndex:       field.HasTag(indexTagKey) || field.HasTag(uniqueIndexTagKey),
			IsUniqueIndex: field.HasTag(uniqueIndexTagKey),
		}

		if annotations.Fields[i].Has(primarykeyKeyword) {
			rField.IsPrimaryKey = true
			rField.KeyOrdinalPosition = keyCount
			keyCount++
		}

		fields = append(fields, rField)
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
		annotations, err := scanStruct(s)
		if err != nil {
			errs = append(errs, err)
		}

		if !annotations.Struct.Has(rpcKeyword) {
			continue
		}

		// The annotations other kinds own: a resource's bindings, a view's table, and a
		// field type's TypeScript type; and the masking tag, which a method's request
		// never carries.
		if err := errors.Join(rejectBindingAnnotations(s, annotations, "RPC method"), rejectRowsOf(s, annotations, "RPC method"), rejectTypescriptAnnotation(s, annotations, "RPC method"), rejectMaskingTags(s, "RPC method")); err != nil {
			errs = append(errs, err)

			continue
		}

		if err := validate(s, validators...); err != nil {
			errs = append(errs, err)

			continue
		}

		rpcMethod, err := c.classifyRPCMethod(s)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		for i, field := range s.Fields() {
			field := rpcField{Field: field, wire: rpcMethod.Request.Fields[i], namespace: s.Name(), typescriptType: rpcMethod.Request.Fields[i].TypescriptDisplayType()}
			if annotations.Fields[i].Has(enumerateKeyword) {
				src, err := c.resolveEnumerate(annotations.Fields[i].Get(enumerateKeyword))
				if err != nil {
					field.AddError(err.Error())

					continue
				}
				field.applyEnumeration(src)
			}

			rpcMethod.Fields = append(rpcMethod.Fields, &field)
		}

		if s.HasErrors() {
			errs = append(errs, errors.Newf("%s has errors:\n%s", s.Name(), s.PrintErrors()))

			continue
		}

		rpcMethod.SuppressHandler = annotations.Struct.Has(suppressKeyword)

		if err := resolvePermissionScope(annotations, &rpcMethod.PermissionScope); err != nil {
			errs = append(errs, errors.Wrapf(err, "on %s", s.Name()))

			continue
		}

		if err := resolveOutlets(annotations.Struct, &rpcMethod.outletMembership); err != nil {
			errs = append(errs, errors.Wrapf(err, "on %s", s.Name()))

			continue
		}

		if err := c.resolveTransition(rpcMethod, s, annotations); err != nil {
			errs = append(errs, err)

			continue
		}

		if err := resolveAnswers(rpcMethod, s, annotations); err != nil {
			errs = append(errs, err)

			continue
		}

		if err := resolveUpload(rpcMethod, s, annotations); err != nil {
			errs = append(errs, err)

			continue
		}

		rpcMethods = append(rpcMethods, rpcMethod)
	}

	if len(errs) != 0 {
		return nil, errors.Wrap(errors.Join(errs...), "RPC method errors")
	}

	return rpcMethods, nil
}

// classifyRPCMethod reads what the struct's Execute declares: how it runs, what it
// answers, and the wire shapes of its request and result. A struct without a
// recognizable Execute never reaches the templates, so no handler can be generated
// that decodes and returns without running it.
func (c *client) classifyRPCMethod(s *parser.Struct) (*rpcMethodInfo, error) {
	signature, err := classifyExecute(s)
	if err != nil {
		return nil, err
	}

	// One walker for the request and the result: a struct both reach keeps one
	// mirror in the handler, and every name is checked against the whole file.
	walker := newWireWalker(c.leaves(), s.PackageName(), c.resource.Package())
	request, err := c.walkRequest(walker, s)
	if err != nil {
		return nil, err
	}
	result, err := c.walkResult(walker, s, signature)
	if err != nil {
		return nil, err
	}

	return &rpcMethodInfo{
		Struct:        s,
		Form:          signature.form,
		Request:       request,
		Result:        result,
		ResultPointer: signature.resultPointer,
		choosesStatus: signature.choosesStatus,
		takesFiles:    signature.takesFiles,
		Fields:        make([]*rpcField, 0, len(s.Fields())),
	}, nil
}

// walkRequest reads an RPC struct's wire shape: what the handler's request mirror
// declares and every struct it reaches, under the one vocabulary shared with
// results and computed resources. It refuses the one leaf the RPC decoder does
// not carry, a *bool at the top level, which the decoder cannot tell apart from
// an absent field.
func (c *client) walkRequest(walker *wireWalker, s *parser.Struct) (*wireShape, error) {
	request, err := walker.walk(s)
	if err != nil {
		return nil, errors.Wrap(err, "RPC request")
	}
	for _, f := range request.Fields {
		if f.IsLeaf() && f.Pointer && !f.Slice && f.tsLeaf == booleanStr {
			return nil, errors.Newf("struct %s.%s: *bool is not supported in RPC requests; use bool", s.Name(), f.Name)
		}
	}

	return request, nil
}

// walkResult reads the wire shape of the struct Execute answers with, nil when it
// answers with error alone. A method answers with identifiers and outcomes, never
// rows: a @resource or @computed struct in the result position is refused, since
// rows are read through their resource routes, where permission masking applies.
func (c *client) walkResult(walker *wireWalker, s *parser.Struct, signature executeSignature) (*wireShape, error) {
	if signature.result == nil {
		return nil, nil
	}
	name, pkg := signature.result.Obj().Name(), ""
	if signature.result.Obj().Pkg() != nil {
		pkg = signature.result.Obj().Pkg().Name()
	}
	if pkg == c.resource.Package() {
		for _, res := range c.resources {
			if res.Name() == name {
				return nil, errors.Newf("struct %s: Execute answers with the resource %s.%s; a method answers with identifiers and outcomes, and rows are read through the resource's own routes, where permission masking applies", s.Name(), pkg, name)
			}
		}
	}
	if pkg == c.computed.Package() {
		for _, res := range c.computedResources {
			if res.Name() == name {
				return nil, errors.Newf("struct %s: Execute answers with the computed resource %s.%s; a method answers with identifiers and outcomes, and rows are read through the resource's own routes, where permission masking applies", s.Name(), pkg, name)
			}
		}
	}

	result, err := walker.walkNamed(signature.result, s.Name()+" result "+typeStringer(signature.result))
	if err != nil {
		return nil, errors.Wrap(err, "RPC result")
	}

	return result, nil
}

func (c *client) structsToCompResources(structs []*parser.Struct, validators ...structValidator) ([]*computedResource, error) {
	compResources := make([]*computedResource, 0, len(structs))
	var resourceErrors []error
	for _, s := range structs {
		annotations, err := scanStruct(s)
		if err != nil {
			resourceErrors = append(resourceErrors, err)

			continue
		}

		if !annotations.Struct.Has(computedKeyword) {
			continue
		}

		if err := rejectBindingAnnotations(s, annotations, "computed resource"); err != nil {
			resourceErrors = append(resourceErrors, err)

			continue
		}

		if err := errors.Join(rejectRPCOnlyAnnotations(s, annotations, "computed resource"), rejectTypescriptAnnotation(s, annotations, "computed resource")); err != nil {
			resourceErrors = append(resourceErrors, err)

			continue
		}

		if err := validate(s, validators...); err != nil {
			resourceErrors = append(resourceErrors, err)

			continue
		}

		// The row's wire shape, under the one vocabulary shared with RPC requests
		// and results. A nested field is opaque to the resource machinery.
		shape, err := newWireWalker(c.leaves(), s.PackageName()).walk(s)
		if err != nil {
			resourceErrors = append(resourceErrors, errors.Wrap(err, "computed resource"))

			continue
		}

		res := &computedResource{
			Struct: s,
			Shape:  shape,
		}
		declareRowsOf(annotations, &res.rowsOfDecl)

		if annotations.Struct.Has(suppressKeyword) {
			if err := applyComputedSuppressDirectives(res, annotations.Struct.Get(suppressKeyword).Seq()); err != nil {
				resourceErrors = append(resourceErrors, errors.Wrapf(err, "@suppress on %s", s.Name()))

				continue
			}
		}

		if err := resolvePermissionScope(annotations, &res.PermissionScope); err != nil {
			resourceErrors = append(resourceErrors, errors.Wrapf(err, "on %s", s.Name()))

			continue
		}

		if err := resolveOutlets(annotations.Struct, &res.outletMembership); err != nil {
			resourceErrors = append(resourceErrors, errors.Wrapf(err, "on %s", s.Name()))

			continue
		}

		if err := c.computedFields(res, annotations); err != nil {
			resourceErrors = append(resourceErrors, err)

			continue
		}

		if err := resolveComputedPaging(res, annotations); err != nil {
			resourceErrors = append(resourceErrors, err)

			continue
		}
		compResources = append(compResources, res)
	}

	if resourceErrors != nil {
		return nil, errors.Wrap(errors.Join(resourceErrors...), "structsToCompResources()")
	}

	return compResources, nil
}

// computedFields builds the resource's fields off its walked shape and the
// primarykey annotations, enforcing the opaque rule on every nested field.
func (c *client) computedFields(res *computedResource, annotations genlang.StructAnnotations) error {
	res.Fields = make([]*computedField, 0, len(res.Struct.Fields()))
	var keyCount int
	var errs []error
	for i, field := range res.Struct.Fields() {
		field := &computedField{
			Field:          field,
			wire:           res.Shape.Fields[i],
			namespace:      c.pluralize(res.Name()),
			typescriptType: res.Shape.Fields[i].TypescriptDisplayType(),
		}

		if annotations.Fields[i].Has(primarykeyKeyword) {
			field.IsPrimaryKey = true
			field.KeyOrdinalPosition = keyCount
			keyCount++
		}
		if annotations.Fields[i].Has(enumerateKeyword) {
			arg := annotations.Fields[i].Get(enumerateKeyword)
			field.enumerateArg = &arg
		}

		if err := checkOpaqueField(res.Name(), field); err != nil {
			errs = append(errs, err)
		}
		if err := checkComputedQueryTags(res.Name(), field); err != nil {
			errs = append(errs, err)
		}

		res.Fields = append(res.Fields, field)
	}
	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "computed resource fields")
	}

	return nil
}

// checkComputedQueryTags enforces the query tags a computed field may carry. index
// and uniqueindex name database indexes, which a computed resource has none of,
// so they are refused; allow_filter declares a filterable field, whose type the
// in-memory evaluator must be able to compare, decided here with the field named
// rather than at request time.
func checkComputedQueryTags(resource string, field *computedField) error {
	path := resource + "." + field.Name()
	for _, key := range []string{indexTagKey, uniqueIndexTagKey} {
		if _, ok := field.LookupTag(key); ok {
			return errors.Newf("%s: the %s tag names a database index, which a computed resource has none of; use allow_filter to make a field filterable", path, key)
		}
	}
	if _, ok := field.LookupTag(maskingTagKey); ok {
		return errors.Newf("%s: the %s tag says how a masked cell meets a sort or a filter, and a computed resource never masks: its permission checks run at decode time, where conditional grants are refused", path, maskingTagKey)
	}
	if _, ok := field.LookupTag(allowFilterTagKey); !ok {
		return nil
	}
	if field.wire == nil || field.wire.IsLeaf() {
		base := strings.TrimPrefix(field.DerefUnqualifiedType(), "*")
		if field.wire != nil {
			base = strings.TrimPrefix(field.wire.SourceType, "*")
		}
		if field.wire != nil && field.wire.Slice {
			return errors.Newf("%s: allow_filter on a list field; the filter evaluator compares single values", path)
		}
		// The evaluator compares exactly the types a grant condition compares.
		if _, ok := goTypeToAttributeType(base); ok {
			return nil
		}

		return errors.Newf("%s: allow_filter on a field of type %s, which the filter evaluator cannot compare (text, numbers, booleans, time, date, decimal, and UUID compare)", path, base)
	}

	return nil
}

// opaqueTagKeys are the tags that mean nothing on or inside a nested computed
// field: the field is one unit for permission, PII, and selection, and the
// query decoder never filters or sorts into it.
var opaqueTagKeys = []string{allowFilterTagKey, indexTagKey, uniqueIndexTagKey}

// checkOpaqueField enforces the opaque rule on a computed resource's nested field:
// never a primary key, never filterable or indexed, and no permission, PII, or
// filter tag on any field inside it.
func checkOpaqueField(resource string, field *computedField) error {
	if field.wire == nil || field.wire.IsLeaf() {
		return nil
	}
	path := resource + "." + field.Name()
	if field.IsPrimaryKey {
		return errors.Newf("%s: a nested field cannot be a primary key", path)
	}
	if field.enumerateArg != nil {
		return errors.Newf("%s: a nested field is opaque and cannot carry a field-scope @%s; a picker stores one value", path, enumerateKeyword)
	}
	for _, key := range opaqueTagKeys {
		if _, ok := field.LookupTag(key); ok {
			return errors.Newf("%s: a nested field is opaque and cannot carry the %s tag; the query decoder never filters or sorts into it", path, key)
		}
	}

	return checkOpaqueInner(path, field.wire.Nested, map[*wireShape]bool{})
}

// checkOpaqueInner refuses permission, PII, and filter tags on the fields inside a
// nested shape: the nested field is granted, masked, and selected whole.
func checkOpaqueInner(path string, shape *wireShape, seen map[*wireShape]bool) error {
	if seen[shape] {
		return nil
	}
	seen[shape] = true
	for _, f := range shape.Fields {
		for _, key := range append([]string{permTagKey, conditionsTagKey}, opaqueTagKeys...) {
			if _, ok := f.Tag.Lookup(key); ok {
				return errors.Newf("%s: %s.%s carries the %s tag, which means nothing inside a nested field: the field is granted, masked, and selected whole", path, shape.Source, f.Name, key)
			}
		}
		if f.Nested != nil {
			if err := checkOpaqueInner(path, f.Nested, seen); err != nil {
				return err
			}
		}
	}

	return nil
}

// reservedRowName reports whether a column or field name collides with one of
// the row envelope's reserved names, returning the name it collides with.
func reservedRowName(spannerTag, fieldName string) (string, bool) {
	for _, reserved := range []string{reservedMaskedNamesColumn, reservedCapabilitiesProperty, reservedCapabilityChecksColumn} {
		if strings.EqualFold(spannerTag, reserved) || strings.EqualFold(fieldName, reserved) {
			return reserved, true
		}
	}
	for _, name := range []string{spannerTag, fieldName} {
		if len(name) >= len(reservedCursorColumnPrefix) && strings.EqualFold(name[:len(reservedCursorColumnPrefix)], reservedCursorColumnPrefix) {
			return reservedCursorColumnPrefix + "…", true
		}
	}

	return "", false
}

func validateNullability(pStruct *parser.Struct, table *tableMetadata) error {
	nullableFields, err := fieldNullability(pStruct)
	if err != nil {
		return err
	}

	var errRows []string
	for _, field := range pStruct.Fields() {
		spannerTag, _ := field.LookupTag(spannerTagKey)
		if nullableFields[spannerTag] != table.Columns[spannerTag].IsNullable {
			errRow := fmt.Sprintf("| %-32s | %13t | %15t |", spannerTag, nullableFields[spannerTag], table.Columns[spannerTag].IsNullable)
			errRows = append(errRows, errRow)
		}
	}

	if len(errRows) > 0 {
		msg := strings.Builder{}
		msg.WriteString("| ------------------------------------------------------------------ |\n")
		fmt.Fprintf(&msg, "| %*s |\n", -66, fmt.Sprintf("%*s", (66+len(pStruct.Name()))/2, pStruct.Name())) // string centering voodoo black magic
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
		spannerTag, ok := field.LookupTag(spannerTagKey)
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

// rejectMaskingTags refuses the masking tag on a kind that never masks: a
// method's request is neither listed nor masked, so the tag has nothing to say.
func rejectMaskingTags(s *parser.Struct, kind string) error {
	var errs []error
	for _, field := range s.Fields() {
		if _, ok := field.LookupTag(maskingTagKey); ok {
			errs = append(errs, errors.Newf("field %s.%s carries the %s tag, which says how a masked cell meets a sort or a filter; an %s is never listed or masked", s.Name(), field.Name(), maskingTagKey, kind))
		}
	}

	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "masking tag error")
	}

	return nil
}

// checkMaskingDeclarations refuses a masking:"positional" declaration the field
// cannot honor. On a primary key nothing is ever masked: keys are exempt from the
// visibility rules, so there is no hidden cell to order positionally. On a field no
// list orders or filters by with an index behind it — neither indexed, nor
// allow_filter, nor named in @order — there is no index for the declaration to
// restore, and it would disclose the field's rank for nothing.
func checkMaskingDeclarations(res *resourceInfo) error {
	var errs []error
	for _, field := range res.Fields {
		if !field.IsPositional() {
			continue
		}
		path := res.Name() + "." + field.Name()
		switch {
		case field.IsPrimaryKey:
			errs = append(errs, errors.Newf("%s: masking:%q on a primary key: keys are exempt from masking, so no cell of it is ever hidden and there is nothing to order positionally", path, maskingPositional))
		case !field.IsQueryClauseEligible() && !res.declaresOrderOn(field.Name()):
			errs = append(errs, errors.Newf("%s: masking:%q on a field no list orders or filters by with an index behind it (neither indexed, nor %s, nor named in @%s): there is no index to restore, and the declaration would disclose the field's rank for nothing", path, maskingPositional, allowFilterTagKey, orderKeyword))
		}
	}

	if len(errs) > 0 {
		return errors.Wrap(errors.Join(errs...), "masking declarations")
	}

	return nil
}
