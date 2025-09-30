// Package errwrap defines a linter that checks if error wrapping has the correct function name.
package errwrap

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// New creates a new instance of the errwrap analyzer.
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
			if stmt, ok := node.(*ast.IfStmt); ok {
				checkErrorHandlingStatement(pass, stmt)
			}

			return true
		})
	}

	return nil, nil
}

// checkErrorHandlingStatement checks if an if statement is handling an error and validates error wrapping
func checkErrorHandlingStatement(pass *analysis.Pass, stmt *ast.IfStmt) {
	if !isErrorCheckStatement(stmt) {
		return
	}

	// Look for return statements inside the if block
	ast.Inspect(stmt.Body, func(n ast.Node) bool {
		if retStmt, ok := n.(*ast.ReturnStmt); ok {
			checkReturnStatement(pass, stmt, retStmt)
		}

		return true
	})
}

// isErrorCheckStatement checks if the if statement is checking for an error (err != nil)
func isErrorCheckStatement(stmt *ast.IfStmt) bool {
	binExpr, ok := stmt.Cond.(*ast.BinaryExpr)
	if !ok || binExpr.Op != token.NEQ {
		return false
	}

	ident, ok := binExpr.X.(*ast.Ident)

	return ok && ident.Name == "err"
}

// checkReturnStatement checks return statements for error wrapping calls
func checkReturnStatement(pass *analysis.Pass, stmt *ast.IfStmt, retStmt *ast.ReturnStmt) {
	for _, expr := range retStmt.Results {
		if callExpr, ok := expr.(*ast.CallExpr); ok {
			checkErrorWrapCall(pass, stmt, callExpr)
		}
	}
}

// checkErrorWrapCall checks if a call expression is an errors.Wrap call and validates it
func checkErrorWrapCall(pass *analysis.Pass, stmt *ast.IfStmt, callExpr *ast.CallExpr) {
	if !isErrorsWrapCall(callExpr) {
		return
	}

	// Check if second argument is a string
	if len(callExpr.Args) != 2 {
		return
	}

	lit, ok := callExpr.Args[1].(*ast.BasicLit)
	if !ok {
		return
	}

	expected := getExpectedFunctionName(stmt)
	if expected == "" || strings.Contains(lit.Value, expected) {
		return
	}

	offset := calculateErrorOffset(lit.Value)
	pass.Reportf(lit.Pos()+token.Pos(offset), "error wrapping message should match function: expected \"*.%s\", found %s", expected, lit.Value)
}

// isErrorsWrapCall checks if a call expression is errors.Wrap
func isErrorsWrapCall(callExpr *ast.CallExpr) bool {
	fun, ok := callExpr.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	ident, ok := fun.X.(*ast.Ident)

	return ok && ident.Name == "errors" && fun.Sel.Name == "Wrap"
}

// calculateErrorOffset calculates the position offset for error reporting
func calculateErrorOffset(value string) int {
	if !strings.Contains(value, ".") {
		return 0
	}

	offset := 0
	argSplit := strings.Split(strings.Trim(value, "\""), ".")

	for _, part := range argSplit[:len(argSplit)-1] {
		offset += len(part) + 1 // +1 for the dot
	}

	if offset > 0 {
		offset++ // Account for the starting quote
	}

	return offset
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
