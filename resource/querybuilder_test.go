package resource

import (
	"fmt"
	"reflect"
	"testing"
	// "errors" // Not explicitly used in the final code, but good for general Go files.
)

// Minimal AResource for testing
type AResource struct {
	ID   int    `db:"id,pk"`
	Name string `db:"name"`
	B_ID int    `db:"b_id"` // Foreign key to a conceptual BResource table
}

// Implement the resource.Resource interface for AResource
func (a *AResource) GetID() any { return a.ID }
func (a *AResource) SetID(id any) error {
	switch idVal := id.(type) {
	case int:
		a.ID = idVal
	case int32:
		a.ID = int(idVal)
	case int64:
		a.ID = int(idVal)
	default:
		return fmt.Errorf("invalid ID type %T for AResource, expected int or compatible", id)
	}
	return nil
}
func (a *AResource) GetType() string { return "AResource" }

// TableName returns the database table name for AResource.
// This is an example implementation. In a real scenario, this might come from rMeta or be more dynamic.
func (a *AResource) TableName() string { return "a_resources" }

// Schema returns a simplified schema for AResource.
// This is an example implementation.
func (a *AResource) Schema() map[string]any {
	return map[string]any{
		"id":   "INT",
		"name": "STRING",
		"b_id": "INT",
	}
}

type testQuery struct {
	qSet *QuerySet[AResource]
}

func newTestQuery(dbType DBType, tableName string) *testQuery {
	// Ensure rMeta has enough information if TableName() on Resource is used by generator
	// For this test setup, we explicitly pass tableName to GenerateSQL,
	// but good practice to have rMeta populated.
	return &testQuery{
		qSet: NewQuerySet(&ResourceMetadata[AResource]{
			dbType: dbType,
			Name:   tableName, // Set the table name for rMeta
			// fields, pk, etc., would be properly initialized in a real scenario
		}),
	}
}

func (q *testQuery) Where(qc testQueryExpr) *testQuery {
	if q.qSet.err != nil {
		return q
	}
	q.qSet.SetWhereClause(qc.expr)
	return q
}

func (q *testQuery) InnerJoin(targetResourceName string, on testQueryExpr) *testQuery {
	if q.qSet.err != nil {
		return q
	}
	q.qSet.InnerJoin(targetResourceName, on.expr)
	return q
}

func (q *testQuery) LeftJoin(targetResourceName string, on testQueryExpr) *testQuery {
	if q.qSet.err != nil {
		return q
	}
	q.qSet.LeftJoin(targetResourceName, on.expr)
	return q
}

type testQueryPartialExpr struct {
	partialExpr PartialQueryClause
}

func newTestQueryFilter() testQueryPartialExpr {
	return testQueryPartialExpr{
		partialExpr: NewPartialQueryClause(), // Use constructor
	}
}

func (px testQueryPartialExpr) Group(x testQueryExpr) testQueryExpr {
	return testQueryExpr{px.partialExpr.Group(x.expr)}
}

func (o testQueryPartialExpr) ID() testQueryIdent[int] {
	return testQueryIdent[int]{
		Ident: NewIdent[int]("ID", o.partialExpr), // Use constructor
	}
}

func (o testQueryPartialExpr) Name() testQueryIdent[string] {
	return testQueryIdent[string]{
		Ident: NewIdent[string]("Name", o.partialExpr), // Use constructor
	}
}

func (o testQueryPartialExpr) FullColumnName(name string) testQueryIdent[any] {
	return testQueryIdent[any]{
		Ident: NewIdent[any](name, o.partialExpr), // Use constructor
	}
}

func (o testQueryPartialExpr) B_ID() testQueryIdent[int] { // For AResource.B_ID
	return testQueryIdent[int]{
		Ident: NewIdent[int]("B_ID", o.partialExpr), // Use constructor
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
		// Existing tests updated with newTestQuery(DBType, "a_resources")
		{
			name:    "basic output spanner",
			filter:  newTestQuery(SpannerDBType, "a_resources").Where(newTestQueryFilter().Name().Equal("test")),
			wantSQL: "SELECT * FROM `a_resources` WHERE `Name` = @p1",
			wantSpParams: map[string]any{"p1": "test"},
		},
		{
			name:         "basic output pg",
			filter:       newTestQuery(PostgresDBType, "a_resources").Where(newTestQueryFilter().Name().Equal("test")),
			wantSQL:      `SELECT * FROM "a_resources" WHERE "Name" = $1`,
			wantPgParams: []any{"test"},
		},
		{
			name:    "AND has higher precedence than OR spanner",
			filter:  newTestQuery(SpannerDBType, "a_resources").Where(newTestQueryFilter().ID().NotEqual(1).Or().ID().GreaterThan(1).And().Name().Equal("test")),
			wantSQL: "SELECT * FROM `a_resources` WHERE `ID` <> @p1 OR `ID` > @p2 AND `Name` = @p3",
			wantSpParams: map[string]any{"p1": 1, "p2": 1, "p3": "test"},
		},
		{
			name:    "AND has same precedence as Group spanner",
			filter:  newTestQuery(SpannerDBType, "a_resources").Where(newTestQueryFilter().Group(newTestQueryFilter().ID().Equal(10).Or().ID().GreaterThan(2)).And().Name().Equal("test")),
			wantSQL: "SELECT * FROM `a_resources` WHERE (`ID` = @p1 OR `ID` > @p2) AND `Name` = @p3",
			wantSpParams: map[string]any{"p1": 10, "p2": 2, "p3": "test"},
		},
		{
			name:    "multiple AND's has higher precedence as OR spanner",
			filter:  newTestQuery(SpannerDBType, "a_resources").Where(newTestQueryFilter().ID().Equal(10).And().Name().Equal("test").Or().ID().GreaterThan(2)),
			wantSQL: "SELECT * FROM `a_resources` WHERE `ID` = @p1 AND `Name` = @p2 OR `ID` > @p3",
			wantSpParams: map[string]any{"p1": 10, "p2": "test", "p3": 2},
		},
		{
			name:    "Group later in expression spanner",
			filter:  newTestQuery(SpannerDBType, "a_resources").Where(newTestQueryFilter().ID().Equal(10).And().Group(newTestQueryFilter().Name().Equal("test").Or().ID().GreaterThan(2))),
			wantSQL: "SELECT * FROM `a_resources` WHERE `ID` = @p1 AND (`Name` = @p2 OR `ID` > @p3)",
			wantSpParams: map[string]any{"p1": 10, "p2": "test", "p3": 2},
		},
		{
			name:         "IS NULL check spanner",
			filter:       newTestQuery(SpannerDBType, "a_resources").Where(newTestQueryFilter().Name().IsNull()),
			wantSQL:      "SELECT * FROM `a_resources` WHERE `Name` IS NULL",
			wantSpParams: map[string]any{},
		},
		{
			name:         "IS NOT NULL check spanner",
			filter:       newTestQuery(SpannerDBType, "a_resources").Where(newTestQueryFilter().Name().IsNotNull()),
			wantSQL:      "SELECT * FROM `a_resources` WHERE `Name` IS NOT NULL",
			wantSpParams: map[string]any{},
		},
		{
			name:    "basic output with NOT NULL spanner",
			filter:  newTestQuery(SpannerDBType, "a_resources").Where(newTestQueryFilter().Name().Equal("test").And().Name().IsNotNull()),
			wantSQL: "SELECT * FROM `a_resources` WHERE `Name` = @p1 AND `Name` IS NOT NULL",
			wantSpParams: map[string]any{"p1": "test"},
		},
		{
			name:    "GreaterThanEq spanner",
			filter:  newTestQuery(SpannerDBType, "a_resources").Where(newTestQueryFilter().ID().GreaterThanEq(5)),
			wantSQL: "SELECT * FROM `a_resources` WHERE `ID` >= @p1",
			wantSpParams: map[string]any{"p1": 5},
		},
		{
			name:    "LessThan spanner",
			filter:  newTestQuery(SpannerDBType, "a_resources").Where(newTestQueryFilter().ID().LessThan(10)),
			wantSQL: "SELECT * FROM `a_resources` WHERE `ID` < @p1",
			wantSpParams: map[string]any{"p1": 10},
		},
		{
			name:    "LessThanEq spanner",
			filter:  newTestQuery(SpannerDBType, "a_resources").Where(newTestQueryFilter().ID().LessThanEq(15)),
			wantSQL: "SELECT * FROM `a_resources` WHERE `ID` <= @p1",
			wantSpParams: map[string]any{"p1": 15},
		},
		{
			name:    "IN clause with multiple integer values spanner",
			filter:  newTestQuery(SpannerDBType, "a_resources").Where(newTestQueryFilter().ID().Equal(5, 6, 7)),
			wantSQL: "SELECT * FROM `a_resources` WHERE `ID` IN (@p1, @p2, @p3)",
			wantSpParams: map[string]any{"p1": 5, "p2": 6, "p3": 7},
		},
		{
			name:    "NOT IN clause with multiple string values spanner",
			filter:  newTestQuery(SpannerDBType, "a_resources").Where(newTestQueryFilter().Name().NotEqual("abc", "def")),
			wantSQL: "SELECT * FROM `a_resources` WHERE `Name` NOT IN (@p1, @p2)",
			wantSpParams: map[string]any{"p1": "abc", "p2": "def"},
		},
		{
			name: "complex nested grouped conditions spanner",
			filter: newTestQuery(SpannerDBType, "a_resources").Where(
				newTestQueryFilter().Group(newTestQueryFilter().ID().Equal(1).And().Name().Equal("X")).Or().Group(newTestQueryFilter().ID().Equal(2).Or().Group(newTestQueryFilter().Name().Equal("Y").And().ID().Equal(3))),
			),
			wantSQL: "SELECT * FROM `a_resources` WHERE (`ID` = @p1 AND `Name` = @p2) OR (`ID` = @p3 OR (`Name` = @p4 AND `ID` = @p5))",
			wantSpParams: map[string]any{"p1": 1, "p2": "X", "p3": 2, "p4": "Y", "p5": 3},
		},
		{
			name:         "nil whereClause (no .Where called) spanner",
			filter:       newTestQuery(SpannerDBType, "a_resources"), // No Where called
			wantSQL:      "SELECT * FROM `a_resources`",             // Now generates base query
			wantSpParams: map[string]any{},
		},
		{
			name: "whereClause with nil tree spanner", // e.g. .Where(testQueryExpr{expr: QueryClause{tree: nil}})
			filter: newTestQuery(SpannerDBType, "a_resources").Where(testQueryExpr{expr: QueryClause{tree: nil}}),
			wantSQL:      "SELECT * FROM `a_resources`", // Filter is nil, so no WHERE clause
			wantSpParams: map[string]any{},
		},
		{
			name: "parameter generation with many repeated column names spanner",
			filter: newTestQuery(SpannerDBType, "a_resources").Where(
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
			wantSQL: "SELECT * FROM `a_resources` WHERE `ID` = @p1 OR `ID` = @p2 OR `ID` = @p3 OR `ID` = @p4 OR `ID` = @p5 OR `ID` = @p6 OR `ID` = @p7 OR `ID` = @p8 OR `ID` = @p9 OR `ID` = @p10 OR `ID` = @p11 OR `ID` = @p12",
			wantSpParams: map[string]any{"p1": 0, "p2": 1, "p3": 2, "p4": 3, "p5": 4, "p6": 5, "p7": 6, "p8": 7, "p9": 8, "p10": 9, "p11": 10, "p12": 11},
		},

		// New JOIN test cases
		{
			name: "inner join spanner",
			filter: newTestQuery(SpannerDBType, "a_resources").
				InnerJoin("b_resources", newTestQueryFilter().B_ID().Equal(12345)),
			wantSQL:      "SELECT * FROM `a_resources` INNER JOIN `b_resources` ON `B_ID` = @p1",
			wantSpParams: map[string]any{"p1": 12345},
		},
		{
			name: "inner join pg",
			filter: newTestQuery(PostgresDBType, "a_resources").
				InnerJoin("b_resources", newTestQueryFilter().FullColumnName("a_resources.B_ID").Equal(54321)),
			wantSQL:      `SELECT * FROM "a_resources" INNER JOIN "b_resources" ON "a_resources.B_ID" = $1`,
			wantPgParams: []any{54321},
		},
		{
			name: "left join with where spanner",
			filter: newTestQuery(SpannerDBType, "a_resources").
				LeftJoin("b_resources", newTestQueryFilter().B_ID().Equal(789)).
				Where(newTestQueryFilter().Name().Equal("test_A")),
			wantSQL:      "SELECT * FROM `a_resources` LEFT JOIN `b_resources` ON `B_ID` = @p1 WHERE `Name` = @p2",
			wantSpParams: map[string]any{"p1": 789, "p2": "test_A"},
		},
		{
			name: "left join with where on joined table column pg",
			filter: newTestQuery(PostgresDBType, "a_resources").
				LeftJoin("b_resources",
					newTestQueryFilter().B_ID().Equal(111).And().FullColumnName("b_resources.status").Equal("active"),
				).
				Where(newTestQueryFilter().Name().Equal("test_A")),
			wantSQL:      `SELECT * FROM "a_resources" LEFT JOIN "b_resources" ON ("B_ID" = $1 AND "b_resources.status" = $2) WHERE "Name" = $3`,
			wantPgParams: []any{111, "active", "test_A"},
		},
		{
			name: "multiple joins spanner",
			filter: newTestQuery(SpannerDBType, "a_resources").
				InnerJoin("b_resources", newTestQueryFilter().B_ID().Equal(101)).
				LeftJoin("c_resources", newTestQueryFilter().FullColumnName("b_resources.c_id").Equal(202)),
			wantSQL:      "SELECT * FROM `a_resources` INNER JOIN `b_resources` ON `B_ID` = @p1 LEFT JOIN `c_resources` ON `b_resources.c_id` = @p2",
			wantSpParams: map[string]any{"p1": 101, "p2": 202},
		},
		{
			name: "inner join no where pg",
			filter: newTestQuery(PostgresDBType, "a_resources").
				InnerJoin("b_resources", newTestQueryFilter().B_ID().Equal(303)),
			wantSQL:      `SELECT * FROM "a_resources" INNER JOIN "b_resources" ON "B_ID" = $1`,
			wantPgParams: []any{303},
		},
		{
			name: "join with complex on condition pg",
			filter: newTestQuery(PostgresDBType, "orders").
				InnerJoin("customers", newTestQueryFilter().FullColumnName("orders.customer_id").Equal(1).And().FullColumnName("customers.email").NotEqual("spam@example.com")).
				Where(newTestQueryFilter().FullColumnName("orders.status").Equal("completed")),
			wantSQL:      `SELECT * FROM "orders" INNER JOIN "customers" ON ("orders.customer_id" = $1 AND "customers.email" <> $2) WHERE "orders.status" = $3`,
			wantPgParams: []any{1, "spam@example.com", "completed"},
		},
		{
			name: "join with group in on condition spanner",
			filter: newTestQuery(SpannerDBType, "tableA").
				LeftJoin("tableB", newTestQueryFilter().Group(
					newTestQueryFilter().FullColumnName("tableA.key").Equal(10).Or().FullColumnName("tableB.type").Equal("primary"),
				).And().FullColumnName("tableA.val").NotEqual(0),
				),
			wantSQL:      "SELECT * FROM `tableA` LEFT JOIN `tableB` ON ((`tableA.key` = @p1 OR `tableB.type` = @p2) AND `tableA.val` <> @p3)",
			wantSpParams: map[string]any{"p1": 10, "p2": "primary", "p3": 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Do not run t.Parallel() here if any part of the QuerySet or generator is not concurrency-safe
			// or if tests modify shared state. Given paramCount is reset, it might be okay, but safer without.

			var gotSQL string
			var gotSpParams map[string]any
			var gotPgParams []any
			var err error

			// Extract components for GenerateSQL
			expressionNode := tt.filter.qSet.filterAst
			joins := tt.filter.qSet.joins
			// Ensure rMeta is not nil and Name is set
			if tt.filter.qSet.rMeta == nil {
				t.Fatalf("Test case %s: rMeta is nil in testQuery", tt.name)
			}
			baseTable := tt.filter.qSet.rMeta.Name
			if baseTable == "" && (len(joins) > 0 || expressionNode != nil) {
				// If we expect SQL output (not just SELECT * FROM ""), baseTable should be set.
				// For tests that expect empty SQL from an empty filter (like "nil whereClause"), this might be okay.
				// However, with SELECT * FROM, baseTable is always needed.
				t.Fatalf("Test case %s: baseTable is empty in rMeta", tt.name)
			}

			dbType := tt.filter.qSet.rMeta.dbType
			switch dbType {
			case SpannerDBType:
				gen := NewSpannerGenerator()
				gotSQL, gotSpParams, err = gen.GenerateSQL(baseTable, joins, expressionNode)
			case PostgresDBType:
				gen := NewPostgreSQLGenerator()
				gotSQL, gotPgParams, err = gen.GenerateSQL(baseTable, joins, expressionNode)
			default:
				t.Fatalf("Unsupported DBType: %v", dbType)
			}

			if (err != nil) != tt.wantErr {
				t.Fatalf("GenerateSQL() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return // If error was expected and occurred, or unexpected error, stop here.
			}

			if tt.wantSQL != gotSQL {
				t.Errorf("output SQL != wantSQL\ngot = %q\nwant = %q", gotSQL, tt.wantSQL)
			}

			switch dbType {
			case SpannerDBType:
				// Check length first for more precise error messages
				if len(tt.wantSpParams) != len(gotSpParams) {
					t.Errorf("Spanner params length mismatch: got %d, want %d. Got: %v, Want: %v", len(gotSpParams), len(tt.wantSpParams), gotSpParams, tt.wantSpParams)
				} else if !reflect.DeepEqual(tt.wantSpParams, gotSpParams) {
					t.Errorf("Spanner output params != wantParams\ngot = %v\nwant = %v", gotSpParams, tt.wantSpParams)
				}
			case PostgresDBType:
				if !reflect.DeepEqual(tt.wantPgParams, gotPgParams) {
					t.Errorf("Postgres output params != wantParams\ngot = %v\nwant = %v", gotPgParams, tt.wantPgParams)
				}
			}
		})
	}
}
