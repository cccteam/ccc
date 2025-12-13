// Package resource provides a set of types and functions for working with resources.
package resource

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type (
	// FieldDefaultFunc is the signature for a function that applies a default value to one field of a PatchSet.
	FieldDefaultFunc func(ctx context.Context, txn ReadWriteTransaction) (any, error)

	// DefaultsFunc is the signature for a function that applies default values to a PatchSet.
	DefaultsFunc func(ctx context.Context, txn ReadWriteTransaction) error

	// ValidateFunc is the signature for a function that validates a PatchSet prior to committing it.
	ValidateFunc func(ctx context.Context, txn ReadWriteTransaction) error
)

// Resourcer is an interface that all resource structs must implement.
type Resourcer interface {
	Resource() accesstypes.Resource
}

// configurer is an interface for types that can provide a resource configuration.
type configurer interface {
	Config() Config
}

// defaultConfigurer is an interface for types that can provide a resource default configuration.
type defaultConfigurer interface {
	DefaultConfig() Config
}

// virtualQuerier is an interface for types that can provide a subquery with params.
type virtualQuerier interface {
	Subquery() (string, map[string]any)
}

// Set holds metadata about a resource, including its permissions and field-to-tag mappings.
type Set[Resource Resourcer] struct {
	permissions     []accesstypes.Permission
	requiredTagPerm accesstypes.TagPermissions
	fieldToTag      map[accesstypes.Field]accesstypes.Tag
	immutableFields map[accesstypes.Tag]struct{}
	rMeta           *Metadata[Resource]
}

// NewSet creates a new Set for a given Resource and Request type, parsing permissions from struct tags.
func NewSet[Resource Resourcer, Request any](permissions ...accesstypes.Permission) (*Set[Resource], error) {
	requiredTagPerm, fieldToTag, permissions, immutableFields, err := permissionsFromTags(reflect.TypeFor[Request](), permissions)
	if err != nil {
		return nil, errors.Wrap(err, "permissionsFromTags()")
	}

	return &Set[Resource]{
		permissions:     permissions,
		requiredTagPerm: requiredTagPerm,
		fieldToTag:      fieldToTag,
		immutableFields: immutableFields,
		rMeta:           NewMetadata[Resource](),
	}, nil
}

// BaseResource returns the base name of the resource (without any tags).
func (r *Set[Resource]) BaseResource() accesstypes.Resource {
	var res Resource

	return res.Resource()
}

// ImmutableFields returns a map of tags for fields that are marked as immutable.
func (r *Set[Resource]) ImmutableFields() map[accesstypes.Tag]struct{} {
	return r.immutableFields
}

// ResourceMetadata returns the metadata for the resource.
func (r *Set[Resource]) ResourceMetadata() *Metadata[Resource] {
	return r.rMeta
}

// PermissionRequired checks if a specific permission is required for a given field.
func (r *Set[Resource]) PermissionRequired(fieldName accesstypes.Field, perm accesstypes.Permission) bool {
	return slices.Contains(r.requiredTagPerm[r.fieldToTag[fieldName]], perm)
}

// Permissions returns all permissions associated with the resource set.
func (r *Set[Resource]) Permissions() []accesstypes.Permission {
	return r.permissions
}

// Resource returns the full resource name for a field, including its tag (e.g., "myresource.myfield").
func (r *Set[Resource]) Resource(fieldName accesstypes.Field) accesstypes.Resource {
	var res Resource

	return accesstypes.Resource(fmt.Sprintf("%s.%s", res.Resource(), r.fieldToTag[fieldName]))
}

// TagPermissions returns the mapping of tags to their required permissions.
func (r *Set[Resource]) TagPermissions() accesstypes.TagPermissions {
	return r.requiredTagPerm
}

func permissionsFromTags(t reflect.Type, perms []accesstypes.Permission) (tags accesstypes.TagPermissions, fieldToTag map[accesstypes.Field]accesstypes.Tag, permissions []accesstypes.Permission, immutableFields map[accesstypes.Tag]struct{}, err error) {
	if t.Kind() != reflect.Struct {
		return nil, nil, nil, nil, errors.Newf("expected a struct, got %s", t.Kind())
	}

	tags = make(accesstypes.TagPermissions)
	fieldToTag = make(map[accesstypes.Field]accesstypes.Tag)
	permissionMap := make(map[accesstypes.Permission]struct{})
	mutating := make(map[accesstypes.Permission]struct{})
	nonmutating := make(map[accesstypes.Permission]struct{})
	immutableFields = make(map[accesstypes.Tag]struct{})

	for _, perm := range perms {
		switch perm {
		case accesstypes.NullPermission:
			continue
		case accesstypes.Create, accesstypes.Update, accesstypes.Delete:
			mutating[perm] = struct{}{}
		default:
			nonmutating[perm] = struct{}{}
		}
		permissionMap[perm] = struct{}{}
	}

	for i := range t.NumField() {
		field := t.Field(i)
		jsonTag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		immutableTag, _, _ := strings.Cut(field.Tag.Get("immutable"), ",")
		permTag := field.Tag.Get("perm")
		perms := strings.Split(permTag, ",")

		if immutableTag == trueStr {
			immutableFields[accesstypes.Tag(jsonTag)] = struct{}{}

			// immutability is implemented by requiring the update permission (here) and then
			// disallowing it from being assigned to a role (elsewhere)
			if !slices.Contains(perms, string(accesstypes.Update)) {
				perms = append(perms, string(accesstypes.Update))
			}
		}

		var collected bool
		for _, s := range perms {
			permission := accesstypes.Permission(strings.TrimSpace(s))
			switch permission {
			case accesstypes.NullPermission:
				continue
			case accesstypes.Delete:
				return nil, nil, nil, nil, errors.Newf("delete permission is not allowed in struct tag")
			case accesstypes.Create, accesstypes.Update:
				mutating[permission] = struct{}{}
			default:
				nonmutating[permission] = struct{}{}
			}

			if jsonTag == "" || jsonTag == "-" {
				return nil, nil, nil, nil, errors.Newf("can not set %s permission on the %s field when json tag is empty", permission, field.Name)
			}
			tags[accesstypes.Tag(jsonTag)] = append(tags[accesstypes.Tag(jsonTag)], permission)
			fieldToTag[accesstypes.Field(field.Name)] = accesstypes.Tag(jsonTag)
			permissionMap[permission] = struct{}{}
			collected = true
		}
		if !collected && registerAllResources {
			if jsonTag != "" && jsonTag != "-" {
				tags[accesstypes.Tag(jsonTag)] = append(tags[accesstypes.Tag(jsonTag)], accesstypes.NullPermission)
				fieldToTag[accesstypes.Field(field.Name)] = accesstypes.Tag(jsonTag)
			}
		}
	}

	if len(nonmutating) > 1 {
		return nil, nil, nil, nil, errors.Newf("can not have more then one type of non-mutating permission in the same struct: found %s", slices.Collect(maps.Keys(nonmutating)))
	}

	if len(nonmutating) != 0 && len(mutating) != 0 {
		return nil, nil, nil, nil, errors.Newf("can not have both non-mutating and mutating permissions in the same struct: found %s and %s", slices.Collect(maps.Keys(nonmutating)), slices.Collect(maps.Keys(mutating)))
	}

	permissions = slices.Collect(maps.Keys(permissionMap))
	slices.Sort(permissions)

	return tags, fieldToTag, permissions, immutableFields, nil
}

// Metadata contains cached metadata about a resource, such as its database schema mapping and configuration.
type Metadata[Resource Resourcer] struct {
	dbMap               map[DBType]map[accesstypes.Field]dbFieldMetadata
	changeTrackingTable string
	trackChanges        bool
}

// NewMetadata creates or retrieves cached metadata for a resource.
func NewMetadata[Resource Resourcer]() *Metadata[Resource] {
	var res Resource

	c := resMetadataCache.get(res)

	return &Metadata[Resource]{
		dbMap:               c.dbMap,
		changeTrackingTable: c.cfg.ChangeTrackingTable,
		trackChanges:        c.cfg.TrackChanges,
	}
}

// dbFieldMap returns the mapping of field names to their database column names for a given database type.
func (r *Metadata[Resource]) dbFieldMap(dbType DBType) map[accesstypes.Field]dbFieldMetadata {
	return r.dbMap[dbType]
}

// DBFields returns a slice of all field names for a given database type.
func (r *Metadata[Resource]) DBFields(dbType DBType) []accesstypes.Field {
	return slices.Collect(maps.Keys(r.dbMap[dbType]))
}

// DBFieldCount returns the number of fields for a given database type.
func (r *Metadata[Resource]) DBFieldCount(dbType DBType) int {
	return len(r.dbMap[dbType])
}

var resMetadataCache = resourceMetadataCache{
	cache: make(map[reflect.Type]*resourceMetadataCacheEntry),
}

type resourceMetadataCacheEntry struct {
	dbMap map[DBType]map[accesstypes.Field]dbFieldMetadata
	cfg   Config
}

type resourceMetadataCache struct {
	cache map[reflect.Type]*resourceMetadataCacheEntry
	mu    sync.RWMutex
}

func (c *resourceMetadataCache) get(res Resourcer) *resourceMetadataCacheEntry {
	c.mu.RLock()

	t := reflect.TypeOf(res)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if tagMap, ok := c.cache[t]; ok {
		defer c.mu.RUnlock()

		return tagMap
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	if tagMap, ok := c.cache[t]; ok {
		return tagMap
	}

	if t.Kind() != reflect.Struct {
		panic(errors.Newf("expected struct, got %s", t.Kind()))
	}

	var cfg Config
	switch t := res.(type) {
	case configurer:
		cfg = t.Config()
	case defaultConfigurer:
		cfg = t.DefaultConfig()
	}

	dbMap := make(map[DBType]map[accesstypes.Field]dbFieldMetadata)
	for _, dbType := range dbTypes() {
		dbFieldMap := dbStructTags(t, dbType)
		dbMap[dbType] = dbFieldMap
	}

	c.cache[t] = &resourceMetadataCacheEntry{
		dbMap: dbMap,
		cfg:   cfg,
	}

	return c.cache[t]
}

func dbStructTags(t reflect.Type, dbType DBType) map[accesstypes.Field]dbFieldMetadata {
	tagMap := make(map[accesstypes.Field]dbFieldMetadata)
	for i := range t.NumField() {
		field := t.Field(i)
		tag := field.Tag.Get(string(dbType))

		parts := strings.Split(tag, ",")
		if len(parts) == 0 || parts[0] == "" || parts[0] == "-" {
			continue
		}

		tagMap[accesstypes.Field(field.Name)] = dbFieldMetadata{index: i, ColumnName: parts[0]}
	}

	return tagMap
}
