// Package errwrap defines a linter that checks if error wrapping has the correct function name.
package errwrap

import (
	"fmt"
	"go/ast"
	"go/token"
	"log"
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
				log.Printf("Inspecting if statement at %v", stmt.Pos())

				checkErrorHandlingStatement(pass, file, stmt)
			}

			return true
		})
	}

	return nil, nil
}

// checkErrorHandlingStatement checks if an if statement is handling an error and validates error wrapping
func checkErrorHandlingStatement(pass *analysis.Pass, file *ast.File, stmt *ast.IfStmt) {
	if !isErrorCheckStatement(stmt) {
		return
	}

	log.Printf("Found error handling if statement at %v", stmt.Pos())

	// Look for return statements inside the if block
	ast.Inspect(stmt.Body, func(node ast.Node) bool {
		// Skip nested if statements here, let the outer call handle them so we're not handling them twice
		if _, ok := node.(*ast.IfStmt); ok {
			return false
		}

		if retStmt, ok := node.(*ast.ReturnStmt); ok {
			checkReturnStatement(pass, file, stmt, retStmt)
		}

		return true
	})
}

// isErrorCheckStatement checks if the if statement is checking for an error (err != nil)
func isErrorCheckStatement(stmt *ast.IfStmt) bool {
	if stmt.Cond == nil {
		return false
	}

	// Check for binary expression (err != nil)
	binExpr, ok := stmt.Cond.(*ast.BinaryExpr)
	if !ok {
		return false
	}

	if binExpr.Op == token.NEQ && isErrNotNilCheck(binExpr) {
		return true
	}

	if binExpr.Op == token.LAND {
		if binExpr.X != nil {
			// Check for binary expression (err != nil)
			left, ok := binExpr.X.(*ast.BinaryExpr)
			if ok && left.Op == token.NEQ && isErrNotNilCheck(left) {
				return true
			}
		}

		if binExpr.Y != nil {
			// Check for binary expression (err != nil)
			right, ok := binExpr.Y.(*ast.BinaryExpr)
			if ok && right.Op == token.NEQ && isErrNotNilCheck(right) {
				return true
			}
		}
	}

	return false
}

func isErrNotNilCheck(binExpr *ast.BinaryExpr) bool {
	ident, ok := binExpr.X.(*ast.Ident)

	return ok && ident.Name == "err"
}

// checkReturnStatement checks return statements for error wrapping calls
func checkReturnStatement(pass *analysis.Pass, file *ast.File, stmt *ast.IfStmt, retStmt *ast.ReturnStmt) {
	for _, expr := range retStmt.Results {
		if callExpr, ok := expr.(*ast.CallExpr); ok {
			log.Printf("Found return statement with call expression at %v", retStmt.Pos())

			checkErrorWrapCall(pass, file, stmt, callExpr)
		}
	}
}

// checkErrorWrapCall checks if a call expression is an errors.Wrap call and validates it
func checkErrorWrapCall(pass *analysis.Pass, file *ast.File, stmt *ast.IfStmt, callExpr *ast.CallExpr) {
	if !isErrorsWrapCall(callExpr) {
		return
	}

	// Check if second argument is a string
	if len(callExpr.Args) != 2 {
		return
	}

	// Second argument should be a string literal
	lit, ok := callExpr.Args[1].(*ast.BasicLit)
	if !ok {
		return
	}

	log.Printf("Found errors.Wrap call with message: %s", lit.Value)

	expected := getExpectedFunctionName(file, stmt)
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

func getExpectedFunctionName(file *ast.File, stmt *ast.IfStmt) string {
	// First try to get function name from if statement init
	if stmt.Init != nil {
		if assignStmt, ok := stmt.Init.(*ast.AssignStmt); ok {
			if funcName := extractFunctionNameFromAssignment(assignStmt); funcName != "" {
				log.Printf("Extracted function name from if init: %s", funcName)

				return funcName
			}
		}
	}

	// If not found in init, look for preceding assignment statements
	return findPrecedingAssignment(file, stmt)
}

// extractFunctionNameFromAssignment extracts function name from an assignment statement
func extractFunctionNameFromAssignment(assignStmt *ast.AssignStmt) string {
	for _, expr := range assignStmt.Rhs {
		if funcName := extractFunctionNameFromExpr(expr); funcName != "" {
			return funcName
		}
	}

	return ""
}

// extractFunctionNameFromExpr extracts function name from an expression
func extractFunctionNameFromExpr(expr ast.Expr) string {
	if e, ok := expr.(*ast.CallExpr); ok {
		log.Printf("Extracting function name from call expression at %v", e.Pos())

		// Handle simple function calls
		if fun, ok := e.Fun.(*ast.Ident); ok {
			log.Printf("Found simple function call: %s()", fun.Name)

			return fmt.Sprintf("%s()", fun.Name)
		}

		// Handle chained calls like template.New().Parse()
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			if innerCall, ok := sel.X.(*ast.CallExpr); ok {
				// For chained calls, use the last method name
				if _, ok := innerCall.Fun.(*ast.SelectorExpr); ok {
					log.Printf("Found chained method call: %s()", sel.Sel.Name)

					return fmt.Sprintf("%s()", sel.Sel.Name)
				}
			}

			// Handle method calls like obj.Method()
			if ident, ok := sel.X.(*ast.Ident); ok {
				log.Printf("Found method call: %s.%s()", ident.Name, sel.Sel.Name)

				return fmt.Sprintf("%s()", sel.Sel.Name)
			}

			log.Printf("Found selector expression call: %s", sel.Sel.Name)

			return fmt.Sprintf("%s()", sel.Sel.Name)
		}

		log.Printf("Unhandled call expression type: %T", e.Fun)
	}

	return ""
}

// findPrecedingAssignment looks for assignment statements before the if statement
func findPrecedingAssignment(file *ast.File, ifStmt *ast.IfStmt) string {
	var result string
	var closestPos token.Pos
	ifPos := ifStmt.Pos()

	ast.Inspect(file, func(node ast.Node) bool {
		assignStmt, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}

		// Only consider assignments that come before the if statement
		if assignStmt.Pos() >= ifPos {
			return true
		}

		// Check if this assignment assigns to 'err' variable
		if !assignsToErrVariable(assignStmt) {
			return true
		}

		funcName := extractFunctionNameFromAssignment(assignStmt)
		if funcName == "" {
			return true
		}

		log.Printf("Found preceding assignment to err with function: %s at %v", funcName, assignStmt.Pos())

		// Keep track of the closest assignment to the if statement
		if assignStmt.Pos() > closestPos {
			result = funcName
			closestPos = assignStmt.Pos()
		}

		return true
	})

	return result
}

// assignsToErrVariable checks if an assignment statement assigns to an 'err' variable
func assignsToErrVariable(assignStmt *ast.AssignStmt) bool {
	for _, lhs := range assignStmt.Lhs {
		if ident, ok := lhs.(*ast.Ident); ok && ident.Name == "err" {
			return true
		}
	}

	return false
}
