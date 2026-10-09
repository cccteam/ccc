package gcp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/cloud"
	"github.com/cccteam/logger"
)

// TestOpenWithoutProject: no logging project means console logs and no trace provider, and
// Close has nothing to release.
func TestOpenWithoutProject(t *testing.T) {
	t.Parallel()

	d, err := Open(t.Context(), Settings{}, "harbor")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, ok := d.LogExporter.(*logger.ConsoleExporter); !ok {
		t.Errorf("LogExporter = %T, want the console exporter", d.LogExporter)
	}
	if d.TraceProvider != nil {
		t.Errorf("TraceProvider = %v, want nil", d.TraceProvider)
	}
	if err := d.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// TestOpenRefusesBadSampling: the sampling is read before anything is opened.
func TestOpenRefusesBadSampling(t *testing.T) {
	t.Parallel()

	_, err := Open(t.Context(), Settings{TraceSampling: "half"}, "harbor")
	if err == nil || !strings.Contains(err.Error(), `"half" is not a sampling setting (edge, all)`) {
		t.Fatalf("Open() error = %v, want the sampling refused", err)
	}
}

// TestSettingsDeclaration holds the exported declaration to the struct: the import path
// and name, and each field's name, type and env tag, by reflection; each field's doc
// comment from the source, read the way the tools read a declared field's. A field
// renamed, retagged or re-documented without the declaration following fails here, in
// the module declaring it, and not in impulse or bedrock.
func TestSettingsDeclaration(t *testing.T) {
	t.Parallel()

	st := reflect.TypeFor[Settings]()
	docs := fieldDocs(t, st.Name())
	want := cloud.Declaration{Path: st.PkgPath(), Name: st.Name()}
	for i := range st.NumField() {
		f := st.Field(i)
		tag, ok := f.Tag.Lookup("env")
		if !ok {
			t.Fatalf("Settings.%s has no env tag; every field of the settings struct declares a variable", f.Name)
		}
		doc, ok := docs[f.Name]
		if !ok {
			t.Fatalf("Settings.%s is not in the source read for doc comments", f.Name)
		}
		want.Fields = append(want.Fields, cloud.Field{Name: f.Name, Type: f.Type.String(), Tag: tag, Doc: doc})
	}
	if diff := cmp.Diff(want, SettingsDeclaration()); diff != "" {
		t.Errorf("SettingsDeclaration() mismatch (-want +got):\n%s", diff)
	}
}

// fieldDocs reads the doc comment of every field of the named struct from the package's
// source files: the comment's text without its markers, one line per source line, as
// bedrock's reader and impulse's scan read a field the application declares.
func fieldDocs(t *testing.T, structName string) map[string]string {
	t.Helper()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("filepath.Glob() error = %v", err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parser.ParseFile(%s) error = %v", name, err)
		}
		if st := structNamed(f, structName); st != nil {
			docs := map[string]string{}
			for _, field := range st.Fields.List {
				for _, n := range field.Names {
					docs[n.Name] = strings.TrimSpace(field.Doc.Text())
				}
			}

			return docs
		}
	}
	t.Fatalf("no struct %s in the package's source", structName)

	return nil
}

// structNamed finds the struct type the file declares under the name, or nil.
func structNamed(f *ast.File, name string) *ast.StructType {
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != name {
				continue
			}
			if st, ok := ts.Type.(*ast.StructType); ok {
				return st
			}
		}
	}

	return nil
}
