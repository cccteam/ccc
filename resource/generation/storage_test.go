package generation

import (
	"go/format"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// columnFixtureResourceGenerator builds a resource generator over the column fixture
// whose resources package is the fixture, so its declared types are in a package the
// generator writes into, with the shared package loaded as WithTypes would load it.
func columnFixtureResourceGenerator(t *testing.T) (*resourceGenerator, *resourceInfo) {
	t.Helper()

	c, structs := columnFixtureClientWithShared(t)
	c.resource = packageDir("generation/testdata/columnfixture")
	res := fixtureResource(t, structs, "Row", func(res *resourceInfo) {
		for _, f := range res.Fields {
			f.SpannerType = jsonSpannerType
		}
	})

	return &resourceGenerator{client: c}, res
}

// keepFields narrows a resource to the named fields.
func keepFields(res *resourceInfo, names ...string) {
	res.Fields = slices.DeleteFunc(res.Fields, func(f *resourceField) bool {
		return !slices.Contains(names, f.Name())
	})
}

// Test_resolveColumnStorage pins which column types get the generated Spanner methods:
// a plain struct, a named slice of structs, a declared type over anything but a basic
// type, and a type JSON by declaration (over json.RawMessage with or without a
// declaration, or over a type with JSON methods), on a JSON column, when the type
// implements no Spanner methods of its own (a generated pair found from the previous run
// counts as none, so the output is a fixed point); and the refusals: a column that is
// not JSON, an unnamed slice, and a type declared where the generator writes nothing,
// naming WithTypes among the fixes, which a WithTypes package escapes.
func Test_resolveColumnStorage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fields  []string
		mutate  func(*resourceInfo)
		types   []packageDir
		wantDir packageDir
		want    []string
		wantErr string
	}{
		{
			name:   "structs, a named slice, and declared types get methods; hand-written and basic-typed ones do not",
			fields: []string{"Provenance", "Attachments", "Manifests", "Position", "Label", "Code", "Doc", "Blob", "Kind", "Tags"},
			want:   []string{"Doc", "Label", "Manifests", "Position", "Provenance"},
		},
		{
			name:   "types JSON by declaration get methods, a built-in row and a basic-typed declaration do not",
			fields: []string{"Payload", "Sheet", "Stamp", "Wrapped", "Blob", "Code"},
			want:   []string{"Payload", "Sheet", "Stamp", "Wrapped"},
		},
		{
			name:    "an unnamed slice of a type JSON by declaration has nothing to attach methods to",
			fields:  []string{"Payloads"},
			wantErr: "Rows.Payloads: []columnfixture.Payload is stored as JSON, but an unnamed slice carries no methods; declare a named slice type for the column",
		},
		{
			name:    "a type JSON by declaration in a package WithTypes does not name is refused naming the option",
			fields:  []string{"Shared"},
			wantErr: "Rows.Shared: shared.Manifest is stored by generated JSON methods, but it is declared in github.com/cccteam/ccc/resource/generation/testdata/columnfixture/shared, where the generator writes nothing; declare the type in the resources package, implement EncodeSpanner and DecodeSpanner on it, or name its package with WithTypes",
		},
		{
			name:    "a type in a WithTypes package gets its methods there",
			fields:  []string{"Shared"},
			types:   []packageDir{sharedDir},
			wantDir: sharedDir,
			want:    []string{"Manifest"},
		},
		{
			name:   "a resource with only native columns needs no file",
			fields: []string{"ID", "Kind", "Note", "Tags"},
			want:   nil,
		},
		{
			name:   "a struct on a column that is not JSON is refused",
			fields: []string{"Provenance"},
			mutate: func(res *resourceInfo) {
				res.Fields[0].SpannerType = "STRING(MAX)"
			},
			wantErr: "Rows.Provenance: columnfixture.Provenance is stored by generated JSON methods, but column Provenance is STRING(MAX); declare the column JSON, or implement EncodeSpanner and DecodeSpanner on the type",
		},
		{
			name:    "an unnamed slice of a declared type has nothing to attach methods to",
			fields:  []string{"Positions"},
			wantErr: "Rows.Positions: []columnfixture.Position is stored as JSON, but an unnamed slice carries no methods; declare a named slice type for the column",
		},
		{
			name:    "a type declared where the generator writes nothing is refused",
			fields:  []string{"Tag"},
			wantErr: "Rows.Tag: shapes.Tag is stored by generated JSON methods, but it is declared in github.com/cccteam/ccc/resource/generation/testdata/columnfixture/shapes, where the generator writes nothing; declare the type in the resources package, implement EncodeSpanner and DecodeSpanner on it, or name its package with WithTypes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, res := columnFixtureResourceGenerator(t)
			r.types = tt.types
			keepFields(res, tt.fields...)
			if tt.mutate != nil {
				tt.mutate(res)
			}
			r.resources = []*resourceInfo{res}

			got, err := r.resolveColumnStorage()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("resolveColumnStorage() error = %v, want containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("resolveColumnStorage() error = %v", err)
			}
			dir := tt.wantDir
			if dir == "" {
				dir = r.resource
			}
			if diff := cmp.Diff(tt.want, got[dir]); diff != "" {
				t.Errorf("resolveColumnStorage() mismatch (-want +got):\n%s", diff)
			}
			for other := range got {
				if other != dir {
					t.Errorf("resolveColumnStorage() wrote into %s: %v", other, got[other])
				}
			}
		})
	}
}

// Test_storageFileTemplate pins the generated storage file against the fixture's copy:
// the same types render the same file, gofmt clean, so the fixture stays what the
// generator would write.
func Test_storageFileTemplate(t *testing.T) {
	t.Parallel()

	c := &client{}
	out, err := c.generateTemplateOutput("storageFileTemplate", storageFileTemplate, &storageFileData{
		Source:  "testdata/columnfixture",
		Package: "columnfixture",
		Types:   []string{"Provenance"},
	})
	if err != nil {
		t.Fatalf("generateTemplateOutput() error = %v", err)
	}
	formatted, err := format.Source(out)
	if err != nil {
		t.Fatalf("format.Source() error = %v:\n%s", err, out)
	}

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	want, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "testdata", "columnfixture", "zz_gen_storage.go"))
	if err != nil {
		t.Fatalf("reading the fixture's storage file: %v", err)
	}
	if diff := cmp.Diff(string(want), string(formatted)); diff != "" {
		t.Errorf("storage file mismatch (-fixture +rendered):\n%s", diff)
	}
}
