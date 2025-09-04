// package resource provides a set of types and functions for working with resources.
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
	FieldDefaultFunc func(ctx context.Context, txn TxnBuffer) (any, error)
	DefaultsFunc     func(ctx context.Context, txn TxnBuffer) error
)

type Resourcer interface {
	Resource() accesstypes.Resource
	DefaultConfig() Config
}

type ResourceSet[Resource Resourcer] struct {
	permissions     []accesstypes.Permission
	requiredTagPerm accesstypes.TagPermissions
	fieldToTag      map[accesstypes.Field]accesstypes.Tag
	immutableFields map[accesstypes.Tag]struct{}
	rMeta           *ResourceMetadata[Resource]
}

func NewResourceSet[Resource Resourcer, Request any](permissions ...accesstypes.Permission) (*ResourceSet[Resource], error) {
	requiredTagPerm, fieldToTag, permissions, immutableFields, err := permissionsFromTags(reflect.TypeFor[Request](), permissions)
	if err != nil {
		return nil, errors.Wrap(err, "permissionsFromTags()")
	}

	return &ResourceSet[Resource]{
		permissions:     permissions,
		requiredTagPerm: requiredTagPerm,
		fieldToTag:      fieldToTag,
		immutableFields: immutableFields,
		rMeta:           NewResourceMetadata[Resource](),
	}, nil
}

func (r *ResourceSet[Resource]) BaseResource() accesstypes.Resource {
	var res Resource

	return res.Resource()
}

func (r *ResourceSet[Resource]) ImmutableFields() map[accesstypes.Tag]struct{} {
	return r.immutableFields
}

func (r *ResourceSet[Resource]) ResourceMetadata() *ResourceMetadata[Resource] {
	return r.rMeta
}

func (r *ResourceSet[Resource]) PermissionRequired(fieldName accesstypes.Field, perm accesstypes.Permission) bool {
	return slices.Contains(r.requiredTagPerm[r.fieldToTag[fieldName]], perm)
}

func (r *ResourceSet[Resource]) Permissions() []accesstypes.Permission {
	return r.permissions
}

func (r *ResourceSet[Resource]) Resource(fieldName accesstypes.Field) accesstypes.Resource {
	var res Resource

	return accesstypes.Resource(fmt.Sprintf("%s.%s", res.Resource(), r.fieldToTag[fieldName]))
}

func (r *ResourceSet[Resource]) TagPermissions() accesstypes.TagPermissions {
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

		if immutableTag == "true" {
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

type ResourceMetadata[Resource Resourcer] struct {
	fieldMap            map[accesstypes.Field]cacheEntry
	dbType              DBType
	changeTrackingTable string
	trackChanges        bool
}

func NewResourceMetadata[Resource Resourcer]() *ResourceMetadata[Resource] {
	var res Resource

	c := resMetadataCache.get(res)

	return &ResourceMetadata[Resource]{
		fieldMap:            c.fieldMap,
		dbType:              c.cfg.DBType,
		changeTrackingTable: c.cfg.ChangeTrackingTable,
		trackChanges:        c.cfg.TrackChanges,
	}
}

func (r *ResourceMetadata[Resource]) Fields() []accesstypes.Field {
	return slices.Collect(maps.Keys(r.fieldMap))
}

func (r *ResourceMetadata[Resource]) Len() int {
	return len(r.fieldMap)
}

var resMetadataCache = resourceMetadataCache{
	cache: make(map[reflect.Type]*resourceMetadataCacheEntry),
}

type resourceMetadataCacheEntry struct {
	fieldMap map[accesstypes.Field]cacheEntry
	cfg      Config
}

type resourceMetadataCache struct {
	cache map[reflect.Type]*resourceMetadataCacheEntry
	mu    sync.RWMutex
}

func (c *resourceMetadataCache) get(res Resourcer) *resourceMetadataCacheEntry {
	c.mu.RLock()

	t := reflect.TypeOf(res)
	if t.Kind() == reflect.Ptr {
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
	if t, ok := res.(Configurer); ok {
		cfg = t.Config()
	} else {
		cfg = res.DefaultConfig()
	}
	fieldMap := structTags(t, string(cfg.DBType))

	c.cache[t] = &resourceMetadataCacheEntry{
		fieldMap: fieldMap,
		cfg:      cfg,
	}

	return c.cache[t]
}

func structTags(t reflect.Type, key string) map[accesstypes.Field]cacheEntry {
	tagMap := make(map[accesstypes.Field]cacheEntry)
	for i := range t.NumField() {
		field := t.Field(i)
		tag := field.Tag.Get(key)

		list := strings.Split(tag, ",")
		if len(list) == 0 || list[0] == "" || list[0] == "-" {
			continue
		}

		tagMap[accesstypes.Field(field.Name)] = cacheEntry{index: i, tag: list[0]}
	}

	return tagMap
}
