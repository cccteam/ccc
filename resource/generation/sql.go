package generation

import (
	"fmt"
	"strings"

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

	var selectStmt *ast.Select
	switch t := stmt.Query.(type) {
	case *ast.Select:
		selectStmt = t
	case *ast.Query:
		s, ok := t.Query.(*ast.Select)
		if !ok {
			return "", errors.Newf("could not cast %T to *ast.Select", stmt.Query)
		}

		selectStmt = s
	default:
		return "", errors.Newf("columnName=%q not found (%T)", columnName, t)
	}

	pathName, err := baseIdentifier(selectStmt.Results, columnName)
	if err != nil {
		return "", err
	}

	tableNames := make(map[string]string) // map[tableName|tableAlias]tableName
	if err := fromClauseIdentities(selectStmt.From.Source, tableNames); err != nil {
		return "", err
	}

	tableName, ok := tableNames[pathName]
	if !ok {
		return "", errors.Newf("columnName=%q not found, pathName=%q not in fromClause=%q", columnName, pathName, selectStmt.From.SQL())
	}

	return tableName, nil
}

func originColumnName(query, columnName string) (string, error) {
	stmt, err := memefish.ParseQuery("", query)
	if err != nil {
		return "", errors.Wrap(err, "memefish.ParseQuery()")
	}

	var selectStmt *ast.Select
	switch t := stmt.Query.(type) {
	case *ast.Select:
		selectStmt = t
	case *ast.Query:
		s, ok := t.Query.(*ast.Select)
		if !ok {
			return "", errors.Newf("could not cast %T to *ast.Select", stmt.Query)
		}

		selectStmt = s
	default:
		return "", errors.Newf("columnName=%q not found, unexpected *ast.QueryExpr type (%T)", columnName, t)
	}

	for _, item := range selectStmt.Results {
		switch tt := item.(type) {
		case *ast.ExprSelectItem:
			path, ok := tt.Expr.(*ast.Path)
			if !ok || len(path.Idents) < 2 {
				continue
			}

			if path.Idents[len(path.Idents)-1].Name == columnName {
				return columnName, nil
			}
		case *ast.Alias:
			if tt.As.Alias.Name != columnName {
				continue
			}

			switch aliasExprType := tt.Expr.(type) {
			case *ast.Path:
				return aliasExprType.Idents[0].Name, nil
			case *ast.ParenExpr:
				path, ok := aliasExprType.Expr.(*ast.Path)
				if !ok {
					return "", errors.Newf("columnName=%q not found, *ast.ParenExpr (%T) unsupported", columnName, aliasExprType.Expr)
				}

				return path.Idents[0].Name, nil
			default:
				return "", errors.Newf("columnName=%q not found, *ast.Alias.Expr (%T) unsupported", columnName, aliasExprType)
			}
		default:
			continue
		}
	}

	return "", errors.Newf("could not find columnName=%q in query=%q", columnName, query)
}

// Returns the first identity of a path whose last identity matches columnName,
// or an error is the columnName doesn't match any path.
// e.g. in Foo.Id the base identifier is Foo
func baseIdentifier(items []ast.SelectItem, columnName string) (string, error) {
	for _, item := range items {
		switch tt := item.(type) {
		case *ast.ExprSelectItem:
			path, ok := tt.Expr.(*ast.Path)
			if !ok || len(path.Idents) < 2 {
				continue
			}

			if path.Idents[len(path.Idents)-1].Name == columnName {
				return path.Idents[0].Name, nil
			}
		case *ast.Alias:
			if tt.As.Alias.Name != columnName {
				continue
			}

			switch aliasExprType := tt.Expr.(type) {
			case *ast.Path:
				return aliasExprType.Idents[0].Name, nil
			case *ast.ParenExpr:
				path, ok := aliasExprType.Expr.(*ast.Path)
				if !ok {
					return "", errors.Newf("columnName=%q not found, *ast.ParenExpr (%T) unsupported", columnName, aliasExprType.Expr)
				}

				return path.Idents[0].Name, nil
			default:
				return "", errors.Newf("columnName=%q not found, *ast.Alias.Expr (%T) unsupported", columnName, aliasExprType)
			}
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

// Adds nullability information to view columns in schemaMetadata if it can be determined from the view's definition.
func viewColumnNullability(schemaMetadata map[string]*tableMetadata, viewColumns []*informationSchemaResult) (map[string]*tableMetadata, error) {
	for i := range viewColumns {
		sourceTableName, err := originTableName(*viewColumns[i].ViewDefinition, viewColumns[i].ColumnName)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				continue // if the view definition is too complex, we'll just let the view column be nullable
			}

			return nil, err
		}

		sourceTable, ok := schemaMetadata[sourceTableName]
		if !ok {
			continue
		}

		sourceColumnName, err := originColumnName(*viewColumns[i].ViewDefinition, viewColumns[i].ColumnName)
		if err != nil {
			return nil, err
		}

		sourceColumn, ok := sourceTable.Columns[sourceColumnName]
		if !ok {
			continue
		}

		viewColumn := schemaMetadata[viewColumns[i].TableName].Columns[viewColumns[i].ColumnName]
		viewColumn.IsNullable = sourceColumn.IsNullable
		schemaMetadata[viewColumns[i].TableName].Columns[viewColumns[i].ColumnName] = viewColumn
	}

	return schemaMetadata, nil
}
