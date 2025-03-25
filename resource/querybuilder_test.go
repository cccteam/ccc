package resource

import (
	"fmt"
	"testing"
)

type testQuery struct {
	qSet *QuerySet[AResource]
}

func newTestQuery() *testQuery {
	return &testQuery{}
}

func (q *testQuery) Where() testQueryPartialExpr {
	return newTestQueryFilter()
}

type testQueryPartialExpr struct {
	partialExpr PartialExpr
}

type testQueryExpr struct {
	expr Expr
}

func newTestQueryFilter() testQueryPartialExpr {
	return testQueryPartialExpr{
		partialExpr: PartialExpr{
			tree: nil,
		},
	}
}

func (px testQueryPartialExpr) Group(x testQueryExpr) testQueryExpr {
	return testQueryExpr{px.partialExpr.Group(x.expr)}
}

func (o testQueryPartialExpr) ID() testQueryIdent[int] {
	return testQueryIdent[int]{
		Ident: Ident[int]{
			column:      "ID",
			partialExpr: o.partialExpr,
		},
	}
}

func (o testQueryPartialExpr) Name() testQueryIdent[string] {
	return testQueryIdent[string]{
		Ident: Ident[string]{
			column:      "Name",
			partialExpr: o.partialExpr,
		},
	}
}

func (e testQueryExpr) And() testQueryPartialExpr {
	return testQueryPartialExpr{partialExpr: e.expr.And()}
}

func (e testQueryExpr) Or() testQueryPartialExpr {
	return testQueryPartialExpr{partialExpr: e.expr.Or()}
}

type testQueryIdent[T comparable] struct {
	Ident[T]
}

func (i testQueryIdent[T]) Equal(v ...T) testQueryExpr {
	return testQueryExpr{expr: i.Ident.Equal(v...)}
}

func (i testQueryIdent[T]) NotEqual(v ...T) testQueryExpr {
	return testQueryExpr{expr: i.Ident.NotEqual(v...)}
}

func (i testQueryIdent[T]) GreaterThan(v T) testQueryExpr {
	return testQueryExpr{expr: i.Ident.GreaterThan(v)}
}

func Test_Filtering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		filter testQueryExpr
		want   string
	}{
		{
			name:   "AND has higher precedence than OR",
			filter: newTestQuery().Where().ID().Equal(1).Or().ID().GreaterThan(1).And().Name().Equal("test"),
			want:   "(ID.EQUAL[1] <-- OR --> (ID.GREATERTHAN[1] <-- AND --> Name.EQUAL[test]))",
		},
		{
			name:   "AND has higher precedence than OR (2)",
			filter: newTestQuery().Where().Group(newTestQueryFilter().ID().Equal(1).Or().ID().GreaterThan(1)).And().Name().Equal("test"),
			want:   "((ID.EQUAL[1] <-- OR --> ID.GREATERTHAN[1]) <-- AND --> Name.EQUAL[test])",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := PrintTree(t, tt.filter.expr.tree)
			if tt.want != got {
				t.Errorf("got = %s, want %s", got, tt.want)
			}
		})
	}
}

func PrintTree(t *testing.T, root Node) string {
	t.Helper()

	if root == nil {
		return ""
	}

	var s string

	if root.Left() != nil {
		s += "("
		s += PrintTree(t, root.Left())
		s += " <-- "
	}

	s += fmt.Sprintf("%s", root.Operator())

	if root.Right() != nil {
		s += " --> "
		s += PrintTree(t, root.Right())
		s += ")"
	}

	return s
}
