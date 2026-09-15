package generation

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/ettle/strcase"
	"github.com/go-playground/errors/v5"
	"golang.org/x/tools/go/packages"
)

type typescriptGenerator struct {
	*client
	genPermission         bool
	genMetadata           bool
	genEnums              bool
	typescriptDestination string
	rc                    *resource.GeneratedCollection
	routerResources       []accesstypes.Resource
	// manualRegistrations are the declared registrations with no generated handler;
	// each carries its own outlet membership for the outlet filter.
	manualRegistrations []ManualRegistration
	// domainRouteSegment/domainRouteParam mirror the resourceGenerator's route pair so
	// TypeScript route metadata can render the domain segment of domain-scoped routes.
	domainRouteSegment     string
	domainRouteParam       string
	spannerEmulatorVersion string
	// outletName is the router outlet this target serves (ForOutlet): every emitted
	// file is filtered to the outlet's members. Empty means the default outlet
	// (resolveOptions fills the name in; targetOutlet covers directly constructed
	// generators in tests).
	outletName string
	// outletExcluded are the collection resource names owned exclusively by other
	// outlets — the parsed resources, computed resources, RPC methods, and manual
	// registrations that are not on the target outlet — which the constants output
	// omits.
	outletExcluded []accesstypes.Resource
	// outletExcludedTables are the excluded resource and computed-resource names,
	// for enum filtering: an @enumerate type whose table belongs exclusively to
	// other outlets is not emitted.
	outletExcludedTables map[string]struct{}
}

// targetOutlet is the router outlet this target serves (ForOutlet), defaulting to
// the default outlet.
func (t *typescriptGenerator) targetOutlet() string {
	if t.outletName == "" {
		return defaultOutletName
	}

	return t.outletName
}

// excludeFromOutlet reports whether the member is off the target outlet, recording
// an excluded member's collection resource name — and, for table-backed kinds, its
// table name for enum filtering — so the collection-derived outputs drop the same
// registrations the parsed sets drop.
func (t *typescriptGenerator) excludeFromOutlet(m *outletMembership, resourceName string, isTable bool) bool {
	if m.OnOutlet(t.targetOutlet()) {
		return false
	}

	t.outletExcluded = append(t.outletExcluded, accesstypes.Resource(resourceName))
	if isTable {
		t.outletExcludedTables[resourceName] = struct{}{}
	}

	return true
}

// validateOutletMemberReferences rejects a member that references a resource excluded
// from the target outlet — a method's declared transition root, or the resource a
// declared @enumerate names on a method's, resource's, or computed resource's field.
// The emitted metadata names such a resource through the Resources constant, which
// the filtered constants file no longer declares; silently narrowing the metadata in
// this client alone would misrepresent the member, so the mismatch fails generation
// with the fix instead. (A key's inferred enumeration is not a declaration and
// degrades to its plain type; see resourceFieldsTypescriptType.)
func (t *typescriptGenerator) validateOutletMemberReferences(resources []*resourceInfo, computedResources []*computedResource) error {
	excluded := func(name string) bool {
		return slices.Contains(t.outletExcluded, accesstypes.Resource(name))
	}
	enumerates := func(kind, member, field, enumerated string) error {
		return errors.Newf("outlet %q: %s %s field %s enumerates %s, which is not on the outlet; attach %s to the outlet via @%s, or drop the @enumerate annotation", t.targetOutlet(), kind, member, field, enumerated, enumerated, outletKeyword)
	}

	for _, method := range t.rpcMethods {
		if tr := method.Transition; tr != nil && excluded(tr.RootResource) {
			return errors.Newf("outlet %q: RPC method %s declares a transition on %s, which is not on the outlet; a client cannot carry a transition whose resource it does not emit — attach %s to the outlet via @%s, or move the method off it", t.targetOutlet(), method.Name(), tr.RootResource, tr.RootResource, outletKeyword)
		}
		for _, field := range method.Fields {
			if field.IsEnumerated() && excluded(field.EnumeratedResource()) {
				return enumerates("RPC method", method.Name(), field.Name(), field.EnumeratedResource())
			}
		}
	}
	for _, res := range resources {
		for _, field := range res.Fields {
			if field.HasDeclaredEnumeration() && excluded(field.EnumeratedResource()) {
				return enumerates("resource", t.pluralize(res.Name()), field.Name(), field.EnumeratedResource())
			}
		}
	}
	for _, res := range computedResources {
		for _, field := range res.Fields {
			if field.IsEnumerated && excluded(field.EnumeratedResource()) {
				return enumerates("computed resource", t.pluralize(res.Name()), field.Name(), field.EnumeratedResource())
			}
		}
	}

	return nil
}

// applyOutletFilter narrows the parsed sets to the target outlet's members. Every
// member on another outlet falls away here — resources, computed resources, and
// RPC methods — and is recorded so the collection-derived constants drop the same
// registrations; the surviving members' cross-outlet references are then validated.
// A key's inferred enumeration resolves against the outlet's members only, so a key
// into a resource on another outlet degrades to its plain type instead of referencing
// a Resources constant the filtered output no longer declares; a declared enumeration
// is a statement about the field and fails generation instead.
func (t *typescriptGenerator) applyOutletFilter(resources []*resourceInfo, computedResources []*computedResource) ([]*resourceInfo, []*computedResource, error) {
	t.outletExcludedTables = make(map[string]struct{})
	resources = slices.DeleteFunc(resources, func(res *resourceInfo) bool {
		return t.excludeFromOutlet(&res.outletMembership, t.pluralize(res.Name()), true)
	})
	computedResources = slices.DeleteFunc(computedResources, func(res *computedResource) bool {
		return t.excludeFromOutlet(&res.outletMembership, t.pluralize(res.Name()), true)
	})
	t.rpcMethods = slices.DeleteFunc(t.rpcMethods, func(method *rpcMethodInfo) bool {
		return t.excludeFromOutlet(&method.outletMembership, method.Name(), false)
	})

	if err := t.validateOutletMemberReferences(resources, computedResources); err != nil {
		return nil, nil, err
	}

	t.excludeManualRegistrations(resources, computedResources)

	t.routerResources = slices.DeleteFunc(slices.Clone(t.routerResources), func(res accesstypes.Resource) bool {
		return slices.Contains(t.outletExcluded, res)
	})

	return resources, computedResources, nil
}

// excludeManualRegistrations drops the manual registrations off the target outlet
// from the collection-derived constants. Exclusion is by resource name, so a name
// the outlet still claims — through a surviving member or a manual registration on
// the outlet — stays even when another registration on it is off the outlet.
func (t *typescriptGenerator) excludeManualRegistrations(resources []*resourceInfo, computedResources []*computedResource) {
	claimed := make(map[accesstypes.Resource]struct{}, len(resources)+len(computedResources)+len(t.rpcMethods)+len(t.manualRegistrations))
	for _, res := range resources {
		claimed[accesstypes.Resource(t.pluralize(res.Name()))] = struct{}{}
	}
	for _, res := range computedResources {
		claimed[accesstypes.Resource(t.pluralize(res.Name()))] = struct{}{}
	}
	for _, method := range t.rpcMethods {
		claimed[accesstypes.Resource(method.Name())] = struct{}{}
	}
	for _, reg := range t.manualRegistrations {
		m := outletMembership{OutletNames: reg.Outlets}
		if m.OnOutlet(t.targetOutlet()) {
			claimed[reg.Resource] = struct{}{}
		}
	}

	for _, reg := range t.manualRegistrations {
		if _, ok := claimed[reg.Resource]; ok || slices.Contains(t.outletExcluded, reg.Resource) {
			continue
		}
		t.outletExcluded = append(t.outletExcluded, reg.Resource)
	}
}

// parseResources parses the resource and virtual-resource packages, returning the parsed
// resources alongside the resources package, whose named types the enum generation
// consumes.
func (t *typescriptGenerator) parseResources(packageMap map[string]*packages.Package) ([]*resourceInfo, *parser.Package, error) {
	pkg := packageMap[t.resource.Package()]
	if pkg == nil {
		return nil, nil, errors.Newf("no packages found in %q", t.resource.Dir())
	}
	resourcesPkg := parser.ParsePackage(pkg)
	if err := t.registerEnumerations(resourcesPkg.NamedTypes); err != nil {
		return nil, nil, err
	}

	resources, err := t.structsToResources(resourcesPkg.Structs, t.validateStructNameMatchesFile(pkg, true), validateNoPermTags, validateConditionsTags, validateMaskingTags)
	if err != nil {
		return nil, nil, err
	}

	if t.genVirtualResources {
		pkg := packageMap[t.virtual.Package()]
		virtualStructs := parser.ParsePackage(pkg).Structs
		virtualResources, err := t.structsToVirtualResources(virtualStructs, t.validateStructNameMatchesFile(pkg, true), validateNoPermTags, validateConditionsTags, validateMaskingTags)
		if err != nil {
			return nil, nil, err
		}

		resources = append(resources, virtualResources...)
		sortResources(resources)
	}

	return resources, resourcesPkg, nil
}

func (t *typescriptGenerator) Generate() error {
	log.Println("Starting TypescriptGenerator Generation")

	begin := time.Now()

	packageMap, err := parser.LoadPackages(t.loadPackages...)
	if err != nil {
		return errors.Wrap(err, "parser.LoadPackages()")
	}
	t.notePackages(packageMap)

	resources, resourcesPkg, err := t.parseResources(packageMap)
	if err != nil {
		return err
	}

	// Every package parses against the whole application before the outlet filter
	// runs: an RPC method's transition root, target, or enumerated field resolves by
	// name against the parsed resources, and a method on another outlet may name a
	// resource on another outlet. Only the members that survive the filter have
	// references worth validating (validateOutletMemberReferences).
	var computedResources []*computedResource
	if t.genComputedResources {
		pkg := packageMap[t.computed.Package()]
		compStructs := parser.ParsePackage(pkg).Structs
		computedResources, err = t.structsToCompResources(compStructs, t.validateStructNameMatchesFile(pkg, true), validateNoPermTags, validateConditionsTags, validateMaskingTags)
		if err != nil {
			return err
		}
	}

	// A field-scope @enumerate may name a computed resource, so the declarations
	// resolve only now, against every kind, before the filter narrows the sets.
	t.computedResources = computedResources
	if err := t.resolveFieldEnumerations(resources, computedResources); err != nil {
		return err
	}

	t.resources = resources
	// A view's @rowsOf names a table-backed resource, refusing a view of either kind,
	// so it too resolves against every kind before the filter narrows the sets.
	if err := t.resolveRowsOf(resources, computedResources); err != nil {
		return err
	}

	if t.genRPCMethods {
		pkg := packageMap[t.rpc.Package()]
		rpcStructs := parser.ParsePackage(pkg).Structs
		t.rpcMethods, err = t.structsToRPCMethods(rpcStructs, t.validateStructNameMatchesFile(pkg, false), validateNoPermTags, validateConditionsTags, validateMaskingTags)
		if err != nil {
			return err
		}
	}

	resources, computedResources, err = t.applyOutletFilter(resources, computedResources)
	if err != nil {
		return err
	}

	if err := t.resolveTypescriptTypes(resources, computedResources); err != nil {
		return err
	}

	// The target directory may not exist on a first generate into a fresh
	// application; nothing else in the pipeline creates it.
	if err := os.MkdirAll(t.typescriptDestination, 0o750); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}

	if err := t.runTypescriptMetadataGeneration(); err != nil {
		return err
	}

	if err := t.runTypescriptPermissionGeneration(); err != nil {
		return err
	}

	if err := t.runTypescriptEnumGeneration(resourcesPkg.NamedTypes); err != nil {
		return err
	}

	log.Printf("Finished Typescript generation in %s\n", time.Since(begin))

	return nil
}

func (t *typescriptGenerator) runTypescriptEnumGeneration(namedTypes []*parser.NamedType) error {
	if !t.genEnums {
		return nil
	}

	if !t.genMetadata && !t.genPermission {
		if err := removeGeneratedFiles(t.typescriptDestination, headerComment); err != nil {
			return errors.Wrap(err, "RemoveGeneratedFiles()")
		}
	}

	if err := t.generateEnums(namedTypes); err != nil {
		return errors.Wrap(err, "generateEnums")
	}

	return nil
}

func (t *typescriptGenerator) runTypescriptPermissionGeneration() error {
	if !t.genPermission {
		return nil
	}
	begin := time.Now()
	if !t.genMetadata {
		if err := removeGeneratedFiles(t.typescriptDestination, headerComment); err != nil {
			return errors.Wrap(err, "RemoveGeneratedFiles()")
		}
	}

	log.Println("Starting typescript resource permission generation...")

	routerData := t.rc.TypescriptDataExcluding(t.outletExcluded...)

	piiResourceFields := make(map[accesstypes.Resource]map[accesstypes.Tag]bool, len(t.resources)+len(t.computedResources))
	for _, res := range t.resources {
		for _, field := range res.Fields {
			if field.IsPII() {
				if _, ok := piiResourceFields[accesstypes.Resource(t.pluralize(res.Name()))]; !ok {
					piiResourceFields[accesstypes.Resource(t.pluralize(res.Name()))] = make(map[accesstypes.Tag]bool)
				}
				piiResourceFields[accesstypes.Resource(t.pluralize(res.Name()))][accesstypes.Tag(caser.ToCamel(field.Name()))] = true
			}
		}
	}

	for _, res := range t.computedResources {
		for _, field := range res.Fields {
			if field.IsPII() {
				if _, ok := piiResourceFields[accesstypes.Resource(t.pluralize(res.Name()))]; !ok {
					piiResourceFields[accesstypes.Resource(t.pluralize(res.Name()))] = make(map[accesstypes.Tag]bool)
				}
				piiResourceFields[accesstypes.Resource(t.pluralize(res.Name()))][accesstypes.Tag(caser.ToCamel(field.Name()))] = true
			}
		}
	}

	templateData := tsConstantsData{
		File:          t,
		Data:          routerData,
		RPCMethods:    t.rpcMethods,
		ManualMethods: t.manualMethods(routerData.Methods),
		PIIMap:        piiResourceFields,
	}

	output, err := t.generateTemplateOutput(typescriptConstantsTemplate, typescriptConstantsTemplate, templateData)
	if err != nil {
		return errors.Wrap(err, "c.generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(t.typescriptDestination, generatedTypescriptFileName("constants"))
	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated Permissions in %s: %s\n", time.Since(begin), file.Name())

	return nil
}

func (t *typescriptGenerator) runTypescriptMetadataGeneration() error {
	if !t.genMetadata {
		return nil
	}

	if err := removeGeneratedFiles(t.typescriptDestination, headerComment); err != nil {
		return errors.Wrap(err, "removeGeneratedFiles()")
	}

	if err := t.generateTypescriptMetadata(); err != nil {
		return errors.Wrap(err, "generateTypescriptResources")
	}

	return nil
}

func (t *typescriptGenerator) generateTypescriptMetadata() error {
	begin := time.Now()
	log.Println("Starting typescript metadata generation...")

	if err := t.generateResourceMetadata(); err != nil {
		return errors.Wrap(err, "generateResourceMetadata()")
	}

	if err := t.generateMethodMetadata(); err != nil {
		return errors.Wrap(err, "generateMethodMetadata()")
	}

	if err := t.generateAPIClient(); err != nil {
		return errors.Wrap(err, "generateAPIClient()")
	}

	log.Printf("Generated typescript metadata in %s\n", time.Since(begin))

	return nil
}

func (t *typescriptGenerator) generateResourceMetadata() error {
	begin := time.Now()
	log.Println("Starting resource metadata generation...")
	hasDomainScoped := false
	hasConsolidated := false
	for _, res := range t.resources {
		if res.IsDomainScoped() {
			hasDomainScoped = true
		}
		if res.IsConsolidated {
			hasConsolidated = true
		}
	}
	for _, res := range t.computedResources {
		if res.IsDomainScoped() {
			hasDomainScoped = true
		}
	}

	output, err := t.generateTemplateOutput(typescriptResourcesTemplate, typescriptResourcesTemplate, tsResourcesData{
		File:                t,
		Resources:           t.resources,
		ComputedResources:   t.computedResources,
		ConsolidatedRoute:   t.ConsolidatedRoute,
		GenPrefix:           genPrefix,
		DomainRoutePrefix:   fmt.Sprintf("%s/{%s}", t.domainRouteSegment, t.domainRouteParam),
		DomainRoutePrefixTS: t.domainRouteSegment + "/${string}",
		DomainRouteParam:    t.domainRouteParam,
		HasDomainScoped:     hasDomainScoped,
		HasConsolidated:     hasConsolidated,
		Workflows:           t.assembleWorkflows(),
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(t.typescriptDestination, generatedTypescriptFileName("resources"))
	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated resource metadata in %s: %s\n", time.Since(begin), file.Name())

	return nil
}

func (t *typescriptGenerator) generateMethodMetadata() error {
	begin := time.Now()
	log.Println("Starting method metadata generation...")

	output, err := t.generateTemplateOutput(typescriptMethodsTemplate, typescriptMethodsTemplate, tsMethodsData{
		File:       t,
		RPCMethods: t.rpcMethods,
		GenPrefix:  genPrefix,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(t.typescriptDestination, generatedTypescriptFileName("methods"))
	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated methods metadata in %s: %s\n", time.Since(begin), file.Name())

	return nil
}

func (t *typescriptGenerator) generateEnums(namedTypes []*parser.NamedType) error {
	begin := time.Now()
	log.Println("Starting enum generation...")

	enumMap, enumTables, err := t.retrieveDatabaseEnumValues(namedTypes)
	if err != nil {
		return err
	}

	// An @enumerate type whose table belongs exclusively to other outlets is not
	// this client's to enumerate; a table the generator cannot attribute to an
	// outlet (schema-only, no parsed resource) always stays.
	for typeName, tableName := range enumTables {
		if _, gone := t.outletExcludedTables[tableName]; gone {
			delete(enumMap, typeName)
		}
	}

	output, err := t.generateTemplateOutput("typescriptEnumsTemplate", typescriptEnumsTemplate, tsEnumsData{
		Source:     t.resource.Dir(),
		NamedTypes: namedTypes,
		EnumMap:    enumMap,
	})
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	file, err := os.Create(filepath.Join(t.typescriptDestination, generatedTypescriptFileName("enums")))
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated enums in %s: %s\n", time.Since(begin), file.Name())

	return nil
}

// resolveTypescriptTypes types every field the target renders before anything does:
// computed and RPC fields from their walked wire types, table and view fields on the
// column path, keeping the resources the collection registers. Every field that
// resolves to no type is reported in one run.
func (t *typescriptGenerator) resolveTypescriptTypes(resources []*resourceInfo, computedResources []*computedResource) error {
	var errs []error
	for _, res := range computedResources {
		if err := computedFieldsTypescriptType(res); err != nil {
			errs = append(errs, err)
		}
	}
	t.computedResources = computedResources

	t.resources = make([]*resourceInfo, 0, len(resources))
	for _, res := range resources {
		if t.rc.ResourceExists(accesstypes.Resource(t.pluralize(res.Name()))) {
			if err := t.resourceFieldsTypescriptType(res); err != nil {
				errs = append(errs, err)
			}
			t.resources = append(t.resources, res)
		}
	}

	for _, rpcMethod := range t.rpcMethods {
		if err := rpcFieldsTypescriptType(rpcMethod); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Wrapf(errors.Join(errs...), "encountered %d TypeScript type errors", len(errs))
	}

	return nil
}

// resourceFieldsTypescriptType resolves a table or view resource's field types on the
// column path: the leaf a field's Go type reaches (the built-in table, a @typescript
// declaration, a NullEnum's argument, a named basic type), or the interface derived
// from the struct it holds, rendered in the resource's namespace. A field that reaches
// neither fails generation naming the field, the type, and the fix; every such field
// in the resource is reported together. A key's picker resolves afterwards, on the
// resolved type, as before.
func (t *typescriptGenerator) resourceFieldsTypescriptType(res *resourceInfo) error {
	plural := t.pluralize(res.Name())
	// One walker per resource: a struct two of its columns hold is one interface.
	walker := newColumnWalker(t.leaves())
	var errs []error
	for _, field := range res.Fields {
		path := plural + "." + field.Name()
		class, err := t.leaves().classifyColumn(field.GoType())
		if err != nil {
			errs = append(errs, columnTypeRefusal(path, err))

			continue
		}
		switch {
		case class.Derive != nil:
			shape, err := walker.walkColumn(class.Derive, path)
			if err != nil {
				errs = append(errs, err)

				continue
			}
			field.setColumnType(plural+"."+shape.TypescriptName(), objectDisplayType, nil, class.Slice)
		default:
			field.setColumnType(class.Leaf.TS, class.Leaf.DisplayType(), class.Leaf.Import, class.Slice)
		}

		t.resolveKeyEnumeration(field)
	}
	res.ColumnShapes = walker.Shapes()

	if len(errs) > 0 {
		return errors.Wrapf(errors.Join(errs...), "resource %s", plural)
	}

	return nil
}

// columnTypeRefusal is the message for a column type that reaches no TypeScript type:
// the field's path, the classifier's finding, and the fix. A refusal that names its
// own fix (a database/sql Null wrapper, which no application can annotate) is reported
// as it stands.
func columnTypeRefusal(path string, cause error) error {
	var own *ownFixRefusal
	if errors.As(cause, &own) {
		return errors.Newf("%s: %s", path, own.Error())
	}

	return errors.Newf("%s: %s; declare the type's TypeScript form with @%s(Name, from: %q) on its declaration, or use a struct for a derived interface", path, errors.Cause(cause).Error(), typescriptKeyword, "module")
}

// resolveKeyEnumeration marks a key's picker source on a resolved field.
func (t *typescriptGenerator) resolveKeyEnumeration(field *resourceField) {
	// A declared enumeration names the picker's source itself and is resolved
	// already (resolveFieldEnumerations); the schema's foreign key adds nothing.
	if field.HasDeclaredEnumeration() {
		return
	}
	if !field.IsForeignKey {
		return
	}
	// A key into an @enumerate table renders from the generated values: the set is
	// fixed at generation time, so the picker needs neither a request nor a List
	// grant, even when a struct also exposes the table as a read-only resource.
	if typeName, ok := t.enumerationOf(field.ReferencedResource); ok {
		if _, gone := t.outletExcludedTables[field.ReferencedResource]; !gone {
			field.IsEnumerated = true
			field.Enumeration = typeName
			field.EnumerationValues = t.enumValues[field.ReferencedResource]

			return
		}
	}
	if slices.Contains(t.routerResources, accesstypes.Resource(field.ReferencedResource)) {
		field.IsEnumerated = true
	}
}

// computedFieldsTypescriptType types a computed resource's fields from their walked
// wire types. Every field carries one: the walk ran at extraction and refused what it
// could not type, so a field without one is a construction error.
func computedFieldsTypescriptType(res *computedResource) error {
	for _, field := range res.Fields {
		if field.wire == nil {
			return errors.Newf("computed resource %s field %s: no wire type; the field was not walked", res.Name(), field.Name())
		}
		field.typescriptType = field.wire.TypescriptDisplayType()
	}

	return nil
}

// rpcFieldsTypescriptType types an RPC method's request fields from their walked wire
// types; see computedFieldsTypescriptType.
func rpcFieldsTypescriptType(method *rpcMethodInfo) error {
	for _, field := range method.Fields {
		if field.wire == nil {
			return errors.Newf("RPC method %s field %s: no wire type; the field was not walked", method.Name(), field.Name())
		}
		field.typescriptType = field.wire.TypescriptDisplayType()
	}

	return nil
}

// manualMethods returns the Execute registrations the collection carries without a
// parsed RPC struct behind them — @manualAddResource(Execute) declarations on
// hand-written handlers — so the Methods constants name every Execute-gated
// resource, not only the generated ones.
func (t *typescriptGenerator) manualMethods(methods []accesstypes.Resource) []accesstypes.Resource {
	manual := make([]accesstypes.Resource, 0, len(methods))
	for _, method := range methods {
		if slices.ContainsFunc(t.rpcMethods, func(m *rpcMethodInfo) bool { return m.Name() == string(method) }) {
			continue
		}
		manual = append(manual, method)
	}

	return manual
}

// generateAPIClient emits zz_gen_api.ts: the typed client surface over the generated
// API for the @cccteam/resource runtime — the descriptor (routes, scopes, keys,
// operations), per-resource create/patch shapes and key tuples, and the Api type
// that places global handles on the client root and domain-scoped handles under
// domain(...).
func (t *typescriptGenerator) generateAPIClient() error {
	begin := time.Now()
	log.Println("Starting API client generation...")

	output, err := t.generateTemplateOutput("typescriptAPITemplate", typescriptAPITemplate, t.apiClientData())
	if err != nil {
		return errors.Wrap(err, "generateTemplateOutput()")
	}

	destinationFilePath := filepath.Join(t.typescriptDestination, generatedTypescriptFileName("api"))
	file, err := os.Create(destinationFilePath)
	if err != nil {
		return errors.Wrap(err, "os.Create()")
	}
	defer file.Close()

	if err := t.WriteBytesToFile(file, output); err != nil {
		return err
	}

	log.Printf("Generated API client in %s: %s\n", time.Since(begin), file.Name())

	return nil
}

// apiClientData assembles the client template's payload from the parsed resources,
// computed resources, and RPC methods on the target outlet.
func (t *typescriptGenerator) apiClientData() *tsAPIData {
	data := &tsAPIData{
		File:               t,
		GenPrefix:          genPrefix,
		DomainRouteSegment: t.domainRouteSegment,
		DomainRouteParam:   t.domainRouteParam,
	}

	outlet := t.targetOutlet()
	for _, res := range t.resources {
		if !res.OnOutlet(outlet) || res.RoutingDisabled() {
			continue
		}
		data.Resources = append(data.Resources, t.apiResource(res))
	}
	for _, res := range t.computedResources {
		if !res.OnOutlet(outlet) || res.RoutingDisabled() {
			continue
		}
		data.Resources = append(data.Resources, t.apiComputedResource(res))
	}
	for _, method := range t.rpcMethods {
		if !method.OnOutlet(outlet) || method.SuppressHandler {
			continue
		}
		apiMethod := &tsAPIMethod{
			Name:     method.Name(),
			Property: strcase.ToCamel(method.Name()),
			Route:    strcase.ToKebab(method.Name()),
			Scope:    method.PermissionScope,
			Answers:  method.Answers(),
			Statuses: method.Statuses,
		}
		if method.Upload != nil {
			apiMethod.UploadMaxBytes = method.Upload.MaxBytes
		}
		data.Methods = append(data.Methods, apiMethod)
	}

	for _, res := range data.Resources {
		if res.Consolidated {
			data.ConsolidatedRoute = t.ConsolidatedRoute
		}
		data.noteScope(res.Scope)
	}
	for _, method := range data.Methods {
		data.noteScope(method.Scope)
	}

	return data
}

func (d *tsAPIData) noteScope(scope accesstypes.PermissionScope) {
	if scope == accesstypes.DomainPermissionScope {
		d.HasDomainScoped = true
		d.HasDomain = true
	} else {
		d.HasGlobal = true
	}
}

func (t *typescriptGenerator) apiResource(res *resourceInfo) *tsAPIResource {
	plural := t.pluralize(res.Name())
	out := &tsAPIResource{
		Name:         plural,
		Property:     strcase.ToCamel(plural),
		Route:        strcase.ToKebab(plural),
		Scope:        res.PermissionScope,
		Consolidated: res.IsConsolidated,
		PageDefault:  pageDefault(res.PageDefault),
		PageMax:      res.PageMax,
		Order:        apiOrder(res.DeclaredOrder),
	}

	for _, field := range res.PrimaryKeys() {
		out.Keys = append(out.Keys, newTSAPIField(field, false))
	}

	if !res.ListHandlerDisabled() {
		out.Operations = append(out.Operations, "list")
	}
	if !res.ReadHandlerDisabled() {
		out.Operations = append(out.Operations, "read")
	}
	// A virtual resource is a read-only view: the router registers no PATCH for it.
	if res.IsVirtual {
		return out
	}

	if !res.CreateHandlerDisabled() {
		out.Operations = append(out.Operations, "create")
		out.HasCreate = true
		for _, field := range res.Fields {
			switch {
			case field.IsPrimaryKey:
				// A server-generated key is never supplied; any other key is.
				if !res.PrimaryKeyIsGeneratedUUID() {
					out.CreateFields = append(out.CreateFields, newTSAPIField(field, true))
				}
			case field.IsOutputOnly():
				// Server-owned: the wire cannot write it.
			default:
				out.CreateFields = append(out.CreateFields, newTSAPIField(field, field.IsRequired()))
			}
		}
	}
	if !res.UpdateHandlerDisabled() {
		out.Operations = append(out.Operations, "patch")
		out.HasPatch = true
		for _, field := range res.Fields {
			if field.IsPrimaryKey || field.IsOutputOnly() || field.IsImmutable() {
				continue
			}
			out.PatchFields = append(out.PatchFields, newTSAPIField(field, false))
		}
	}
	if !res.DeleteHandlerDisabled() {
		out.Operations = append(out.Operations, "remove")
	}
	if res.IsConsolidated && (out.HasCreate || out.HasPatch || !res.DeleteHandlerDisabled()) {
		out.Operations = append(out.Operations, "batch")
	}

	return out
}

// newTSAPIField renders a resource field for the client file.
func newTSAPIField(field *resourceField, required bool) *tsAPIField {
	return &tsAPIField{Name: strcase.ToCamel(field.Name()), Type: field.TypescriptDataType(), Required: required, Import: field.tsImport}
}

// apiOrder renders a declared @order for the descriptor: the JSON name of each field
// and its direction, in the declared sequence; nil when nothing is declared.
func apiOrder(declared []resource.SortField) []*tsAPISort {
	if len(declared) == 0 {
		return nil
	}

	order := make([]*tsAPISort, 0, len(declared))
	for _, sf := range declared {
		direction := "asc"
		if sf.Direction == resource.SortDescending {
			direction = "desc"
		}
		order = append(order, &tsAPISort{Field: strcase.ToCamel(sf.Field), Direction: direction})
	}

	return order
}

// pageDefault resolves an undeclared default page to the generator-wide size, so
// the descriptor always states the page a limit-less request receives.
func pageDefault(declared uint64) uint64 {
	if declared == 0 {
		return resource.DefaultPageSize
	}

	return declared
}

func (t *typescriptGenerator) apiComputedResource(res *computedResource) *tsAPIResource {
	plural := t.pluralize(res.Name())
	out := &tsAPIResource{
		Name:        plural,
		Property:    strcase.ToCamel(plural),
		Route:       strcase.ToKebab(plural),
		Scope:       res.PermissionScope,
		PageDefault: pageDefault(res.PageDefault),
		PageMax:     res.PageMax,
		Order:       apiOrder(res.DeclaredOrder),
	}
	for _, field := range res.PrimaryKeys() {
		out.Keys = append(out.Keys, &tsAPIField{Name: strcase.ToCamel(field.Name()), Type: field.TypescriptDataType()})
	}
	if !res.SuppressListHandler {
		out.Operations = append(out.Operations, "list")
	}
	if !res.SuppressReadHandler {
		out.Operations = append(out.Operations, "read")
	}

	return out
}
