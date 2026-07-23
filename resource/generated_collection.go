package resource

import (
	"slices"

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
	Perm      string // raw perm tag value (comma-separated permissions)
	Immutable bool   // immutable:"true"
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
// mirroring NewSet over the equivalent struct, always registering every field (even ones
// without a permission tag) so generated Collection/TypeScript output is complete.
func NewSetData(fields []FieldTags, permissions ...accesstypes.Permission) (SetData, error) {
	tagPermissions, _, perms, immutableFields, err := permissionsFromFieldTags(fields, permissions, true)
	if err != nil {
		return SetData{}, errors.Wrap(err, "permissionsFromFieldTags()")
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
	Name          accesstypes.Resource
	Scope         accesstypes.PermissionScope
	Permissions   []accesstypes.Permission
	Tags          []TagData
	ImmutableTags []accesstypes.Tag
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
	permissionMap     map[accesstypes.Resource]map[accesstypes.Permission]bool
	immutableFieldMap map[accesstypes.Resource]map[accesstypes.Tag]struct{}
)

// GeneratedCollection is a read-only permission collection constructed from generated
// CollectionData. It is immutable after construction: nothing in its API mutates it once
// built, so concurrent reads require no synchronization.
type GeneratedCollection struct {
	tagStore        map[accesstypes.PermissionScope]tagStore
	resourceStore   map[accesstypes.PermissionScope]resourceStore
	immutableFields map[accesstypes.PermissionScope]immutableFieldMap
}

// newGeneratedCollection creates an empty, populatable GeneratedCollection.
func newGeneratedCollection() *GeneratedCollection {
	return &GeneratedCollection{
		tagStore:        make(map[accesstypes.PermissionScope]tagStore, 2),
		resourceStore:   make(map[accesstypes.PermissionScope]resourceStore, 2),
		immutableFields: make(map[accesstypes.PermissionScope]immutableFieldMap, 2),
	}
}

// NewGeneratedCollection validates data and constructs the collection. It rejects
// duplicate resources, duplicate or null permissions, and duplicate tags, so invalid
// generated data fails at startup rather than surfacing as wrong permission decisions.
func NewGeneratedCollection(data CollectionData) (*GeneratedCollection, error) {
	g := newGeneratedCollection()

	type resourceKey struct {
		scope accesstypes.PermissionScope
		name  accesstypes.Resource
	}
	seen := make(map[resourceKey]struct{}, len(data.Resources))

	for _, res := range data.Resources {
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

		if len(res.Tags) > 0 {
			if g.tagStore[res.Scope] == nil {
				g.tagStore[res.Scope] = make(tagStore)
			}
			g.tagStore[res.Scope][res.Name] = make(map[accesstypes.Tag][]accesstypes.Permission, len(res.Tags))

			for _, tag := range res.Tags {
				if _, ok := g.tagStore[res.Scope][res.Name][tag.Name]; ok {
					return nil, errors.Newf("duplicate tag %q under resource %q", tag.Name, res.Name)
				}

				var permissions []accesstypes.Permission
				for _, perm := range tag.Permissions {
					if perm == accesstypes.NullPermission {
						return nil, errors.Newf("tag %q under resource %q registers a null permission", tag.Name, res.Name)
					}
					if slices.Contains(permissions, perm) {
						return nil, errors.Newf("found existing mapping between tag (%s) and permission (%s) under resource (%s)", tag.Name, perm, res.Name)
					}
					permissions = append(permissions, perm)
				}
				g.tagStore[res.Scope][res.Name][tag.Name] = permissions
			}
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
	}

	return g, nil
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

// Resources returns a sorted list of all unique base resource names in the collection.
func (g *GeneratedCollection) Resources() []accesstypes.Resource {
	resources := []accesstypes.Resource{}
	for _, stores := range g.resourceStore {
		for resource, permissions := range stores {
			if slices.Contains(permissions, accesstypes.Execute) {
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
	return &TypescriptData{
		Permissions:           g.permissions(),
		ResourcePermissions:   g.resourcePermissions(),
		Resources:             g.Resources(),
		ResourceTags:          g.tags(),
		ResourcePermissionMap: g.resourcePermissionMap(),
		Domains:               g.domains(),
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

// addResourceSet is the registration core shared by CollectionBuilder.AddResourceSet and
// NewGeneratedCollection, applying identical semantics: duplicate detection,
// null-permission filtering, and immutable-field replacement (the last registration for a
// resource wins).
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

func (g *GeneratedCollection) permissions() []accesstypes.Permission {
	permissions := []accesstypes.Permission{}
	for _, stores := range g.resourceStore {
		for _, perms := range stores {
			permissions = append(permissions, perms...)
		}
	}
	for _, stores := range g.tagStore {
		for _, tags := range stores {
			for _, perms := range tags {
				permissions = append(permissions, perms...)
			}
		}
	}
	slices.Sort(permissions)

	return slices.Compact(permissions)
}

func (g *GeneratedCollection) resourcePermissions() []accesstypes.Permission {
	permissions := []accesstypes.Permission{}
	for _, stores := range g.resourceStore {
		for _, perms := range stores {
			permissions = append(permissions, perms...)
		}
	}
	for _, stores := range g.tagStore {
		for _, tags := range stores {
			for _, perms := range tags {
				permissions = append(permissions, perms...)
			}
		}
	}
	slices.Sort(permissions)

	filteredPermissions := permissions[:0]
	for _, perm := range permissions {
		if perm != accesstypes.Execute {
			filteredPermissions = append(filteredPermissions, perm)
		}
	}
	clear(permissions[len(filteredPermissions):])

	return slices.Compact(filteredPermissions)
}

func (g *GeneratedCollection) tags() map[accesstypes.Resource][]accesstypes.Tag {
	resourcetags := make(map[accesstypes.Resource][]accesstypes.Tag)

	for _, tagStore := range g.tagStore {
		for resource, tags := range tagStore {
			for tag := range tags {
				resourcetags[resource] = append(resourcetags[resource], tag)
				slices.Sort(resourcetags[resource])
			}
		}
	}

	return resourcetags
}

func (g *GeneratedCollection) resourcePermissionMap() permissionMap {
	permMap := make(map[accesstypes.Resource]map[accesstypes.Permission]bool)
	permSet := make(map[accesstypes.Permission]struct{})
	resources := make(map[accesstypes.Resource]struct{})

	setRequiredPerms := func(res accesstypes.Resource, permissions []accesstypes.Permission) {
		permMap[res] = make(map[accesstypes.Permission]bool)
		for _, perm := range permissions {
			permSet[perm] = struct{}{}
			permMap[res][perm] = true
		}
	}

	for _, store := range g.resourceStore {
		for resource, permissions := range store {
			if slices.Contains(permissions, accesstypes.Execute) {
				continue
			}

			resources[resource] = struct{}{}
			setRequiredPerms(resource, permissions)
		}
	}

	for _, store := range g.tagStore {
		for resource, tagmap := range store {
			for tag, permissions := range tagmap {
				if slices.Contains(permissions, accesstypes.Execute) {
					continue
				}

				resources[resource.ResourceWithTag(tag)] = struct{}{}
				setRequiredPerms(resource.ResourceWithTag(tag), permissions)
			}
		}
	}

	for resource := range resources {
		for perm := range permSet {
			if _, ok := permMap[resource][perm]; !ok {
				permMap[resource][perm] = false
			}
		}
	}

	return permMap
}

func (g *GeneratedCollection) domains() []accesstypes.PermissionScope {
	domains := make([]accesstypes.PermissionScope, 0, len(g.resourceStore))
	for domain := range g.resourceStore {
		domains = append(domains, domain)
	}

	return domains
}

// collectionDataFrom canonicalizes a collection's stores: resources sorted by scope then
// name, tags and permissions sorted, and resource-level permissions deduplicated (manual
// registration permits duplicates).
func collectionDataFrom(g *GeneratedCollection) CollectionData {
	type resourceKey struct {
		scope accesstypes.PermissionScope
		name  accesstypes.Resource
	}

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

	data := CollectionData{Resources: make([]CollectionResource, 0, len(keys))}
	for _, key := range keys {
		res := CollectionResource{
			Name:  key.name,
			Scope: key.scope,
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

		data.Resources = append(data.Resources, res)
	}

	return data
}
