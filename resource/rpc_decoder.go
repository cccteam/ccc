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
	// collection lets a body's armed writes render conditional grants into their
	// live check; nil when the application generates no collection.
	collection *GeneratedCollection
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

// WithCollection wires the generated collection to the decoder, so the caller it
// stamps can render conditional grants into the live check of a body's armed writes.
func (s *RPCDecoder[Request]) WithCollection(collection *GeneratedCollection) *RPCDecoder[Request] {
	decoder := *s
	decoder.collection = collection

	return &decoder
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
	req, _, err := s.DecodeCaller(request, scope)

	return req, err
}

// DecodeCaller decodes and checks like Decode and also returns the Caller the
// check ran as — checker, scope, and sampled environment — for the handler to stamp
// into the context the method's body runs under.
func (s *RPCDecoder[Request]) DecodeCaller(request *http.Request, scope accesstypes.Scope) (*Request, *Caller, error) {
	req, err := s.d.Decode(request)
	if err != nil {
		return nil, nil, errors.Wrap(err, "resource.StructDecoder.Decode()")
	}

	userPermissions := s.userPermissions(request)
	env := newRequestEnvironment()
	decisions, err := userPermissions.Check(request.Context(), env, scope, s.requiredPermission, s.res)
	if err != nil {
		return nil, nil, errors.Wrap(err, "resource.UserPermissions.Check()")
	}
	if denied := decisions.DeniedResources(); len(denied) > 0 {
		return nil, nil, httpio.NewForbiddenMessagef("user %s, scope %s, does not have %s on %s", userPermissions.User(), scope, s.requiredPermission, denied)
	}
	if conditional := decisions.ConditionalResources(); len(conditional) > 0 {
		return nil, nil, errConditionalAtDecode(s.requiredPermission, conditional)
	}

	return req, &Caller{Permissions: userPermissions, Scope: scope, Env: env, collection: s.collection}, nil
}
