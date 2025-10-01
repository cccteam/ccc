package errwrap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestNestedScopeAssignment(t *testing.T) {
	t.Parallel()

	// Create a new ast.File from the testdata/test.go file
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "testdata/test.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	pass := &analysis.Pass{
		Files: []*ast.File{file},
		Report: func(d analysis.Diagnostic) {
			// We expect no diagnostic reports, so log and fail if we get one
			t.Logf("Diagnostic: %s at %v", d.Message, fset.Position(d.Pos))
			t.Fail()
		},
	}

	ret, err := run(pass)
	if err != nil {
		t.Fatalf("Analyzer run failed: %v", err)
	}
	if ret != nil {
		t.Logf("Analyzer returned: %v", ret)
	}
}
