// package resourceset is a set of resources that provides a way to map permissions to fields in a struct.
package resourceset

import (
	"fmt"
	reflect "reflect"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type ResourceSet struct {
	requiredPermission accesstypes.Permission
	requiredTagPerm    accesstypes.TagPermission
	fieldToTag         map[accesstypes.Field]accesstypes.Tag
	resource           accesstypes.Resource
}

func New(v any, resource accesstypes.Resource, requiredPermission accesstypes.Permission) (*ResourceSet, error) {
	requiredTagPerm, fieldToTag, err := permissionsFromTags(v, requiredPermission)
	if err != nil {
		panic(err)
	}

	return &ResourceSet{
		requiredPermission: requiredPermission,
		requiredTagPerm:    requiredTagPerm,
		fieldToTag:         fieldToTag,
		resource:           resource,
	}, nil
}

func (r *ResourceSet) TagPermissions() accesstypes.TagPermission {
	return r.requiredTagPerm
}

func (r *ResourceSet) RequiredPermission() accesstypes.Permission {
	return r.requiredPermission
}

func (r *ResourceSet) PermissionRequired(fieldName accesstypes.Field) bool {
	return r.requiredTagPerm[r.fieldToTag[fieldName]] != accesstypes.NullPermission
}

func (r *ResourceSet) Resource(fieldName accesstypes.Field) accesstypes.Resource {
	return accesstypes.Resource(fmt.Sprintf("%s.%s", r.resource, r.fieldToTag[fieldName]))
}

func permissionsFromTags(v any, permission accesstypes.Permission) (tags accesstypes.TagPermission, fieldToTag map[accesstypes.Field]accesstypes.Tag, err error) {
	vType := reflect.TypeOf(v)
	if vType.Kind() == reflect.Ptr {
		vType = vType.Elem()
	}
	if vType.Kind() != reflect.Struct {
		return nil, nil, errors.Newf("expected a struct, got %s", vType.Kind())
	}

	tags = make(accesstypes.TagPermission)
	fieldToTag = make(map[accesstypes.Field]accesstypes.Tag)
	for i := range vType.NumField() {
		field := vType.Field(i)
		jsonTag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		permTag := field.Tag.Get("perm") // `perm:"required"`
		if permTag == "required" {
			if jsonTag == "" || jsonTag == "-" {
				return nil, nil, errors.Newf("can not set %s permission on the %s field when json tag is empty", permission, field.Name)
			}
			tags[accesstypes.Tag(jsonTag)] = permission
			fieldToTag[accesstypes.Field(field.Name)] = accesstypes.Tag(jsonTag)
		} else if registerAllResources {
			if jsonTag != "" && jsonTag != "-" {
				tags[accesstypes.Tag(jsonTag)] = accesstypes.NullPermission
				fieldToTag[accesstypes.Field(field.Name)] = accesstypes.Tag(jsonTag)
			}
		}
	}

	return tags, fieldToTag, nil
}
