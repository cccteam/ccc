package parser

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func Test_TypeInfo_Imports(t *testing.T) {
	t.Parallel()

	const testdataPath = "github.com/cccteam/ccc/resource/generation/testdata/resources"

	pkgMap, err := LoadPackages("../testdata/resources")
	if err != nil {
		t.Fatalf("LoadPackages() error = %v", err)
	}

	pkg := ParsePackage(pkgMap["resources"])

	fieldImports := make(map[string][]Import)
	structImports := make(map[string][]Import)
	for _, pStruct := range pkg.Structs {
		structImports[pStruct.Name()] = pStruct.Imports()
		for _, field := range pStruct.Fields() {
			fieldImports[pStruct.Name()+"."+field.Name()] = field.Imports()
		}
	}

	tests := []struct {
		name string
		got  []Import
		want []Import
	}{
		{
			name: "struct's own package is included",
			got:  structImports["FileRecordSet"],
			want: []Import{{Name: "resources", Path: testdataPath}},
		},
		{
			name: "third-party named type",
			got:  fieldImports["FileRecordSet.ID"],
			want: []Import{{Name: "ccc", Path: "github.com/cccteam/ccc"}},
		},
		{
			name: "stdlib type behind a pointer",
			got:  fieldImports["FileRecordSet.UpdatedAt"],
			want: []Import{{Name: "time", Path: "time"}},
		},
		{
			name: "slice of local named type resolves to own package",
			got:  fieldImports["FileRecordSet.ManyIDs"],
			want: []Import{{Name: "resources", Path: testdataPath}},
		},
		{
			name: "builtin types contribute nothing",
			got:  fieldImports["AddressType.ID"],
			want: []Import{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, tt.got); diff != "" {
				t.Errorf("Imports() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
