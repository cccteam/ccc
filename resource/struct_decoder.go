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

// StructDecoder is a struct that can be used for decoding http requests and validating those requests
type StructDecoder[Request any] struct {
	validate    ValidatorFunc
	fieldMapper *RequestFieldMapper
	resourceSet *Set[nilResource]
}

// NewStructDecoder creates a new StructDecoder for a given request type.
func NewStructDecoder[Request any]() (*StructDecoder[Request], error) {
	target := new(Request)

	m, err := NewRequestFieldMapper(target)
	if err != nil {
		return nil, errors.Wrap(err, "NewFieldMapper()")
	}

	rSet, err := NewSet[nilResource, Request]()
	if err != nil {
		return nil, errors.Wrap(err, "NewSet()")
	}

	return &StructDecoder[Request]{
		fieldMapper: m,
		resourceSet: rSet,
	}, nil
}

// WithValidator sets a validator function on the decoder.
func (s *StructDecoder[Request]) WithValidator(v ValidatorFunc) *StructDecoder[Request] {
	decoder := *s
	decoder.validate = v

	return &decoder
}

// Decode decodes the HTTP request body into the target Request struct.
func (s *StructDecoder[Request]) Decode(request *http.Request) (*Request, error) {
	_, target, err := decodeToPatch[nilResource, Request](s.resourceSet, s.fieldMapper, request, s.validate, accesstypes.NullPermission)
	if err != nil {
		return nil, err
	}

	return target, nil
}
