package parser

import (
	"go/types"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func Test_LoadPackages(t *testing.T) {
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
			args:             args{packagePatterns: []string{"../testdata/resources/res1.go"}},
			WantPackageNames: []string{"resources"},
			wantErr:          false,
		},
		{
			name:             "loads 1 package by name",
			args:             args{packagePatterns: []string{"../testdata/resources"}},
			WantPackageNames: []string{"resources"},
			wantErr:          false,
		},
		{
			name:             "loads 1 package by 2 file names",
			args:             args{packagePatterns: []string{"../testdata/resources/res1.go", "../testdata/resources/res2.go"}},
			WantPackageNames: []string{"resources"},
			wantErr:          false,
		},
		{
			name:             "loads 2 packages by name",
			args:             args{packagePatterns: []string{"../testdata/resources", "../testdata/otherresources"}},
			WantPackageNames: []string{"resources", "otherresources"},
			wantErr:          false,
		},
		{
			name:             "loads 2 packages by by name and filename",
			args:             args{packagePatterns: []string{"../testdata/resources/res1.go", "../testdata/otherresources"}},
			WantPackageNames: []string{"resources", "otherresources"},
			wantErr:          false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			packageMap, err := LoadPackages(tt.args.packagePatterns...)
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

func Test_ParseStructs(t *testing.T) {
	type args struct {
		packageName string
		packagePath string
	}

	tests := []struct {
		name    string
		args    args
		want    []Struct
		wantErr bool
	}{
		{
			name: "parse 1 file",
			args: args{packageName: "resources", packagePath: "../testdata/resources/res1.go"},
			want: []Struct{
				testStruct(t, "AddressType",
					testField{"ID", basic(types.String), `spanner:"Id"`},
					testField{"Description", basic(types.String), `spanner:"description"`},
				),
				testStruct(t, "Status",
					testField{"ID", named("ccc.UUID", &types.Struct{}), `spanner:"Id"`},
					testField{"Description", basic(types.String), `spanner:"description"`},
				),
				testStruct(t, "ExampleStruct",
					testField{"Foo", basic(types.Int), ""},
				),
				testStruct(t, "FileRecordSet",
					testField{"ID", named("ccc.UUID", &types.Struct{}), `spanner:"Id"`},
					testField{"FileID", named("ccc.UUID", &types.Struct{}), `spanner:"FileId" index:"true"`},
					testField{"ManyIDs", named("[]resources.FileID", basic(types.String)), `spanner:"FileIdArray"`},
					testField{"Status", named("resources.FileRecordSetStatus", basic(types.String)), `spanner:"Status"`},
					testField{"ErrorDetails", pointer(basic(types.String)), `spanner:"ErrorDetails"`},
					testField{"UpdatedAt", pointer(named("time.Time", &types.Struct{})), `spanner:"UpdatedAt" conditions:"immutable"`},
				),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pkgMap, err := LoadPackages(tt.args.packagePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadPackages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			parsedStructs, err := ParseStructs(pkgMap[tt.args.packageName])
			if (err != nil) != tt.wantErr {
				t.Errorf("parseStructs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(parsedStructs) != len(tt.want) {
				t.Errorf("parseStructs() length of parsed structs slice does not match length of expected structs slice: got= %v \nwant = %v", parsedStructs, tt.want)
				return
			}

			for i := range parsedStructs {
				if parsedStructs[i].name != tt.want[i].name {
					t.Errorf("parseStructs() struct name = %s, want %v", parsedStructs[i].name, tt.want[i].name)
				}

				for j := range parsedStructs[i].fields {
					if parsedStructs[i].fields[j].Name() != tt.want[i].fields[j].Name() {
						t.Errorf("parseStructs() field name = %v, want %v", parsedStructs[i].fields[j].Name(), tt.want[i].fields[j].Name())
					}
					if parsedStructs[i].fields[j].Type() != tt.want[i].fields[j].Type() {
						t.Errorf("parseStructs() field Type = %v, want %v", parsedStructs[i].fields[j].Type(), tt.want[i].fields[j].Type())
					}
					if parsedStructs[i].fields[j].tags != tt.want[i].fields[j].tags {
						t.Errorf("parseStructs() field %q.%q has tags = %v, want %v", parsedStructs[i].name, parsedStructs[i].fields[j].name, parsedStructs[i].fields[j].tags, tt.want[i].fields[j].tags)
					}
				}
			}
		})
	}
}

func Test_FilterStructsByInterface(t *testing.T) {
	type args struct {
		packagePath string
		packageName string
		interfaces  []string
	}
	tests := []struct {
		name    string
		args    args
		want    []string
		wantErr bool
	}{
		{
			name:    "returns structs that implement a given interface",
			args:    args{packagePath: "../testdata/rpc", packageName: "rpc", interfaces: []string{"TxnRunner"}},
			want:    []string{"Banana", "Cofveve"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			pkgMap, err := LoadPackages(tt.args.packagePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("loadPackages() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			rpcStructs, err := ParseStructs(pkgMap[tt.args.packageName])
			if (err != nil) != tt.wantErr {
				t.Errorf("extractRPCMethods() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			rpcStructs = FilterStructsByInterface(rpcStructs, tt.args.interfaces)

			var rpcStructNames []string
			for _, s := range rpcStructs {
				rpcStructNames = append(rpcStructNames, s.Name())
			}

			if !reflect.DeepEqual(rpcStructNames, tt.want) {
				t.Errorf("extractRPCMethods() = %v, want %v", rpcStructNames, tt.want)
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
			args:     args{packagePath: "../testdata/nestedtypes", pkgName: "nestedtypes"},
			want:     []string{"nestedtypes.A", "nestedtypes.B", "nestedtypes.C"},
			wantFail: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pkgMap, err := LoadPackages(tt.args.packagePath)
			if err != nil {
				t.Errorf("loadPackages() error = %v", err)
				return
			}

			var obj types.Object
			pkg := pkgMap[tt.args.pkgName]
			for _, name := range pkg.Types.Scope().Names() {
				obj = pkg.Types.Scope().Lookup(name)
			}

			var typeNames []string
			for _, localType := range localTypesFromStruct(obj, map[string]struct{}{}) {
				typeNames = append(typeNames, typeStringer(localType.tt))
			}

			if !slices.Equal(typeNames, tt.want) && !tt.wantFail {
				t.Errorf("localTypeDependencies() = %v, want %v", typeNames, tt.want)
			}
		})
	}
}

type testField struct {
	name string
	typ  types.Type
	tag  string
}

func pkgAndObjName(name string) (*types.Package, string) {
	var pkgName string
	if s := strings.Split(name, "."); len(s) > 1 {
		pkgName = s[0]
		name = s[1]
	}

	return types.NewPackage(pkgName, pkgName), name
}

func typeName(name string, pkg *types.Package, typ types.Type) *types.TypeName {
	return types.NewTypeName(types.Universe.Pos(), pkg, name, typ)
}

func field(pkg *types.Package, fp testField) *types.Var {
	return types.NewField(types.Universe.Pos(), pkg, fp.name, fp.typ, false)
}

func pointer(t types.Type) *types.Pointer {
	return types.NewPointer(t)
}

func basic(tb types.BasicKind) *types.Basic {
	return types.Typ[tb]
}

func named(name string, typ types.Type) *types.Named {
	pkg, objName := pkgAndObjName(name)

	return types.NewNamed(typeName(objName, pkg, typ), typ, nil)
}

func testStruct(t *testing.T, qualifiedName string, fieldParams ...testField) Struct {
	t.Helper()

	pkg, structName := pkgAndObjName(qualifiedName)

	fields := make([]*types.Var, len(fieldParams))
	tags := make([]string, len(fieldParams))
	for i, fieldParam := range fieldParams {
		fields[i] = field(pkg, fieldParam)
		tags[i] = fieldParam.tag
	}

	structType := types.NewStruct(fields, tags)

	namedType := types.NewNamed(typeName(structName, pkg, structType), structType, nil)

	s, ok := newStruct(namedType.Obj(), false)
	if !ok {
		panic("could not create struct")
	}

	return s
}
