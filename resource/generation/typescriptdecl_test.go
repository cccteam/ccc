package generation

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// Test_parseTypescriptDecl pins how a type's doc comment is read for its declaration:
// the annotation with and without a module, a quoted scoped module verbatim, prose and
// other lines ignored, and each malformed declaration refused naming the type.
func Test_parseTypescriptDecl(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		doc     string
		want    *tsImport
		wantErr string
	}{
		{name: "no declaration", doc: "Position is where the call was made.\n", want: nil},
		{name: "an empty doc", doc: "", want: nil},
		{
			name: "a name with a module",
			doc:  "Position is a GeoJSON Point.\n\n@typescript(Point, from: \"geojson\")\n",
			want: &tsImport{Name: "Point", From: "geojson"},
		},
		{
			name: "a scoped module is spelled verbatim",
			doc:  "@typescript(Document, from: \"@contentful/rich-text-types\")\n",
			want: &tsImport{Name: "Document", From: "@contentful/rich-text-types"},
		},
		{
			name: "a built-in alone",
			doc:  "Code is a string on the wire.\n@typescript(string)\n",
			want: &tsImport{Name: "string"},
		},
		{
			name: "other annotation lines in the doc are not judged here",
			doc:  "@enumerate(Kinds)\n@typescript(unknown)\n",
			want: &tsImport{Name: "unknown"},
		},
		{
			name:    "a built-in with a module",
			doc:     "@typescript(number, from: \"numbers\")\n",
			wantErr: `resources.T: @typescript(number, from: "numbers"): number is a TypeScript built-in, which no module exports; drop from:`,
		},
		{
			name:    "a non-built-in without a module",
			doc:     "@typescript(Money)\n",
			wantErr: `resources.T: @typescript(Money): Money is not a TypeScript built-in (string, number, boolean, unknown); add from: "module" naming the module that exports it`,
		},
		{
			name:    "a name that is no identifier",
			doc:     "@typescript(Geo.Point, from: \"geojson\")\n",
			wantErr: `resources.T: @typescript(Geo.Point): "Geo.Point" is not a TypeScript identifier`,
		},
		{
			name:    "a module with no name",
			doc:     "@typescript(from: \"geojson\")\n",
			wantErr: "expected 1 positional argument(s), found 0",
		},
		{
			name:    "an unknown argument",
			doc:     "@typescript(Point, module: \"geojson\")\n",
			wantErr: `unknown argument "module"`,
		},
		{
			name:    "a second declaration",
			doc:     "@typescript(Point, from: \"geojson\")\n@typescript(Point, from: \"geojson\")\n",
			wantErr: "typescript used twice here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseTypescriptDecl("resources.T", tt.doc)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseTypescriptDecl() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("parseTypescriptDecl() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("parseTypescriptDecl() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// Test_rejectTypescriptAnnotation pins that the declaration belongs on a type used as a
// field, never on a struct the generator extracts as a row or a request.
func Test_rejectTypescriptAnnotation(t *testing.T) {
	t.Parallel()

	structs := fixtureStructs(loadFixture(t, "columnfixture"))
	annotated, err := scanStruct(structs["Label"])
	if err != nil {
		t.Fatalf("scanStruct(Label) error = %v", err)
	}
	plain, err := scanStruct(structs["Provenance"])
	if err != nil {
		t.Fatalf("scanStruct(Provenance) error = %v", err)
	}

	tests := []struct {
		name       string
		structName string
		kind       string
		wantErr    string
	}{
		{name: "a declaration on a resource struct is refused", structName: "Label", kind: "table-backed resource", wantErr: "struct Label: @typescript declares the TypeScript type of a type used as a field; this struct is a table-backed resource, typed field by field"},
		{name: "a declaration on a method struct is refused", structName: "Label", kind: "RPC method", wantErr: "this struct is a RPC method"},
		{name: "a struct without one passes", structName: "Provenance", kind: "table-backed resource"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			annotations := plain
			if tt.structName == "Label" {
				annotations = annotated
			}
			err := rejectTypescriptAnnotation(structs[tt.structName], annotations, tt.kind)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("rejectTypescriptAnnotation() error = %v, want nil", err)
				}

				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("rejectTypescriptAnnotation() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
