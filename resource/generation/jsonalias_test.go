package generation

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/cccteam/ccc/resource"
)

// Go 1.27 made json.RawMessage an alias of encoding/json/jsontext.Value: the generator
// knows the JSON value type under both names, and spells an alias as the source did.
func Test_jsonValueTypeNames(t *testing.T) {
	t.Parallel()

	jsontext := types.NewPackage("encoding/json/jsontext", "jsontext")
	value := types.NewNamed(types.NewTypeName(token.NoPos, jsontext, "Value", nil), types.NewSlice(types.Typ[types.Byte]), nil)
	jsonPkg := types.NewPackage("encoding/json", "json")
	rawMessage := types.NewAlias(types.NewTypeName(token.NoPos, jsonPkg, "RawMessage", nil), value)
	number := types.NewNamed(types.NewTypeName(token.NoPos, jsonPkg, "Number", nil), types.Typ[types.String], nil)
	app := types.NewPackage("example.com/app", "app")

	if !isJSONValueType(value) {
		t.Error("isJSONValueType(jsontext.Value) = false, want true")
	}
	if isJSONValueType(number) {
		t.Error("isJSONValueType(json.Number) = true, want false")
	}
	if !isRawMessage(rawMessage) {
		t.Error("isRawMessage(the json.RawMessage alias) = false, want true")
	}
	if kind, slice := valueKindOf(rawMessage); kind != resource.ValueKindOther || slice {
		t.Errorf("valueKindOf(the json.RawMessage alias) = (%v, %v), want (%v, false)", kind, slice, resource.ValueKindOther)
	}
	if got := types.TypeString(rawMessage, qualifierOutside(app)); got != "json.RawMessage" {
		t.Errorf("TypeString(the alias) = %q, want json.RawMessage", got)
	}
	if imports := packageImports(rawMessage, app); len(imports) != 1 || imports[0].path != "encoding/json" || imports[0].name != "json" {
		t.Errorf("packageImports(the alias) = %+v, want encoding/json alone", imports)
	}
}
