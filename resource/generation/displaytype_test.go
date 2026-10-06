package generation

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Test_renderDisplayType pins the one way a display type reaches the metadata: a member
// renders as itself, a leaf's table spelling lower-cases into its member with its []
// kept, and anything outside the vocabulary fails naming the value, the two arrays the
// vocabulary leaves out included.
func Test_renderDisplayType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "a member renders as itself", raw: "string", want: "string"},
		{name: "the timestamp leaf lower-cases", raw: "Date", want: "date"},
		{name: "the civil date leaf lower-cases", raw: "civilDate", want: "civildate"},
		{name: "a list keeps its suffix", raw: "uuid[]", want: "uuid[]"},
		{name: "a list of the civil date leaf lower-cases", raw: "civilDate[]", want: "civildate[]"},
		{name: "a picker is a member", raw: "enumerated", want: "enumerated"},
		{name: "a nullable boolean column is a member", raw: "nullboolean", want: "nullboolean"},
		{name: "a name outside the vocabulary is refused", raw: "link", wantErr: true},
		{name: "an empty name is refused", raw: "", wantErr: true},
		{name: "a list of nullable booleans is refused", raw: "nullboolean[]", wantErr: true},
		{name: "a list of pickers is refused", raw: "enumerated[]", wantErr: true},
		{name: "the interface type of a value with no fixed shape is refused", raw: "unknown", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := renderDisplayType(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("renderDisplayType(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), `"`+tt.raw+`" is not a display type`) {
				t.Errorf("renderDisplayType(%q) error = %v, want it to name the value", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("renderDisplayType(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// vocabularyLeaves are the leaves a field may resolve to, as the built-in table and the
// declaration reader spell them: the table's rows by their TypeScript type, a value with
// no fixed shape, and an imported declaration.
var vocabularyLeaves = []struct {
	name string
	ts   string
	imp  *tsImport
}{
	{name: "string", ts: stringTSType},
	{name: "number", ts: numberTSType},
	{name: "boolean", ts: booleanStr},
	{name: "Date", ts: dateTSType},
	{name: "civilDate", ts: civilDateTSType},
	{name: "uuid", ts: uuidTSType},
	{name: "bytes", ts: bytesTSType},
	{name: "unknown", ts: unknownTSType},
	{name: "imported", ts: "Point", imp: &tsImport{Name: "Point", From: "geojson"}},
}

// Test_displayTypeVocabulary pins the closed set: every path (a table or view column,
// a computed field, an RPC field) emits, for every leaf as a scalar and as a slice, for
// a nested struct, for a picker, and for a nullable boolean column, a member of the
// vocabulary and nothing else, and together they emit every member, so the list, the
// README's table, and the client's union describe exactly what a run can produce.
func Test_displayTypeVocabulary(t *testing.T) {
	t.Parallel()

	wireFields := func(slice bool) []*wireField {
		fields := make([]*wireField, 0, len(vocabularyLeaves)+1)
		for _, leaf := range vocabularyLeaves {
			fields = append(fields, &wireField{Name: leaf.name, tsLeaf: leaf.ts, tsImport: leaf.imp, Slice: slice})
		}

		return append(fields, &wireField{Name: "nested", Nested: &wireShape{Mirror: "reading"}, Slice: slice})
	}

	tests := []struct {
		name string
		emit func() []string
	}{
		{
			name: "the RPC path",
			emit: func() []string {
				var raw []string
				for _, slice := range []bool{false, true} {
					for _, wf := range wireFields(slice) {
						raw = append(raw, (&rpcField{wire: wf}).TypescriptDisplayType())
					}
				}

				return append(raw, (&rpcField{enumeratedResource: "Gadgets"}).TypescriptDisplayType())
			},
		},
		{
			name: "the computed path",
			emit: func() []string {
				var raw []string
				for _, slice := range []bool{false, true} {
					for _, wf := range wireFields(slice) {
						raw = append(raw, (&computedField{wire: wf}).TypescriptDisplayType())
					}
				}

				return append(raw, (&computedField{IsEnumerated: true}).TypescriptDisplayType())
			},
		},
		{
			name: "the column path",
			emit: func() []string {
				var raw []string
				for _, slice := range []bool{false, true} {
					for _, leaf := range vocabularyLeaves {
						f := &resourceField{}
						f.setColumnType(tsDataType(leaf.ts), leafDisplayType(leaf.ts, leaf.imp), leaf.imp, slice)
						raw = append(raw, f.TypescriptDisplayType())
					}
					derived := &resourceField{}
					derived.setColumnType("Rows.Reading", objectDisplayType, nil, slice)
					raw = append(raw, derived.TypescriptDisplayType())
					// A nullable BOOL column is the tri-state; a nullable list of them is a list.
					nullable := &resourceField{IsNullable: true}
					nullable.setColumnType(booleanStr, booleanStr, nil, slice)
					raw = append(raw, nullable.TypescriptDisplayType())
				}

				return append(raw, (&resourceField{IsEnumerated: true}).TypescriptDisplayType())
			},
		},
	}

	want := make([]string, 0, len(displayTypes))
	for _, member := range displayTypes {
		want = append(want, string(member))
	}
	slices.Sort(want)

	// The paths feed one set, so they run in sequence rather than as subtests.
	emitted := make(map[string]bool)
	for _, tt := range tests {
		for _, raw := range tt.emit() {
			rendered, err := renderDisplayType(raw)
			if err != nil {
				t.Errorf("%s emits %q, outside the vocabulary: %v", tt.name, raw, err)

				continue
			}
			emitted[rendered] = true
		}
	}

	got := make([]string, 0, len(emitted))
	for member := range emitted {
		got = append(got, member)
	}
	slices.Sort(got)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the paths together emit a set other than the vocabulary (-want +got):\n%s", diff)
	}
}
