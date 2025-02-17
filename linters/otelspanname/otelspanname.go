// otelspanname is a custom linter that checks if otel span names match function names.
package main

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

//go:generate go build -buildmode=plugin -o otelspanname.so otelspanname.go

func New(conf any) ([]*analysis.Analyzer, error) {
	return []*analysis.Analyzer{{
		Name: "otelspanname",
		Doc:  "Checks if otel span names match function names",
		Run:  run,
	}}, nil
}

func run(pass *analysis.Pass) (interface{}, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			// Look for function declarations
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Name == nil {
				return true
			}

			funcName := fn.Name.Name // Extract function name

			// Search for `otel.Tracer(...).Start(ctx, "...")`
			ast.Inspect(fn.Body, func(subNode ast.Node) bool {
				callExpr, ok := subNode.(*ast.CallExpr)
				if !ok {
					return true
				}

				// Ensure function call is `Start(ctx, "...")`
				if !isOtelStartCall(pass, callExpr) {
					return true
				}

				// Extract the span name argument
				if len(callExpr.Args) < 2 {
					return true
				}

				spanArg, ok := callExpr.Args[1].(*ast.BasicLit)
				if !ok || spanArg.Kind != token.STRING {
					return true
				}

				spanSplit := strings.Split(strings.Trim(spanArg.Value, "\""), ".")
				if len(spanSplit) == 0 {
					return true
				}

				spanName := spanSplit[len(spanSplit)-1]

				// Check if the span name matches expected format
				expectedSpanName := funcName + "()"
				if spanName != expectedSpanName {
					pass.Reportf(spanArg.Pos(), "Incorrect span name: expected %q, found %q", expectedSpanName, spanName)
				}

				return false
			})

			return true
		})
	}

	return nil, nil
}

// Checks if the function call is `otel.Tracer(...).Start(ctx, "...")`
func isOtelStartCall(_ *analysis.Pass, callExpr *ast.CallExpr) bool {
	selector, ok := callExpr.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel == nil || selector.Sel.Name != "Start" {
		return false
	}

	// Ensure receiver is `otel.Tracer`
	if ident, ok := selector.X.(*ast.CallExpr); ok {
		if sel, ok := ident.Fun.(*ast.SelectorExpr); ok {
			if pkgIdent, ok := sel.X.(*ast.Ident); ok && pkgIdent.Name == "otel" && sel.Sel.Name == "Tracer" {
				return true
			}
		}
	}

	return false
}
