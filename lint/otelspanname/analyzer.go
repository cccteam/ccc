package otelspanname

import (
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

func New() (*analysis.Analyzer, error) {
	return &analysis.Analyzer{
		Name: "ccc_otelspanname",
		Doc:  "Checks if otel span names match function names",
		Run:  run,
	}, nil
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
				if !isOtelStartCall(callExpr) {
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

				// Extract just the function name from the span name
				// spanArgStr := strings.Trim(spanArg.Value, "\"")
				spanSplit := strings.Split(strings.Trim(spanArg.Value, "\""), ".")
				if len(spanSplit) == 0 {
					return true
				}

				foundFuncName := spanSplit[len(spanSplit)-1]

				// Calculate the offset to report the error at the correct position
				offset := 0
				for _, part := range spanSplit[:len(spanSplit)-1] {
					offset += len(part) + 1 // +1 for the dot
				}

				if offset > 0 {
					offset += 1 // Account for the starting quote
				}

				// Check if the function name matches expected format
				expectedFuncName := funcName + "()"
				if foundFuncName != expectedFuncName {
					pass.Reportf(spanArg.Pos()+token.Pos(offset), "Incorrect span name: expected \"*.%s\", found %s", expectedFuncName, spanArg.Value)
				}

				return false
			})

			return true
		})
	}

	return nil, nil
}

// Checks if the function call is `otel.Tracer(...).Start(ctx, "...")`
func isOtelStartCall(callExpr *ast.CallExpr) bool {
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
