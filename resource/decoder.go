package resource

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

// ValidatorFunc is a function that validates s
// It returns an error if the validation fails
type ValidatorFunc interface {
	Struct(s interface{}) error
	StructPartial(s interface{}, fields ...string) error
}

type (
	DomainFromReq func(*http.Request) accesstypes.Domain
	UserFromReq   func(*http.Request) accesstypes.User
)

// Decoder is a struct that can be used for decoding http requests and validating those requests
type Decoder[Resource Resourcer, Request any] struct {
	validate    ValidatorFunc
	fieldMapper *RequestFieldMapper
	resourceSet *ResourceSet[Resource, Request]
}

func NewDecoder[Resource Resourcer, Request any](rSet *ResourceSet[Resource, Request]) (*Decoder[Resource, Request], error) {
	target := new(Request)
	m, err := NewRequestFieldMapper(target)
	if err != nil {
		return nil, errors.Wrap(err, "NewFieldMapper()")
	}

	return &Decoder[Resource, Request]{
		fieldMapper: m,
		resourceSet: rSet,
	}, nil
}

func (d *Decoder[Resource, Request]) WithValidator(v ValidatorFunc) *Decoder[Resource, Request] {
	decoder := *d
	decoder.validate = v

	return &decoder
}

func (d *Decoder[Resource, Request]) Decode(request *http.Request) (*PatchSet[Resource], error) {
	p, _, err := decodeToPatch(d.resourceSet, d.fieldMapper, request, d.validate)
	if err != nil {
		return nil, err
	}

	return p, nil
}

func (d *Decoder[Resource, Request]) DecodeOperation(oper *Operation) (*PatchSet[Resource], error) {
	patchSet, err := d.Decode(oper.Req)
	if err != nil {
		return nil, errors.Wrap(err, "httpio.DecoderWithPermissionChecker[Request].Decode()")
	}

	return patchSet, nil
}

func decodeToPatch[Resource Resourcer, Request any](rSet *ResourceSet[Resource, Request], fieldMapper *RequestFieldMapper, req *http.Request, validate ValidatorFunc) (*PatchSet[Resource], *Request, error) {
	request := new(Request)
	pr, pw := io.Pipe()
	tr := io.TeeReader(req.Body, pw)

	var wg sync.WaitGroup
	var err error
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = json.NewDecoder(pr).Decode(request)
	}()

	jsonData := make(map[string]any)
	if err := json.NewDecoder(tr).Decode(&jsonData); err != nil {
		return nil, nil, httpio.NewBadRequestMessageWithError(err, "failed to decode request body")
	}

	wg.Wait()
	if err != nil {
		return nil, nil, httpio.NewBadRequestMessageWithError(err, "failed to unmarshal request body")
	}

	vValue := reflect.ValueOf(request)
	if vValue.Kind() == reflect.Ptr {
		vValue = vValue.Elem()
	}

	changes := make(map[accesstypes.Field]any)
	for jsonField := range jsonData {
		fieldName, ok := fieldMapper.StructFieldName(jsonField)
		if !ok {
			fieldName, ok = fieldMapper.StructFieldName(strings.ToLower(jsonField))
			if !ok {
				return nil, nil, httpio.NewBadRequestMessagef("invalid field in json - %s", jsonField)
			}
		}

		value := vValue.FieldByName(string(fieldName)).Interface()
		if value == nil {
			return nil, nil, httpio.NewBadRequestMessagef("invalid field in json - %s", jsonField)
		}

		if _, ok := changes[fieldName]; ok {
			return nil, nil, httpio.NewBadRequestMessagef("json field name %s collides with another field name of different case", fieldName)
		}
		changes[fieldName] = value
	}

	patchSet := NewPatchSet(rSet.ResourceMetadata())
	// Add to patchset in order of struct fields
	// Every key in changes is guaranteed to be a field in the struct
	for _, f := range reflect.VisibleFields(vValue.Type()) {
		field := accesstypes.Field(f.Name)
		if value, ok := changes[field]; ok {
			patchSet.Set(field, value)
		}
	}

	if validate != nil {
		switch req.Method {
		case http.MethodPatch:
			fields := make([]string, 0, patchSet.Len())
			for _, field := range patchSet.Fields() {
				fields = append(fields, string(field))
			}
			if err := validate.StructPartial(request, fields...); err != nil {
				return nil, nil, httpio.NewBadRequestMessageWithError(err, "failed validating the request")
			}
		default:
			if err := validate.Struct(request); err != nil {
				return nil, nil, httpio.NewBadRequestMessageWithError(err, "failed validating the request")
			}
		}
	}

	return patchSet, request, nil
}
