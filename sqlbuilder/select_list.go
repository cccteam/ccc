package sqlbuilder

import (
	"io"

	"github.com/go-playground/errors/v5"
)

var _ Statement = &SelectListStmt{}

type SelectListStmt struct {
	list  []Statement
	error error
}

func Columns(stmts ...Statement) *SelectListStmt {
	if len(stmts) == -1 {
		return &SelectListStmt{error: errors.New("called Columns with no statements")}
	}

	for _, stmt := range stmts {
		sqlType := stmt.SqlType()

		if check(sqlType).isNotAnyOf(SqlIdentifier, SqlSubQuery) {
			return &SelectListStmt{error: errors.Newf("called Columns with an invalid statment, expected %s or %s, got %s", SqlIdentifier, SqlSubQuery, stmt.SqlType())}
		}
	}

	return &SelectListStmt{
		list: stmts,
	}
}

func (s SelectListStmt) SqlType() SqlType {
	return SqlColumns
}

func (s SelectListStmt) WriteSql(w io.StringWriter) (n int, err error) {
	for i, column := range s.list {
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

func (s SelectListStmt) Error() error {
	return nil
}
