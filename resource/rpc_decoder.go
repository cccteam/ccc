package resource

import (
	"net/http"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

// RPCDecoder decodes an HTTP request for an RPC-style endpoint, validates the request body,
// and enforces permissions for the RPC method.
type RPCDecoder[Request any] struct {
	d                  *StructDecoder[Request]
	res                accesstypes.Resource
	requiredPermission accesstypes.Permission
	userPermissions    func(*http.Request) UserPermissions
}

// NewRPCDecoder creates a new RPCDecoder for a given request type, method name, and required permission.
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

// WithValidator sets a validator function on the decoder.
func (s *RPCDecoder[Request]) WithValidator(v ValidatorFunc) *RPCDecoder[Request] {
	decoder := *s
	decoder.d = s.d.WithValidator(v)

	return &decoder
}

// Decode decodes the HTTP request body into the Request struct and checks user permissions.
func (s *RPCDecoder[Request]) Decode(request *http.Request) (*Request, error) {
	req, err := s.d.Decode(request)
	if err != nil {
		return nil, errors.Wrap(err, "resource.StructDecoder.Decode()")
	}

	userPermissions := s.userPermissions(request)
	if ok, missing, err := userPermissions.Check(request.Context(), s.requiredPermission, s.res); err != nil {
		return nil, errors.Wrap(err, "enforcer.RequireResource()")
	} else if !ok {
		return nil, httpio.NewForbiddenMessagef("user %s, domain %s, does not have %s on %s", userPermissions.User(), userPermissions.Domain(), s.requiredPermission, missing)
	}

	return req, nil
}
