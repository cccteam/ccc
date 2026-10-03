package generation

import (
	"go/format"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/generation/parser"
	"github.com/google/go-cmp/cmp"
)

// timeTimeType is time.Time as a right-hand side renders, the one library type the pairs
// convert through in these tests.
const timeTimeType = "time.Time"

// Test_resolveJSONMethods pins which defined types get the generated JSON pair: a type
// declared over json.RawMessage, with or without a declaration, and one over a type with
// JSON methods that is no byte slice, when the type implements no JSON methods of its
// own (the fixture's zz_gen_json.go already carries Payload's pair, found from the
// previous run and counted as none, so the output is a fixed point); none for a plain
// struct or a basic type on the right-hand side, for a type with its own methods, or for
// a built-in row; the pair reached through a derived struct's field and through an RPC
// request; and the refusals: one method without the other, and a type declared where the
// generator writes nothing, naming WithTypes among the fixes, which a WithTypes package
// escapes.
func Test_resolveJSONMethods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// resource names the fixture struct read as a table, fields the columns kept.
		resource string
		fields   []string
		// request names the fixture struct read as an RPC request instead.
		request string
		types   []packageDir
		wantDir packageDir
		want    []jsonPair
		wantErr string
	}{
		{
			name:     "types over json.RawMessage and over a type with methods get the pair; own methods, plain structs, and basic types do not",
			resource: "Row",
			fields:   []string{"Payload", "Sheet", "Stamp", "Position", "Wrapped", "Provenance", "Code", "Kind", "Blob", "Tag"},
			want:     []jsonPair{{Name: "Payload", Over: "json.RawMessage"}, {Name: "Sheet", Over: "json.RawMessage"}, {Name: "Stamp", Over: timeTimeType}},
		},
		{
			name:     "a pair is reached through a derived struct's field",
			resource: "Row",
			fields:   []string{"Doc"},
			want:     []jsonPair{{Name: "Payload", Over: "json.RawMessage"}},
		},
		{
			name:     "a slice of a type over json.RawMessage reaches the element's pair",
			resource: "Row",
			fields:   []string{"Payloads"},
			want:     []jsonPair{{Name: "Payload", Over: "json.RawMessage"}},
		},
		{
			name:    "an RPC request's fields are covered",
			request: "Request",
			want:    []jsonPair{{Name: "Payload", Over: "json.RawMessage"}, {Name: "Sheet", Over: "json.RawMessage"}, {Name: "Stamp", Over: timeTimeType}},
		},
		{
			name:     "a resource with only built-in rows needs no file",
			resource: "Row",
			fields:   []string{"ID", "Note", "Tags", "Seal"},
			want:     nil,
		},
		{
			name:     "one method without the other is refused",
			resource: "Partial",
			fields:   []string{"Half"},
			wantErr:  "Partials.Half: columnfixture.Half implements one of MarshalJSON and UnmarshalJSON but not the other; implement both, or neither and let the generator carry the type",
		},
		{
			name:     "a type declared where the generator writes nothing is refused naming the three fixes",
			resource: "Row",
			fields:   []string{"Memo"},
			wantErr:  "Rows.Memo: shapes.Memo is carried by generated JSON methods, but it is declared in github.com/cccteam/ccc/resource/generation/testdata/columnfixture/shapes, where the generator writes nothing; declare the type in the resources package, implement MarshalJSON and UnmarshalJSON on it, or name its package with WithTypes",
		},
		{
			name:     "a type in a package WithTypes does not name is refused the same way",
			resource: "Row",
			fields:   []string{"Shared"},
			wantErr:  "Rows.Shared: shared.Manifest is carried by generated JSON methods, but it is declared in github.com/cccteam/ccc/resource/generation/testdata/columnfixture/shared, where the generator writes nothing",
		},
		{
			name:     "a type in a WithTypes package gets its pair there",
			resource: "Row",
			fields:   []string{"Shared"},
			types:    []packageDir{sharedDir},
			wantDir:  sharedDir,
			want:     []jsonPair{{Name: "Manifest", Over: "json.RawMessage"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, _ := columnFixtureResourceGenerator(t)
			r.types = tt.types
			switch {
			case tt.request != "":
				r.rpcMethods = []*rpcMethodInfo{fixtureJSONRequest(t, r, tt.request)}
			default:
				res := fixtureResource(t, fixtureStructsOf(t, r), tt.resource, nil)
				keepFields(res, tt.fields...)
				r.resources = []*resourceInfo{res}
			}

			got, err := r.resolveJSONMethods()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveJSONMethods() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveJSONMethods() error = %v", err)
			}
			dir := tt.wantDir
			if dir == "" {
				dir = r.resource
			}
			if diff := cmp.Diff(tt.want, got[dir], cmp.AllowUnexported(jsonPair{}), cmp.FilterPath(func(p cmp.Path) bool {
				return p.Last().String() == ".imports"
			}, cmp.Ignore())); diff != "" {
				t.Errorf("resolveJSONMethods() mismatch (-want +got):\n%s", diff)
			}
			for other := range got {
				if other != dir {
					t.Errorf("resolveJSONMethods() wrote into %s: %v", other, got[other])
				}
			}
		})
	}
}

// fixtureStructsOf re-reads the fixture's structs for a generator built over it.
func fixtureStructsOf(t *testing.T, r *resourceGenerator) map[string]*parser.Struct {
	t.Helper()

	loaded := r.loadedPackages["columnfixture"]
	if loaded == nil {
		t.Fatal("columnfixture not loaded")
	}

	return fixtureStructs(parser.ParsePackage(loaded))
}

// fixtureJSONRequest reads a column fixture struct as an RPC request: its fields as the
// method's, with no result, for the JSON pass alone.
func fixtureJSONRequest(t *testing.T, r *resourceGenerator, name string) *rpcMethodInfo {
	t.Helper()

	s := fixtureStructsOf(t, r)[name]
	if s == nil {
		t.Fatalf("struct %q not in fixture", name)
	}
	method := &rpcMethodInfo{Struct: s}
	for _, f := range s.Fields() {
		method.Fields = append(method.Fields, &rpcField{Field: f})
	}

	return method
}

// Test_jsonFileTemplate pins the generated JSON file: against the fixture's copy for a
// type over json.RawMessage, gofmt clean, so the fixture stays what the generator would
// write; and through the import fixer for a right-hand side from another package, whose
// import the fixer adds.
func Test_jsonFileTemplate(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	fixture, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "testdata", "columnfixture", "zz_gen_json.go"))
	if err != nil {
		t.Fatalf("reading the fixture's JSON file: %v", err)
	}

	tests := []struct {
		name string
		data *jsonFileData
		// want is the whole file, when pinned against the fixture.
		want string
		// wantContains are fragments the fixed output carries.
		wantContains []string
	}{
		{
			name: "a type over json.RawMessage renders the fixture's file",
			data: &jsonFileData{Source: "testdata/columnfixture", Package: "columnfixture", Types: []jsonPair{{Name: "Payload", Over: "json.RawMessage"}}},
			want: string(fixture),
		},
		{
			name: "a right-hand side from another package brings its import",
			data: &jsonFileData{Source: "testdata/columnfixture", Package: "columnfixture", Types: []jsonPair{
				{Name: "Payload", Over: "json.RawMessage"},
				{Name: "Stamp", Over: timeTimeType, imports: []fixerImport{{name: "time", path: "time"}}},
			}},
			wantContains: []string{
				"import (\n\t\"encoding/json\"\n\t\"time\"\n)",
				"func (v Stamp) MarshalJSON() ([]byte, error) {\n\treturn json.Marshal(time.Time(v))\n}",
				"func (v *Stamp) UnmarshalJSON(b []byte) error {\n\treturn json.Unmarshal(b, (*time.Time)(v))\n}",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &client{}
			out, err := c.generateTemplateOutput("jsonFileTemplate", jsonFileTemplate, tt.data)
			if err != nil {
				t.Fatalf("generateTemplateOutput() error = %v", err)
			}
			fixed, err := c.formatGoBytes("zz_gen_json.go", "jsonFileTemplate", out, tt.data)
			if err != nil {
				t.Fatalf("formatGoBytes() error = %v:\n%s", err, out)
			}
			if _, err := format.Source(fixed); err != nil {
				t.Fatalf("format.Source() error = %v:\n%s", err, fixed)
			}
			if tt.want != "" {
				if diff := cmp.Diff(tt.want, string(fixed)); diff != "" {
					t.Errorf("JSON file mismatch (-fixture +rendered):\n%s", diff)
				}
			}
			for _, fragment := range tt.wantContains {
				if !strings.Contains(string(fixed), fragment) {
					t.Errorf("rendered file is missing %q:\n%s", fragment, fixed)
				}
			}
		})
	}
}
