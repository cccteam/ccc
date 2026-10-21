package resource

import (
	"net/http"

	"github.com/go-playground/errors/v5"
)

// BodyDecoder decodes a JSON request body into a request struct and validates it, for a
// generated route that serves no resource and no method of the collection and so meets
// no permission gate: the create-your-tenant route, whose gate is the session holding
// no tenant. The field tags the generated patch requests carry (json, sqltype,
// nullable) are read the same way, so a value the column cannot hold answers 400
// naming the field, and the validator the application supplies runs over the struct's
// validate tags.
type BodyDecoder[Request any] struct {
	d *structDecoder[Request]
}

// NewBodyDecoder builds the decoder for a request type, which must be a struct, with
// the application's validator.
func NewBodyDecoder[Request any](validate ValidatorFunc) (*BodyDecoder[Request], error) {
	d, err := newStructDecoder[Request]()
	if err != nil {
		return nil, errors.Wrap(err, "newStructDecoder()")
	}

	return &BodyDecoder[Request]{d: d.WithValidator(validate)}, nil
}

// MustNewBodyDecoder is NewBodyDecoder for generated code, which builds its decoders
// where the application wires its handlers: a request type the decoder cannot read is a
// programming error surfaced there.
func MustNewBodyDecoder[Request any](validate ValidatorFunc) *BodyDecoder[Request] {
	d, err := NewBodyDecoder[Request](validate)
	if err != nil {
		panic(err)
	}

	return d
}

// Decode decodes the request's body into the request type and validates it.
func (b *BodyDecoder[Request]) Decode(r *http.Request) (*Request, error) {
	req, err := b.d.Decode(r)
	if err != nil {
		return nil, errors.Wrap(err, "structDecoder.Decode()")
	}

	return req, nil
}
