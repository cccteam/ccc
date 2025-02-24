package generation

import (
	"reflect"
	"slices"
	"testing"
)

func Test_loadPackages(t *testing.T) {
	type args struct {
		packagePatterns []string
	}

	tests := []struct {
		name             string
		args             args
		WantPackageNames []string
		wantErr          bool
	}{
		{
			name:             "loads 1 package by file name",
			args:             args{packagePatterns: []string{"testdata/resources/res1.go"}},
			WantPackageNames: []string{"resources"},
			wantErr:          false,
		},
		{
			name:             "loads 1 package by name",
			args:             args{packagePatterns: []string{"./testdata/resources"}},
			WantPackageNames: []string{"resources"},
			wantErr:          false,
		},
		{
			name:             "loads 1 package by 2 file names",
			args:             args{packagePatterns: []string{"testdata/resources/res1.go", "testdata/resources/res2.go"}},
			WantPackageNames: []string{"resources"},
			wantErr:          false,
		},
		{
			name:             "loads 2 packages by name",
			args:             args{packagePatterns: []string{"./testdata/resources", "./testdata/otherresources"}},
			WantPackageNames: []string{"resources", "otherresources"},
			wantErr:          false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			packageMap, err := loadPackages(tt.args.packagePatterns...)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadPackages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			packageNames := make([]string, 0, len(packageMap))
			for k := range packageMap {
				packageNames = append(packageNames, k)
			}

			for _, packageName := range tt.WantPackageNames {
				if !slices.Contains(packageNames, packageName) {
					t.Errorf("loadPackages() = `%v`, does not contain expected package %s", packageNames, packageName)
				}
			}
		})
	}
}

func Test_parseStructs(t *testing.T) {
	type args struct {
		packageName string
		packagePath string
	}
	tests := []struct {
		name    string
		args    args
		want    []parsedStruct
		wantErr bool
	}{
		{
			name: "parse 1 file",
			args: args{packageName: "resources", packagePath: "testdata/resources/res1.go"},
			want: []parsedStruct{
				{
					name: "AddressType",
					fields: []structField{
						{
							Name: "ID",
							Type: "string",
							tags: reflect.StructTag(`spanner:"Id"`),
						},
						{
							Name: "Description",
							Type: "string",
							tags: reflect.StructTag(`spanner:"description"`),
						},
					},
				},
				{
					name: "Status",
					fields: []structField{
						{
							Name: "ID",
							Type: "ccc.UUID",
							tags: reflect.StructTag(`spanner:"Id"`),
						},
						{
							Name: "Description",
							Type: "string",
							tags: reflect.StructTag(`spanner:"description"`),
						},
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pkgMap, err := loadPackages(tt.args.packagePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadPackages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			parsedStructs, err := parseStructs(pkgMap[tt.args.packageName])
			if (err != nil) != tt.wantErr {
				t.Errorf("parseStructs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			for i := range parsedStructs {
				if parsedStructs[i].name != tt.want[i].name {
					t.Errorf("parseStructs() = %v, want %v", parsedStructs[i], tt.want[i])
				}

				for j := range parsedStructs[i].fields {
					if parsedStructs[i].fields[j].Name != tt.want[i].fields[j].Name {
						t.Errorf("parseStructs() name = %v, want %v", parsedStructs[i].fields[j].Name, tt.want[i].fields[j].Name)
					}
					if parsedStructs[i].fields[j].Type != tt.want[i].fields[j].Type {
						t.Errorf("parseStructs() Type = %v, want %v", parsedStructs[i].fields[j].Type, tt.want[i].fields[j].Type)
					}
					if parsedStructs[i].fields[j].tags != tt.want[i].fields[j].tags {
						t.Errorf("parseStructs() tags = %v, want %v", parsedStructs[i].fields[j].tags, tt.want[i].fields[j].tags)
					}
				}
			}
		})
	}
}
