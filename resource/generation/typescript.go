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
	typescriptOverrides   map[string]string
	rc                    *resource.GeneratedCollection
	routerResources       []accesstypes.Resource
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
	// outlets — the parsed resources, computed resources, and RPC methods that are
	// not on the target outlet — which the constants output omits. Registrations the
	// generator cannot attribute to an outlet (manual declarations) always stay.
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

// validateOutletMemberReferences rejects a member RPC method that references a
// resource excluded from the target outlet — a declared transition's root, or an
// enumerated field's resource. The emitted metadata names such a resource through
// the Resources constant, which the filtered constants file no longer declares;
// silently narrowing the metadata in this client alone would misrepresent the
// method, so the mismatch fails generation with the fix instead.
func (t *typescriptGenerator) validateOutletMemberReferences() error {
	excluded := func(name string) bool {
		return slices.Contains(t.outletExcluded, accesstypes.Resource(name))
	}

	for _, method := range t.rpcMethods {
		if tr := method.Transition; tr != nil && excluded(tr.RootResource) {
			return errors.Newf("outlet %q: RPC method %s declares a transition on %s, which is not on the outlet; a client cannot carry a transition whose resource it does not emit — attach %s to the outlet via @%s, or move the method off it", t.targetOutlet(), method.Name(), tr.RootResource, tr.RootResource, outletKeyword)
		}
		for _, field := range method.Fields {
			if field.IsEnumerated() && excluded(field.EnumeratedResource()) {
				return errors.Newf("outlet %q: RPC method %s field %s enumerates %s, which is not on the outlet; attach %s to the outlet via @%s, or drop the enumerated tag", t.targetOutlet(), method.Name(), field.Name(), field.EnumeratedResource(), field.EnumeratedResource(), outletKeyword)
			}
		}
	}

	return nil
}

// applyOutletFilter narrows the parsed sets to the target outlet's members. Every
// member on another outlet falls away here — resources, computed resources, and
// RPC methods — and is recorded so the collection-derived constants drop the same
// registrations; the surviving methods' cross-outlet references are then validated.
// Enumerated-field references resolve against the outlet's members only, so a field
// referencing a resource on another outlet degrades to its plain type instead of
// referencing a Resources constant the filtered output no longer declares.
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

	if err := t.validateOutletMemberReferences(); err != nil {
		return nil, nil, err
	}

	t.routerResources = slices.DeleteFunc(slices.Clone(t.routerResources), func(res accesstypes.Resource) bool {
		return slices.Contains(t.outletExcluded, res)
	})

	return resources, computedResources, nil
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

	resources, err := t.structsToResources(resourcesPkg.Structs, t.validateStructNameMatchesFile(pkg, true), validateNoPermTags)
	if err != nil {
		return nil, nil, err
	}

	if t.genVirtualResources {
		pkg := packageMap[t.virtual.Package()]
		virtualStructs := parser.ParsePackage(pkg).Structs
		virtualResources, err := t.structsToVirtualResources(virtualStructs, t.validateStructNameMatchesFile(pkg, true), validateNoPermTags)
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
		computedResources, err = structsToCompResources(compStructs, t.validateStructNameMatchesFile(pkg, true), validateNoPermTags)
		if err != nil {
			return err
		}
	}

	t.resources = resources
	if t.genRPCMethods {
		pkg := packageMap[t.rpc.Package()]
		rpcStructs := parser.ParsePackage(pkg).Structs
		t.rpcMethods, err = t.structsToRPCMethods(rpcStructs, t.validateStructNameMatchesFile(pkg, false), validateNoPermTags)
		if err != nil {
			return err
		}
	}

	resources, computedResources, err = t.applyOutletFilter(resources, computedResources)
	if err != nil {
		return err
	}

	for _, res := range computedResources {
		res.Fields = t.computedFieldsTypescriptType(res.Fields)
	}
	t.computedResources = computedResources

	t.resources = make([]*resourceInfo, 0, len(resources))
	for _, res := range resources {
		if t.rc.ResourceExists(accesstypes.Resource(t.pluralize(res.Name()))) {
			res.Fields = t.resourceFieldsTypescriptType(res.Fields)
			t.resources = append(t.resources, res)
		}
	}

	for _, rpcMethod := range t.rpcMethods {
		rpcMethod.Fields = t.rpcFieldsTypescriptType(rpcMethod.Fields)
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
		File:       t,
		Data:       routerData,
		RPCMethods: t.rpcMethods,
		PIIMap:     piiResourceFields,
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

func (t *typescriptGenerator) resourceFieldsTypescriptType(fields []*resourceField) []*resourceField {
	for _, field := range fields {
		if override, ok := t.typescriptOverrides[field.TypeName()]; ok {
			field.typescriptType = override
		} else {
			field.typescriptType = stringGoType
		}

		if field.IsIterable() {
			field.typescriptType = fmt.Sprintf("%s[]", field.typescriptType)
		}

		if field.IsForeignKey && slices.Contains(t.routerResources, accesstypes.Resource(field.ReferencedResource)) {
			field.IsEnumerated = true
		}
	}

	return fields
}

func (t *typescriptGenerator) computedFieldsTypescriptType(fields []*computedField) []*computedField {
	for _, field := range fields {
		if override, ok := t.typescriptOverrides[field.TypeName()]; ok {
			field.typescriptType = override
		} else {
			field.typescriptType = stringGoType
		}

		if field.IsIterable() {
			field.typescriptType = fmt.Sprintf("%s[]", field.typescriptType)
		}
	}

	return fields
}

func (t *typescriptGenerator) rpcFieldsTypescriptType(fields []*rpcField) []*rpcField {
	for _, field := range fields {
		if override, ok := t.typescriptOverrides[field.TypeName()]; ok {
			if override == booleanStr && field.Type() == "*bool" {
				panic("Bool pointer (*bool) not currently supported for rpc methods.")
			}
			field.typescriptType = override
		} else {
			field.typescriptType = stringGoType
		}

		if field.IsIterable() {
			field.typescriptType = fmt.Sprintf("%s[]", field.typescriptType)
		}
	}

	return fields
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
		data.Methods = append(data.Methods, &tsAPIMethod{
			Name:     method.Name(),
			Property: strcase.ToCamel(method.Name()),
			Route:    strcase.ToKebab(method.Name()),
			Scope:    method.PermissionScope,
		})
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
	}

	for _, field := range res.PrimaryKeys() {
		out.Keys = append(out.Keys, &tsAPIField{Name: strcase.ToCamel(field.Name()), Type: field.TypescriptDataType()})
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
					out.CreateFields = append(out.CreateFields, &tsAPIField{Name: strcase.ToCamel(field.Name()), Type: field.TypescriptDataType(), Required: true})
				}
			case field.IsOutputOnly():
				// Server-owned: the wire cannot write it.
			default:
				out.CreateFields = append(out.CreateFields, &tsAPIField{Name: strcase.ToCamel(field.Name()), Type: field.TypescriptDataType(), Required: field.IsRequired()})
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
			out.PatchFields = append(out.PatchFields, &tsAPIField{Name: strcase.ToCamel(field.Name()), Type: field.TypescriptDataType()})
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

func (t *typescriptGenerator) apiComputedResource(res *computedResource) *tsAPIResource {
	plural := t.pluralize(res.Name())
	out := &tsAPIResource{
		Name:     plural,
		Property: strcase.ToCamel(plural),
		Route:    strcase.ToKebab(plural),
		Scope:    res.PermissionScope,
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
