package resource

import (
	"reflect"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// FieldTags describes the registration-relevant struct tags of a single request-struct
// field. It is the shared input for permission collection: the runtime path builds it by
// reflecting over generated request structs, and the generator builds it from the same
// tag values it writes into those structs, so both paths flow through identical logic.
type FieldTags struct {
	Field     accesstypes.Field
	JSON      string // json tag name (first comma-separated part); "" or "-" is unregistered
	Perm      string // raw perm tag value; "" (enforced) or "-" (primary-key exemption) are the only legal values
	Immutable bool   // immutable:"true"
}

// FieldTagsFromStructTag extracts the registration-relevant values from a struct tag: the
// runtime path (permissionsFromTags) calls it directly on a reflected field's tag, and the
// generator's static path (fieldTagsFromTemplateTags) assembles an equivalent
// reflect.StructTag from the same fragments the templates render, so both share this exact
// parsing and can never disagree on it.
func FieldTagsFromStructTag(field accesstypes.Field, tag reflect.StructTag) FieldTags {
	jsonTag, _, _ := strings.Cut(tag.Get(jsonTagKey), ",")
	immutableTag, _, _ := strings.Cut(tag.Get(immutableTagKey), ",")

	return FieldTags{
		Field:     field,
		JSON:      jsonTag,
		Perm:      tag.Get(permTagKey),
		Immutable: immutableTag == trueStr,
	}
}

// SetData describes what registering a resource.Set built from a request struct adds to
// a GeneratedCollection: the resource-level permissions, the tag-to-permission mappings
// (including tags registered without permissions), and the immutable tags.
type SetData struct {
	Permissions     []accesstypes.Permission
	TagPermissions  accesstypes.TagPermissions
	ImmutableFields map[accesstypes.Tag]struct{}
}

// NewSetData computes the registration data for a request struct described by fields,
// mirroring NewSet over the equivalent struct, always registering every field (even
// exempt ones) so generated Collection/TypeScript output is complete.
//
// It diverges from the runtime Set in exactly one documented way: Update is stripped
// from immutable tags. The grantable matrix is the enforcement matrix minus
// Update-on-immutable — an immutable field's Update requirement must never be
// satisfiable, so the Collection (and therefore MigrateRoles) never exposes it as
// grantable, while the runtime Set keeps requiring it (defense-in-depth behind the
// decoder's 400-on-update).
func NewSetData(fields []FieldTags, permissions ...accesstypes.Permission) (SetData, error) {
	tagPermissions, _, perms, immutableFields, err := permissionsFromFieldTags(fields, permissions, true)
	if err != nil {
		return SetData{}, errors.Wrap(err, "permissionsFromFieldTags()")
	}

	for tag := range immutableFields {
		tagPermissions[tag] = slices.DeleteFunc(tagPermissions[tag], func(p accesstypes.Permission) bool {
			return p == accesstypes.Update
		})
	}

	return SetData{
		Permissions:     perms,
		TagPermissions:  tagPermissions,
		ImmutableFields: immutableFields,
	}, nil
}

// ManualRegistration declares a permission registration that is not derived from
// generated handlers: a hand-written route that checks a permission on a resource with
// no generated handler. Declare them to the Resource Generator with an
// @manualAddResource annotation or WithManualResources.
type ManualRegistration struct {
	// Scope is empty when the declaration leaves the scope to the default
	// (accesstypes.GlobalPermissionScope).
	Scope      accesstypes.PermissionScope
	Permission accesstypes.Permission
	Resource   accesstypes.Resource
}

// CollectionData is the stable, serializable description of a permission collection. It
// is the schema the generator emits into generated collection files and the input
// NewGeneratedCollection validates, decoupling generated code from the collection's
// internal representation.
type CollectionData struct {
	Resources []CollectionResource
}

// CollectionResource describes one resource's registrations within a permission collection.
// RPC methods and manually registered resources carry only Permissions.
type CollectionResource struct {
	Name  accesstypes.Resource
	Scope accesstypes.PermissionScope

	// Computed marks a computed resource: a hand-written query surface whose
	// permission checks run at decode time, where no row exists. Deploy-time
	// grant validation (access.MigrateRoles) uses it to reject conditions that
	// could never settle there.
	Computed bool

	Permissions   []accesstypes.Permission
	Tags          []TagData
	ImmutableTags []accesstypes.Tag

	// The resource's binding vocabulary (ABAC design plan §04), compiled from
	// the field-level binding annotations: attributes conditions reference,
	// the structural tenancy binding, and the subject-side vocabulary.
	Attributes    []AttributeData
	Domain        *DomainBindingData
	SubjectSets   []SubjectBindingData
	SubjectValues []SubjectBindingData

	// Transition is an RPC method resource's declared state transition
	// (@transition, design plan §09); nil for everything else.
	Transition *TransitionData

	// Target is the row resource an RPC method's @target field addresses —
	// set for every @target method, transition or plain (a transition's
	// Target repeats its TransitionData.Target); empty for everything else.
	// A targeted method's Execute grants may carry row-referencing
	// conditions: the generated handler locates the row in its transaction
	// and evaluates them there (design plan §12).
	Target accesstypes.Resource

	// Parent is a workflow member's immediate parent: the resource its
	// @stateRoot foreign key references (the root, or another member on the
	// chain); empty for everything else. The create-under-parent affordance
	// (design plan §11) rides it: capabilities=Create on the parent's read
	// answers, per row, which member resources the user may create beneath it.
	Parent accesstypes.Resource
}

// TransitionData records a declared state transition on an RPC method
// resource: the workflow root it moves, the pre-image states it may run from,
// and the state the generated handler stamps after the body.
type TransitionData struct {
	Target accesstypes.Resource
	From   []string
	To     string
}

// TagData describes one field-level tag registration. An empty Permissions slice records
// a tag that is registered without requiring any permission.
type TagData struct {
	Name        accesstypes.Tag
	Permissions []accesstypes.Permission
}

// CollectionBuilder assembles CollectionData by replaying the registration semantics a
// permission collection enforces: duplicate detection, null-permission filtering, and
// immutable-field replacement (the last registration for a resource wins).
type CollectionBuilder struct {
	g *GeneratedCollection
}

// NewCollectionBuilder creates an empty CollectionBuilder.
func NewCollectionBuilder() *CollectionBuilder {
	return &CollectionBuilder{g: newGeneratedCollection()}
}

// AddResourceSet registers a request struct's SetData under scope.
func (b *CollectionBuilder) AddResourceSet(scope accesstypes.PermissionScope, res accesstypes.Resource, set SetData) error {
	return b.g.addResourceSet(scope, res, set.Permissions, set.TagPermissions, set.ImmutableFields)
}

// AddResource registers a single resource permission, allowing duplicate registrations
// (the hand-written registration path).
func (b *CollectionBuilder) AddResource(scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) error {
	if permission == accesstypes.NullPermission {
		return errors.New("cannot register null permission")
	}

	return b.g.addResource(true, scope, permission, res)
}

// AddMethodResource registers a method resource permission, rejecting duplicate
// registrations (generated RPC handlers).
func (b *CollectionBuilder) AddMethodResource(scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) error {
	if permission == accesstypes.NullPermission {
		return errors.New("cannot register null permission")
	}

	return b.g.addResource(false, scope, permission, res)
}

// SetResourceComputed marks res as a computed resource within scope: a hand-written
// query surface whose permission checks run at decode time, where no row exists.
func (b *CollectionBuilder) SetResourceComputed(scope accesstypes.PermissionScope, res accesstypes.Resource) {
	b.g.setResourceComputed(scope, res)
}

// SetMethodTransition records an RPC method resource's declared state transition
// (@transition) within scope.
func (b *CollectionBuilder) SetMethodTransition(scope accesstypes.PermissionScope, method accesstypes.Resource, transition TransitionData) {
	b.g.setMethodTransition(scope, method, transition)
}

// SetMethodTarget records the row resource an RPC method's @target field
// addresses within scope.
func (b *CollectionBuilder) SetMethodTarget(scope accesstypes.PermissionScope, method, target accesstypes.Resource) {
	b.g.setMethodTarget(scope, method, target)
}

// SetResourceParent records a workflow member's immediate parent within scope:
// the resource its @stateRoot foreign key references.
func (b *CollectionBuilder) SetResourceParent(scope accesstypes.PermissionScope, member, parent accesstypes.Resource) {
	b.g.setResourceParent(scope, member, parent)
}

// Data returns the canonical, deterministically sorted form of everything registered so
// far.
func (b *CollectionBuilder) Data() CollectionData {
	return collectionDataFrom(b.g)
}

// GeneratedCollection returns the built collection directly, for consumers that do not
// need the serializable form. The builder must not be used again afterward: the returned
// collection shares its storage.
func (b *CollectionBuilder) GeneratedCollection() *GeneratedCollection {
	return b.g
}

type (
	tagStore          map[accesstypes.Resource]map[accesstypes.Tag][]accesstypes.Permission
	resourceStore     map[accesstypes.Resource][]accesstypes.Permission
	immutableFieldMap map[accesstypes.Resource]map[accesstypes.Tag]struct{}
)

// GeneratedCollection is a read-only permission collection constructed from generated
// CollectionData. It is immutable after construction: nothing in its API mutates it once
// built, so concurrent reads require no synchronization.
type GeneratedCollection struct {
	tagStore        map[accesstypes.PermissionScope]tagStore
	resourceStore   map[accesstypes.PermissionScope]resourceStore
	immutableFields map[accesstypes.PermissionScope]immutableFieldMap
	bindings        map[accesstypes.PermissionScope]map[accesstypes.Resource]Bindings
	computed        map[accesstypes.PermissionScope]map[accesstypes.Resource]struct{}
	transitions     map[accesstypes.PermissionScope]map[accesstypes.Resource]TransitionData
	targets         map[accesstypes.PermissionScope]map[accesstypes.Resource]accesstypes.Resource
	parents         map[accesstypes.PermissionScope]map[accesstypes.Resource]accesstypes.Resource
}

// newGeneratedCollection creates an empty, populatable GeneratedCollection.
func newGeneratedCollection() *GeneratedCollection {
	return &GeneratedCollection{
		tagStore:        make(map[accesstypes.PermissionScope]tagStore, 2),
		resourceStore:   make(map[accesstypes.PermissionScope]resourceStore, 2),
		immutableFields: make(map[accesstypes.PermissionScope]immutableFieldMap, 2),
		bindings:        make(map[accesstypes.PermissionScope]map[accesstypes.Resource]Bindings, 2),
		computed:        make(map[accesstypes.PermissionScope]map[accesstypes.Resource]struct{}, 2),
		transitions:     make(map[accesstypes.PermissionScope]map[accesstypes.Resource]TransitionData, 2),
		targets:         make(map[accesstypes.PermissionScope]map[accesstypes.Resource]accesstypes.Resource, 2),
		parents:         make(map[accesstypes.PermissionScope]map[accesstypes.Resource]accesstypes.Resource, 2),
	}
}

// NewGeneratedCollection validates data and constructs the collection. It rejects
// duplicate resources, duplicate or null permissions, and duplicate tags, so invalid
// generated data fails at startup rather than surfacing as wrong permission decisions.
func NewGeneratedCollection(data CollectionData) (*GeneratedCollection, error) {
	g := newGeneratedCollection()

	if err := validateCollectionBindings(data.Resources); err != nil {
		return nil, err
	}

	type resourceKey struct {
		scope accesstypes.PermissionScope
		name  accesstypes.Resource
	}
	seen := make(map[resourceKey]struct{}, len(data.Resources))

	for i := range data.Resources {
		res := &data.Resources[i]
		if res.Name == "" {
			return nil, errors.New("resource with empty name")
		}
		if res.Scope == "" {
			return nil, errors.Newf("resource %q has an empty permission scope", res.Name)
		}
		key := resourceKey{scope: res.Scope, name: res.Name}
		if _, ok := seen[key]; ok {
			return nil, errors.Newf("duplicate resource %q in scope %q", res.Name, res.Scope)
		}
		seen[key] = struct{}{}

		for _, perm := range res.Permissions {
			if perm == accesstypes.NullPermission {
				return nil, errors.Newf("resource %q registers a null permission", res.Name)
			}
			if err := g.addResource(false, res.Scope, perm, res.Name); err != nil {
				return nil, err
			}
		}

		if err := g.addResourceTags(res); err != nil {
			return nil, err
		}

		if len(res.ImmutableTags) > 0 {
			if _, ok := g.immutableFields[res.Scope]; !ok {
				g.immutableFields[res.Scope] = make(immutableFieldMap)
			}
			immutable := make(map[accesstypes.Tag]struct{}, len(res.ImmutableTags))
			for _, tag := range res.ImmutableTags {
				immutable[tag] = struct{}{}
			}
			g.immutableFields[res.Scope][res.Name] = immutable
		}

		g.setResourceBindings(res.Scope, res.Name, &Bindings{
			Attributes:    res.Attributes,
			Domain:        res.Domain,
			SubjectSets:   res.SubjectSets,
			SubjectValues: res.SubjectValues,
		})

		if res.Computed {
			g.setResourceComputed(res.Scope, res.Name)
		}

		if res.Transition != nil {
			if res.Transition.Target == "" || len(res.Transition.From) == 0 || res.Transition.To == "" {
				return nil, errors.Newf("method resource %q declares an incomplete transition", res.Name)
			}
			if res.Target != "" && res.Target != res.Transition.Target {
				return nil, errors.Newf("method resource %q declares target %q but its transition targets %q", res.Name, res.Target, res.Transition.Target)
			}
			g.setMethodTransition(res.Scope, res.Name, *res.Transition)
		}

		if res.Target != "" {
			g.setMethodTarget(res.Scope, res.Name, res.Target)
		}

		if res.Parent != "" {
			g.setResourceParent(res.Scope, res.Name, res.Parent)
		}
	}

	return g, nil
}

// addResourceTags validates and stores one CollectionResource's tag registrations:
// duplicate tags, duplicate tag permissions, and null tag permissions are invalid.
func (g *GeneratedCollection) addResourceTags(res *CollectionResource) error {
	if len(res.Tags) == 0 {
		return nil
	}

	if g.tagStore[res.Scope] == nil {
		g.tagStore[res.Scope] = make(tagStore)
	}
	g.tagStore[res.Scope][res.Name] = make(map[accesstypes.Tag][]accesstypes.Permission, len(res.Tags))

	for _, tag := range res.Tags {
		if _, ok := g.tagStore[res.Scope][res.Name][tag.Name]; ok {
			return errors.Newf("duplicate tag %q under resource %q", tag.Name, res.Name)
		}

		var permissions []accesstypes.Permission
		for _, perm := range tag.Permissions {
			if perm == accesstypes.NullPermission {
				return errors.Newf("tag %q under resource %q registers a null permission", tag.Name, res.Name)
			}
			if slices.Contains(permissions, perm) {
				return errors.Newf("found existing mapping between tag (%s) and permission (%s) under resource (%s)", tag.Name, perm, res.Name)
			}
			permissions = append(permissions, perm)
		}
		g.tagStore[res.Scope][res.Name][tag.Name] = permissions
	}

	return nil
}

// MustNewGeneratedCollection is NewGeneratedCollection panicking on invalid data, for
// use by generated code.
func MustNewGeneratedCollection(data CollectionData) *GeneratedCollection {
	g, err := NewGeneratedCollection(data)
	if err != nil {
		panic(err)
	}

	return g
}

// List returns a map of permissions to the resources that have them.
func (g *GeneratedCollection) List() map[accesstypes.Permission][]accesstypes.Resource {
	permissionResources := make(map[accesstypes.Permission][]accesstypes.Resource)
	for _, store := range g.resourceStore {
		for resource, permissions := range store {
			for _, permission := range permissions {
				permissionResources[permission] = append(permissionResources[permission], resource)
			}
		}
	}

	for _, store := range g.tagStore {
		for resource, tags := range store {
			for tag, permissions := range tags {
				for _, permission := range permissions {
					permissionResources[permission] = append(permissionResources[permission], resource.ResourceWithTag(tag))
				}
			}
		}
	}

	return permissionResources
}

// Scope returns the permission scope for a given resource, or an empty scope if the
// resource is not found.
func (g *GeneratedCollection) Scope(resource accesstypes.Resource) accesstypes.PermissionScope {
	for scope, store := range g.resourceStore {
		if _, ok := store[resource]; ok {
			return scope
		}
	}

	for scope, store := range g.tagStore {
		r, t := resource.ResourceAndTag()
		if _, ok := store[r][t]; ok {
			return scope
		}
	}

	return ""
}

// IsResourceImmutable checks if a resource is marked as immutable within a given scope.
func (g *GeneratedCollection) IsResourceImmutable(scope accesstypes.PermissionScope, res accesstypes.Resource) bool {
	resource, tag := res.ResourceAndTag()
	_, ok := g.immutableFields[scope][resource][tag]

	return ok
}

// IsComputedResource reports whether res is a computed resource within scope: a
// hand-written query surface whose permission checks run at decode time, where
// no row exists. A field resource answers as its base resource.
func (g *GeneratedCollection) IsComputedResource(scope accesstypes.PermissionScope, res accesstypes.Resource) bool {
	base, _ := res.ResourceAndTag()
	_, ok := g.computed[scope][base]

	return ok
}

// setResourceComputed records res as a computed resource within scope.
func (g *GeneratedCollection) setResourceComputed(scope accesstypes.PermissionScope, res accesstypes.Resource) {
	if g.computed[scope] == nil {
		g.computed[scope] = make(map[accesstypes.Resource]struct{})
	}
	g.computed[scope][res] = struct{}{}
}

// setMethodTransition records an RPC method resource's declared transition
// within scope. Every transition addresses a target row, so the target
// registers alongside it — a transition-only registration still answers
// MethodTarget and MethodsTargeting.
func (g *GeneratedCollection) setMethodTransition(scope accesstypes.PermissionScope, method accesstypes.Resource, transition TransitionData) {
	if g.transitions[scope] == nil {
		g.transitions[scope] = make(map[accesstypes.Resource]TransitionData)
	}
	g.transitions[scope][method] = transition
	g.setMethodTarget(scope, method, transition.Target)
}

// setMethodTarget records the row resource an RPC method's @target field
// addresses within scope.
func (g *GeneratedCollection) setMethodTarget(scope accesstypes.PermissionScope, method, target accesstypes.Resource) {
	if g.targets[scope] == nil {
		g.targets[scope] = make(map[accesstypes.Resource]accesstypes.Resource)
	}
	g.targets[scope][method] = target
}

// setResourceParent records a workflow member's immediate parent within scope.
func (g *GeneratedCollection) setResourceParent(scope accesstypes.PermissionScope, member, parent accesstypes.Resource) {
	if g.parents[scope] == nil {
		g.parents[scope] = make(map[accesstypes.Resource]accesstypes.Resource)
	}
	g.parents[scope][member] = parent
}

// MembersOf lists the workflow member resources whose immediate parent hop is
// parent, sorted by name — the create-under-parent affordance's candidates
// (design plan §11): capabilities=Create on the parent's read answers, per
// row, which of these the user may create beneath it. Every scope is
// searched: a member and its parent share their scope kind by construction.
func (g *GeneratedCollection) MembersOf(parent accesstypes.Resource) []accesstypes.Resource {
	var members []accesstypes.Resource
	for _, store := range g.parents {
		for member, memberParent := range store {
			if memberParent == parent {
				members = append(members, member)
			}
		}
	}
	slices.Sort(members)

	return members
}

// MethodTarget reports the row resource method's @target field addresses
// within scope, and whether the method declares one. A targeted method's
// Execute grants may carry row-referencing conditions — the generated handler
// locates the row inside its transaction and evaluates them there — so
// deploy-time grant validation (access.MigrateRoles) asks this to tell a
// targeted method from a plain one.
func (g *GeneratedCollection) MethodTarget(scope accesstypes.PermissionScope, method accesstypes.Resource) (accesstypes.Resource, bool) {
	target, ok := g.targets[scope][method]

	return target, ok
}

// TransitionMethod pairs an RPC method resource with its declared transition.
type TransitionMethod struct {
	Method     accesstypes.Resource
	Transition TransitionData
}

// TargetedMethod pairs an RPC method resource with the row resource its
// @target field addresses; Transition carries the declared edge when the
// method is a transition, nil for the plain located-row form.
type TargetedMethod struct {
	Method     accesstypes.Resource
	Target     accesstypes.Resource
	Transition *TransitionData
}

// MethodsTargeting lists the RPC method resources whose @target field
// addresses target, sorted by method name — the capability envelope's Execute
// candidates: a transition method's affordance is gated on the row's state
// membership in its from set, a plain method's only on its Execute grant's
// condition. Every scope is searched: a targeted method and its row resource
// share their scope kind by construction.
func (g *GeneratedCollection) MethodsTargeting(target accesstypes.Resource) []TargetedMethod {
	var methods []TargetedMethod
	for scope, store := range g.targets {
		for method, methodTarget := range store {
			if methodTarget != target {
				continue
			}
			tm := TargetedMethod{Method: method, Target: methodTarget}
			if transition, ok := g.transitions[scope][method]; ok {
				tm.Transition = &transition
			}
			methods = append(methods, tm)
		}
	}
	slices.SortFunc(methods, func(a, b TargetedMethod) int {
		return strings.Compare(string(a.Method), string(b.Method))
	})

	return methods
}

// TransitionsOnto lists the RPC method resources whose declared transitions
// target res, sorted by method name — the capability envelope's Execute
// candidates. Every scope is searched: a transition's method and root share
// their scope kind by construction.
func (g *GeneratedCollection) TransitionsOnto(target accesstypes.Resource) []TransitionMethod {
	var methods []TransitionMethod
	for _, store := range g.transitions {
		for method, transition := range store {
			if transition.Target == target {
				methods = append(methods, TransitionMethod{Method: method, Transition: transition})
			}
		}
	}
	slices.SortFunc(methods, func(a, b TransitionMethod) int {
		return strings.Compare(string(a.Method), string(b.Method))
	})

	return methods
}

// Resources returns a sorted list of all unique base resource names in the collection.
func (g *GeneratedCollection) Resources() []accesstypes.Resource {
	return g.resources(nil)
}

func (g *GeneratedCollection) resources(skip map[accesstypes.Resource]struct{}) []accesstypes.Resource {
	resources := []accesstypes.Resource{}
	for _, stores := range g.resourceStore {
		for resource, permissions := range stores {
			if slices.Contains(permissions, accesstypes.Execute) {
				continue
			}
			if _, skipped := skip[resource]; skipped {
				continue
			}

			resources = append(resources, resource)
		}
	}

	slices.Sort(resources)

	return slices.Compact(resources)
}

// ResourceExists checks if a resource exists in the collection.
func (g *GeneratedCollection) ResourceExists(r accesstypes.Resource) bool {
	for _, stores := range g.resourceStore {
		for resource, permissions := range stores {
			if slices.Contains(permissions, accesstypes.Execute) {
				continue
			}
			if resource == r {
				return true
			}
		}
	}

	return false
}

// TypescriptData returns a struct containing all the data needed for TypeScript code generation.
func (g *GeneratedCollection) TypescriptData() *TypescriptData {
	return g.TypescriptDataExcluding()
}

// TypescriptDataExcluding returns the TypeScript generation data with every
// registration on the named resources omitted: the resources themselves, their
// tags, and any permission or scope no remaining registration carries. The
// TypeScript generator passes the resources and methods that belong exclusively
// to other router outlets, so an outlet-scoped target emits only its own members.
func (g *GeneratedCollection) TypescriptDataExcluding(excluded ...accesstypes.Resource) *TypescriptData {
	var skip map[accesstypes.Resource]struct{}
	if len(excluded) > 0 {
		skip = make(map[accesstypes.Resource]struct{}, len(excluded))
		for _, res := range excluded {
			skip[res] = struct{}{}
		}
	}

	return &TypescriptData{
		Permissions:      g.permissions(skip),
		Resources:        g.resources(skip),
		ResourceTags:     g.tags(skip),
		PermissionScopes: g.permissionScopes(skip),
	}
}

// HasPermission reports whether the collection registers permission on res within scope.
func (g *GeneratedCollection) HasPermission(scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) bool {
	return slices.Contains(g.resourceStore[scope][res], permission)
}

// Data returns the canonical, deterministically sorted form of the collection.
func (g *GeneratedCollection) Data() CollectionData {
	return collectionDataFrom(g)
}

func (g *GeneratedCollection) addResource(allowDuplicateRegistration bool, scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) error {
	if !allowDuplicateRegistration {
		if ok := slices.Contains(g.resourceStore[scope][res], permission); ok {
			return errors.Newf("found existing entry under resource: %s and permission: %s", res, permission)
		}
	}

	if g.resourceStore[scope] == nil {
		g.resourceStore[scope] = resourceStore{}
	}

	g.resourceStore[scope][res] = append(g.resourceStore[scope][res], permission)

	return nil
}

// addResourceSet is CollectionBuilder.AddResourceSet's registration core: duplicate
// detection, null-permission filtering, and immutable-field replacement (the last
// registration for a resource wins). NewGeneratedCollection populates a GeneratedCollection
// from already-canonicalized CollectionData directly instead: its input is already
// deduplicated per resource, so it validates rather than merges.
func (g *GeneratedCollection) addResourceSet(scope accesstypes.PermissionScope, res accesstypes.Resource, perms []accesstypes.Permission, tags accesstypes.TagPermissions, immutableFields map[accesstypes.Tag]struct{}) error {
	for _, perm := range perms {
		if err := g.addResource(false, scope, perm, res); err != nil {
			return err
		}
	}

	if g.tagStore[scope][res] == nil {
		if g.tagStore[scope] == nil {
			g.tagStore[scope] = make(tagStore)
		}

		g.tagStore[scope][res] = make(map[accesstypes.Tag][]accesstypes.Permission, len(tags))
	}

	for tag, tagPermissions := range tags {
		for _, permission := range tagPermissions {
			permissions := g.tagStore[scope][res][tag]
			if slices.Contains(permissions, permission) {
				return errors.Newf("found existing mapping between tag (%s) and permission (%s) under resource (%s)", tag, permission, res)
			}

			if permission != accesstypes.NullPermission {
				g.tagStore[scope][res][tag] = append(permissions, permission)
			} else {
				g.tagStore[scope][res][tag] = permissions
			}
		}
	}

	if _, ok := g.immutableFields[scope]; !ok {
		g.immutableFields[scope] = make(map[accesstypes.Resource]map[accesstypes.Tag]struct{})
	}

	g.immutableFields[scope][res] = immutableFields

	return nil
}

func (g *GeneratedCollection) permissions(skip map[accesstypes.Resource]struct{}) []accesstypes.Permission {
	permissions := []accesstypes.Permission{}
	for _, stores := range g.resourceStore {
		for resource, perms := range stores {
			if _, skipped := skip[resource]; skipped {
				continue
			}
			permissions = append(permissions, perms...)
		}
	}
	for _, stores := range g.tagStore {
		for resource, tags := range stores {
			if _, skipped := skip[resource]; skipped {
				continue
			}
			for _, perms := range tags {
				permissions = append(permissions, perms...)
			}
		}
	}
	slices.Sort(permissions)

	return slices.Compact(permissions)
}

func (g *GeneratedCollection) tags(skip map[accesstypes.Resource]struct{}) map[accesstypes.Resource][]accesstypes.Tag {
	resourcetags := make(map[accesstypes.Resource][]accesstypes.Tag)

	for _, tagStore := range g.tagStore {
		for resource, tags := range tagStore {
			if _, skipped := skip[resource]; skipped {
				continue
			}
			for tag := range tags {
				resourcetags[resource] = append(resourcetags[resource], tag)
				slices.Sort(resourcetags[resource])
			}
		}
	}

	return resourcetags
}

// permissionScopes returns the permission scopes the collection registers resources
// under, sorted for deterministic generated output. These are scopes (global/domain),
// not tenant domains — the tenant universe is app-owned.
func (g *GeneratedCollection) permissionScopes(skip map[accesstypes.Resource]struct{}) []accesstypes.PermissionScope {
	scopes := make([]accesstypes.PermissionScope, 0, len(g.resourceStore))
	for scope, store := range g.resourceStore {
		for resource := range store {
			if _, skipped := skip[resource]; skipped {
				continue
			}
			scopes = append(scopes, scope)

			break
		}
	}
	slices.Sort(scopes)

	return scopes
}

// resourceKey identifies one resource registration: its scope and name.
type resourceKey struct {
	scope accesstypes.PermissionScope
	name  accesstypes.Resource
}

// collectionResourceKeys enumerates every (scope, resource) pair any of the
// collection's stores mention, sorted by scope then name.
func collectionResourceKeys(g *GeneratedCollection) []resourceKey {
	keySet := make(map[resourceKey]struct{})
	for scope, store := range g.resourceStore {
		for res := range store {
			keySet[resourceKey{scope: scope, name: res}] = struct{}{}
		}
	}
	for scope, store := range g.tagStore {
		for res := range store {
			keySet[resourceKey{scope: scope, name: res}] = struct{}{}
		}
	}
	for scope, store := range g.immutableFields {
		for res := range store {
			keySet[resourceKey{scope: scope, name: res}] = struct{}{}
		}
	}
	for scope, store := range g.bindings {
		for res := range store {
			keySet[resourceKey{scope: scope, name: res}] = struct{}{}
		}
	}
	for scope, store := range g.computed {
		for res := range store {
			keySet[resourceKey{scope: scope, name: res}] = struct{}{}
		}
	}
	for scope, store := range g.transitions {
		for res := range store {
			keySet[resourceKey{scope: scope, name: res}] = struct{}{}
		}
	}

	keys := make([]resourceKey, 0, len(keySet))
	for key := range keySet {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b resourceKey) int {
		if a.scope != b.scope {
			if a.scope < b.scope {
				return -1
			}

			return 1
		}
		if a.name < b.name {
			return -1
		} else if a.name > b.name {
			return 1
		}

		return 0
	})

	return keys
}

// collectionDataFrom canonicalizes a collection's stores: resources sorted by scope then
// name, tags and permissions sorted, and resource-level permissions deduplicated (manual
// registration permits duplicates).
func collectionDataFrom(g *GeneratedCollection) CollectionData {
	keys := collectionResourceKeys(g)

	data := CollectionData{Resources: make([]CollectionResource, 0, len(keys))}
	for _, key := range keys {
		res := CollectionResource{
			Name:  key.name,
			Scope: key.scope,
		}

		if _, ok := g.computed[key.scope][key.name]; ok {
			res.Computed = true
		}

		perms := slices.Clone(g.resourceStore[key.scope][key.name])
		slices.Sort(perms)
		res.Permissions = slices.Compact(perms)
		if len(res.Permissions) == 0 {
			res.Permissions = nil
		}

		tagMap := g.tagStore[key.scope][key.name]
		if len(tagMap) > 0 {
			tagNames := make([]accesstypes.Tag, 0, len(tagMap))
			for tag := range tagMap {
				tagNames = append(tagNames, tag)
			}
			slices.Sort(tagNames)

			res.Tags = make([]TagData, 0, len(tagNames))
			for _, tag := range tagNames {
				tagPerms := slices.Clone(tagMap[tag])
				slices.Sort(tagPerms)
				if len(tagPerms) == 0 {
					tagPerms = nil
				}
				res.Tags = append(res.Tags, TagData{Name: tag, Permissions: tagPerms})
			}
		}

		immutable := g.immutableFields[key.scope][key.name]
		if len(immutable) > 0 {
			res.ImmutableTags = make([]accesstypes.Tag, 0, len(immutable))
			for tag := range immutable {
				res.ImmutableTags = append(res.ImmutableTags, tag)
			}
			slices.Sort(res.ImmutableTags)
		}

		if bindings, ok := g.bindings[key.scope][key.name]; ok {
			applyBindingData(&res, &bindings)
		}

		if transition, ok := g.transitions[key.scope][key.name]; ok {
			transition.From = slices.Clone(transition.From)
			res.Transition = &transition
		}

		if target, ok := g.targets[key.scope][key.name]; ok {
			res.Target = target
		}

		if parent, ok := g.parents[key.scope][key.name]; ok {
			res.Parent = parent
		}

		data.Resources = append(data.Resources, res)
	}

	return data
}

// applyBindingData copies a resource's stored binding vocabulary onto its
// serializable form, in canonical (name-sorted) order.
func applyBindingData(res *CollectionResource, bindings *Bindings) {
	sorted := bindings.sorted()
	res.Attributes = sorted.Attributes
	res.Domain = sorted.Domain
	res.SubjectSets = sorted.SubjectSets
	res.SubjectValues = sorted.SubjectValues
}
