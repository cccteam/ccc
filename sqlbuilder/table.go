package sqlbuilder

import (
	"io"

	"github.com/go-playground/errors/v5"
)

var _ Statement = &FromStmt{}

type FromStmt struct {
	expression Statement
	alias      Statement

	error error
}

func From(expression Statement, alias ...Statement) *FromStmt {
	if expression == nil {
		return &FromStmt{error: errors.Newf("called From with a nil expression")}
	}
	if len(alias) > 1 {
		return &FromStmt{error: errors.Newf("called From with multiple aliases")}
	}

	from := &FromStmt{
		expression: expression,
	}
	if len(alias) == 1 {
		from.alias = alias[0]
	}

	exprType := expression.SqlType()
	if check(exprType).isNotAnyOf(SqlIdentifier, SqlExpression) {
		return &FromStmt{error: errors.Newf("called From with an invalid statement, expected %s, got %s", SqlExpression, exprType)}
	}

	if from.alias != nil {
		aliasType := from.alias.SqlType()
		if check(aliasType).isNotAnyOf(SqlAlias) {
			return &FromStmt{error: errors.Newf("called From with an invalid statment, expected %s, got %s", SqlAlias, aliasType)}
		}
	}

	return from
}

func (s FromStmt) SqlType() SqlType {
	return SqlColumns
}

func (s FromStmt) WriteSql(w io.StringWriter) (n int, err error) {
	return 0, nil
}

func (s FromStmt) Error() error {
	return nil
}
