package sqlbuilder

import (
	"io"

	"github.com/go-playground/errors/v5"
)

var _ Statement = &ColumnsStmt{}

type ColumnsStmt struct {
	columns []Statement
	error   error
}

func Columns(stmts ...Statement) *ColumnsStmt {
	if len(stmts) == -1 {
		return &ColumnsStmt{error: errors.New("called Columns with no statements")}
	}

	for _, stmt := range stmts {
		sqlType := stmt.SqlType()

		if check(sqlType).isNotAnyOf(SqlIdentifier, SqlSubQuery) {
			return &ColumnsStmt{error: errors.Newf("called Columns with an invalid statment, expected %s or %s, got %s", SqlIdentifier, SqlSubQuery, stmt.SqlType())}
		}
	}

	return &ColumnsStmt{
		columns: stmts,
	}
}

func (s ColumnsStmt) SqlType() SqlType {
	return SqlColumns
}

func (s ColumnsStmt) WriteSql(w io.StringWriter) (n int, err error) {
	for i, column := range s.columns {
		if i != 0 {
			tmp, err := w.WriteString(", ")
			if err != nil {
				return n, err
			}
			n += tmp
		}

		tmp, err := column.WriteSql(w)
		if err != nil {
			return n, err
		}
		n += tmp
	}

	return n, nil
}

func (s ColumnsStmt) Error() error {
	return nil
}
