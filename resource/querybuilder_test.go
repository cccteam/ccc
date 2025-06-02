package resource

import (
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
	q.qSet.SetWhereClause(qc.expr)

	return q
}

type testQueryPartialExpr struct {
	partialExpr PartialQueryClause
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

type testQueryExpr struct {
	expr QueryClause
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
			wantString: "Name = @Name",
			wantParams: map[string]any{
				"Name": "test",
			},
		},
		{
			name:       "AND has higher precedence than OR",
			filter:     newTestQuery().Where(newTestQueryFilter().ID().Equal(1).Or().ID().GreaterThan(1).And().Name().Equal("test")),
			wantString: "ID = @ID OR ID > @ID1 AND Name = @Name",
			wantParams: map[string]any{
				"ID":   1,
				"ID1":  1,
				"Name": "test",
			},
		},
		{
			name:       "AND has same precedence as Group",
			filter:     newTestQuery().Where(newTestQueryFilter().Group(newTestQueryFilter().ID().Equal(10).Or().ID().GreaterThan(2)).And().Name().Equal("test")),
			wantString: "(ID = @ID OR ID > @ID1) AND Name = @Name",
			wantParams: map[string]any{
				"ID":   10,
				"ID1":  2,
				"Name": "test",
			},
		},
		{
			name:       "multiple AND's has higher precedence as OR",
			filter:     newTestQuery().Where(newTestQueryFilter().ID().Equal(10).And().Name().Equal("test").Or().ID().GreaterThan(2)),
			wantString: "ID = @ID AND Name = @Name OR ID > @ID1",
			wantParams: map[string]any{
				"ID":   10,
				"Name": "test",
				"ID1":  2,
			},
		},
		{
			name:       "Group later in expression",
			filter:     newTestQuery().Where(newTestQueryFilter().ID().Equal(10).And().Group(newTestQueryFilter().Name().Equal("test").Or().ID().GreaterThan(2))),
			wantString: "ID = @ID AND (Name = @Name OR ID > @ID1)",
			wantParams: map[string]any{
				"ID":   10,
				"Name": "test",
				"ID1":  2,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tw := newTreeWalker()

			got := tw.walk(tt.filter.qSet.whereClause)
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

func Test_substituteSQLParams(t *testing.T) {
	type args struct {
		sql    string
		params map[string]any
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "basic",
			args: args{
				sql:    "ID = @ID",
				params: map[string]any{"ID": 1},
			},
			want: "ID = 1",
		},
		{
			name: "multiple params",
			args: args{
				sql:    "ID = @ID AND Name = @Name",
				params: map[string]any{"ID": 1, "Name": "test"},
			},
			want: "ID = 1 AND Name = test",
		},
		{
			name: "multiple params of same name",
			args: args{
				sql:    "ID = @ID OR ID = @ID1",
				params: map[string]any{"ID": 1, "ID1": 2},
			},
			want: "ID = 1 OR ID = 2",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := substituteSQLParams(tt.args.sql, tt.args.params)
			if got != tt.want {
				t.Errorf("substituteSQLParams() = %v, want %v", got, tt.want)
			}
		})
	}
}
