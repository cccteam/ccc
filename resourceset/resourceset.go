// package resourceset is a set of resources that provides a way to map permissions to fields in a struct.
package resourceset

import (
	"fmt"
	"maps"
	reflect "reflect"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type ResourceSet struct {
	requiredPermissions []accesstypes.Permission
	requiredTagPerm     accesstypes.TagPermissions
	fieldToTag          map[accesstypes.Field]accesstypes.Tag
	resource            accesstypes.Resource
}

func New(v any, resource accesstypes.Resource) (*ResourceSet, error) {
	requiredTagPerm, fieldToTag, permissions, err := permissionsFromTags(v)
	if err != nil {
		return nil, errors.Wrap(err, "permissionsFromTags()")
	}

	return &ResourceSet{
		requiredPermissions: permissions,
		requiredTagPerm:     requiredTagPerm,
		fieldToTag:          fieldToTag,
		resource:            resource,
	}, nil
}

func (r *ResourceSet) TagPermissions() accesstypes.TagPermissions {
	return r.requiredTagPerm
}

func (r *ResourceSet) RequiredPermission() accesstypes.Permission {
	switch len(r.requiredPermissions) {
	case 0:
		return accesstypes.NullPermission
	case 1:
		return r.requiredPermissions[0]
	default:
		panic("resource set has more than one required permission")
	}
}

func (r *ResourceSet) PermissionRequired(fieldName accesstypes.Field, perm accesstypes.Permission) bool {
	return slices.Contains(r.requiredTagPerm[r.fieldToTag[fieldName]], perm)
}

func (r *ResourceSet) Resource(fieldName accesstypes.Field) accesstypes.Resource {
	return accesstypes.Resource(fmt.Sprintf("%s.%s", r.resource, r.fieldToTag[fieldName]))
}

func (r *ResourceSet) BaseResource() accesstypes.Resource {
	return r.resource
}

func permissionsFromTags(v any) (tags accesstypes.TagPermissions, fieldToTag map[accesstypes.Field]accesstypes.Tag, permissions []accesstypes.Permission, err error) {
	vType := reflect.TypeOf(v)
	if vType.Kind() == reflect.Ptr {
		vType = vType.Elem()
	}
	if vType.Kind() != reflect.Struct {
		return nil, nil, nil, errors.Newf("expected a struct, got %s", vType.Kind())
	}

	tags = make(accesstypes.TagPermissions)
	fieldToTag = make(map[accesstypes.Field]accesstypes.Tag)
	permissionMap := make(map[accesstypes.Permission]struct{})
	mutating := make(map[accesstypes.Permission]struct{})
	viewing := make(map[accesstypes.Permission]struct{})
	for i := range vType.NumField() {
		field := vType.Field(i)
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
				return nil, nil, nil, errors.Newf("delete permission is not allowed in struct tag")
			case accesstypes.Create, accesstypes.Update:
				mutating[permission] = struct{}{}
			case accesstypes.Permission("Immutable"):
				// Store this resource as Immutable
				// TODO(jwatson): Store
				permission = accesstypes.Update
				mutating[permission] = struct{}{}
			default:
				viewing[permission] = struct{}{}
			}

			if jsonTag == "" || jsonTag == "-" {
				return nil, nil, nil, errors.Newf("can not set %s permission on the %s field when json tag is empty", permission, field.Name)
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
		return nil, nil, nil, errors.Newf("can not have more then one type of viewing permission in the same struct: found %s", slices.Collect(maps.Keys(viewing)))
	}

	if len(viewing) != 0 && len(mutating) != 0 {
		return nil, nil, nil, errors.Newf("can not have both viewing and mutating permissions in the same struct: found %s and %s", slices.Collect(maps.Keys(viewing)), slices.Collect(maps.Keys(mutating)))
	}

	return tags, fieldToTag, slices.Collect(maps.Keys(permissionMap)), nil
}
