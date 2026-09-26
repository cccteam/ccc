// Package legacy is a fixture for the paging check: a hand-written list caller that
// still positions its page by offset.
package legacy

// Query stands in for a generated query builder.
type Query struct{}

// Offset is the retired positioning method.
func (q Query) Offset(uint64) Query { return q }

// SecondPage pages the old way.
func SecondPage() Query {
	return Query{}.Offset(50)
}
