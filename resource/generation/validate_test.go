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
