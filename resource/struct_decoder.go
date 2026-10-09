package resource

import (
	"net/http"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

type nilResource struct{}

func (n nilResource) Resource() accesstypes.Resource {
	return "nilResources"
}

func (n nilResource) DefaultConfig() Config {
	return Config{}
}

// structDecoder decodes a request body into a plain struct through decodeToPatch, reading
// the generated struct tags (immutable and nullable fields, former names, value limits)
// with no permission registrations. It backs the RPC decoders (RPCDecoder,
// TargetedRPCDecoder, the execute gate) and the list filter body. It is not exported: a
// plain request body outside this package decodes with httpio's StructDecoder, and a body
// bound to no resource cannot be decoded through this one by mistake.
type structDecoder[Request any] struct {
	validate    ValidatorFunc
	fieldMapper *RequestFieldMapper
	resourceSet *Set[nilResource]
}

// newStructDecoder creates a structDecoder for a request type, which must be a struct.
func newStructDecoder[Request any]() (*structDecoder[Request], error) {
	target := new(Request)

	m, err := NewRequestFieldMapper(target)
	if err != nil {
		return nil, errors.Wrap(err, "NewFieldMapper()")
	}

	rSet, err := newUnenforcedSet[nilResource, Request]()
	if err != nil {
		return nil, errors.Wrap(err, "newUnenforcedSet()")
	}

	return &structDecoder[Request]{
		fieldMapper: m,
		resourceSet: rSet,
	}, nil
}

// WithValidator sets a validator function on the decoder.
func (s *structDecoder[Request]) WithValidator(v ValidatorFunc) *structDecoder[Request] {
	decoder := *s
	decoder.validate = v

	return &decoder
}

// Decode decodes the HTTP request body into the target Request struct.
func (s *structDecoder[Request]) Decode(request *http.Request) (*Request, error) {
	_, target, err := decodeToPatch[nilResource, Request](s.resourceSet, s.fieldMapper, request, s.validate, accesstypes.NullPermission, nil)
	if err != nil {
		return nil, err
	}

	return target, nil
}
