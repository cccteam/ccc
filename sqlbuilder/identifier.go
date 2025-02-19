package sqlbuilder

import (
	"io"
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

func (s IdentifierStmt) WriteSql(w io.StringWriter) (int, error) {
	return w.WriteString(s.name)
}

func (s IdentifierStmt) Error() error {
	return nil
}
