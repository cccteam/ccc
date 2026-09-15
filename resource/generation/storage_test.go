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
// generator writes into.
func columnFixtureResourceGenerator(t *testing.T) (*resourceGenerator, *resourceInfo) {
	t.Helper()

	c, structs := columnFixtureClient(t)
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
// a plain struct, a named slice of structs, and a declared type over anything but a
// basic type, on a JSON column, when the type implements no Spanner methods of its own
// (a generated pair found from the previous run counts as none, so the output is a fixed
// point); and the refusals: a column that is not JSON, an unnamed slice, and a type
// declared where the generator writes nothing.
func Test_resolveColumnStorage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fields  []string
		mutate  func(*resourceInfo)
		want    []string
		wantErr string
	}{
		{
			name:   "structs, a named slice, and declared types get methods; hand-written and basic-typed ones do not",
			fields: []string{"Provenance", "Attachments", "Manifests", "Position", "Label", "Code", "Doc", "Blob", "Kind", "Tags"},
			want:   []string{"Doc", "Label", "Manifests", "Position", "Provenance"},
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
			wantErr: "Rows.Tag: shapes.Tag is stored by generated JSON methods, but it is declared in github.com/cccteam/ccc/resource/generation/testdata/columnfixture/shapes, where the generator writes nothing; declare the type in the resources package, or implement EncodeSpanner and DecodeSpanner on it",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, res := columnFixtureResourceGenerator(t)
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
			if diff := cmp.Diff(tt.want, got[r.resource]); diff != "" {
				t.Errorf("resolveColumnStorage() mismatch (-want +got):\n%s", diff)
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
