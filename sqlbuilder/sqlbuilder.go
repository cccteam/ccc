package sqlbuilder

import (
	"io"
	"slices"
)

type Statement interface {
	SqlType() SqlType
	WriteSql(io.StringWriter) (int, error)
	Error() error
}

type SqlType string

const (
	SqlSelect     SqlType = "SELECT"
	SqlFrom               = "FROM"
	SqlWhere              = "WHERE"
	SqlOrderBy            = "ORDER BY"
	SqlColumns            = "COLUMNS"
	SqlIdentifier         = "IDENTIFIER"
	SqlExpression         = "EXPRESSION"
	SqlSubQuery           = "SUBQUERY"
	SqlAlias              = "ALIAS"
)

type checker[C comparable] struct {
	base C
}

func check[C comparable](base C) checker[C] {
	return checker[C]{base: base}
}

func (ch checker[C]) isAnyOf(targets ...C) bool {
	return slices.Contains(targets, ch.base)
}

func (ch checker[C]) isNotAnyOf(targets ...C) bool {
	return !slices.Contains(targets, ch.base)
}
