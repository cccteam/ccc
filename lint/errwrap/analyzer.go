package errwrap

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

func New() (*analysis.Analyzer, error) {
	return &analysis.Analyzer{
		Name: "ccc_errwrap",
		Doc:  "Checks if error wrapping has the correct function name",
		Run:  run,
	}, nil
}

func run(pass *analysis.Pass) (interface{}, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			stmt, ok := node.(*ast.IfStmt)
			if !ok {
				return true
			}

			// Ensure it is checking for an error
			binExpr, ok := stmt.Cond.(*ast.BinaryExpr)
			if !ok {
				return true
			}

			if binExpr.Op != token.NEQ {
				return true
			}

			ident, ok := binExpr.X.(*ast.Ident)
			if !ok || ident.Name != "err" {
				return true
			}

			// Look for return statements inside the if block
			ast.Inspect(stmt.Body, func(n ast.Node) bool {
				retStmt, ok := n.(*ast.ReturnStmt)
				if !ok {
					return true
				}

				for _, expr := range retStmt.Results {
					callExpr, ok := expr.(*ast.CallExpr)
					if !ok {
						continue
					}

					fun, ok := callExpr.Fun.(*ast.SelectorExpr)
					if !ok {
						continue
					}

					ident, ok := fun.X.(*ast.Ident)
					if !ok || ident.Name != "errors" || fun.Sel.Name != "Wrap" {
						continue
					}

					// Check if second argument is a string
					if len(callExpr.Args) == 2 {
						lit, ok := callExpr.Args[1].(*ast.BasicLit)
						if !ok {
							continue
						}

						expected := getExpectedFunctionName(stmt)

						if expected == "" || strings.Contains(lit.Value, expected) {
							continue
						}

						// Calculate the offset to report the error at the correct position
						offset := 0
						if strings.Contains(lit.Value, ".") {
							argSplit := strings.Split(strings.Trim(lit.Value, "\""), ".")

							for _, part := range argSplit[:len(argSplit)-1] {
								offset += len(part) + 1 // +1 for the dot
							}

							if offset > 0 {
								offset += 1 // Account for the starting quote
							}
						}

						pass.Reportf(lit.Pos()+token.Pos(offset), "error wrapping message should match function: expected \"*.%s\", found %s", expected, lit.Value)
					}
				}

				return true
			})

			return true
		})
	}

	return nil, nil
}

func getExpectedFunctionName(stmt *ast.IfStmt) string {
	assignStmt, ok := stmt.Init.(*ast.AssignStmt)
	if !ok {
		return ""
	}

	for _, expr := range assignStmt.Rhs {
		callExpr, ok := expr.(*ast.CallExpr)
		if !ok {
			continue
		}

		fun, ok := callExpr.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}

		return fmt.Sprintf("%s()", fun.Sel.Name)
	}

	return ""
}
