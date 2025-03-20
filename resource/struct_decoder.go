package resource

import (
	"context"
	"net/http"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

type nilResource struct{}

func (n nilResource) Resource() accesstypes.Resource {
	return "nil"
}

func (n nilResource) DefaultConfig() Config {
	return Config{}
}

// StructDecoder is a struct that can be used for decoding http requests and validating those requests
type StructDecoder[Request any] struct {
	validate    ValidatorFunc
	fieldMapper *RequestFieldMapper
	resourceSet *ResourceSet[nilResource]
}

func NewStructDecoder[Request any](permissions ...accesstypes.Permission) (*StructDecoder[Request], error) {
	target := new(Request)

	m, err := NewRequestFieldMapper(target)
	if err != nil {
		return nil, errors.Wrap(err, "NewFieldMapper()")
	}

	rSet, err := NewResourceSet[nilResource, Request](permissions...)
	if err != nil {
		return nil, errors.Wrap(err, "NewResourceSet()")
	}

	return &StructDecoder[Request]{
		fieldMapper: m,
		resourceSet: rSet,
	}, nil
}

func (d *StructDecoder[Request]) WithValidator(v ValidatorFunc) *StructDecoder[Request] {
	decoder := *d
	decoder.validate = v

	return &decoder
}

func (d *StructDecoder[Request]) DecodeWithoutPermissions(request *http.Request) (*Request, error) {
	_, target, err := decodeToPatch[nilResource, Request](d.resourceSet, d.fieldMapper, request, d.validate)
	if err != nil {
		return nil, err
	}

	return target, nil
}

func (d *StructDecoder[Request]) Decode(request *http.Request, userPermissions UserPermissions, requiredPermission accesstypes.Permission) (*Request, error) {
	p, target, err := decodeToPatch[nilResource, Request](d.resourceSet, d.fieldMapper, request, d.validate)
	if err != nil {
		return nil, err
	}

	if err := checkPermissions(request.Context(), p.Fields(), d.resourceSet, userPermissions, requiredPermission); err != nil {
		return nil, err
	}

	return target, nil
}

func checkPermissions[Resource Resourcer](
	ctx context.Context, fields []accesstypes.Field, rSet *ResourceSet[Resource], userPermissions UserPermissions, perm accesstypes.Permission,
) error {
	resources := make([]accesstypes.Resource, 0, len(fields)+1)
	resources = append(resources, rSet.BaseResource())
	for _, fieldName := range fields {
		if rSet.PermissionRequired(fieldName, perm) {
			resources = append(resources, rSet.Resource(fieldName))
		}
	}

	if ok, missing, err := userPermissions.Check(ctx, perm, resources...); err != nil {
		return errors.Wrap(err, "enforcer.RequireResource()")
	} else if !ok {
		return httpio.NewForbiddenMessagef("user %s, domain %s, does not have %s on %s", userPermissions.User(), userPermissions.Domain(), perm, missing)
	}

	return nil
}
