package resource

import (
	"reflect"
	"testing"
)

type testQuery struct {
	qSet *QuerySet[AResource]
}

func newTestQuery(dbType DBType) *testQuery {
	return &testQuery{
		qSet: NewQuerySet(&ResourceMetadata[AResource]{
			dbType: dbType,
		}),
	}
}

func (q *testQuery) Where(qc testQueryExpr) *testQuery {
	if err := qc.expr.Validate(); err != nil {
		panic(err)
	}
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
		Ident: NewIdent[int]("ID", o.partialExpr, true),
	}
}

func (o testQueryPartialExpr) Name() testQueryIdent[string] {
	return testQueryIdent[string]{
		Ident: NewIdent[string]("Name", o.partialExpr, true),
	}
}

func (o testQueryPartialExpr) IndexedID() testQueryIdent[int] {
	return testQueryIdent[int]{
		Ident: NewIdent[int]("ID", o.partialExpr, true),
	}
}

func (o testQueryPartialExpr) NonIndexedField() testQueryIdent[string] {
	return testQueryIdent[string]{
		Ident: NewIdent[string]("NonIndexedField", o.partialExpr, false),
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
		name         string
		filter       *testQuery
		wantSQL      string
		wantSpParams map[string]any
		wantPgParams []any
		wantErr      bool
	}{
		{
			name:    "basic output spanner",
			filter:  newTestQuery(SpannerDBType).Where(newTestQueryFilter().Name().Equal("test")),
			wantSQL: "`Name` = @p1",
			wantSpParams: map[string]any{
				"p1": "test",
			},
		},
		{
			name:         "basic output pg",
			filter:       newTestQuery(PostgresDBType).Where(newTestQueryFilter().Name().Equal("test")),
			wantSQL:      `"Name" = $1`,
			wantPgParams: []any{"test"},
		},
		{
			name:    "AND has higher precedence than OR spanner",
			filter:  newTestQuery(SpannerDBType).Where(newTestQueryFilter().ID().NotEqual(1).Or().ID().GreaterThan(1).And().Name().Equal("test")),
			wantSQL: "`ID` <> @p1 OR `ID` > @p2 AND `Name` = @p3",
			wantSpParams: map[string]any{
				"p1": 1,
				"p2": 1,
				"p3": "test",
			},
		},
		{
			name:    "AND has same precedence as Group spanner",
			filter:  newTestQuery(SpannerDBType).Where(newTestQueryFilter().Group(newTestQueryFilter().ID().Equal(10).Or().ID().GreaterThan(2)).And().Name().Equal("test")),
			wantSQL: "(`ID` = @p1 OR `ID` > @p2) AND `Name` = @p3",
			wantSpParams: map[string]any{
				"p1": 10,
				"p2": 2,
				"p3": "test",
			},
		},
		{
			name:    "multiple AND's has higher precedence as OR spanner",
			filter:  newTestQuery(SpannerDBType).Where(newTestQueryFilter().ID().Equal(10).And().Name().Equal("test").Or().ID().GreaterThan(2)),
			wantSQL: "`ID` = @p1 AND `Name` = @p2 OR `ID` > @p3",
			wantSpParams: map[string]any{
				"p1": 10,
				"p2": "test",
				"p3": 2,
			},
		},
		{
			name:    "Group later in expression spanner",
			filter:  newTestQuery(SpannerDBType).Where(newTestQueryFilter().ID().Equal(10).And().Group(newTestQueryFilter().Name().Equal("test").Or().ID().GreaterThan(2))),
			wantSQL: "`ID` = @p1 AND (`Name` = @p2 OR `ID` > @p3)",
			wantSpParams: map[string]any{
				"p1": 10,
				"p2": "test",
				"p3": 2,
			},
		},
		{
			name:         "IS NULL check spanner",
			filter:       newTestQuery(SpannerDBType).Where(newTestQueryFilter().Name().IsNull()),
			wantSQL:      "`Name` IS NULL",
			wantSpParams: map[string]any{},
		},
		{
			name:         "IS NOT NULL check spanner",
			filter:       newTestQuery(SpannerDBType).Where(newTestQueryFilter().Name().IsNotNull()),
			wantSQL:      "`Name` IS NOT NULL",
			wantSpParams: map[string]any{},
		},
		{
			name:    "basic output with NOT NULL spanner",
			filter:  newTestQuery(SpannerDBType).Where(newTestQueryFilter().Name().Equal("test").And().Name().IsNotNull()),
			wantSQL: "`Name` = @p1 AND `Name` IS NOT NULL",
			wantSpParams: map[string]any{
				"p1": "test",
			},
		},
		{
			name:    "GreaterThanEq spanner",
			filter:  newTestQuery(SpannerDBType).Where(newTestQueryFilter().ID().GreaterThanEq(5)),
			wantSQL: "`ID` >= @p1",
			wantSpParams: map[string]any{
				"p1": 5,
			},
		},
		{
			name:    "LessThan spanner",
			filter:  newTestQuery(SpannerDBType).Where(newTestQueryFilter().ID().LessThan(10)),
			wantSQL: "`ID` < @p1",
			wantSpParams: map[string]any{
				"p1": 10,
			},
		},
		{
			name:    "LessThanEq spanner",
			filter:  newTestQuery(SpannerDBType).Where(newTestQueryFilter().ID().LessThanEq(15)),
			wantSQL: "`ID` <= @p1",
			wantSpParams: map[string]any{
				"p1": 15,
			},
		},
		{
			name:    "IN clause with multiple integer values spanner",
			filter:  newTestQuery(SpannerDBType).Where(newTestQueryFilter().ID().Equal(5, 6, 7)),
			wantSQL: "`ID` IN (@p1, @p2, @p3)",
			wantSpParams: map[string]any{
				"p1": 5,
				"p2": 6,
				"p3": 7,
			},
		},
		{
			name:    "NOT IN clause with multiple string values spanner",
			filter:  newTestQuery(SpannerDBType).Where(newTestQueryFilter().Name().NotEqual("abc", "def")),
			wantSQL: "`Name` NOT IN (@p1, @p2)",
			wantSpParams: map[string]any{
				"p1": "abc",
				"p2": "def",
			},
		},
		{
			name: "complex nested grouped conditions spanner",
			filter: newTestQuery(SpannerDBType).Where(
				newTestQueryFilter().Group(newTestQueryFilter().ID().Equal(1).And().Name().Equal("X")).Or().Group(newTestQueryFilter().ID().Equal(2).Or().Group(newTestQueryFilter().Name().Equal("Y").And().ID().Equal(3))),
			),
			wantSQL: "(`ID` = @p1 AND `Name` = @p2) OR (`ID` = @p3 OR (`Name` = @p4 AND `ID` = @p5))",
			wantSpParams: map[string]any{
				"p1": 1,
				"p2": "X",
				"p3": 2,
				"p4": "Y",
				"p5": 3,
			},
		},
		{
			name:         "nil whereClause (no .Where called) spanner",
			filter:       newTestQuery(SpannerDBType),
			wantSQL:      "", // Empty SQL for nil expression
			wantSpParams: map[string]any{},
		},
		{
			name:         "whereClause with nil tree spanner",
			filter:       newTestQuery(SpannerDBType).Where(testQueryExpr{expr: QueryClause{tree: nil, hasIndexedField: true}}),
			wantSQL:      "", // Empty SQL for nil expression
			wantSpParams: map[string]any{},
		},
		{
			name: "parameter generation with many repeated column names spanner",
			filter: newTestQuery(SpannerDBType).Where(
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
			wantSQL: "`ID` = @p1 OR `ID` = @p2 OR `ID` = @p3 OR `ID` = @p4 OR `ID` = @p5 OR `ID` = @p6 OR `ID` = @p7 OR `ID` = @p8 OR `ID` = @p9 OR `ID` = @p10 OR `ID` = @p11 OR `ID` = @p12",
			wantSpParams: map[string]any{
				"p1":  0,
				"p2":  1,
				"p3":  2,
				"p4":  3,
				"p5":  4,
				"p6":  5,
				"p7":  6,
				"p8":  7,
				"p9":  8,
				"p10": 9,
				"p11": 10,
				"p12": 11,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotSQL string
			var gotSpParams map[string]any
			var gotPgParams []any
			var err error
			expressionNode := tt.filter.qSet.filterAst

			dbType := tt.filter.qSet.rMeta.dbType
			switch dbType {
			case SpannerDBType:
				gotSQL, gotSpParams, err = NewSpannerGenerator().GenerateSQL(expressionNode)
			case PostgresDBType:
				gotSQL, gotPgParams, err = NewPostgreSQLGenerator().GenerateSQL(expressionNode)
			}
			if (err != nil) != tt.wantErr {
				t.Fatalf("GenerateSQL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}

			if tt.wantSQL != gotSQL {
				t.Errorf("output SQL != wantSQL\ngot = %q\nwant = %q", gotSQL, tt.wantSQL)
			}

			switch dbType {
			case SpannerDBType:
				for k := range tt.wantSpParams {
					v, ok := gotSpParams[k]
					if !ok {
						t.Errorf("wanted param %s not in output params", k)
					}

					if tt.wantSpParams[k] != v {
						t.Errorf("value for param %s does not match: got=%v, want=%v", k, v, tt.wantSpParams[k])
					}
				}
			case PostgresDBType:
				if !reflect.DeepEqual(tt.wantPgParams, gotPgParams) {
					t.Errorf("output params != wantParams\ngot = %v\nwant = %v", gotPgParams, tt.wantPgParams)
				}
			}
		})
	}
}

func TestQueryClause_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		qc        QueryClause
		expectErr bool
	}{
		{
			name:      "Query with only indexed field",
			qc:        newTestQueryFilter().IndexedID().Equal(1).expr,
			expectErr: false,
		},
		{
			name:      "Query with only non-indexed field",
			qc:        newTestQueryFilter().NonIndexedField().Equal("test").expr,
			expectErr: true,
		},
		{
			name:      "Query with indexed AND non-indexed fields",
			qc:        newTestQueryFilter().IndexedID().Equal(1).And().NonIndexedField().Equal("test").expr,
			expectErr: false, // True because one of them is indexed
		},
		{
			name:      "Query with non-indexed AND indexed fields",
			qc:        newTestQueryFilter().NonIndexedField().Equal("test").And().IndexedID().Equal(1).expr,
			expectErr: false, // True because one of them is indexed
		},
		{
			name:      "Query with indexed OR non-indexed fields",
			qc:        newTestQueryFilter().IndexedID().Equal(1).Or().NonIndexedField().Equal("test").expr,
			expectErr: false, // True because one of them is indexed
		},
		{
			name:      "Query with non-indexed OR indexed fields",
			qc:        newTestQueryFilter().NonIndexedField().Equal("test").Or().IndexedID().Equal(1).expr,
			expectErr: false, // True because one of them is indexed
		},
		{
			name:      "Query with only non-indexed fields ANDed",
			qc:        newTestQueryFilter().NonIndexedField().Equal("test").And().NonIndexedField().Equal("another").expr,
			expectErr: true,
		},
		{
			name:      "Query with only non-indexed fields ORed",
			qc:        newTestQueryFilter().NonIndexedField().Equal("test").Or().NonIndexedField().Equal("another").expr,
			expectErr: true,
		},
		{
			name: "Grouped clause with indexed field inside",
			qc: newTestQueryFilter().Group(
				newTestQueryFilter().IndexedID().Equal(1),
			).expr,
			expectErr: false,
		},
		{
			name: "Grouped clause with non-indexed field inside",
			qc: newTestQueryFilter().Group(
				newTestQueryFilter().NonIndexedField().Equal("test"),
			).expr,
			expectErr: true,
		},
		{
			name: "Grouped clause with indexed field outside, non-indexed inside",
			qc: newTestQueryFilter().IndexedID().Equal(1).And().Group(
				newTestQueryFilter().NonIndexedField().Equal("test"),
			).expr,
			expectErr: false,
		},
		{
			name: "Grouped clause with non-indexed field outside, indexed inside",
			qc: newTestQueryFilter().NonIndexedField().Equal("test").And().Group(
				newTestQueryFilter().IndexedID().Equal(1),
			).expr,
			expectErr: false, // True because the overall expression contains an indexed field.
		},
		{
			name: "Complex query with nested groups and mixed fields - overall valid",
			qc: newTestQueryFilter().Group(
				newTestQueryFilter().NonIndexedField().Equal("A").Or().IndexedID().Equal(1),
			).And().NonIndexedField().Equal("B").expr,
			expectErr: false, // Valid because the first group has an indexed field
		},
		{
			name: "Complex query with nested groups and mixed fields - overall invalid",
			qc: newTestQueryFilter().Group(
				newTestQueryFilter().NonIndexedField().Equal("A").Or().NonIndexedField().Equal("C"),
			).And().NonIndexedField().Equal("B").expr,
			expectErr: true,
		},
		{
			name:      "Query with nil tree and hasIndexedField=true", // Should not happen with current builder but test case for Validate
			qc:        QueryClause{tree: nil, hasIndexedField: true},
			expectErr: false,
		},
		{
			name:      "Query with nil tree and hasIndexedField=false", // Should not happen with current builder but test case for Validate
			qc:        QueryClause{tree: nil, hasIndexedField: false},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		tc := tt // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.qc.Validate()
			if tc.expectErr {
				if err == nil {
					t.Errorf("%s: expected an error, but got nil", tc.name)
				}
				// Specific error message check, as per original assert.EqualError
				// The Validate() method returns a static error string.
				expectedErrMsg := "Invalid filter query. Filter must contain at least one column that is indexed"
				if err != nil && err.Error() != expectedErrMsg {
					t.Errorf("%s: expected error message %q, got %q", tc.name, expectedErrMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("%s: expected no error, got %v", tc.name, err)
				}
			}
		})
	}
}
