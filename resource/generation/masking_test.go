package generation

// These tests pin the masking declaration at generation time: the tag's values, the
// kinds that refuse it, the contradictions a positional declaration can carry, the
// reserved alias prefix, and the query keys a listed resource hands the collection.

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/google/go-cmp/cmp"
)

// Test_validateMaskingTags pins that an unrecognized masking value is refused naming
// the field and the value, with the nearest recognized one suggested, and that both
// recognized values pass.
func Test_validateMaskingTags(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "hygienefixture"))

	tests := []struct {
		name         string
		structName   string
		wantContains string
	}{
		{name: "a misspelling is refused with a suggestion", structName: "MaskingMisspelled", wantContains: `field MaskingMisspelled.Fee: masking tag value "positonal" is not recognized; did you mean "positional"? (recognized: positional, concealing)`},
		{name: "both recognized values pass", structName: "MaskingClean"},
		{name: "a struct without the tag passes", structName: "Clean"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := structs[tt.structName]
			if s == nil {
				t.Fatalf("struct %q not found in fixture package", tt.structName)
			}
			err := validateMaskingTags(s)
			if (err != nil) != (tt.wantContains != "") {
				t.Fatalf("validateMaskingTags() error = %v, wantErr %v", err, tt.wantContains != "")
			}
			if err != nil && !strings.Contains(err.Error(), tt.wantContains) {
				t.Errorf("validateMaskingTags() error = %v, want it to contain %q", err, tt.wantContains)
			}
		})
	}
}

// Test_rejectMaskingTags pins the kind refusal a method's request answers with.
func Test_rejectMaskingTags(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "hygienefixture"))

	tests := []struct {
		name         string
		structName   string
		wantContains string
	}{
		{name: "a masking tag on a method's request is refused naming the field", structName: "MaskingClean", wantContains: "field MaskingClean.Fee carries the masking tag, which says how a masked cell meets a sort or a filter; an RPC method is never listed or masked"},
		{name: "a struct without the tag passes", structName: "Clean"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := rejectMaskingTags(structs[tt.structName], "RPC method")
			if (err != nil) != (tt.wantContains != "") {
				t.Fatalf("rejectMaskingTags() error = %v, wantErr %v", err, tt.wantContains != "")
			}
			if err != nil && !strings.Contains(err.Error(), tt.wantContains) {
				t.Errorf("rejectMaskingTags() error = %v, want it to contain %q", err, tt.wantContains)
			}
		})
	}
}

// TestMaskingDeclarations pins the contradictions a positional declaration can carry
// on a view (a table answers the same way once its schema fills in the indexes) and
// the refusal a computed resource answers with.
func TestMaskingDeclarations(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "pagingfixture"))

	tests := []struct {
		name           string
		fixture        string
		computed       bool
		wantPositional []string
		wantErr        string
	}{
		{name: "positional on the indexed order column passes, and concealing spelled out is the default", fixture: "PositionalDeck", wantPositional: []string{"Deadline"}},
		{name: "positional on an unindexed field the order names passes", fixture: "PositionalOrderedDeck", wantPositional: []string{"Title"}},
		{name: "positional on an allow_filter field passes", fixture: "PositionalFilterDeck", wantPositional: []string{"Title"}},
		{name: "positional on a primary key is a contradiction", fixture: "PositionalKeyDeck", wantErr: `PositionalKeyDeck.ID: masking:"positional" on a primary key: keys are exempt from masking`},
		{name: "positional on a field no list sorts or filters by with an index is a contradiction", fixture: "PositionalPlainDeck", wantErr: `PositionalPlainDeck.Title: masking:"positional" on a field no list orders or filters by with an index behind it (neither indexed, nor allow_filter, nor named in @order)`},
		{name: "a computed resource never masks", fixture: "PositionalBoard", computed: true, wantErr: "PositionalBoard.Name: the masking tag says how a masked cell meets a sort or a filter, and a computed resource never masks"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			var positional []string
			var err error
			if tt.computed {
				_, err = c.structsToCompResources([]*parser.Struct{structs[tt.fixture]})
			} else {
				var resources []*resourceInfo
				resources, err = c.structsToVirtualResources([]*parser.Struct{structs[tt.fixture]})
				for _, res := range resources {
					for _, f := range res.Fields {
						if f.IsPositional() {
							positional = append(positional, f.Name())
						}
					}
				}
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("extraction error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("extraction error = %v", err)
			}
			if diff := cmp.Diff(tt.wantPositional, positional); diff != "" {
				t.Errorf("positional fields mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_maskingTags pins what a positional field renders into the generated request
// structs and the collection, and what a concealing one leaves out.
func Test_maskingTags(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "pagingfixture"))
	c := &client{}
	resources, err := c.structsToVirtualResources([]*parser.Struct{structs["PositionalDeck"]})
	if err != nil {
		t.Fatalf("structsToVirtualResources() error = %v", err)
	}
	deck := resources[0]

	tests := []struct {
		name    string
		field   string
		wantTag string
	}{
		{name: "a positional field carries the tag", field: "Deadline", wantTag: `masking:"positional"`},
		{name: "a concealing field carries none, spelled out or not", field: "Title"},
		{name: "the primary key carries none", field: "ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, f := range deck.Fields {
				if f.Name() != tt.field {
					continue
				}
				if got := f.MaskingTag(); got != tt.wantTag {
					t.Errorf("MaskingTag() = %q, want %q", got, tt.wantTag)
				}
			}
		})
	}

	order, keys := listQueryKeys(deck)
	if diff := cmp.Diff([]accesstypes.Tag{"deadline"}, order); diff != "" {
		t.Errorf("listQueryKeys() order mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]accesstypes.Tag{"deadline"}, keys); diff != "" {
		t.Errorf("listQueryKeys() keys mismatch (-want +got): the key is excluded, the indexed order column is a key\n%s", diff)
	}

	set, err := handlerSetData(deck, ListHandler)
	if err != nil {
		t.Fatalf("handlerSetData() error = %v", err)
	}
	if diff := cmp.Diff(map[accesstypes.Tag]struct{}{"deadline": {}}, set.PositionalFields); diff != "" {
		t.Errorf("handlerSetData() positional fields mismatch (-want +got):\n%s", diff)
	}
	if _, err := resource.NewSetData([]resource.FieldTags{{Field: "Deadline", JSON: "deadline", Masking: "positional"}}, accesstypes.List); err != nil {
		t.Errorf("NewSetData() on the rendered tag error = %v", err)
	}
}

// Test_reservedRowName pins the reserved names a resource column may not take: the
// three fixed envelope names, case-insensitively, and the positional alias prefix.
func Test_reservedRowName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		column     string
		field      string
		wantHit    bool
		wantNaming string
	}{
		{name: "an ordinary column", column: "Fee", field: "Fee"},
		{name: "the masked-names column", column: "zzMaskedFields", field: "Masked", wantHit: true, wantNaming: "zzMaskedFields"},
		{name: "the capability property by field name, any case", column: "Caps", field: "ZZCAPABILITIES", wantHit: true, wantNaming: "zzCapabilities"},
		{name: "a column beginning with the positional prefix", column: "zzPositionalFee", field: "Fee", wantHit: true, wantNaming: "zzPositional"},
		{name: "a field beginning with the positional prefix, any case", column: "Fee", field: "ZzPositionalKey", wantHit: true, wantNaming: "zzPositional"},
		{name: "the prefix alone is reserved too", column: "zzPositional", field: "Fee", wantHit: true, wantNaming: "zzPositional"},
		{name: "a column merely containing the prefix is not", column: "MyzzPositional", field: "Fee"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			naming, hit := reservedRowName(tt.column, tt.field)
			if hit != tt.wantHit {
				t.Fatalf("reservedRowName(%q, %q) = %t, want %t", tt.column, tt.field, hit, tt.wantHit)
			}
			if hit && !strings.HasPrefix(naming, tt.wantNaming) {
				t.Errorf("reservedRowName(%q, %q) names %q, want it to name %q", tt.column, tt.field, naming, tt.wantNaming)
			}
		})
	}
}
