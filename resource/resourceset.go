// package resource provides a set of types and functions for working with resources.
package resource

import (
	"fmt"
	"maps"
	reflect "reflect"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type Resourcer interface {
	Resource() accesstypes.Resource
}

type ResourceSet[Resource Resourcer, Request any] struct {
	permissions     []accesstypes.Permission
	requiredTagPerm accesstypes.TagPermissions
	fieldToTag      map[accesstypes.Field]accesstypes.Tag
	resource        accesstypes.Resource
	immutableFields map[accesstypes.Tag]struct{}
	rMeta           *ResourceMetadata[Resource]
}

func NewResourceSet[Resource Resourcer, Request any](permissions ...accesstypes.Permission) (*ResourceSet[Resource, Request], error) {
	var res Resource
	var req Request

	// TODO(jwatson): Verify this check will not be needed
	// if !t.ConvertibleTo(reflect.TypeOf(res)) {
	// 	return nil, errors.Newf("Request (%T) is not convertible to resource (%T)", req, res)
	// }

	requiredTagPerm, fieldToTag, permissions, immutableFields, err := permissionsFromTags(reflect.TypeOf(req), permissions)
	if err != nil {
		return nil, errors.Wrap(err, "permissionsFromTags()")
	}

	return &ResourceSet[Resource, Request]{
		permissions:     permissions,
		requiredTagPerm: requiredTagPerm,
		fieldToTag:      fieldToTag,
		resource:        res.Resource(),
		immutableFields: immutableFields,
		rMeta:           NewResourceMetadata[Resource](),
	}, nil
}

func (r *ResourceSet[Resource, Request]) BaseResource() accesstypes.Resource {
	return r.resource
}

func (r *ResourceSet[Resource, Request]) ImmutableFields() map[accesstypes.Tag]struct{} {
	return r.immutableFields
}

func (r *ResourceSet[Resource, Request]) ResourceMetadata() *ResourceMetadata[Resource] {
	return r.rMeta
}

func (r *ResourceSet[Resource, Request]) PermissionRequired(fieldName accesstypes.Field, perm accesstypes.Permission) bool {
	return slices.Contains(r.requiredTagPerm[r.fieldToTag[fieldName]], perm)
}

func (r *ResourceSet[Resource, Request]) Permission() accesstypes.Permission {
	switch len(r.permissions) {
	case 0:
		return accesstypes.NullPermission
	case 1:
		return r.permissions[0]
	default:
		panic("resource set has more than one required permission")
	}
}

func (r *ResourceSet[Resource, Request]) Permissions() []accesstypes.Permission {
	return r.permissions
}

func (r *ResourceSet[Resource, Request]) Resource(fieldName accesstypes.Field) accesstypes.Resource {
	return accesstypes.Resource(fmt.Sprintf("%s.%s", r.resource, r.fieldToTag[fieldName]))
}

func (r *ResourceSet[Resource, Request]) TagPermissions() accesstypes.TagPermissions {
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
	viewing := make(map[accesstypes.Permission]struct{})
	immutableFields = make(map[accesstypes.Tag]struct{})

	for _, perm := range perms {
		switch perm {
		case accesstypes.NullPermission:
			continue
		case accesstypes.Create, accesstypes.Update, accesstypes.Delete:
			mutating[perm] = struct{}{}
		default:
			viewing[perm] = struct{}{}
		}
		permissionMap[perm] = struct{}{}
	}

	for i := range t.NumField() {
		field := t.Field(i)
		jsonTag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		permTag := field.Tag.Get("perm")
		perms := strings.Split(permTag, ",")
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
			case accesstypes.Permission("Immutable"):
				immutableFields[accesstypes.Tag(jsonTag)] = struct{}{}
				permission = accesstypes.Update
				mutating[permission] = struct{}{}
			default:
				viewing[permission] = struct{}{}
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

	if len(viewing) > 1 {
		return nil, nil, nil, nil, errors.Newf("can not have more then one type of viewing permission in the same struct: found %s", slices.Collect(maps.Keys(viewing)))
	}

	if len(viewing) != 0 && len(mutating) != 0 {
		return nil, nil, nil, nil, errors.Newf("can not have both viewing and mutating permissions in the same struct: found %s and %s", slices.Collect(maps.Keys(viewing)), slices.Collect(maps.Keys(mutating)))
	}

	permissions = slices.Collect(maps.Keys(permissionMap))
	slices.Sort(permissions)

	return tags, fieldToTag, permissions, immutableFields, nil
}

type ResourceMetadata[Resource Resourcer] struct {
	fieldMap            map[accesstypes.Field]cacheEntry
	dbType              dbType
	changeTrackingTable string
	trackChanges        bool
}

func NewResourceMetadata[Resource Resourcer]() *ResourceMetadata[Resource] {
	var res Resource

	// TODO(jwatson): This should come from the Resource type perameter .Config() mehtod.
	tag := "spanner"
	changeTrackingTable := "DataChangeEvents"
	trackChanges := true

	return &ResourceMetadata[Resource]{
		fieldMap:            structTags(reflect.TypeOf(res), tag),
		dbType:              dbType(tag),
		changeTrackingTable: changeTrackingTable,
		trackChanges:        trackChanges,
	}
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