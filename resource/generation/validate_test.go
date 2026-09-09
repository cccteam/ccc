package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/cccteam/ccc/resource/generation/parser/genlang"
)

func Test_validateNoPermTags(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))

	tests := []struct {
		name       string
		structName string
		wantErr    bool
	}{
		{
			name:       "perm tag on a source struct is rejected",
			structName: "Antique",
			wantErr:    true,
		},
		{
			name:       "annotation-free struct passes",
			structName: "Widget",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := structs[tt.structName]
			if s == nil {
				t.Fatalf("struct %q not found in fixture package", tt.structName)
			}

			err := validateNoPermTags(s)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateNoPermTags() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "perm tag") {
				t.Errorf("validateNoPermTags() error = %v, want mention of the perm tag", err)
			}
		})
	}
}

// Test_validateConditionsTags pins that an unrecognized conditions value is refused
// naming the field and the value, with the nearest recognized value suggested when one
// is close, since every reader of the tag matches exactly and would otherwise drop it.
func Test_validateConditionsTags(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "hygienefixture"))

	tests := []struct {
		name         string
		structName   string
		wantContains string
	}{
		{name: "a misspelling is refused with a suggestion", structName: "Misspelled", wantContains: `field Misspelled.Name: conditions tag value "immutble" is not recognized; did you mean "immutable"?`},
		{name: "a space-padded value is refused with the trimmed value suggested", structName: "Spaced", wantContains: `conditions tag value " pii" is not recognized; did you mean "pii"?`},
		{name: "a trailing comma's empty value is refused without a suggestion", structName: "Trailing", wantContains: `conditions tag value "" is not recognized (recognized: immutable, pii, input_only, output_only)`},
		{name: "recognized values pass", structName: "Clean"},
		{name: "a struct without the tag passes", structName: "OneKind"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := structs[tt.structName]
			if s == nil {
				t.Fatalf("struct %q not found in fixture package", tt.structName)
			}

			err := validateConditionsTags(s)
			if (err != nil) != (tt.wantContains != "") {
				t.Fatalf("validateConditionsTags() error = %v, wantErr %v", err, tt.wantContains != "")
			}
			if err != nil && !strings.Contains(err.Error(), tt.wantContains) {
				t.Errorf("validateConditionsTags() error = %v, want it to contain %q", err, tt.wantContains)
			}
		})
	}
}

// Test_rejectMultipleKinds pins that a struct carrying two kind keywords is refused
// with both kinds named, where the per-keyword Exclusive flag alone would let every
// extractor claim it.
func Test_rejectMultipleKinds(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "hygienefixture"))

	tests := []struct {
		name         string
		structName   string
		wantContains string
	}{
		{name: "two kinds are refused, both named", structName: "TwoKinds", wantContains: "struct TwoKinds carries @resource and @computed: exactly one of"},
		{name: "one kind passes", structName: "OneKind"},
		{name: "no kind passes; the extractors skip it", structName: "Clean"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := structs[tt.structName]
			if s == nil {
				t.Fatalf("struct %q not found in fixture package", tt.structName)
			}
			annotations, err := genlang.NewScanner(resourceKeywords()).ScanStruct(s)
			if err != nil {
				t.Fatalf("ScanStruct() error = %v", err)
			}

			err = rejectMultipleKinds(s, annotations)
			if (err != nil) != (tt.wantContains != "") {
				t.Fatalf("rejectMultipleKinds() error = %v, wantErr %v", err, tt.wantContains != "")
			}
			if err != nil && !strings.Contains(err.Error(), tt.wantContains) {
				t.Errorf("rejectMultipleKinds() error = %v, want it to contain %q", err, tt.wantContains)
			}
		})
	}
}

// Test_validateStructNameMatchesFile pins the file-name rule and its one exception: a
// struct whose snake-cased name ends in _test would live in (and generate into) a Go
// test file, so its expected file carries the _rpc marker and the message says why.
func Test_validateStructNameMatchesFile(t *testing.T) {
	t.Parallel()

	pkg, parsed := loadCollectionFixturePackage(t)
	structs := fixtureStructs(parsed)

	tests := []struct {
		name         string
		structName   string
		plural       bool
		wantContains string
	}{
		{name: "a struct in its expected file passes", structName: "DoNothing"},
		{name: "a struct in another file is rejected", structName: "DoSomething", wantContains: `does not match its file name fixture.go (expected "do_something.go")`},
		{name: "a name ending in Test passes in its marked file", structName: "SmokeTest"},
		{name: "a name ending in Test in another file is told about the marker", structName: "DrillTest", wantContains: `(expected "drill_test_rpc.go": the name ends in Test, so the file carries the _rpc marker`},
		{name: "a pluralized name ending in Tests is not a test file", structName: "SmokeTest", plural: true, wantContains: `(expected "smoke_tests.go")`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := structs[tt.structName]
			if s == nil {
				t.Fatalf("struct %q not found in fixture package", tt.structName)
			}

			err := (&client{}).validateStructNameMatchesFile(pkg, tt.plural)(s)
			if tt.wantContains == "" {
				if err != nil {
					t.Fatalf("validateStructNameMatchesFile() error = %v, want nil", err)
				}

				return
			}
			if err == nil {
				t.Fatalf("validateStructNameMatchesFile() = nil, want error containing %q", tt.wantContains)
			}
			if !strings.Contains(err.Error(), tt.wantContains) {
				t.Errorf("validateStructNameMatchesFile() error = %q, want containing %q", err, tt.wantContains)
			}
		})
	}
}

func Test_structsToVirtualResources_primarykey(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadCollectionFixture(t))
	c := &client{}

	resources, err := c.structsToVirtualResources([]*parser.Struct{structs["Curio"]})
	if err != nil {
		t.Fatalf("structsToVirtualResources() error = %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("structsToVirtualResources() returned %d resources, want 1", len(resources))
	}

	tests := []struct {
		name         string
		fieldName    string
		wantPK       bool
		wantOrdinal  int64
		wantMarkerGo string // rendered PermTag fragment
	}{
		{
			name:         "annotated field is the primary key and emits the exemption marker",
			fieldName:    "ID",
			wantPK:       true,
			wantOrdinal:  0,
			wantMarkerGo: `perm:"-"`,
		},
		{
			name:      "plain field is not a primary key and emits no marker",
			fieldName: "Name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var field *resourceField
			for _, f := range resources[0].Fields {
				if f.Name() == tt.fieldName {
					field = f
				}
			}
			if field == nil {
				t.Fatalf("field %q not found on virtual resource", tt.fieldName)
			}

			if field.IsPrimaryKey != tt.wantPK {
				t.Errorf("IsPrimaryKey = %v, want %v", field.IsPrimaryKey, tt.wantPK)
			}
			if field.KeyOrdinalPosition != tt.wantOrdinal {
				t.Errorf("KeyOrdinalPosition = %d, want %d", field.KeyOrdinalPosition, tt.wantOrdinal)
			}
			if got := field.PermTag(); got != tt.wantMarkerGo {
				t.Errorf("PermTag() = %q, want %q", got, tt.wantMarkerGo)
			}
		})
	}
}

// Test_fileStem pins the stem every derived file shares, including the one exception.
func Test_fileStem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plural resource", in: "Assets", want: "assets"},
		{name: "rpc method", in: "ApproveRequisition", want: "approve_requisition"},
		{name: "rpc method ending in Test gets the marker", in: "StartFlightTest", want: "start_flight_test_rpc"},
		{name: "pluralized Tests is not a test file", in: "SmokeTests", want: "smoke_tests"},
		{name: "Test alone is not a test file", in: "Test", want: "test"},
		{name: "Test inside the name is not a suffix", in: "TestFlightStart", want: "test_flight_start"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := fileStem(tt.in); got != tt.want {
				t.Errorf("fileStem(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
