package generation

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/tools/go/packages"
)

// columnFixtureClient builds a client over the column fixture as a run would: the loaded
// package noted, so the @typescript reader finds the fixture's declarations without
// loading the package again, and loads the one package it did not load on first sight.
func columnFixtureClient(t *testing.T) (c *client, structs map[string]*parser.Struct) {
	t.Helper()

	loaded, parsed := loadFixturePackage(t, "columnfixture")
	c = &client{}
	c.notePackages(map[string]*packages.Package{loaded.Name: loaded})

	return c, fixtureStructs(parsed)
}

// columnFixtureClientWithShared builds the client over the column fixture and its shared
// package, loaded as WithTypes loads a named package, so a pair may be written into it
// when the generator's types name it (sharedDir).
func columnFixtureClientWithShared(t *testing.T) (c *client, structs map[string]*parser.Struct) {
	t.Helper()

	loaded, parsed := loadFixturePackage(t, "columnfixture")
	sharedPkgs, err := parser.LoadPackages(filepath.Join(fixtureRoot(t), "columnfixture", "shared"))
	if err != nil {
		t.Fatalf("parser.LoadPackages(shared) error = %v", err)
	}
	shared := sharedPkgs["shared"]
	if shared == nil {
		t.Fatal("fixture package shared not loaded")
	}
	c = &client{}
	c.notePackages(map[string]*packages.Package{loaded.Name: loaded, shared.Name: shared})

	return c, fixtureStructs(parsed)
}

// sharedDir is the shared fixture package as a WithTypes option names it.
const sharedDir = packageDir("generation/testdata/columnfixture/shared")

// fixtureRoot is the testdata directory relative to the working directory, as
// loadFixturePackage resolves it.
func fixtureRoot(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}
	root, err := filepath.Rel(cwd, filepath.Join(filepath.Dir(thisFile), "testdata"))
	if err != nil {
		t.Fatalf("filepath.Rel() error = %v", err)
	}

	return root
}

// columnFixtureGenerator builds a TypeScript generator over the client with nothing on
// another outlet and no router resources, so a key's picker rule never fires.
func columnFixtureGenerator(c *client) *typescriptGenerator {
	return &typescriptGenerator{client: c, outletExcludedTables: map[string]struct{}{}}
}

// Test_resourceFieldsTypescriptType_columns pins the column path over every shape the
// classifier resolves or derives: the built-in rows and the generic row through an
// alias, a pointer read through, a list of every leaf (element pointers read through,
// and a nullable list of booleans staying boolean[], since the tri-state rule is for one
// BOOL column), the byte slices (one bytes leaf each, a list of them, and the byte array
// that stays a list of numbers), a plain struct derived into the resource's namespace, a
// named slice of structs, a declared type, a declared type in a package the generator
// did not load, a built-in declaration, and the types JSON by declaration (over
// json.RawMessage with and without a declaration, in a package the generator loaded and
// in ones it did not, and over a type with JSON methods that is no byte slice), never
// bytes; and that every display type renders into the metadata as its lower-cased self.
func Test_resourceFieldsTypescriptType_columns(t *testing.T) {
	t.Parallel()

	c, structs := columnFixtureClientWithShared(t)
	res := fixtureResource(t, structs, "Row", func(res *resourceInfo) {
		for _, f := range res.Fields {
			if f.Name() == "Toggles" {
				f.IsNullable = true
			}
		}
	})
	if err := columnFixtureGenerator(c).resourceFieldsTypescriptType(res); err != nil {
		t.Fatalf("resourceFieldsTypescriptType() error = %v", err)
	}
	fields := make(map[string]*resourceField, len(res.Fields))
	for _, f := range res.Fields {
		fields[f.Name()] = f
	}

	tests := []struct {
		name        string
		field       string
		wantData    string
		wantDisplay string
		wantImport  *tsImport
	}{
		{name: "a UUID is a string with the uuid display type", field: "ID", wantData: "string", wantDisplay: "uuid"},
		{name: "an alias of NullEnum over a named string is a string", field: "Kind", wantData: "string", wantDisplay: "string"},
		{name: "an alias of NullEnum over a named int64 is a number", field: "Rank", wantData: "number", wantDisplay: "number"},
		{name: "a Spanner Null wrapper maps by its row", field: "Note", wantData: "string", wantDisplay: "string"},
		{name: "spanner.NullJSON is unknown, one opaque object", field: "Blob", wantData: "unknown", wantDisplay: "object"},
		{name: "a pointer to a mapped type is the mapped type", field: "When", wantData: "Date", wantDisplay: "Date"},
		{name: "a slice of a basic type is a list", field: "Tags", wantData: "string[]", wantDisplay: "string[]"},
		{name: "a slice of int64 is a list of numbers", field: "Counts", wantData: "number[]", wantDisplay: "number[]"},
		{name: "a slice of bool is a list of booleans", field: "Flags", wantData: "boolean[]", wantDisplay: "boolean[]"},
		{name: "a slice of time.Time is a list of dates", field: "Stamps", wantData: "Date[]", wantDisplay: "Date[]"},
		{name: "a slice of civil.Date is a list of Dates with the civildate display type", field: "Days", wantData: "Date[]", wantDisplay: "civilDate[]"},
		{name: "a slice of UUIDs is a list of strings with the uuid display type", field: "IDs", wantData: "string[]", wantDisplay: "uuid[]"},
		{name: "a slice of pointers is a list of the element", field: "Ranks", wantData: "number[]", wantDisplay: "number[]"},
		{name: "a nullable slice of bool pointers is boolean[], never nullboolean[]", field: "Toggles", wantData: "boolean[]", wantDisplay: "boolean[]"},
		{name: "a byte slice is one bytes leaf, a string in the interface", field: "Seal", wantData: "string", wantDisplay: "bytes"},
		{name: "a named byte slice is the bytes leaf", field: "Digest", wantData: "string", wantDisplay: "bytes"},
		{name: "a slice of byte slices is a list of the bytes leaf", field: "Chunks", wantData: "string[]", wantDisplay: "bytes[]"},
		{name: "a byte array is a list of numbers, as encoding/json writes it", field: "Checksum", wantData: "number[]", wantDisplay: "number[]"},
		{name: "a plain struct is derived into the resource's namespace", field: "Provenance", wantData: "Rows.Provenance", wantDisplay: "object"},
		{name: "a named slice of structs with its own storage is a list of the derived interface", field: "Attachments", wantData: "Rows.Attachment[]", wantDisplay: "object[]"},
		{name: "a named slice of structs is a list of the derived interface", field: "Manifests", wantData: "Rows.Manifest[]", wantDisplay: "object[]"},
		{name: "a declared type is the imported name", field: "Position", wantData: "Point", wantDisplay: "object", wantImport: &tsImport{Name: "Point", From: "geojson"}},
		{name: "a slice of a declared type is a list of it", field: "Positions", wantData: "Point[]", wantDisplay: "object[]", wantImport: &tsImport{Name: "Point", From: "geojson"}},
		{name: "a declaration on a plain struct wins over derivation", field: "Label", wantData: "Label", wantDisplay: "object", wantImport: &tsImport{Name: "Label", From: "labels"}},
		{name: "a built-in declaration is the built-in", field: "Code", wantData: "string", wantDisplay: "string", wantImport: &tsImport{Name: "string"}},
		{name: "a derived struct reaching declared types is derived", field: "Doc", wantData: "Rows.Doc", wantDisplay: "object"},
		{name: "a declared type in a package the generator did not load is read on first sight", field: "Tag", wantData: "Tag", wantDisplay: "object", wantImport: &tsImport{Name: "Tag", From: "tags"}},
		{name: "a type over json.RawMessage with no declaration is unknown, as json.RawMessage is", field: "Payload", wantData: "unknown", wantDisplay: "object"},
		{name: "a slice of a type over json.RawMessage is a list of unknown", field: "Payloads", wantData: "unknown[]", wantDisplay: "object[]"},
		{name: "a type over json.RawMessage with a declaration is typed as declared, never bytes", field: "Sheet", wantData: "Sheet", wantDisplay: "object", wantImport: &tsImport{Name: "Sheet", From: "sheets"}},
		{name: "a type over a type with JSON methods resolves as that type does", field: "Stamp", wantData: "Date", wantDisplay: "Date"},
		{name: "a type over a plain struct is derived from the struct's fields", field: "Wrapped", wantData: "Rows.Wrapped", wantDisplay: "object"},
		{name: "a type over json.RawMessage in a package the generator did not load is read with its declaration", field: "Memo", wantData: "Memo", wantDisplay: "object", wantImport: &tsImport{Name: "Memo", From: "memos"}},
		{name: "a type over json.RawMessage in a WithTypes package is typed as declared", field: "Shared", wantData: "Manifest", wantDisplay: "object", wantImport: &tsImport{Name: "Manifest", From: "manifests"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := fields[tt.field]
			if f == nil {
				t.Fatalf("field %q not on the fixture", tt.field)
			}
			if got := f.TypescriptDataType(); got != tt.wantData {
				t.Errorf("TypescriptDataType() = %q, want %q", got, tt.wantData)
			}
			if got := f.TypescriptDisplayType(); got != tt.wantDisplay {
				t.Errorf("TypescriptDisplayType() = %q, want %q", got, tt.wantDisplay)
			}
			rendered, err := renderDisplayType(f.TypescriptDisplayType())
			if err != nil {
				t.Errorf("renderDisplayType(%q) error = %v", f.TypescriptDisplayType(), err)
			}
			if want := strings.ToLower(tt.wantDisplay); rendered != want {
				t.Errorf("renderDisplayType(%q) = %q, want %q", f.TypescriptDisplayType(), rendered, want)
			}
			if diff := cmp.Diff(tt.wantImport, f.tsImport); diff != "" {
				t.Errorf("tsImport mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_resourceFieldsTypescriptType_namespace pins the namespace a resource's derived
// structs declare: leaves first, each struct once however many columns reach it, wire
// names from json tags, "-" left out, omitempty and a pointer both optional, a nested
// struct in the same namespace, and a declared type inside a struct the same imported
// leaf; and the imports the resources file gathers from all of it, grouped per module.
func Test_resourceFieldsTypescriptType_namespace(t *testing.T) {
	t.Parallel()

	c, structs := columnFixtureClientWithShared(t)
	res := fixtureResource(t, structs, "Row", nil)
	if err := columnFixtureGenerator(c).resourceFieldsTypescriptType(res); err != nil {
		t.Fatalf("resourceFieldsTypescriptType() error = %v", err)
	}
	c.resources = []*resourceInfo{res}

	wantNamespace := `export namespace Rows {
  export interface Provenance {
    system: string;
    reference?: string;
    receivedAt: Date;
  }

  export interface Attachment {
    title: string;
    url: string;
  }

  export interface Manifest {
    item: string;
    where?: Rows.Provenance;
  }

  export interface Doc {
    where: Point;
    tag: Tag;
    body: unknown;
  }

  export interface Wrapped {
    system: string;
    reference?: string;
    receivedAt: Date;
  }
}
`
	if got := typescriptNamespaceOf("Rows", res.ColumnShapes); got != wantNamespace {
		t.Errorf("typescriptNamespaceOf() mismatch (-want +got):\n%s", cmp.Diff(wantNamespace, got))
	}

	wantImports := []tsImportGroup{
		{From: "geojson", Names: []string{"Point"}},
		{From: "labels", Names: []string{"Label"}},
		{From: "manifests", Names: []string{"Manifest"}},
		{From: "memos", Names: []string{"Memo"}},
		{From: "sheets", Names: []string{"Sheet"}},
		{From: "tags", Names: []string{"Tag"}},
	}
	if diff := cmp.Diff(wantImports, c.ResourceTypeImports()); diff != "" {
		t.Errorf("ResourceTypeImports() mismatch (-want +got):\n%s", diff)
	}
}

// Test_resourceFieldsTypescriptType_refusals pins what the column path refuses, every
// offending field of a resource in one error: a database/sql Null wrapper, refused
// naming the pointer and nothing else (no @typescript clause, since an application
// cannot annotate a standard-library type), a struct field with no json tag, a struct
// writing its own JSON without a declaration, a slice of slices, an interface,
// json.RawMessage in its three forms (the Spanner column refusal, naming the two
// spellings and no @typescript clause), a byte slice declaring a type that promises
// JSON, and the two malformed declarations. Nothing degrades to string.
func Test_resourceFieldsTypescriptType_refusals(t *testing.T) {
	t.Parallel()

	c, structs := columnFixtureClient(t)
	res := fixtureResource(t, structs, "Bad", nil)
	err := columnFixtureGenerator(c).resourceFieldsTypescriptType(res)
	if err == nil {
		t.Fatal("resourceFieldsTypescriptType() error = nil, want every refusal")
	}
	got := err.Error()

	tests := []struct {
		name string
		want string
		// absent is text the error must not carry, when the refusal stands alone.
		absent string
	}{
		{
			name:   "a database/sql Null wrapper is refused naming the pointer, with no declaration clause",
			want:   "Bads.Count: sql.NullInt64 has no JSON form of its own (encoding/json writes it as {Int64, Valid}); type a nullable column with the pointer *int64",
			absent: "*int64; declare the type's TypeScript form",
		},
		{name: "a struct field with no json tag names the field", want: "Bads.Untagged.Name: no json tag"},
		{name: "a struct writing its own JSON names the fix", want: "Bads.Sealed: columnfixture.Sealed writes its own JSON (MarshalJSON or UnmarshalJSON), so its fields do not describe the wire; add @typescript(...) to its declaration"},
		{name: "a slice of slices is refused with the fix clause", want: "Bads.Matrix: a slice of slices has no TypeScript type; declare a struct for the inner element; declare the type's TypeScript form with @typescript(Name, from: \"module\") on its declaration, or use a struct for a derived interface"},
		{name: "an interface has no TypeScript type", want: "Bads.Any: any has no TypeScript type; declare the type's TypeScript form"},
		{
			name:   "json.RawMessage on a column is refused naming the two spellings, with no declaration clause",
			want:   "Bads.Raw: " + rawMessageColumnRefusal,
			absent: "generator writes; declare the type's TypeScript form",
		},
		{name: "a pointer to json.RawMessage is refused the same way", want: "Bads.RawPtr: " + rawMessageColumnRefusal},
		{name: "a slice of json.RawMessage is refused the same way", want: "Bads.Raws: " + rawMessageColumnRefusal},
		{
			name:   "a byte slice declaring a type that promises JSON is refused naming the fix",
			want:   "Bads.Token: columnfixture.Token is a byte slice, which encoding/json carries as a base64 string, so @typescript(Token) promises JSON the wire never carries; declare the type over json.RawMessage, or write its MarshalJSON and UnmarshalJSON",
			absent: "UnmarshalJSON; declare the type's TypeScript form",
		},
		{name: "a built-in declared with a module is refused", want: `columnfixture.Bound: @typescript(string, from: "strings"): string is a TypeScript built-in, which no module exports; drop from:`},
		{name: "a non-built-in declared without a module is refused", want: `columnfixture.Loose: @typescript(Money): Money is not a TypeScript built-in (string, number, boolean, unknown); add from: "module" naming the module that exports it`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if !strings.Contains(got, tt.want) {
				t.Errorf("error is missing %q:\n%s", tt.want, got)
			}
			if tt.absent != "" && strings.Contains(got, tt.absent) {
				t.Errorf("error carries %q, which the refusal must leave out:\n%s", tt.absent, got)
			}
		})
	}
}

// Test_resourceFieldsTypescriptType_rawMessageView pins that a view column follows the
// table's rule for json.RawMessage in each form: the same refusal, the same message.
func Test_resourceFieldsTypescriptType_rawMessageView(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field string
	}{
		{name: "the type itself", field: "Raw"},
		{name: "a pointer to it", field: "RawPtr"},
		{name: "a slice of it", field: "Raws"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c, structs := columnFixtureClient(t)
			res := fixtureResource(t, structs, "Bad", func(res *resourceInfo) {
				keepFields(res, "ID", tt.field)
				res.IsVirtual = true
			})
			err := columnFixtureGenerator(c).resourceFieldsTypescriptType(res)
			want := "Bads." + tt.field + ": " + rawMessageColumnRefusal
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("resourceFieldsTypescriptType() error = %v, want containing %q", err, want)
			}
			if strings.Contains(err.Error(), "generator writes; declare the type's TypeScript form") {
				t.Errorf("error carries the @typescript clause, which the refusal must leave out:\n%s", err)
			}
		})
	}
}

// Test_typescriptDecls_collision pins the one-name-one-module rule: two declarations
// importing the same name from different modules are refused naming both.
func Test_typescriptDecls_collision(t *testing.T) {
	t.Parallel()

	c, structs := columnFixtureClient(t)
	res := fixtureResource(t, structs, "Clashing", nil)
	err := columnFixtureGenerator(c).resourceFieldsTypescriptType(res)
	want := `columnfixture.Clash: @typescript(Point, from: "other-geo") and columnfixture.Position's @typescript(Point, from: "geojson") import the same name from different modules; rename one`
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("resourceFieldsTypescriptType() error = %v, want containing %q", err, want)
	}
}

// Test_wireWalker_declaredLeaves pins the declaration on the wire path: a declared type
// in an RPC request is the same imported leaf, a slice of one a list of it, and a struct
// reaching declared types mirrors them as imported leaves, so the methods file imports
// every module once; the byte slices, one bytes leaf in every shape; the JSON values,
// json.RawMessage and the types declared over it, unknown or as declared, never bytes;
// and the lists of the leaves whose display name is not their interface type.
func Test_wireWalker_declaredLeaves(t *testing.T) {
	t.Parallel()

	c, structs := columnFixtureClient(t)
	shape, err := newWireWalker(c.leaves(), "columnfixture").walk(structs["Request"])
	if err != nil {
		t.Fatalf("walk(Request) error = %v", err)
	}
	fields := make(map[string]*wireField, len(shape.Fields))
	for _, f := range shape.Fields {
		fields[f.Name] = f
	}

	tests := []struct {
		name        string
		field       string
		wantTS      string
		wantDisplay string
	}{
		{name: "a declared type is the imported name", field: "Where", wantTS: "Point", wantDisplay: "object"},
		{name: "a slice of a declared type in another package is a list of it", field: "Marks", wantTS: "Tag[]", wantDisplay: "object[]"},
		{name: "a struct reaching declared types is mirrored", field: "Doc", wantTS: "Request.Doc", wantDisplay: "object"},
		{name: "a byte slice is one bytes leaf, a string in the interface", field: "Seal", wantTS: "string", wantDisplay: "bytes"},
		{name: "a named byte slice is the bytes leaf", field: "Digest", wantTS: "string", wantDisplay: "bytes"},
		{name: "a pointer to a byte slice is a pointer to the leaf", field: "Sealed", wantTS: "string", wantDisplay: "bytes"},
		{name: "a slice of byte slices is a list of the leaf", field: "Chunks", wantTS: "string[]", wantDisplay: "bytes[]"},
		{name: "a slice of named byte slices is a list of the leaf", field: "Hashes", wantTS: "string[]", wantDisplay: "bytes[]"},
		{name: "a slice of int64 is a list of numbers", field: "Counts", wantTS: "number[]", wantDisplay: "number[]"},
		{name: "a slice of civil.Date is a list of Dates with the civildate display type", field: "Days", wantTS: "Date[]", wantDisplay: "civilDate[]"},
		{name: "a slice of UUIDs is a list of strings with the uuid display type", field: "IDs", wantTS: "string[]", wantDisplay: "uuid[]"},
		{name: "json.RawMessage is unknown, one opaque object", field: "Raw", wantTS: "unknown", wantDisplay: "object"},
		{name: "a type over json.RawMessage with no declaration is unknown", field: "Payload", wantTS: "unknown", wantDisplay: "object"},
		{name: "a type over json.RawMessage with a declaration is as declared", field: "Sheet", wantTS: "Sheet", wantDisplay: "object"},
		{name: "a type over a type with JSON methods resolves as that type does", field: "Stamp", wantTS: "Date", wantDisplay: "Date"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := fields[tt.field]
			if got := f.TypescriptType("Request"); got != tt.wantTS {
				t.Errorf("TypescriptType() = %q, want %q", got, tt.wantTS)
			}
			if got := f.TypescriptDisplayType(); got != tt.wantDisplay {
				t.Errorf("TypescriptDisplayType() = %q, want %q", got, tt.wantDisplay)
			}
		})
	}

	c.rpcMethods = []*rpcMethodInfo{{Struct: structs["Request"], Request: shape}}
	wantImports := []tsImportGroup{
		{From: "geojson", Names: []string{"Point"}},
		{From: "sheets", Names: []string{"Sheet"}},
		{From: "tags", Names: []string{"Tag"}},
	}
	if diff := cmp.Diff(wantImports, c.MethodTypeImports()); diff != "" {
		t.Errorf("MethodTypeImports() mismatch (-want +got):\n%s", diff)
	}
	wantNamespace := "export namespace Request {\n  export interface Doc {\n    where: Point;\n    tag: Tag;\n    body: unknown;\n  }\n}\n"
	if got := typescriptNamespace("Request", shape); got != wantNamespace {
		t.Errorf("typescriptNamespace() = %q, want %q", got, wantNamespace)
	}
}

// Test_groupImports pins the import lines: one per module, modules and names sorted,
// names once, built-ins left out, nil entries ignored.
func Test_groupImports(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		imports []*tsImport
		want    []tsImportGroup
	}{
		{name: "nothing imports nothing", imports: nil, want: []tsImportGroup{}},
		{
			name:    "built-ins and nil entries are left out",
			imports: []*tsImport{nil, {Name: "string"}, {Name: "unknown"}},
			want:    []tsImportGroup{},
		},
		{
			name:    "names group per module, sorted and once",
			imports: []*tsImport{{Name: "Point", From: "geojson"}, {Name: "Price", From: "money"}, {Name: "Money", From: "money"}, {Name: "Point", From: "geojson"}},
			want:    []tsImportGroup{{From: "geojson", Names: []string{"Point"}}, {From: "money", Names: []string{"Money", "Price"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, groupImports(tt.imports)); diff != "" {
				t.Errorf("groupImports() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
