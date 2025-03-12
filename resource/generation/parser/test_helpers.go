package parser

import "testing"

func NewTestType(t *testing.T, name string) Type {
	t.Helper()
	return Type{name: name}
}
