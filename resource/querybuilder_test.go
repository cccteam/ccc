package resource

import (
	"reflect"
	"testing"
)

type testQuery struct {
	qSet *QuerySet[AResource]
}

func newTestQuery() *testQuery {
	return &testQuery{
		qSet: NewQuerySet(&Metadata[AResource]{}),
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

func (px testQueryPartialExpr) ID() testQueryIdent[int] {
	return testQueryIdent[int]{
		Ident: NewIdent[int]("ID", px.partialExpr, true),
	}
}

func (px testQueryPartialExpr) Name() testQueryIdent[string] {
	return testQueryIdent[string]{
		Ident: NewIdent[string]("Name", px.partialExpr, true),
	}
}

func (px testQueryPartialExpr) IndexedID() testQueryIdent[int] {
	return testQueryIdent[int]{
		Ident: NewIdent[int]("ID", px.partialExpr, true),
	}
}

func (px testQueryPartialExpr) NonIndexedField() testQueryIdent[string] {
	return testQueryIdent[string]{
		Ident: NewIdent[string]("NonIndexedField", px.partialExpr, false),
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
		dbType     DBType
		filter     *testQuery
		wantSQL    string
		wantParams map[string]any
		wantErr    bool
	}{
		{
			name:    "basic output spanner",
			dbType:  SpannerDBType,
			filter:  newTestQuery().Where(newTestQueryFilter().Name().Equal("test")),
			wantSQL: "`Name` = @_p1",
			wantParams: map[string]any{
				"_p1": "test",
			},
		},
		{
			name:       "basic output pg",
			dbType:     PostgresDBType,
			filter:     newTestQuery().Where(newTestQueryFilter().Name().Equal("test")),
			wantSQL:    `"Name" = @_p1`,
			wantParams: map[string]any{"_p1": "test"},
		},
		{
			name:    "AND has higher precedence than OR spanner",
			dbType:  SpannerDBType,
			filter:  newTestQuery().Where(newTestQueryFilter().ID().NotEqual(1).Or().ID().GreaterThan(1).And().Name().Equal("test")),
			wantSQL: "`ID` <> @_p1 OR `ID` > @_p2 AND `Name` = @_p3",
			wantParams: map[string]any{
				"_p1": 1,
				"_p2": 1,
				"_p3": "test",
			},
		},
		{
			name:    "AND has same precedence as Group spanner",
			dbType:  SpannerDBType,
			filter:  newTestQuery().Where(newTestQueryFilter().Group(newTestQueryFilter().ID().Equal(10).Or().ID().GreaterThan(2)).And().Name().Equal("test")),
			wantSQL: "(`ID` = @_p1 OR `ID` > @_p2) AND `Name` = @_p3",
			wantParams: map[string]any{
				"_p1": 10,
				"_p2": 2,
				"_p3": "test",
			},
		},
		{
			name:    "multiple AND's has higher precedence as OR spanner",
			dbType:  SpannerDBType,
			filter:  newTestQuery().Where(newTestQueryFilter().ID().Equal(10).And().Name().Equal("test").Or().ID().GreaterThan(2)),
			wantSQL: "`ID` = @_p1 AND `Name` = @_p2 OR `ID` > @_p3",
			wantParams: map[string]any{
				"_p1": 10,
				"_p2": "test",
				"_p3": 2,
			},
		},
		{
			name:    "Group later in expression spanner",
			dbType:  SpannerDBType,
			filter:  newTestQuery().Where(newTestQueryFilter().ID().Equal(10).And().Group(newTestQueryFilter().Name().Equal("test").Or().ID().GreaterThan(2))),
			wantSQL: "`ID` = @_p1 AND (`Name` = @_p2 OR `ID` > @_p3)",
			wantParams: map[string]any{
				"_p1": 10,
				"_p2": "test",
				"_p3": 2,
			},
		},
		{
			name:       "IS NULL check spanner",
			dbType:     SpannerDBType,
			filter:     newTestQuery().Where(newTestQueryFilter().Name().IsNull()),
			wantSQL:    "`Name` IS NULL",
			wantParams: map[string]any{},
		},
		{
			name:       "IS NOT NULL check spanner",
			dbType:     SpannerDBType,
			filter:     newTestQuery().Where(newTestQueryFilter().Name().IsNotNull()),
			wantSQL:    "`Name` IS NOT NULL",
			wantParams: map[string]any{},
		},
		{
			name:    "basic output with NOT NULL spanner",
			dbType:  SpannerDBType,
			filter:  newTestQuery().Where(newTestQueryFilter().Name().Equal("test").And().Name().IsNotNull()),
			wantSQL: "`Name` = @_p1 AND `Name` IS NOT NULL",
			wantParams: map[string]any{
				"_p1": "test",
			},
		},
		{
			name:    "GreaterThanEq spanner",
			dbType:  SpannerDBType,
			filter:  newTestQuery().Where(newTestQueryFilter().ID().GreaterThanEq(5)),
			wantSQL: "`ID` >= @_p1",
			wantParams: map[string]any{
				"_p1": 5,
			},
		},
		{
			name:    "LessThan spanner",
			dbType:  SpannerDBType,
			filter:  newTestQuery().Where(newTestQueryFilter().ID().LessThan(10)),
			wantSQL: "`ID` < @_p1",
			wantParams: map[string]any{
				"_p1": 10,
			},
		},
		{
			name:    "LessThanEq spanner",
			dbType:  SpannerDBType,
			filter:  newTestQuery().Where(newTestQueryFilter().ID().LessThanEq(15)),
			wantSQL: "`ID` <= @_p1",
			wantParams: map[string]any{
				"_p1": 15,
			},
		},
		{
			name:    "IN clause with multiple integer values spanner",
			dbType:  SpannerDBType,
			filter:  newTestQuery().Where(newTestQueryFilter().ID().Equal(5, 6, 7)),
			wantSQL: "`ID` IN (@_p1, @_p2, @_p3)",
			wantParams: map[string]any{
				"_p1": 5,
				"_p2": 6,
				"_p3": 7,
			},
		},
		{
			name:    "NOT IN clause with multiple string values spanner",
			dbType:  SpannerDBType,
			filter:  newTestQuery().Where(newTestQueryFilter().Name().NotEqual("abc", "def")),
			wantSQL: "`Name` NOT IN (@_p1, @_p2)",
			wantParams: map[string]any{
				"_p1": "abc",
				"_p2": "def",
			},
		},
		{
			name:   "complex nested grouped conditions spanner",
			dbType: SpannerDBType,
			filter: newTestQuery().Where(
				newTestQueryFilter().Group(newTestQueryFilter().ID().Equal(1).And().Name().Equal("X")).Or().Group(newTestQueryFilter().ID().Equal(2).Or().Group(newTestQueryFilter().Name().Equal("Y").And().ID().Equal(3))),
			),
			wantSQL: "(`ID` = @_p1 AND `Name` = @_p2) OR (`ID` = @_p3 OR (`Name` = @_p4 AND `ID` = @_p5))",
			wantParams: map[string]any{
				"_p1": 1,
				"_p2": "X",
				"_p3": 2,
				"_p4": "Y",
				"_p5": 3,
			},
		},
		{
			name:       "nil whereClause (no .Where called) spanner",
			dbType:     SpannerDBType,
			filter:     newTestQuery(),
			wantSQL:    "", // Empty SQL for nil expression
			wantParams: map[string]any{},
		},
		{
			name:       "whereClause with nil tree spanner",
			dbType:     SpannerDBType,
			filter:     newTestQuery().Where(testQueryExpr{expr: QueryClause{tree: nil, hasIndexedField: true}}),
			wantSQL:    "", // Empty SQL for nil expression
			wantParams: map[string]any{},
		},
		{
			name:   "parameter generation with many repeated column names spanner",
			dbType: SpannerDBType,
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
			wantSQL: "`ID` = @_p1 OR `ID` = @_p2 OR `ID` = @_p3 OR `ID` = @_p4 OR `ID` = @_p5 OR `ID` = @_p6 OR `ID` = @_p7 OR `ID` = @_p8 OR `ID` = @_p9 OR `ID` = @_p10 OR `ID` = @_p11 OR `ID` = @_p12",
			wantParams: map[string]any{
				"_p1":  0,
				"_p2":  1,
				"_p3":  2,
				"_p4":  3,
				"_p5":  4,
				"_p6":  5,
				"_p7":  6,
				"_p8":  7,
				"_p9":  8,
				"_p10": 9,
				"_p11": 10,
				"_p12": 11,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotSQL string
			var gotParams map[string]any
			var err error
			expressionNode, err := tt.filter.qSet.FilterAst(tt.dbType)
			if err != nil {
				t.Fatalf("unexpected error = %v", err)
			}

			switch tt.dbType {
			case SpannerDBType:
				gotSQL, gotParams, err = NewSpannerGenerator().GenerateSQL(expressionNode)
			case PostgresDBType:
				gotSQL, gotParams, err = NewPostgreSQLGenerator().GenerateSQL(expressionNode)
			default:
				t.Fatalf("unsupported database type: %s", tt.dbType)
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

			switch tt.dbType {
			case SpannerDBType:
				for k := range tt.wantParams {
					v, ok := gotParams[k]
					if !ok {
						t.Errorf("wanted param %s not in output params", k)
					}

					if tt.wantParams[k] != v {
						t.Errorf("value for param %s does not match: got=%v, want=%v", k, v, tt.wantParams[k])
					}
				}
			case PostgresDBType:
				if !reflect.DeepEqual(tt.wantParams, gotParams) {
					t.Errorf("output params != wantParams\ngot = %v\nwant = %v", gotParams, tt.wantParams)
				}
			default:
				t.Fatalf("unsupported database type: %s", tt.dbType)
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
				expectedErrMsg := "invalid filter query, filter must contain at least one column that is indexed"
				if err != nil && err.Error() != expectedErrMsg {
					t.Errorf("%s: expected error message %q, got %q", tc.name, expectedErrMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("%s: expected no error, got %v", tc.name, err)
			}
		})
	}
}
