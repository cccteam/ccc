package resource

import (
	"net/http"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

type RPCDecoder[Request any] struct {
	d                  *StructDecoder[Request]
	res                accesstypes.Resource
	requiredPermission accesstypes.Permission
	userPermissions    func(*http.Request) UserPermissions
}

func NewRPCDecoder[Request any](userPermissions func(*http.Request) UserPermissions, methodName accesstypes.Resource, perm accesstypes.Permission) (*RPCDecoder[Request], error) {
	decoder, err := NewStructDecoder[Request]()
	if err != nil {
		return nil, errors.Wrap(err, "NewStructDecoder()")
	}

	return &RPCDecoder[Request]{
		d:                  decoder,
		res:                methodName,
		requiredPermission: perm,
		userPermissions:    userPermissions,
	}, nil
}

func (s *RPCDecoder[Request]) WithValidator(v ValidatorFunc) *RPCDecoder[Request] {
	decoder := *s
	decoder.d = s.d.WithValidator(v)

	return &decoder
}

func (r *RPCDecoder[Request]) Decode(request *http.Request) (*Request, error) {
	req, err := r.d.Decode(request)
	if err != nil {
		return nil, errors.Wrap(err, "resource.StructDecoder.Decode()")
	}

	userPermissions := r.userPermissions(request)
	if ok, missing, err := userPermissions.Check(request.Context(), r.requiredPermission, r.res); err != nil {
		return nil, errors.Wrap(err, "enforcer.RequireResource()")
	} else if !ok {
		return nil, httpio.NewForbiddenMessagef("user %s, domain %s, does not have %s on %s", userPermissions.User(), userPermissions.Domain(), r.requiredPermission, missing)
	}

	return req, nil
}
