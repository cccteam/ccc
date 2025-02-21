package sqlbuilder

import (
	"errors"
)

var _ Statement = &IdentifierStmt{}

type IdentifierStmt struct {
	name string
}

func Identifier(name string) *IdentifierStmt {
	return &IdentifierStmt{
		name: name,
	}
}

func (s IdentifierStmt) SqlType() SqlType {
	return SqlIdentifier
}

func (s IdentifierStmt) Sql() (string, error) {
	if s.name == "" {
		return s.name, errors.New("empty identifier")
	}

	return s.name, nil
}
