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

	tests := []struct {
		name           string
		file           string
		makeReport     func(t *testing.T) func(d analysis.Diagnostic)
		expectedReport bool
	}{
		{
			name: "Nested if with inner assignment",
			file: "testdata/nested_if_test.go",
			makeReport: func(t *testing.T) func(d analysis.Diagnostic) {
				return func(d analysis.Diagnostic) {
					t.Logf("Diagnostic: %s at %v", d.Message, d.Pos)
					t.Fail()
				}
			},
			expectedReport: false,
		},
		{
			name: "Error wrap with logical AND error check",
			file: "testdata/logical_and_err_check.go",
			makeReport: func(t *testing.T) func(d analysis.Diagnostic) {
				return func(d analysis.Diagnostic) {
					switch d.Message[46:] {
					case `expected "*.method2()", found "t.s1.method1().incorrect()"`:
					case `expected "*.method2()", found "t.s1.method1().incorrect2()"`:
						// These are expected diagnostics
					default:
						t.Logf("Unexpected Diagnostic: %s at %v", d.Message, d.Pos)
						t.Fail()
					}
				}
			},
			expectedReport: true,
		},
		{
			name: "For range with error assignment",
			file: "testdata/for_err_test.go",
			makeReport: func(t *testing.T) func(d analysis.Diagnostic) {
				return func(d analysis.Diagnostic) {
					t.Logf("Diagnostic: %s at %v", d.Message, d.Pos)
					t.Fail()
				}
			},
			expectedReport: false,
		},
		{
			name: "Error wrap with no function call",
			file: "testdata/err_wrap_no_fn_call.go",
			makeReport: func(t *testing.T) func(d analysis.Diagnostic) {
				return func(d analysis.Diagnostic) {
					t.Logf("Diagnostic: %s at %v", d.Message, d.Pos)
					t.Fail()
				}
			},
			expectedReport: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create a new ast.File from the testdata/nested_if_test.go file
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, tt.file, nil, parser.ParseComments)
			if err != nil {
				t.Fatalf("Failed to parse source: %v", err)
			}

			var reportCalled bool

			pass := &analysis.Pass{
				Files: []*ast.File{file},
				Report: func(d analysis.Diagnostic) {
					tt.makeReport(t)(d)

					reportCalled = true
				},
			}

			ret, err := run(pass)
			if err != nil {
				t.Fatalf("Analyzer run failed: %v", err)
			}
			if ret != nil {
				t.Logf("Analyzer returned: %v", ret)
			}

			if tt.expectedReport && !reportCalled {
				t.Fatal("Expected report was not called")
			}
		})
	}
}
