package generation

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
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
