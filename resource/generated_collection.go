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
// a Collection: the resource-level permissions, the tag-to-permission mappings (including
// tags registered without permissions), and the immutable tags.
type SetData struct {
	Permissions     []accesstypes.Permission
	TagPermissions  accesstypes.TagPermissions
	ImmutableFields map[accesstypes.Tag]struct{}
}

// NewSetData computes the registration data for a request struct described by fields,
// mirroring NewSet over the equivalent struct in a collect_resource_permissions build.
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
// no generated handler. It is both the generator's declaration input and the record of a
// runtime Collection.AddResource call (see Collection.ManualRegistrations).
type ManualRegistration struct {
	// Scope is empty when the declaration leaves the scope to the default
	// (accesstypes.GlobalPermissionScope); runtime-recorded registrations always
	// carry the scope they were registered under.
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
// runtime-populated Collection enforces (duplicate detection, null-permission filtering,
// immutable-field replacement), without requiring the collect_resource_permissions build
// tag. It wraps Collection so both paths share one registration implementation; the core
// moves here when the deprecated Collection API is removed.
type CollectionBuilder struct {
	c *Collection
}

// NewCollectionBuilder creates an empty CollectionBuilder.
func NewCollectionBuilder() *CollectionBuilder {
	return &CollectionBuilder{c: newPopulatableCollection()}
}

// AddResourceSet registers a request struct's SetData under scope, mirroring
// AddResources.
func (b *CollectionBuilder) AddResourceSet(scope accesstypes.PermissionScope, res accesstypes.Resource, set SetData) error {
	if err := b.c.addResourceSet(scope, res, set.Permissions, set.TagPermissions, set.ImmutableFields); err != nil {
		return err
	}

	return nil
}

// AddResource registers a single resource permission, mirroring Collection.AddResource
// (duplicate registrations are allowed).
func (b *CollectionBuilder) AddResource(scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) error {
	if permission == accesstypes.NullPermission {
		return errors.New("cannot register null permission")
	}

	if err := b.c.addResource(true, scope, permission, res); err != nil {
		return err
	}

	return nil
}

// AddMethodResource registers a method resource permission, mirroring
// Collection.AddMethodResource (duplicate registrations are rejected).
func (b *CollectionBuilder) AddMethodResource(scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) error {
	if permission == accesstypes.NullPermission {
		return errors.New("cannot register null permission")
	}

	if err := b.c.addResource(false, scope, permission, res); err != nil {
		return err
	}

	return nil
}

// Data returns the canonical, deterministically sorted form of everything registered so
// far.
func (b *CollectionBuilder) Data() CollectionData {
	return collectionDataFrom(b.c)
}

// GeneratedCollection returns the built collection directly, for consumers that do not
// need the serializable form.
func (b *CollectionBuilder) GeneratedCollection() *GeneratedCollection {
	return &GeneratedCollection{c: b.c}
}

// GeneratedCollection is a read-only permission collection constructed from generated
// CollectionData. It exposes the same read API as a runtime-populated Collection and is
// immutable after construction. It wraps Collection so the generated and runtime
// collections share one read implementation (the parity checks depend on that); the
// stores and read methods fold into this type when the deprecated Collection API is
// removed.
type GeneratedCollection struct {
	c *Collection
}

// NewGeneratedCollection validates data and constructs the collection. It rejects
// duplicate resources, duplicate or null permissions, and duplicate tags, so invalid
// generated data fails at startup rather than surfacing as wrong permission decisions.
func NewGeneratedCollection(data CollectionData) (*GeneratedCollection, error) {
	c := newPopulatableCollection()

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
			if err := c.addResource(false, res.Scope, perm, res.Name); err != nil {
				return nil, err
			}
		}

		if len(res.Tags) > 0 {
			if c.tagStore[res.Scope] == nil {
				c.tagStore[res.Scope] = make(tagStore)
			}
			c.tagStore[res.Scope][res.Name] = make(map[accesstypes.Tag][]accesstypes.Permission, len(res.Tags))

			for _, tag := range res.Tags {
				if _, ok := c.tagStore[res.Scope][res.Name][tag.Name]; ok {
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
				c.tagStore[res.Scope][res.Name][tag.Name] = permissions
			}
		}

		if len(res.ImmutableTags) > 0 {
			if _, ok := c.immutableFields[res.Scope]; !ok {
				c.immutableFields[res.Scope] = make(immutableFieldMap)
			}
			immutable := make(map[accesstypes.Tag]struct{}, len(res.ImmutableTags))
			for _, tag := range res.ImmutableTags {
				immutable[tag] = struct{}{}
			}
			c.immutableFields[res.Scope][res.Name] = immutable
		}
	}

	return &GeneratedCollection{c: c}, nil
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
	return g.c.List()
}

// Scope returns the permission scope for a given resource, or an empty scope if the
// resource is not found.
func (g *GeneratedCollection) Scope(resource accesstypes.Resource) accesstypes.PermissionScope {
	return g.c.Scope(resource)
}

// IsResourceImmutable checks if a resource is marked as immutable within a given scope.
func (g *GeneratedCollection) IsResourceImmutable(scope accesstypes.PermissionScope, res accesstypes.Resource) bool {
	return g.c.IsResourceImmutable(scope, res)
}

// Resources returns a sorted list of all unique base resource names in the collection.
func (g *GeneratedCollection) Resources() []accesstypes.Resource {
	return g.c.Resources()
}

// ResourceExists checks if a resource exists in the collection.
func (g *GeneratedCollection) ResourceExists(r accesstypes.Resource) bool {
	return g.c.ResourceExists(r)
}

// TypescriptData returns the data needed for TypeScript code generation.
func (g *GeneratedCollection) TypescriptData() *TypescriptData {
	return g.c.TypescriptData()
}

// HasPermission reports whether the collection registers permission on res within scope.
func (g *GeneratedCollection) HasPermission(scope accesstypes.PermissionScope, permission accesstypes.Permission, res accesstypes.Resource) bool {
	return slices.Contains(g.c.resourceStore[scope][res], permission)
}

// Data returns the canonical, deterministically sorted form of the collection.
func (g *GeneratedCollection) Data() CollectionData {
	return collectionDataFrom(g.c)
}

// newPopulatableCollection creates a Collection whose stores are initialized regardless
// of the collect_resource_permissions build tag, for population outside the deprecated
// runtime-registration path.
func newPopulatableCollection() *Collection {
	return &Collection{
		tagStore:        make(map[accesstypes.PermissionScope]tagStore, 2),
		resourceStore:   make(map[accesstypes.PermissionScope]resourceStore, 2),
		immutableFields: make(map[accesstypes.PermissionScope]immutableFieldMap, 2),
	}
}

// collectionDataFrom canonicalizes a collection's stores: resources sorted by scope then
// name, tags and permissions sorted, and resource-level permissions deduplicated (manual
// runtime registration permits duplicates).
func collectionDataFrom(c *Collection) CollectionData {
	type resourceKey struct {
		scope accesstypes.PermissionScope
		name  accesstypes.Resource
	}

	keySet := make(map[resourceKey]struct{})
	for scope, store := range c.resourceStore {
		for res := range store {
			keySet[resourceKey{scope: scope, name: res}] = struct{}{}
		}
	}
	for scope, store := range c.tagStore {
		for res := range store {
			keySet[resourceKey{scope: scope, name: res}] = struct{}{}
		}
	}
	for scope, store := range c.immutableFields {
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

		perms := slices.Clone(c.resourceStore[key.scope][key.name])
		slices.Sort(perms)
		res.Permissions = slices.Compact(perms)
		if len(res.Permissions) == 0 {
			res.Permissions = nil
		}

		tagMap := c.tagStore[key.scope][key.name]
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

		immutable := c.immutableFields[key.scope][key.name]
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
