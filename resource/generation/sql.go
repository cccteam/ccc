package generation

import (
	"fmt"

	"github.com/cloudspannerecosystem/memefish"
	"github.com/cloudspannerecosystem/memefish/ast"
	"github.com/go-playground/errors/v5"
)

// Parses a SQL query string and returns the table name columnName originates from,
// or an error if the column does not exist.
func originTableName(query, columnName string) (string, error) {
	stmt, err := memefish.ParseQuery("", query)
	if err != nil {
		return "", errors.Wrap(err, "memefish.ParseQuery()")
	}

	selectStmt, ok := stmt.Query.(*ast.Select)
	if !ok {
		return "", errors.Newf("could not cast Query=%q to *ast.Select", stmt.SQL())
	}

	pathName, err := baseIdentifier(selectStmt.Results, columnName)
	if err != nil {
		return "", err
	}

	tableNames := make(map[string]string)
	if err := fromClauseIdentities(selectStmt.From.Source, tableNames); err != nil {
		return "", err
	}

	tableName, ok := tableNames[pathName]
	if !ok {
		return "", errors.Newf("columnName=%q not found", columnName)
	}

	return tableName, nil
}

// Returns the first identity of a path whose last identity matches columnName,
// or an error is the columnName doesn't match any path.
func baseIdentifier(items []ast.SelectItem, columnName string) (string, error) {
	for _, item := range items {
		switch tt := item.(type) {
		case *ast.ExprSelectItem:
			path, ok := tt.Expr.(*ast.Path)
			if !ok || len(path.Idents) < 2 {
				continue
			}

			if path.Idents[1].Name == columnName {
				return path.Idents[0].Name, nil
			}
		case *ast.Alias:
			if tt.As.Alias.Name != columnName {
				continue
			}

			path, ok := tt.Expr.(*ast.Path)
			if !ok {
				continue
			}

			return path.Idents[0].Name, nil
		default:
			continue
		}
	}

	return "", errors.Newf("columnName=%q not found", columnName)
}

// Maps all table names or aliases in a FROM clause a concrete table name
// More info: https://cloud.google.com/spanner/docs/reference/standard-sql/query-syntax#from_clause
func fromClauseIdentities(fromExpr ast.TableExpr, accumulator map[string]string) error {
	switch tt := fromExpr.(type) {
	case *ast.Unnest:
		if tt.As == nil {
			break // There is no implicit alias for UNNEST, so we ignore it if unaliased.
		}

		accumulator[tt.As.Alias.Name] = tt.As.Alias.Name

	case *ast.TableName:
		if tt.As == nil {
			accumulator[tt.Table.Name] = tt.Table.Name

			break
		}

		accumulator[tt.As.Alias.Name] = tt.Table.Name

	case *ast.PathTableExpr:
		if tt.As == nil {
			// The implicit alias is the last identity in the path
			name := tt.Path.Idents[len(tt.Path.Idents)-1].Name
			accumulator[name] = name

			break
		}

		accumulator[tt.As.Alias.Name] = tt.As.Alias.Name

	case *ast.SubQueryTableExpr:
		if tt.As == nil {
			break // There is no implicit alias for subqueries, so we ignore it if unaliased.
		}

		accumulator[tt.As.Alias.Name] = tt.As.Alias.Name

	case *ast.ParenTableExpr:
		// Parenthesized JOIN expressions are subqueries.
		// There is no implicit alias for subqueries, so we ignore it

	case *ast.Join:
		if err := fromClauseIdentities(tt.Left, accumulator); err != nil {
			return err
		}

		if err := fromClauseIdentities(tt.Right, accumulator); err != nil {
			return err
		}

	case *ast.TVFCallExpr:
		return errors.Newf("table-valued function call expressions are not supported: %q", tt.SQL())

	default:
		panic(fmt.Sprintf("unexpected type for ast.TableExpr=%q", fromExpr.SQL()))
	}

	return nil
}
