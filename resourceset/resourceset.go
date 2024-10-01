// package resourceset is a set of resources that provides a way to map permissions to fields in a struct.
package resourceset

import (
	"fmt"
	reflect "reflect"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type ResourceSet struct {
	requiredPermission accesstypes.Permission
	requiredFieldPerm  accesstypes.FieldPermission
	resource           accesstypes.Resource
}

func New(v any, resource accesstypes.Resource, requiredPermission accesstypes.Permission) (*ResourceSet, error) {
	requiredPermFields, err := permissionsFromTags(v, requiredPermission)
	if err != nil {
		panic(err)
	}

	return &ResourceSet{
		requiredPermission: requiredPermission,
		requiredFieldPerm:  requiredPermFields,
		resource:           resource,
	}, nil
}

func (r *ResourceSet) FieldPermissions() accesstypes.FieldPermission {
	return r.requiredFieldPerm
}

func (r *ResourceSet) RequiredPermission() accesstypes.Permission {
	return r.requiredPermission
}

func (r *ResourceSet) PermissionRequired(fieldName accesstypes.Field) bool {
	if r.requiredFieldPerm[fieldName] != accesstypes.NullPermission {
		return true
	}

	return false
}

func (r *ResourceSet) Resource(fieldName accesstypes.Field) accesstypes.Resource {
	return accesstypes.Resource(fmt.Sprintf("%s.%s", r.resource, fieldName))
}

func permissionsFromTags(v any, permission accesstypes.Permission) (fields accesstypes.FieldPermission, err error) {
	vType := reflect.TypeOf(v)
	if vType.Kind() == reflect.Ptr {
		vType = vType.Elem()
	}
	if vType.Kind() != reflect.Struct {
		return nil, errors.Newf("expected a struct, got %s", vType.Kind())
	}

	fields = make(accesstypes.FieldPermission)
	for i := range vType.NumField() {
		field := vType.Field(i)
		tagList := field.Tag.Get("perm") // `perm:"required"`
		if tagList == "required" {
			fields[accesstypes.Field(field.Name)] = permission
		} else if registerAllResources {
			fields[accesstypes.Field(field.Name)] = accesstypes.NullPermission
		}
	}

	return fields, nil
}
