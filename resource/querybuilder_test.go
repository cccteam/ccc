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

func (i testQueryIdent[T]) GreaterThanEq(v T) testQueryExpr {
	return testQueryExpr{expr: i.Ident.GreaterThanEq(v)}
}

func (i testQueryIdent[T]) LessThan(v T) testQueryExpr {
	return testQueryExpr{expr: i.Ident.LessThan(v)}
}

func (i testQueryIdent[T]) LessThanEq(v T) testQueryExpr {
	return testQueryExpr{expr: i.Ident.LessThanEq(v)}
}

func (i testQueryIdent[T]) IsNull() testQueryExpr {
	return testQueryExpr{expr: i.Ident.IsNull()}
}

func (i testQueryIdent[T]) IsNotNull() testQueryExpr {
	return testQueryExpr{expr: i.Ident.IsNotNull()}
}

func Test_QueryClause(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		filter     *testQuery
		wantSQL    string
		wantParams map[string]any
	}{
		{
			name:    "basic output",
			filter:  newTestQuery().Where(newTestQueryFilter().Name().Equal("test")),
			wantSQL: "Name = @Name",
			wantParams: map[string]any{
				"Name": "test",
			},
		},
		{
			name:    "AND has higher precedence than OR",
			filter:  newTestQuery().Where(newTestQueryFilter().ID().NotEqual(1).Or().ID().GreaterThan(1).And().Name().Equal("test")),
			wantSQL: "ID <> @ID OR ID > @ID1 AND Name = @Name",
			wantParams: map[string]any{
				"ID":   1,
				"ID1":  1,
				"Name": "test",
			},
		},
		{
			name:    "AND has same precedence as Group",
			filter:  newTestQuery().Where(newTestQueryFilter().Group(newTestQueryFilter().ID().Equal(10).Or().ID().GreaterThan(2)).And().Name().Equal("test")),
			wantSQL: "(ID = @ID OR ID > @ID1) AND Name = @Name",
			wantParams: map[string]any{
				"ID":   10,
				"ID1":  2,
				"Name": "test",
			},
		},
		{
			name:    "multiple AND's has higher precedence as OR",
			filter:  newTestQuery().Where(newTestQueryFilter().ID().Equal(10).And().Name().Equal("test").Or().ID().GreaterThan(2)),
			wantSQL: "ID = @ID AND Name = @Name OR ID > @ID1",
			wantParams: map[string]any{
				"ID":   10,
				"Name": "test",
				"ID1":  2,
			},
		},
		{
			name:    "Group later in expression",
			filter:  newTestQuery().Where(newTestQueryFilter().ID().Equal(10).And().Group(newTestQueryFilter().Name().Equal("test").Or().ID().GreaterThan(2))),
			wantSQL: "ID = @ID AND (Name = @Name OR ID > @ID1)",
			wantParams: map[string]any{
				"ID":   10,
				"Name": "test",
				"ID1":  2,
			},
		},
		{
			name:       "IS NULL check",
			filter:     newTestQuery().Where(newTestQueryFilter().Name().IsNull()),
			wantSQL:    "Name IS NULL",
			wantParams: map[string]any{},
		},
		{
			name:       "IS NOT NULL check",
			filter:     newTestQuery().Where(newTestQueryFilter().Name().IsNotNull()),
			wantSQL:    "Name IS NOT NULL",
			wantParams: map[string]any{},
		},
		{
			name:    "basic output with NOT NULL",
			filter:  newTestQuery().Where(newTestQueryFilter().Name().Equal("test").And().Name().IsNotNull()),
			wantSQL: "Name = @Name AND Name IS NOT NULL",
			wantParams: map[string]any{
				"Name": "test",
			},
		},
		{
			name:    "GreaterThanEq",
			filter:  newTestQuery().Where(newTestQueryFilter().ID().GreaterThanEq(5)),
			wantSQL: "ID >= @ID",
			wantParams: map[string]any{
				"ID": 5,
			},
		},
		{
			name:    "LessThan",
			filter:  newTestQuery().Where(newTestQueryFilter().ID().LessThan(10)),
			wantSQL: "ID < @ID",
			wantParams: map[string]any{
				"ID": 10,
			},
		},
		{
			name:    "LessThanEq",
			filter:  newTestQuery().Where(newTestQueryFilter().ID().LessThanEq(15)),
			wantSQL: "ID <= @ID",
			wantParams: map[string]any{
				"ID": 15,
			},
		},
		{
			name:    "IN clause with multiple integer values",
			filter:  newTestQuery().Where(newTestQueryFilter().ID().Equal(5, 6, 7)),
			wantSQL: "ID IN (@ID, @ID1, @ID2)",
			wantParams: map[string]any{
				"ID":  5,
				"ID1": 6,
				"ID2": 7,
			},
		},
		{
			name:    "NOT IN clause with multiple string values",
			filter:  newTestQuery().Where(newTestQueryFilter().Name().NotEqual("abc", "def")),
			wantSQL: "Name NOT IN (@Name, @Name1)",
			wantParams: map[string]any{
				"Name":  "abc",
				"Name1": "def",
			},
		},
		{
			name: "complex nested grouped conditions",
			filter: newTestQuery().Where(
				newTestQueryFilter().Group(newTestQueryFilter().ID().Equal(1).And().Name().Equal("X")).Or().Group(newTestQueryFilter().ID().Equal(2).Or().Group(newTestQueryFilter().Name().Equal("Y").And().ID().Equal(3))),
			),
			wantSQL: "(ID = @ID AND Name = @Name) OR (ID = @ID1 OR (Name = @Name1 AND ID = @ID2))",
			wantParams: map[string]any{
				"ID":    1,
				"Name":  "X",
				"ID1":   2,
				"Name1": "Y",
				"ID2":   3,
			},
		},
		{
			name:       "nil whereClause (no .Where called)",
			filter:     newTestQuery(),
			wantSQL:    "",
			wantParams: map[string]any{},
		},
		{
			name:       "whereClause with nil tree",
			filter:     newTestQuery().Where(testQueryExpr{expr: QueryClause{tree: nil}}),
			wantSQL:    "",
			wantParams: map[string]any{},
		},
		{
			name: "parameter generation with many repeated column names",
			filter: newTestQuery().Where(
				newTestQueryFilter().ID().Equal(0).
					Or().ID().Equal(1).
					Or().ID().Equal(2).
					Or().ID().Equal(3).
					Or().ID().Equal(4).
					Or().ID().Equal(5).
					Or().ID().Equal(6).
					Or().ID().Equal(7).
					Or().ID().Equal(8).
					Or().ID().Equal(9).
					Or().ID().Equal(10).
					Or().ID().Equal(11),
			),
			wantSQL: "ID = @ID OR ID = @ID1 OR ID = @ID2 OR ID = @ID3 OR ID = @ID4 OR ID = @ID5 OR ID = @ID6 OR ID = @ID7 OR ID = @ID8 OR ID = @ID9 OR ID = @ID10 OR ID = @ID11",
			wantParams: map[string]any{
				"ID":   0,
				"ID1":  1,
				"ID2":  2,
				"ID3":  3,
				"ID4":  4,
				"ID5":  5,
				"ID6":  6,
				"ID7":  7,
				"ID8":  8,
				"ID9":  9,
				"ID10": 10,
				"ID11": 11,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tw := newTreeWalker()

			gotSQL, gotParams := tw.Walk(tt.filter.qSet.whereClause)
			if tt.wantSQL != gotSQL {
				t.Errorf("output SQL != wantSQL\ngot = %q\nwant = %q", gotSQL, tt.wantSQL)
			}

			for k := range tt.wantParams {
				v, ok := gotParams[k]
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
