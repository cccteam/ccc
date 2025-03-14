package resource

import (
	"net/http"

	"github.com/cccteam/ccc/accesstypes"
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
	resourceSet *ResourceSet[nilResource, Request]
}

func NewStructDecoder[Request any]() (*StructDecoder[Request], error) {
	target := new(Request)

	m, err := NewRequestFieldMapper(target)
	if err != nil {
		return nil, errors.Wrap(err, "NewFieldMapper()")
	}

	rSet, err := NewResourceSet[nilResource, Request]()
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

// Decode parses the http request body and validates it against the struct validation rules
// and returns a named patchset
func (d *StructDecoder[Request]) Decode(request *http.Request) (*Request, error) {
	_, target, err := decodeToPatch(d.resourceSet, d.fieldMapper, request, d.validate)
	if err != nil {
		return nil, err
	}

	return target, nil
}
