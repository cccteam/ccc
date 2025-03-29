package resource

import (
	"fmt"
	"testing"
)

type testQuery struct {
	qSet *QuerySet[AResource]
}

func newTestQuery() *testQuery {
	return &testQuery{
		qSet: &QuerySet[AResource]{},
	}
}

func (q *testQuery) Where(qc testQueryExpr) *testQuery {
	q.qSet.SetClause(qc.expr)
	return q
}

type testQueryPartialExpr struct {
	partialExpr PartialQueryClause
}

type testQueryExpr struct {
	expr QueryClause
}

func newTestQueryFilter() testQueryPartialExpr {
	return testQueryPartialExpr{
		partialExpr: PartialQueryClause{
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

func Test_QueryClause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filter     *testQuery
		wantString string
		wantParams map[string]any
	}{
		{
			name:       "basic output",
			filter:     newTestQuery().Where(newTestQueryFilter().Name().Equal("test")),
			wantString: "Name = @EQUAL0",
			wantParams: map[string]any{
				"@EQUAL0": "test",
			},
		},
		{
			name:       "AND has higher precedence than OR",
			filter:     newTestQuery().Where(newTestQueryFilter().ID().Equal(1).Or().ID().GreaterThan(1).And().Name().Equal("test")),
			wantString: "ID = @EQUAL0 OR ID > @GREATERTHAN0 AND Name = @EQUAL1",
			wantParams: map[string]any{
				"@EQUAL0":       1,
				"@EQUAL1":       "test",
				"@GREATERTHAN0": 1,
			},
		},
		{
			name:       "AND has same precedence as Group",
			filter:     newTestQuery().Where(newTestQueryFilter().Group(newTestQueryFilter().ID().Equal(10).Or().ID().GreaterThan(2)).And().Name().Equal("test")),
			wantString: "(ID = @EQUAL0 OR ID > @GREATERTHAN0) AND Name = @EQUAL1",
			wantParams: map[string]any{
				"@EQUAL0":       10,
				"@EQUAL1":       "test",
				"@GREATERTHAN0": 2,
			},
		},
		{
			name:       "multiple AND's has higher precedence as OR",
			filter:     newTestQuery().Where(newTestQueryFilter().ID().Equal(10).And().Name().Equal("test").Or().ID().GreaterThan(2)),
			wantString: "ID = @EQUAL0 AND Name = @EQUAL1 OR ID > @GREATERTHAN0",
			wantParams: map[string]any{
				"@EQUAL0":       10,
				"@EQUAL1":       "test",
				"@GREATERTHAN0": 2,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tw := newTreeWalker()

			got := tw.walk(tt.filter.qSet.clause)
			if tt.wantString != got {
				t.Errorf("output string != wantString\ngot = %q\nwnt = %q", got, tt.wantString)
			}

			for k := range tt.wantParams {
				v, ok := tw.params[k]
				if !ok {
					t.Errorf("wanted param %s not in output params", k)
				}

				if tt.wantParams[k] != v {
					t.Errorf("value for param %s does not match: got=%v, want=%v", k, v, tt.wantParams[k])
				}
			}
		})
	}
}

func PrintTree(t *testing.T, root clauseExprTree) string {
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
