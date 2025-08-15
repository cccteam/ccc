package resource

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
	guid "github.com/google/uuid"
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
	resourceSet *ResourceSet[Resource]
}

func NewDecoder[Resource Resourcer, Request any](rSet *ResourceSet[Resource]) (*Decoder[Resource, Request], error) {
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

func (d *Decoder[Resource, Request]) DecodeWithoutPermissions(request *http.Request) (*PatchSet[Resource], error) {
	p, _, err := decodeToPatch[Resource, Request](d.resourceSet, d.fieldMapper, request, d.validate, accesstypes.NullPermission)
	if err != nil {
		return nil, err
	}

	return p, nil
}

func (d *Decoder[Resource, Request]) Decode(request *http.Request, userPermissions UserPermissions, requiredPermission accesstypes.Permission) (*PatchSet[Resource], error) {
	p, _, err := decodeToPatch[Resource, Request](d.resourceSet, d.fieldMapper, request, d.validate, requiredPermission)
	if err != nil {
		return nil, err
	}

	p.EnableUserPermissionEnforcement(d.resourceSet, userPermissions, requiredPermission)

	return p, nil
}

func (d *Decoder[Resource, Request]) DecodeOperationWithoutPermissions(oper *Operation) (*PatchSet[Resource], error) {
	if oper.Type == OperationDelete {
		return NewPatchSet(d.resourceSet.ResourceMetadata()), nil
	}

	patchSet, err := d.DecodeWithoutPermissions(oper.Req)
	if err != nil {
		return nil, errors.Wrap(err, "httpio.DecoderWithPermissionChecker[Request].Decode()")
	}

	return patchSet, nil
}

func (d *Decoder[Resource, Request]) DecodeOperation(oper *Operation, userPermissions UserPermissions) (*PatchSet[Resource], error) {
	if oper.Type == OperationDelete {
		return NewPatchSet(d.resourceSet.ResourceMetadata()).EnableUserPermissionEnforcement(d.resourceSet, userPermissions, permissionFromType(oper.Type)), nil
	}

	patchSet, err := d.Decode(oper.Req, userPermissions, permissionFromType(oper.Type))
	if err != nil {
		return nil, errors.Wrap(err, "httpio.DecoderWithPermissionChecker[Request].Decode()")
	}

	return patchSet, nil
}

func decodeToPatch[Resource Resourcer, Request any](rSet *ResourceSet[Resource], fieldMapper *RequestFieldMapper, req *http.Request, validate ValidatorFunc, operationPerm accesstypes.Permission) (*PatchSet[Resource], *Request, error) {
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
	for jsonField, jsonValue := range jsonData {
		if operationPerm == accesstypes.Update {
			if _, found := rSet.immutableFields[accesstypes.Tag(jsonField)]; found {
				return nil, nil, httpio.NewBadRequestMessagef("json field %s is immutable", jsonField)
			}
		}

		fieldName, ok := fieldMapper.StructFieldName(jsonField)
		if !ok {
			fieldName, ok = fieldMapper.StructFieldName(strings.ToLower(jsonField))
			if !ok {
				return nil, nil, httpio.NewBadRequestMessagef("invalid field in json - %s", jsonField)
			}
		}

		if _, ok := changes[fieldName]; ok {
			return nil, nil, httpio.NewBadRequestMessagef("json field name %s collides with another field name of different case", fieldName)
		}

		field := vValue.FieldByName(string(fieldName))
		value := field.Interface()
		switch jsonValue.(type) {
		case nil:
			if field.Kind() != reflect.Ptr {
				switch value.(type) {
				// Taken from cloud.google.com/go/spanner@v1.83.0/value.go
				// these types are handled by the driver
				case spanner.NullInt64, spanner.NullFloat64, spanner.NullFloat32, spanner.NullBool,
					spanner.NullString, spanner.NullTime, spanner.NullDate, spanner.NullNumeric,
					spanner.NullProtoEnum, spanner.NullUUID, guid.NullUUID, spanner.Encoder:
				default:
					return nil, nil, httpio.NewBadRequestMessagef(`%s cannot be null`, jsonField)
				}
			}
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
