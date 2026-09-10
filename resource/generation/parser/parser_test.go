package parser

import (
	"go/types"
	"slices"
	"strings"
	"testing"
)

func Test_LoadPackages(t *testing.T) {
	t.Parallel()
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
		{
			name:    "strict load fails on stale generated output",
			args:    args{packagePatterns: []string{"../testdata/staleoutput"}},
			wantErr: true,
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

func Test_LoadPackagesResilient(t *testing.T) {
	t.Parallel()
	type args struct {
		packagePatterns []string
	}

	tests := []struct {
		name             string
		args             args
		wantPackageNames []string
		wantTolerated    bool
		wantErr          bool
	}{
		{
			name:             "tolerates stale generated output in the loaded package",
			args:             args{packagePatterns: []string{"../testdata/staleoutput"}},
			wantPackageNames: []string{"staleoutput"},
			wantTolerated:    true,
			wantErr:          false,
		},
		{
			name:             "clean package loads with nothing tolerated",
			args:             args{packagePatterns: []string{"../testdata/resources"}},
			wantPackageNames: []string{"resources"},
			wantTolerated:    false,
			wantErr:          false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			packageMap, tolerated, err := LoadPackagesResilient(tt.args.packagePatterns...)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadPackagesResilient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tolerated != tt.wantTolerated {
				t.Errorf("LoadPackagesResilient() tolerated = %v, want %v", tolerated, tt.wantTolerated)
			}

			for _, packageName := range tt.wantPackageNames {
				pkg, ok := packageMap[packageName]
				if !ok {
					t.Errorf("LoadPackagesResilient() missing expected package %s", packageName)

					continue
				}

				// The tolerated package must still be parseable: generation needs its
				// structs even while the stale generated file fails type-checking.
				parsed := ParsePackage(pkg)
				if len(parsed.Structs) == 0 {
					t.Errorf("ParsePackage(%s) returned no structs", packageName)
				}
			}
		})
	}
}

func Test_ParseStructs(t *testing.T) {
	t.Parallel()
	type args struct {
		packageName string
		packagePath string
	}

	tests := []struct {
		name    string
		args    args
		want    []*Struct
		wantErr bool
	}{
		{
			name: "parse 1 file",
			args: args{packageName: "resources", packagePath: "../testdata/resources/res1.go"},
			want: []*Struct{
				testStruct(t, "AddressType",
					testField{"ID", basic(types.String), `spanner:"Id"`},
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
				testStruct(t, "Status",
					testField{"ID", named("ccc.UUID", &types.Struct{}), `spanner:"Id"`},
					testField{"Description", basic(types.String), `spanner:"description"`},
				),
				testStruct(t, "alias"),
				testStruct(t, "named"),
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

			parsedStructs := ParsePackage(pkgMap[tt.args.packageName]).Structs

			if len(parsedStructs) != len(tt.want) {
				t.Errorf("parseStructs() length of parsed structs slice does not match length of expected structs slice: got= %v \nwant = %v", len(parsedStructs), len(tt.want))
				return
			}

			for i := range parsedStructs {
				if parsedStructs[i].Name() != tt.want[i].Name() {
					t.Errorf("parseStructs() struct name = %s, want %v", parsedStructs[i].Name(), tt.want[i].Name())
				}

				for j := range parsedStructs[i].fields {
					if parsedStructs[i].fields[j].Name() != tt.want[i].fields[j].Name() {
						t.Errorf("parseStructs() field name = %v, want %v", parsedStructs[i].fields[j].Name(), tt.want[i].fields[j].Name())
					}
					if parsedStructs[i].fields[j].Type() != tt.want[i].fields[j].Type() {
						t.Errorf("parseStructs() field Type = %v, want %v", parsedStructs[i].fields[j].Type(), tt.want[i].fields[j].Type())
					}
					if parsedStructs[i].fields[j].tags != tt.want[i].fields[j].tags {
						t.Errorf("parseStructs() field %q.%q has tags = %v, want %v", parsedStructs[i].Name(), parsedStructs[i].fields[j].Name(), parsedStructs[i].fields[j].tags, tt.want[i].fields[j].tags)
					}
				}
			}
		})
	}
}

// Test_ParsePackage_docComments pins where a declaration's doc comment is read from:
// a type declared on its own line carries it on the GenDecl, a grouped one on the
// TypeSpec, and the parser reads both, so a struct's annotations are never lost to
// the declaration style.
func Test_ParsePackage_docComments(t *testing.T) {
	t.Parallel()

	pkgMap, err := LoadPackages("../testdata/doccomments")
	if err != nil {
		t.Fatalf("LoadPackages() error = %v", err)
	}
	comments := make(map[string]string)
	for _, s := range ParsePackage(pkgMap["doccomments"]).Structs {
		comments[s.Name()] = s.Comments()
	}

	tests := []struct {
		name       string
		structName string
		want       string
	}{
		{name: "a struct declared on its own line keeps its doc", structName: "Standalone", want: "Standalone is declared on its own line.\n\n@rpc\n"},
		{name: "a struct declared in a group keeps its doc", structName: "Grouped", want: "Grouped is declared in a type group.\n\n@rpc\n"},
		{name: "a spec in a shared group keeps its own doc", structName: "Sibling", want: "Sibling shares a group with another spec; its doc is its own.\n"},
		{name: "the second spec of a shared group keeps its own doc", structName: "Other", want: "Other is the second spec of the group.\n"},
		{name: "an undocumented struct has none", structName: "Undocumented", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := comments[tt.structName]
			if !ok {
				t.Fatalf("struct %q not parsed", tt.structName)
			}
			if got != tt.want {
				t.Errorf("Struct.Comments() = %q, want %q", got, tt.want)
			}
		})
	}
}

func Test_Struct_Method(t *testing.T) {
	t.Parallel()

	pkgMap, err := LoadPackages("../testdata/rpc")
	if err != nil {
		t.Fatalf("LoadPackages() error = %v", err)
	}
	structs := make(map[string]*Struct)
	for _, s := range ParsePackage(pkgMap["rpc"]).Structs {
		structs[s.Name()] = s
	}

	tests := []struct {
		name       string
		structName string
		method     string
		wantParams int
	}{
		{name: "pointer-receiver Execute is found", structName: "Banana", method: "Execute", wantParams: 3},
		{name: "Execute declared in another file is found", structName: "Cofveve", method: "Execute", wantParams: 3},
		{name: "value-receiver method is found", structName: "Durian", method: "Execute", wantParams: 3},
		{name: "a struct without the method answers nil", structName: "Apple", method: "Execute", wantParams: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := structs[tt.structName]
			if s == nil {
				t.Fatalf("struct %q not in fixture", tt.structName)
			}
			fn := s.Method(tt.method)
			if tt.wantParams < 0 {
				if fn != nil {
					t.Fatalf("Method(%q) = %v, want nil", tt.method, fn)
				}

				return
			}
			if fn == nil {
				t.Fatalf("Method(%q) = nil, want the method", tt.method)
			}
			sig, ok := fn.Type().(*types.Signature)
			if !ok {
				t.Fatalf("Method(%q).Type() = %T, want *types.Signature", tt.method, fn.Type())
			}
			if got := sig.Params().Len(); got != tt.wantParams {
				t.Errorf("Method(%q) params = %d, want %d", tt.method, got, tt.wantParams)
			}
		})
	}
}

func Test_typeStringer(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
				typeNames = append(typeNames, typeStringer(localType.obj.Type()))
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

func pkgAndObjName(name string) (pkg *types.Package, objName string) {
	var pkgName string
	if s := strings.Split(name, "."); len(s) > 1 {
		pkgName = s[0]
		objName = s[1]
	} else {
		objName = name
	}

	return types.NewPackage(pkgName, pkgName), objName
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

func testStruct(t *testing.T, qualifiedName string, fieldParams ...testField) *Struct {
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

	s := newStruct(namedType.Obj())

	return s
}
