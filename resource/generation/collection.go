package generation

import (
	"iter"
	"reflect"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
	"github.com/go-playground/errors/v5"
)

// ManualRegistration declares a permission registration the generator cannot derive from
// generated handlers: a hand-written route that checks a permission on a resource with no
// generated handler. Declare them with an @manualAddResource annotation on the resource
// constant, or with WithManualResources. An empty Scope registers under
// accesstypes.GlobalPermissionScope.
type ManualRegistration = resource.ManualRegistration

// accesstypesResourceType is the fully qualified type of the constants
// @manualAddResource annotations apply to.
const accesstypesResourceType = "github.com/cccteam/ccc/accesstypes.Resource"

// manualRegistrationsFromConstants extracts @manualAddResource annotations from the
// resources package's accesstypes.Resource constants. The registered resource name is
// the constant's value; the annotation supplies the permission and, optionally, the
// scope: @manualAddResource(Execute) or @manualAddResource(Read, domain).
func manualRegistrationsFromConstants(constants []*parser.Constant) ([]ManualRegistration, error) {
	var registrations []ManualRegistration
	for _, c := range constants {
		if c.TypeName() != accesstypesResourceType {
			continue
		}

		annotations, err := genlang.NewScanner(resourceKeywords()).ScanConstant(c)
		if err != nil {
			return nil, errors.Wrapf(err, "scanning annotations on constant %q", c.Name())
		}

		if !annotations.Const.Has(manualAddResourceKeyword) {
			continue
		}

		for arg := range annotations.Const.Get(manualAddResourceKeyword).Seq() {
			registration, err := parseManualAddResourceArgs(c, arg)
			if err != nil {
				return nil, err
			}

			registrations = append(registrations, registration)
		}
	}

	return registrations, nil
}

// parseManualAddResourceArgs parses one @manualAddResource argument list:
// "<permission>[, <scope>]".
func parseManualAddResourceArgs(c *parser.Constant, arg string) (ManualRegistration, error) {
	parts := strings.Split(arg, ",")
	if len(parts) > 2 {
		return ManualRegistration{}, errors.Newf("constant %q: @%s takes at most two arguments (permission and scope), got %q", c.Name(), manualAddResourceKeyword, arg)
	}

	permission := accesstypes.Permission(strings.TrimSpace(parts[0]))
	if permission == accesstypes.NullPermission {
		return ManualRegistration{}, errors.Newf("constant %q: @%s requires a permission argument", c.Name(), manualAddResourceKeyword)
	}

	// An absent scope stays empty; the global default applies at registration.
	var scope accesstypes.PermissionScope
	if len(parts) == 2 {
		parsedScope, err := parsePermissionScopeAnnotation(genlang.Arg(parts[1]))
		if err != nil {
			return ManualRegistration{}, errors.Wrapf(err, "constant %q: @%s", c.Name(), manualAddResourceKeyword)
		}
		scope = parsedScope
	}

	return ManualRegistration{
		Scope:      scope,
		Permission: permission,
		Resource:   accesstypes.Resource(c.Value()),
	}, nil
}

// applyManualAddResourceSetDirectives resolves @manualAddResourceSet arguments into
// res.ManualAddResourceSets. Each argument names the handler type whose request-struct
// shape a hand-written handler registers (listHandler, readHandler, patchHandler, or
// allHandlers); both comma lists and repeated annotations are accepted. Validation
// against the generated handlers happens later, in validateManualAddResourceSets.
func applyManualAddResourceSetDirectives(res *resourceInfo, args iter.Seq[string]) error {
	for arg := range args {
		for part := range strings.SplitSeq(arg, ",") {
			token := strings.TrimSpace(part)

			var declared []HandlerType
			switch handlerType := HandlerType(token); handlerType {
			case AllHandlers:
				declared = []HandlerType{ListHandler, ReadHandler, PatchHandler}
			case ListHandler, ReadHandler, PatchHandler:
				declared = []HandlerType{handlerType}
			default:
				return errors.Newf("unexpected argument %[1]q in @%[2]s(%[1]s), must be one of %[3]v", token, manualAddResourceSetKeyword, validManualAddResourceSetArgs())
			}

			for _, handlerType := range declared {
				if slices.Contains(res.ManualAddResourceSets, handlerType) {
					return errors.Newf("@%s declares %s twice", manualAddResourceSetKeyword, handlerType)
				}
				res.ManualAddResourceSets = append(res.ManualAddResourceSets, handlerType)
			}
		}
	}

	return nil
}

// validateManualAddResourceSets rejects @manualAddResourceSet declarations that
// duplicate a registration the generated route wiring already performs: a declared
// handler type must not also be generated (that would register the Set twice), and
// consolidated resources cannot declare patchHandler because their patch registration
// comes from the shared consolidated handler. Without GenerateRoutes no generated
// registration exists, so every declaration is legitimate.
func (r *resourceGenerator) validateManualAddResourceSets() error {
	if !r.genRoutes {
		return nil
	}

	var errs []error
	for _, res := range r.resources {
		if slices.Contains(res.ManualAddResourceSets, PatchHandler) && res.IsConsolidated {
			errs = append(errs, errors.Newf("@%s(%s) on %s is not supported on a consolidated resource: its patch registration comes from the shared consolidated handler; exclude the resource from the consolidated handlers first", manualAddResourceSetKeyword, PatchHandler, res.Name()))
		}

		if res.RoutingDisabled() {
			continue
		}

		for _, handlerType := range res.ManualAddResourceSets {
			if slices.Contains(resourceEndpoints(res), handlerType) {
				errs = append(errs, errors.Newf("@%[1]s(%[2]s) on %[3]s conflicts with the generated %[2]s, which already registers this resource: add @%[4]s(%[2]s) or remove the declaration", manualAddResourceSetKeyword, handlerType, res.Name(), suppressKeyword))
			}
		}
	}

	if len(errs) > 0 {
		return errors.Wrapf(errors.Join(errs...), "encountered %d errors validating @%s declarations", len(errs), manualAddResourceSetKeyword)
	}

	return nil
}

// validManualAddResourceSetArgs returns the handler-type arguments accepted by
// @manualAddResourceSet.
func validManualAddResourceSetArgs() []string {
	return []string{string(AllHandlers), string(ListHandler), string(ReadHandler), string(PatchHandler)}
}

// scopeOrGlobal returns the declared per-resource scope, defaulting to the global scope
// when no @permissionScope annotation is present.
func scopeOrGlobal(scope accesstypes.PermissionScope) accesstypes.PermissionScope {
	if scope == "" {
		return accesstypes.GlobalPermissionScope
	}

	return scope
}

// computeCollectionData derives the permission collection statically from the same
// parsed state the generator renders handlers from. Without GenerateRoutes no generated
// wiring performs registrations, so only the manual declarations (@manualAddResource and
// @manualAddResourceSet) are collected.
func (r *resourceGenerator) computeCollectionData() (resource.CollectionData, error) {
	b := resource.NewCollectionBuilder()

	consolidatedRouteWired, err := r.collectResourceRegistrations(b)
	if err != nil {
		return resource.CollectionData{}, err
	}

	if consolidatedRouteWired {
		if err := r.collectConsolidatedRegistrations(b); err != nil {
			return resource.CollectionData{}, err
		}
	}

	if err := r.collectComputedRegistrations(b); err != nil {
		return resource.CollectionData{}, err
	}

	if err := r.collectRPCRegistrations(b); err != nil {
		return resource.CollectionData{}, err
	}

	if err := r.collectManualRegistrations(b); err != nil {
		return resource.CollectionData{}, err
	}

	return b.Data(), nil
}

// collectResourceRegistrations registers every routed resource's endpoints, plus the
// Sets declared via @manualAddResourceSet (whose handlers are hand-written), and
// reports whether the shared consolidated patch route is wired: it is when route
// generation is enabled and at least one consolidated resource has routing enabled and
// an unsuppressed patch handler. Endpoints only register when this run generates
// routes, because they model what the generated route wiring registers.
func (r *resourceGenerator) collectResourceRegistrations(b *resource.CollectionBuilder) (consolidatedRouteWired bool, err error) {
	for _, res := range r.resources {
		var endpoints []HandlerType
		if r.genRoutes && !res.RoutingDisabled() {
			endpoints = resourceEndpoints(res)

			if hasConsolidatedHandler(res) {
				consolidatedRouteWired = true
			}
		}

		// Generated and manually declared Sets merge in canonical handler order so the
		// patch registration lands last and its immutable fields win, as at runtime.
		for _, handlerType := range []HandlerType{ListHandler, ReadHandler, PatchHandler} {
			generated := slices.Contains(endpoints, handlerType)
			if !generated && !slices.Contains(res.ManualAddResourceSets, handlerType) {
				continue
			}

			set, err := handlerSetData(res, handlerType)
			if err != nil {
				return false, errors.Wrapf(err, "resource %q %s request struct", res.Name(), handlerType)
			}

			if err := b.AddResourceSet(scopeOrGlobal(res.PermissionScope), accesstypes.Resource(r.pluralize(res.Name())), set); err != nil {
				return false, errors.Wrapf(err, "registering resource %q %s handler", res.Name(), handlerType)
			}
		}
	}

	return consolidatedRouteWired, nil
}

// collectConsolidatedRegistrations registers the patch permissions of ALL consolidated
// resources, without filtering on suppression or routing, mirroring the generated
// PatchResources handler, which builds a decoder per consolidated resource
// unconditionally.
func (r *resourceGenerator) collectConsolidatedRegistrations(b *resource.CollectionBuilder) error {
	for _, res := range r.resources {
		if !res.IsConsolidated {
			continue
		}

		set, err := handlerSetData(res, PatchHandler)
		if err != nil {
			return errors.Wrapf(err, "resource %q consolidated patch request struct", res.Name())
		}

		if err := b.AddResourceSet(scopeOrGlobal(res.PermissionScope), accesstypes.Resource(r.pluralize(res.Name())), set); err != nil {
			return errors.Wrapf(err, "registering resource %q consolidated patch handler", res.Name())
		}
	}

	return nil
}

func (r *resourceGenerator) collectComputedRegistrations(b *resource.CollectionBuilder) error {
	// Computed handlers only register when the generated routes invoke them.
	if !r.genComputedResources || !r.genRoutes {
		return nil
	}

	for _, res := range r.computedResources {
		if res.RoutingDisabled() {
			continue
		}

		fields := computedFieldTags(res)
		handlers := []struct {
			suppressed bool
			permission accesstypes.Permission
		}{
			{suppressed: res.SuppressListHandler, permission: accesstypes.List},
			{suppressed: res.SuppressReadHandler, permission: accesstypes.Read},
		}
		for _, handler := range handlers {
			if handler.suppressed {
				continue
			}

			set, err := resource.NewSetData(fields, handler.permission)
			if err != nil {
				return errors.Wrapf(err, "computed resource %q %s request struct", res.Name(), handler.permission)
			}
			if err := b.AddResourceSet(scopeOrGlobal(res.PermissionScope), accesstypes.Resource(r.pluralize(res.Name())), set); err != nil {
				return errors.Wrapf(err, "registering computed resource %q %s handler", res.Name(), handler.permission)
			}
		}
	}

	return nil
}

func (r *resourceGenerator) collectRPCRegistrations(b *resource.CollectionBuilder) error {
	// RPC handlers only register when the generated routes invoke them.
	if !r.genRPCMethods || !r.genRoutes {
		return nil
	}

	for _, method := range r.rpcMethods {
		if method.SuppressHandler {
			continue
		}

		if err := b.AddMethodResource(scopeOrGlobal(method.PermissionScope), accesstypes.Execute, accesstypes.Resource(method.Name())); err != nil {
			return errors.Wrapf(err, "registering RPC method %q", method.Name())
		}
	}

	return nil
}

func (r *resourceGenerator) collectManualRegistrations(b *resource.CollectionBuilder) error {
	for _, reg := range r.manualRegistrations {
		scope := reg.Scope
		if scope == "" {
			scope = accesstypes.GlobalPermissionScope
		}

		if err := b.AddResource(scope, reg.Permission, reg.Resource); err != nil {
			return errors.Wrapf(err, "manual registration for resource %q", reg.Resource)
		}
	}

	return nil
}

// handlerSetData computes the SetData one generated handler registers for res, by
// rendering the handler's request-struct tags through the same helpers the handler
// template calls and parsing them with the same reflect.StructTag semantics the runtime
// applies.
func handlerSetData(res *resourceInfo, handlerType HandlerType) (resource.SetData, error) {
	var fields []resource.FieldTags
	var permissions []accesstypes.Permission

	switch handlerType {
	case ListHandler:
		permissions = []accesstypes.Permission{accesstypes.List}
		for _, field := range res.Fields {
			fields = append(fields, fieldTagsFromTemplateTags(field.Name(),
				field.JSONTag(), field.IndexTag(), field.AllowFilterTag(), field.ListPermTag(), field.PIITag()))
		}
	case ReadHandler:
		permissions = []accesstypes.Permission{accesstypes.Read}
		for _, field := range res.Fields {
			fields = append(fields, fieldTagsFromTemplateTags(field.Name(),
				field.JSONTag(), field.UniqueIndexTag(), field.ReadPermTag(), field.PIITag()))
		}
	case PatchHandler:
		permissions = []accesstypes.Permission{accesstypes.Create, accesstypes.Update, accesstypes.Delete}
		for _, field := range res.Fields {
			fields = append(fields, fieldTagsFromTemplateTags(field.Name(),
				field.JSONTagForPatch(), field.ImmutableTag(), field.PatchPermTag()))
		}
	case AllHandlers:
		return resource.SetData{}, errors.Newf("handlerSetData(): unsupported handler type: %s", handlerType)
	default:
		return resource.SetData{}, errors.Newf("handlerSetData(): unknown handler type: %s", handlerType)
	}

	set, err := resource.NewSetData(fields, permissions...)
	if err != nil {
		return resource.SetData{}, errors.Wrap(err, "resource.NewSetData()")
	}

	return set, nil
}

// computedFieldTags renders a computed resource's field tags as the computed handler
// template emits them (json and pii only; no perm or immutable tags).
func computedFieldTags(res *computedResource) []resource.FieldTags {
	fields := make([]resource.FieldTags, 0, len(res.Fields))
	for _, field := range res.Fields {
		fields = append(fields, fieldTagsFromTemplateTags(field.Name(), field.JSONTag(), field.PIITag()))
	}

	return fields
}

// fieldTagsFromTemplateTags assembles the template-emitted tag fragments into one struct
// tag and extracts the registration-relevant values through resource.FieldTagsFromStructTag,
// the same parsing the runtime reflection path uses, so the two can never disagree.
func fieldTagsFromTemplateTags(fieldName string, tagFragments ...string) resource.FieldTags {
	structTag := reflect.StructTag(strings.Join(tagFragments, " "))

	return resource.FieldTagsFromStructTag(accesstypes.Field(fieldName), structTag)
}
