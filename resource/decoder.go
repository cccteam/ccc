package resource

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
	guid "github.com/google/uuid"
)

// ValidatorFunc is the interface used to validate a struct, in whole or in part.
// Its methods return an error if the validation fails.
type ValidatorFunc interface {
	Struct(s any) error
	StructPartial(s any, fields ...string) error
}

// DecoderAccessor is the application seam the Must* decoder constructors draw on:
// the request validator and the per-request user permissions. Generated application
// code implements and consumes it via the generated decoder constructors.
type DecoderAccessor interface {
	Validator() ValidatorFunc
	UserPermissions(r *http.Request) UserPermissions
}

// Decoder is a struct that can be used for decoding http requests and validating those requests
type Decoder[Resource Resourcer, Request any] struct {
	validate    ValidatorFunc
	fieldMapper *RequestFieldMapper
	resourceSet *Set[Resource]

	// collection resolves condition rendering for the mutations' live
	// check-SELECT; nil leaves conditions unrenderable (an error if one ever
	// arrives).
	collection *GeneratedCollection
	// features is the application's FeatureSet (WithFeatures): a request field behind
	// a flag that is off is unknown to the decoder, so a body naming it is refused as
	// any unknown field is. Nil hides every gated field.
	features *FeatureSet
}

// WithFeatures installs the FeatureSet the decoder reads the gated fields' flags
// from; the generated wiring passes the application's. Without it every gated field
// stays hidden.
func (d *Decoder[Resource, Request]) WithFeatures(features *FeatureSet) *Decoder[Resource, Request] {
	decoder := *d
	decoder.features = features

	return &decoder
}

// NewDecoder creates a new Decoder for a given Resource and Request type.
func NewDecoder[Resource Resourcer, Request any](rSet *Set[Resource]) (*Decoder[Resource, Request], error) {
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

// MustNewDecoder builds a patch decoder for a resource and request pair, validating
// requests with the accessor's validator and wired to the application's generated
// collection so conditional grants can render into the mutations' live check. It
// panics on construction errors: they are programming errors (a request struct out of
// sync with its resource), surfaced at application startup where generated handlers
// construct their decoders.
func MustNewDecoder[Resource Resourcer, Request any](a DecoderAccessor, collection *GeneratedCollection, permissions ...accesstypes.Permission) *Decoder[Resource, Request] {
	rSet, err := NewSet[Resource, Request](permissions...)
	if err != nil {
		panic(err)
	}

	decoder, err := NewDecoder[Resource, Request](rSet)
	if err != nil {
		panic(err)
	}
	decoder.collection = collection

	return decoder.WithValidator(a.Validator())
}

// WithValidator sets the validator function for the Decoder.
func (d *Decoder[Resource, Request]) WithValidator(v ValidatorFunc) *Decoder[Resource, Request] {
	decoder := *d
	decoder.validate = v

	return &decoder
}

// DecodeWithoutPermissions decodes an http.Request into a PatchSet without enforcing any user permissions.
func (d *Decoder[Resource, Request]) DecodeWithoutPermissions(request *http.Request) (*PatchSet[Resource], error) {
	p, _, err := decodeToPatch[Resource, Request](d.resourceSet, d.fieldMapper, request, d.validate, accesstypes.NullPermission, d.resourceSet.hiddenFields(d.features))
	if err != nil {
		return nil, err
	}
	p.querySet.collection = d.collection

	return p, nil
}

// Decode decodes an http.Request into a PatchSet and enables user permission enforcement
// in the given domain partition.
func (d *Decoder[Resource, Request]) Decode(request *http.Request, userPermissions UserPermissions, scope accesstypes.Scope, requiredPermission accesstypes.Permission) (*PatchSet[Resource], error) {
	p, _, err := decodeToPatch[Resource, Request](d.resourceSet, d.fieldMapper, request, d.validate, requiredPermission, d.resourceSet.hiddenFields(d.features))
	if err != nil {
		return nil, err
	}
	p.querySet.collection = d.collection

	p.EnableUserPermissionEnforcement(d.resourceSet, userPermissions, scope, requiredPermission)

	// Structural tenancy: a create's tenant key is stamped from the request's
	// domain partition — the wire cannot express it (design plan §06).
	if requiredPermission == accesstypes.Create {
		if err := p.stampTenantKey(); err != nil {
			return nil, err
		}
	}

	return p, nil
}

// DecodeOperationWithoutPermissions decodes an Operation into a PatchSet without enforcing user permissions.
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

// DecodeOperation decodes an Operation into a PatchSet and enables user permission
// enforcement in the given domain partition.
func (d *Decoder[Resource, Request]) DecodeOperation(oper *Operation, userPermissions UserPermissions, scope accesstypes.Scope) (*PatchSet[Resource], error) {
	if oper.Type == OperationDelete {
		patchSet := NewPatchSet(d.resourceSet.ResourceMetadata())
		patchSet.querySet.env = RequestEnvironment()
		patchSet.querySet.collection = d.collection

		return patchSet.EnableUserPermissionEnforcement(d.resourceSet, userPermissions, scope, permissionFromType(oper.Type)), nil
	}

	patchSet, err := d.Decode(oper.Req, userPermissions, scope, permissionFromType(oper.Type))
	if err != nil {
		return nil, errors.Wrap(err, "httpio.DecoderWithPermissionChecker[Request].Decode()")
	}

	return patchSet, nil
}

// acceptsNull reports whether a JSON null may land in the request field: a pointer, one
// of the Spanner client's Null wrappers, or a slice the generator marked nullable
// because its column allows NULL (nullable_fields.go). The value stored is then the
// field's zero, a nil pointer, an invalid wrapper, or a nil slice, each of which the
// client writes as NULL. An unmarked slice refuses null like every other field whose
// type has no null form: the column behind it is NOT NULL.
func acceptsNull(nullableFields map[accesstypes.Field]struct{}, fieldName accesstypes.Field, field reflect.Value) bool {
	if field.Kind() == reflect.Pointer {
		return true
	}
	switch field.Interface().(type) {
	// Taken from cloud.google.com/go/spanner@v1.83.0/value.go
	// these types are handled by the driver
	case spanner.NullInt64, spanner.NullFloat64, spanner.NullFloat32, spanner.NullBool,
		spanner.NullString, spanner.NullTime, spanner.NullDate, spanner.NullNumeric,
		spanner.NullProtoEnum, spanner.NullUUID, guid.NullUUID, spanner.Encoder:
		return true
	default:
	}
	if field.Kind() != reflect.Slice {
		return false
	}
	_, nullable := nullableFields[fieldName]

	return nullable
}

// nullLiteral is the JSON null as the map decode holds it.
const nullLiteral = "null"

// readBody reads the whole request body. encoding/json holds a complete value in memory
// before it decodes it, so reading the body once ahead of the two decodes costs no memory
// a streaming decode would have saved, and the two decodes then share one copy with no
// pipe and no goroutine between them. It also ends a body that is a bare scalar (null,
// true, a number, a string) at EOF, where a decoder reading a stream waits for a byte
// that never comes. The buffer grows as bytes arrive; nothing is sized from a header the
// client wrote.
func readBody(req *http.Request) ([]byte, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, httpio.NewBadRequestMessageWithError(err, "failed to read request body")
	}

	return body, nil
}

// decodeBody reads the body once and parses it twice: the map pass into the keys the
// body names, each value held raw, and the typed pass into a new Request.
func decodeBody[Request any](fieldMapper *RequestFieldMapper, req *http.Request) (keys map[string]json.RawMessage, request *Request, err error) {
	body, err := readBody(req)
	if err != nil {
		return nil, nil, err
	}
	// A field's former wire name is rewritten to its current one before either decode
	// below, so the map decode and the typed decode see one name.
	if former := fieldMapper.FormerNames(); len(former) > 0 {
		body, err = rewriteFormerKeys(body, former)
		if err != nil {
			return nil, nil, err
		}
	}

	// The map decode sees every key the body names and whether its value is null, and
	// nothing more: RawMessage keeps it to one scan and a copy of each value, with none
	// of the values built. The typed decode then fills the struct from the same bytes.
	keys = make(map[string]json.RawMessage)
	if err = json.Unmarshal(body, &keys); err != nil {
		return nil, nil, httpio.NewBadRequestMessageWithError(err, "failed to decode request body")
	}
	if keys == nil {
		// A body of null decodes into a map as nil and into a struct as nothing at all:
		// it is not an object and says nothing about any field.
		return nil, nil, httpio.NewBadRequestMessage("failed to decode request body")
	}

	request = new(Request)
	if err = json.Unmarshal(body, request); err != nil {
		return nil, nil, httpio.NewBadRequestMessageWithError(err, "failed to unmarshal request body")
	}

	return keys, request, nil
}

// decodeToPatch decodes the body into the request struct and a patch set. hidden names
// the request fields behind a feature flag that is off: a body naming one is refused
// as it would be for a field the struct does not declare.
func decodeToPatch[Resource Resourcer, Request any](rSet *Set[Resource], fieldMapper *RequestFieldMapper, req *http.Request, validate ValidatorFunc, operationPerm accesstypes.Permission, hidden map[accesstypes.Field]struct{}) (*PatchSet[Resource], *Request, error) {
	jsonData, request, err := decodeBody[Request](fieldMapper, req)
	if err != nil {
		return nil, nil, err
	}

	vValue := reflect.ValueOf(request)
	if vValue.Kind() == reflect.Pointer {
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
		if _, isHidden := hidden[fieldName]; isHidden {
			return nil, nil, httpio.NewBadRequestMessagef("invalid field in json - %s", jsonField)
		}

		if _, ok := changes[fieldName]; ok {
			return nil, nil, httpio.NewBadRequestMessagef("json field name %s collides with another field name of different case", fieldName)
		}

		field := vValue.FieldByName(string(fieldName))
		value := field.Interface()
		if string(jsonValue) == nullLiteral && !acceptsNull(rSet.nullableFields, fieldName, field) {
			return nil, nil, httpio.NewBadRequestMessagef(`%s cannot be null`, jsonField)
		}
		changes[fieldName] = value
	}

	// A value the column cannot hold is refused here, naming every such field, before
	// the validator runs and before any permission is checked: the limit is a fact about
	// the wire value alone, like a null into a non-nullable field.
	if err := checkValueLimits(rSet.valueLimits, vValue.Type(), changes); err != nil {
		return nil, nil, err
	}

	patchSet := NewPatchSet(rSet.ResourceMetadata())
	patchSet.querySet.env = RequestEnvironment()
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
