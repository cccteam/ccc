package generation

import (
	"go/types"
	"reflect"
	"slices"
	"testing"
)

func Test_loadPackageMap(t *testing.T) {
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
		{
			name:             "loads 2 packages by by name and filename",
			args:             args{packagePatterns: []string{"testdata/resources/res1.go", "./testdata/otherresources"}},
			WantPackageNames: []string{"resources", "otherresources"},
			wantErr:          false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			packageMap, err := loadPackageMap(tt.args.packagePatterns...)
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

	type field struct {
		name string
		Type string
		tags reflect.StructTag
	}

	type structType struct {
		name   string
		fields []field
	}
	tests := []struct {
		name    string
		args    args
		want    []structType
		wantErr bool
	}{
		{
			name: "parse 1 file",
			args: args{packageName: "resources", packagePath: "testdata/resources/res1.go"},
			want: []structType{
				{
					name: "AddressType",
					fields: []field{
						{
							name: "ID",
							Type: "string",
							tags: reflect.StructTag(`spanner:"Id"`),
						},
						{
							name: "Description",
							Type: "string",
							tags: reflect.StructTag(`spanner:"description"`),
						},
					},
				},
				{
					name: "Status",
					fields: []field{
						{
							name: "ID",
							Type: "ccc.UUID",
							tags: reflect.StructTag(`spanner:"Id"`),
						},
						{
							name: "Description",
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
			pkgMap, err := loadPackageMap(tt.args.packagePath)
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
					t.Errorf("parseStructs() struct name = %s, want %v", parsedStructs[i].name, tt.want[i].name)
				}

				for j := range parsedStructs[i].fields {
					if parsedStructs[i].fields[j].Name() != tt.want[i].fields[j].name {
						t.Errorf("parseStructs() field name = %v, want %v", parsedStructs[i].fields[j].Name(), tt.want[i].fields[j].name)
					}
					if parsedStructs[i].fields[j].Type() != tt.want[i].fields[j].Type {
						t.Errorf("parseStructs() field Type = %v, want %v", parsedStructs[i].fields[j].Type(), tt.want[i].fields[j].Type)
					}
					if parsedStructs[i].fields[j].tags != tt.want[i].fields[j].tags {
						t.Errorf("parseStructs() field tags = %v, want %v", parsedStructs[i].fields[j].tags, tt.want[i].fields[j].tags)
					}
				}
			}
		})
	}
}

func Test_typeStringer(t *testing.T) {
	type args struct {
		t types.Type
	}

	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "works with custom named types",
			args: args{
				t: types.NewNamed(types.NewTypeName(0, nil, "CamFileStatus", &types.Basic{}), &types.Basic{}, nil),
			},
			want: "CamFileStatus",
		},
		{
			name: "basic type aliases",
			args: args{
				t: types.NewAlias(types.NewTypeName(0, nil, "string", &types.Basic{}), &types.Basic{}),
			},
			want: "string",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := typeStringer(tt.args.t); got != tt.want {
				t.Errorf("typeStringer() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_extractStructsByMethod(t *testing.T) {
	type args struct {
		packagePath string
		packageName string
		methods     []string
	}
	tests := []struct {
		name    string
		args    args
		want    []string
		wantErr bool
	}{
		{
			name:    "gets the structs with the methods",
			args:    args{packagePath: "./testdata/rpc", packageName: "rpc", methods: []string{"Method", "Execute"}},
			want:    []string{"Cofveve"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pkgMap, err := loadPackageMap(tt.args.packagePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadPackages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			rpcStructs, err := extractStructsByMethod(pkgMap[tt.args.packageName], tt.args.methods...)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractRPCMethods() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			var rpcStructNames []string
			for _, s := range rpcStructs {
				rpcStructNames = append(rpcStructNames, s.name)
			}

			if !reflect.DeepEqual(rpcStructNames, tt.want) {
				t.Errorf("extractRPCMethods() = %v, want %v", rpcStructNames, tt.want)
			}
		})
	}
}

func Test_localTypesFromStruct(t *testing.T) {
	type args struct {
		packagePath string
		pkgName     string
	}
	tests := []struct {
		name     string
		args     args
		want     []string
		wantFail bool
	}{
		{
			name:     "gets all local dependent types",
			args:     args{packagePath: "./testdata/nestedtypes", pkgName: "nestedtypes"},
			want:     []string{"nestedtypes.A", "nestedtypes.B", "nestedtypes.C"},
			wantFail: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pkgMap, err := loadPackageMap(tt.args.packagePath)
			if err != nil {
				t.Errorf("loadPackages() error = %v", err)
				return
			}

			pkg := pkgMap[tt.args.pkgName]
			var lastStructType types.Type
			for _, name := range pkg.Scope().Names() {
				obj := pkg.Scope().Lookup(name)

				if _, ok := decodeToType[*types.Struct](obj.Type()); ok {
					lastStructType = obj.Type()
				}
			}

			var typeNames []string
			for _, localType := range localTypesFromStruct(tt.args.pkgName, lastStructType, map[string]struct{}{}) {
				typeNames = append(typeNames, typeStringer(localType.tt))
			}

			if !slices.Equal(typeNames, tt.want) && !tt.wantFail {
				t.Errorf("localTypeDependencies() = %v, want %v", typeNames, tt.want)
			}
		})
	}
}
