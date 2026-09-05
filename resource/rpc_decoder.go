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

// MustNewRPCDecoder builds a decoder for an RPC method request, resolving user
// permissions and validating request bodies through the accessor. It panics on
// construction errors: they are programming errors (a malformed request struct),
// surfaced at application startup where generated handlers construct their decoders.
func MustNewRPCDecoder[Request any](a DecoderAccessor, methodName accesstypes.Resource, perm accesstypes.Permission) *RPCDecoder[Request] {
	decoder, err := NewRPCDecoder[Request](a.UserPermissions, methodName, perm)
	if err != nil {
		panic(err)
	}

	return decoder.WithValidator(a.Validator())
}

// WithValidator sets a validator function on the decoder.
func (s *RPCDecoder[Request]) WithValidator(v ValidatorFunc) *RPCDecoder[Request] {
	decoder := *s
	decoder.d = s.d.WithValidator(v)

	return &decoder
}

// Decode decodes the HTTP request body into the Request struct and checks user permissions
// in the given domain partition.
//
// The check is eager (decode is the last library-controlled point before application
// code executes), so it must resolve to Granted or Denied: a Conditional decision here
// is a 500-class invariant breach — an RPC method has no rows for a condition to
// evaluate against, and MigrateRoles rejects such grants at deploy.
func (s *RPCDecoder[Request]) Decode(request *http.Request, scope accesstypes.Scope) (*Request, error) {
	req, err := s.d.Decode(request)
	if err != nil {
		return nil, errors.Wrap(err, "resource.StructDecoder.Decode()")
	}

	userPermissions := s.userPermissions(request)
	decisions, err := userPermissions.Check(request.Context(), newRequestEnvironment(), scope, s.requiredPermission, s.res)
	if err != nil {
		return nil, errors.Wrap(err, "resource.UserPermissions.Check()")
	}
	if denied := decisions.DeniedResources(); len(denied) > 0 {
		return nil, httpio.NewForbiddenMessagef("user %s, scope %s, does not have %s on %s", userPermissions.User(), scope, s.requiredPermission, denied)
	}
	if conditional := decisions.ConditionalResources(); len(conditional) > 0 {
		return nil, errConditionalAtDecode(s.requiredPermission, conditional)
	}

	return req, nil
}
