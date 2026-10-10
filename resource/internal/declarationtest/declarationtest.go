// Package declarationtest holds a driver's settings declaration to its settings struct,
// for the drivers' tests: the live, job and database drivers each publish a declaration
// in the package beside them, and each driver's test calls Hold on it, as the cloud
// driver's test does on its own, so a field renamed, retagged or re-documented without
// the declaration following fails in the resource module and never only in a tool
// reading it.
package declarationtest

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
)

// Hold compares the declaration a driver publishes with its settings struct: the import
// path and name, and each field's name, type and env tag, by reflection; each field's doc
// comment from the package's source in the working directory, read the way the tools
// read a declared field's. Every field of the struct must carry an env tag.
func Hold(t *testing.T, settings reflect.Type, got cloud.Declaration) {
	t.Helper()

	docs := fieldDocs(t, settings.Name())
	want := cloud.Declaration{Path: settings.PkgPath(), Name: settings.Name()}
	for i := range settings.NumField() {
		f := settings.Field(i)
		tag, ok := f.Tag.Lookup("env")
		if !ok {
			t.Fatalf("%s.%s has no env tag; every field of the settings struct declares a variable", settings.Name(), f.Name)
		}
		doc, ok := docs[f.Name]
		if !ok {
			t.Fatalf("%s.%s is not in the source read for doc comments", settings.Name(), f.Name)
		}
		want.Fields = append(want.Fields, cloud.Field{Name: f.Name, Type: f.Type.String(), Tag: tag, Doc: doc})
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("the declaration does not match the struct (-want +got):\n%s", diff)
	}
}

// fieldDocs reads the doc comment of every field of the named struct from the Go files
// of the working directory, the package under test: the comment's text without its
// markers, one line per source line, as bedrock's reader and impulse's scan read a field
// the application declares.
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
