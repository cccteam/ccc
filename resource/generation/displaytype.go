package generation

import (
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
)

// The display-type vocabulary is one closed set for every field the generated metadata
// describes: a table column, a view column, a computed field, and an RPC field. A
// leaf's display type is its own name lower-cased on every path (a time.Time is date, a
// civil.Date civildate, a ccc.UUID uuid), a nested struct, an imported type, and
// spanner.NullJSON are object, a byte slice is bytes, a nullable BOOL column is
// nullboolean, and a declared or inferred picker is enumerated. A slice of a leaf adds
// [], for every leaf but the two that describe one value alone: nullboolean is the
// tri-state rule for one nullable BOOL column, and a picker stores one key.
//
// The list here is the README's table (section 12) and the client's union
// (ValidDisplayTypes in @cccteam/resource), spelled the same in all three places. The
// templates render a display type through renderDisplayType alone, which refuses
// anything outside the list, so the generator and the client agree by construction and
// never through a type error in the adopter's build.

// displayType is one member of the vocabulary, as the metadata spells it.
type displayType string

// The scalar members.
const (
	displayTypeString      displayType = "string"
	displayTypeNumber      displayType = "number"
	displayTypeBoolean     displayType = "boolean"
	displayTypeNullBoolean displayType = "nullboolean"
	displayTypeDate        displayType = "date"
	displayTypeCivilDate   displayType = "civildate"
	displayTypeUUID        displayType = "uuid"
	displayTypeEnumerated  displayType = "enumerated"
	displayTypeObject      displayType = "object"
	displayTypeBytes       displayType = "bytes"
)

// The members the field methods return by name: a picker, a nullable boolean column,
// and a nested field, an imported type, or a value with no fixed shape.
const (
	enumeratedDisplayType  = string(displayTypeEnumerated)
	nullBooleanDisplayType = string(displayTypeNullBoolean)
	objectDisplayType      = string(displayTypeObject)
)

// scalarDisplayTypes lists the scalar members, in the README's order.
var scalarDisplayTypes = []displayType{
	displayTypeString,
	displayTypeNumber,
	displayTypeBoolean,
	displayTypeNullBoolean,
	displayTypeDate,
	displayTypeCivilDate,
	displayTypeUUID,
	displayTypeEnumerated,
	displayTypeObject,
	displayTypeBytes,
}

// arrayDisplayTypes lists the array members: every scalar but nullboolean and
// enumerated, with the [] suffix.
var arrayDisplayTypes = func() []displayType {
	arrays := make([]displayType, 0, len(scalarDisplayTypes))
	for _, scalar := range scalarDisplayTypes {
		if scalar == displayTypeNullBoolean || scalar == displayTypeEnumerated {
			continue
		}
		arrays = append(arrays, scalar+displayType(sliceSuffix))
	}

	return arrays
}()

// displayTypes is the vocabulary: every member the generator may emit, scalars then
// arrays.
var displayTypes = slices.Concat(scalarDisplayTypes, arrayDisplayTypes)

// renderDisplayType renders a field's display type as the metadata carries it: the
// name the field method returned (a leaf's table spelling, Date or civilDate, or a
// member by name), lower-cased, with its [] kept. A value outside the vocabulary fails
// generation naming it, so a new leaf reaches the client's union before it reaches an
// adopter. It is the templates' one way to render a display type.
func renderDisplayType(raw string) (string, error) {
	rendered := displayType(strings.ToLower(raw))
	if !slices.Contains(displayTypes, rendered) {
		return "", errors.Newf("%q is not a display type; the vocabulary is %s", raw, displayTypeList())
	}

	return string(rendered), nil
}

// displayTypeList spells the vocabulary for a refusal.
func displayTypeList() string {
	names := make([]string, 0, len(displayTypes))
	for _, member := range displayTypes {
		names = append(names, string(member))
	}

	return strings.Join(names, ", ")
}
