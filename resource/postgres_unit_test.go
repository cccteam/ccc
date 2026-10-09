package resource

import (
	"database/sql"
	"encoding/json"
	"math/big"
	"net/http"
	"reflect"
	"testing"
	"time"

	"cloud.google.com/go/civil"
	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"
)

// TestTranslatePostgresError pins the 4xx each SQLSTATE answers, with the sentences the
// Spanner translation composes for the same refusal (TestTranslateCommitError).
func TestTranslatePostgresError(t *testing.T) {
	t.Parallel()

	pgErr := func(code string) error {
		return errors.Wrap(&pgconn.PgError{Code: code, Message: "refused"}, "pgx.Tx.Exec()")
	}

	tests := []struct {
		name        string
		err         error
		patches     []PatchSetMetadata
		wantStatus  int
		wantMessage string
	}{
		{
			name:        "foreign_key_violation over a delete answers 409 with the delete sentence",
			err:         pgErr(pgForeignKeyViolation),
			patches:     []PatchSetMetadata{recordedPatch{"Hangars", DeletePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: hangarsDeleteSentence,
		},
		{
			name:        "foreign_key_violation over a create answers 409 with the write sentence",
			err:         pgErr(pgForeignKeyViolation),
			patches:     []PatchSetMetadata{recordedPatch{"Ships", CreatePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: shipsReferencedMissing,
		},
		{
			name:        "not_null_violation is a referential refusal, as Spanner's required column is",
			err:         pgErr(pgNotNullViolation),
			patches:     []PatchSetMetadata{recordedPatch{"Ships", CreatePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: shipsReferencedMissing,
		},
		{
			name:        "string_data_right_truncation is a referential refusal, as Spanner's too-long string is",
			err:         pgErr(pgStringDataRightTruncated),
			patches:     []PatchSetMetadata{recordedPatch{"Ships", UpdatePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: shipsReferencedMissing,
		},
		{
			name:        "unique_violation over a create answers 409 naming the key or a unique value",
			err:         pgErr(pgUniqueViolation),
			patches:     []PatchSetMetadata{recordedPatch{"RefitTasks", CreatePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: "RefitTasks: a record with this key or a unique value already exists.",
		},
		{
			name:        "unique_violation over updates only answers 409 naming another record's unique value",
			err:         pgErr(pgUniqueViolation),
			patches:     []PatchSetMetadata{recordedPatch{"Clients", UpdatePatchType}},
			wantStatus:  http.StatusConflict,
			wantMessage: "Clients: a unique value already exists on another record.",
		},
		{
			name:        "check_violation answers 400 naming the range",
			err:         pgErr(pgCheckViolation),
			patches:     []PatchSetMetadata{recordedPatch{"Missions", UpdatePatchType}},
			wantStatus:  http.StatusBadRequest,
			wantMessage: "Missions: a value is outside the range the record allows.",
		},
		{
			name:        "numeric_value_out_of_range answers 400 naming the range",
			err:         pgErr(pgNumericValueOutOfRange),
			patches:     []PatchSetMetadata{recordedPatch{"Missions", CreatePatchType}},
			wantStatus:  http.StatusBadRequest,
			wantMessage: "Missions: a value is outside the range the record allows.",
		},
		{
			name:        "an UPDATE that matched no row answers 404 for the record",
			err:         errors.Wrap(errPostgresRowNotFound, "UpdatePatchType Clients (c1)"),
			patches:     []PatchSetMetadata{recordedPatch{"Clients", UpdatePatchType}},
			wantStatus:  http.StatusNotFound,
			wantMessage: "Clients: this record does not exist.",
		},
		{
			name:    "any other SQLSTATE passes through",
			err:     pgErr("42P01"),
			patches: []PatchSetMetadata{recordedPatch{"Missions", CreatePatchType}},
		},
		{
			name:    "an error that is no Postgres error passes through",
			err:     errors.New("the function's own failure"),
			patches: []PatchSetMetadata{recordedPatch{"Hangars", DeletePatchType}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buffered := newBufferedPatches()
			for _, p := range tt.patches {
				buffered.record(p)
			}

			got := translatePostgresError(tt.err, buffered)
			if tt.wantStatus == 0 {
				if !errors.Is(got, tt.err) || httpio.HasClientMessage(got) {
					t.Fatalf("translatePostgresError() = %v, want the error passed through with no client message", got)
				}

				return
			}
			if gotStatus := clientStatus(got); gotStatus != tt.wantStatus {
				t.Fatalf("translatePostgresError() = %v, answers %d, want %d", got, gotStatus, tt.wantStatus)
			}
			if msg := httpio.Message(got); msg != tt.wantMessage {
				t.Errorf("httpio.Message() = %q, want %q", msg, tt.wantMessage)
			}
			if !errors.Is(got, tt.err) {
				t.Errorf("the Postgres error is not the cause of the client message: %v", got)
			}
		})
	}
}

// TestRetryablePostgresError pins which failures run the transaction again: a conflict
// with a concurrent transaction, and nothing else.
func TestRetryablePostgresError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "serialization_failure", err: &pgconn.PgError{Code: pgSerializationFailure}, want: true},
		{name: "deadlock_detected", err: &pgconn.PgError{Code: pgDeadlockDetected}, want: true},
		{name: "wrapped at any depth", err: errors.Wrap(errors.Wrap(&pgconn.PgError{Code: pgSerializationFailure}, "a"), "b"), want: true},
		{name: "a constraint refusal is final", err: &pgconn.PgError{Code: pgUniqueViolation}},
		{name: "a plain error is final", err: errors.New("failed")},
		{name: "no error is not a retry", err: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := retryablePostgresError(tt.err); got != tt.want {
				t.Errorf("retryablePostgresError() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestCommitOutcomeUnknown pins that an upload's discard reads the Postgres client's
// outcome-unknown commit as it reads the Spanner client's.
func TestCommitOutcomeUnknown(t *testing.T) {
	t.Parallel()

	if !commitOutcomeUnknown(errors.Wrap(&commitOutcomeUnknownError{err: errors.New("connection reset")}, "pgx.Tx.Commit()")) {
		t.Error("commitOutcomeUnknown() = false for the Postgres client's outcome-unknown error, want true")
	}
	if commitOutcomeUnknown(&pgconn.PgError{Code: pgUniqueViolation}) {
		t.Error("commitOutcomeUnknown() = true for a refusal the server stated, want false")
	}
}

// TestMarkUnknownOutcome pins which commit failures leave the outcome unknown: one the
// server did not state. A deferred constraint the server refused at commit is definite.
func TestMarkUnknownOutcome(t *testing.T) {
	t.Parallel()

	refused := errors.Wrap(&pgconn.PgError{Code: pgForeignKeyViolation}, "pgx.Tx.Commit()")
	if got := markUnknownOutcome(refused); !errors.Is(got, refused) || commitOutcomeUnknown(got) {
		t.Errorf("markUnknownOutcome(refusal) = %v, want it returned as it is", got)
	}
	lost := errors.New("write: connection reset by peer")
	if got := markUnknownOutcome(lost); !commitOutcomeUnknown(got) || !errors.Is(got, lost) {
		t.Errorf("markUnknownOutcome(lost connection) = %v, want an outcome-unknown error caused by it", got)
	}
}

// keyedPatch is a patch that names its key by column, as the feature flag patches do.
type keyedPatch struct {
	res accesstypes.Resource
	typ PatchType
	key KeySet
}

func (p keyedPatch) Resource() accesstypes.Resource { return p.res }
func (p keyedPatch) PatchType() PatchType           { return p.typ }
func (p keyedPatch) PrimaryKey() KeySet             { return p.key }

// TestRenderPostgresMutation pins the statement each patch type renders: quoted
// identifiers, the key in the WHERE or the conflict target, columns in name order, and
// the commit timestamp placeholder as the transaction's own timestamp.
func TestRenderPostgresMutation(t *testing.T) {
	t.Parallel()

	key := KeySet{}.Add("Id", "r1").Add("Seq", int64(2))
	tests := []struct {
		name          string
		patch         PatchSetMetadata
		values        map[string]any
		wantSQL       string
		wantParams    map[string]any
		wantMustMatch bool
		wantErr       bool
	}{
		{
			name:       "a create inserts every column, in name order",
			patch:      keyedPatch{"Ships", CreatePatchType, key},
			values:     map[string]any{"Seq": int64(2), "Id": "r1", "Name": "Argo"},
			wantSQL:    `INSERT INTO "Ships" ("Id", "Name", "Seq") VALUES (@p0, @p1, @p2)`,
			wantParams: map[string]any{"p0": "r1", "p1": "Argo", "p2": int64(2)},
		},
		{
			name:       "a commit timestamp is written as the transaction's timestamp, with no parameter",
			patch:      keyedPatch{"Ships", CreatePatchType, key},
			values:     map[string]any{"Id": "r1", "UpdatedAt": spanner.CommitTimestamp},
			wantSQL:    `INSERT INTO "Ships" ("Id", "UpdatedAt") VALUES (@p0, now())`,
			wantParams: map[string]any{"p0": "r1"},
		},
		{
			name:          "an update sets every column but the key and finds its row by the key",
			patch:         keyedPatch{"Ships", UpdatePatchType, key},
			values:        map[string]any{"Seq": int64(2), "Id": "r1", "Name": "Argo", "Note": nil},
			wantSQL:       `UPDATE "Ships" SET "Name" = @p0, "Note" = @p1 WHERE "Id" = @p2 AND "Seq" = @p3`,
			wantParams:    map[string]any{"p0": "Argo", "p1": nil, "p2": "r1", "p3": int64(2)},
			wantMustMatch: true,
		},
		{
			name:          "an update of the key alone still finds its row",
			patch:         keyedPatch{"Ships", UpdatePatchType, key},
			values:        map[string]any{"Id": "r1", "Seq": int64(2)},
			wantSQL:       `UPDATE "Ships" SET "Id" = "Id" WHERE "Id" = @p0 AND "Seq" = @p1`,
			wantParams:    map[string]any{"p0": "r1", "p1": int64(2)},
			wantMustMatch: true,
		},
		{
			name:       "a create-or-update upserts on the key, taking the proposed values",
			patch:      keyedPatch{"Ships", CreateOrUpdatePatchType, key},
			values:     map[string]any{"Id": "r1", "Seq": int64(2), "Name": "Argo"},
			wantSQL:    `INSERT INTO "Ships" ("Id", "Name", "Seq") VALUES (@p0, @p1, @p2) ON CONFLICT ("Id", "Seq") DO UPDATE SET "Name" = EXCLUDED."Name"`,
			wantParams: map[string]any{"p0": "r1", "p1": "Argo", "p2": int64(2)},
		},
		{
			name:       "a create-or-update of the key alone leaves a row that is there",
			patch:      keyedPatch{"Ships", CreateOrUpdatePatchType, key},
			values:     map[string]any{"Id": "r1", "Seq": int64(2)},
			wantSQL:    `INSERT INTO "Ships" ("Id", "Seq") VALUES (@p0, @p1) ON CONFLICT ("Id", "Seq") DO NOTHING`,
			wantParams: map[string]any{"p0": "r1", "p1": int64(2)},
		},
		{
			name:       "a delete finds its row by the key",
			patch:      keyedPatch{"Ships", DeletePatchType, key},
			wantSQL:    `DELETE FROM "Ships" WHERE "Id" = @p0 AND "Seq" = @p1`,
			wantParams: map[string]any{"p0": "r1", "p1": int64(2)},
		},
		{
			name:    "a delete with no key is refused",
			patch:   keyedPatch{"Ships", DeletePatchType, KeySet{}},
			wantErr: true,
		},
		{
			name:    "a create-or-update with no key is refused",
			patch:   keyedPatch{"Ships", CreateOrUpdatePatchType, KeySet{}},
			values:  map[string]any{"Id": "r1"},
			wantErr: true,
		},
		{
			name:    "an unknown patch type is refused",
			patch:   keyedPatch{"Ships", PatchType("unknown"), key},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := renderPostgresMutation(tt.patch, tt.values)
			if (err != nil) != tt.wantErr {
				t.Fatalf("renderPostgresMutation() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.stmt.SQL != tt.wantSQL {
				t.Errorf("SQL =\n%s\nwant\n%s", got.stmt.SQL, tt.wantSQL)
			}
			if diff := cmp.Diff(tt.wantParams, got.stmt.Params); diff != "" {
				t.Errorf("Params mismatch (-want +got):\n%s", diff)
			}
			if got.mustAffectRow != tt.wantMustMatch {
				t.Errorf("mustAffectRow = %v, want %v", got.mustAffectRow, tt.wantMustMatch)
			}
		})
	}
}

// pgJSONValue is a type stored in a JSON column the way the generator stores one.
type pgJSONValue struct{ Color string }

func (v pgJSONValue) EncodeSpanner() (any, error) {
	return spanner.NullJSON{Value: v, Valid: true}, nil
}

// TestPostgresValue pins the normalization of the values the statement builders bind and
// the patches resolve to: what pgx encodes, NULL for a typed nil, and the Encoder's
// output normalized in turn.
func TestPostgresValue(t *testing.T) {
	t.Parallel()

	id := mustCCCUUID(t, "8a6570c8-1e51-4870-9def-3f68d0447d09")
	date := civil.Date{Year: 2026, Month: time.March, Day: 4}

	tests := []struct {
		name  string
		value any
		want  any
	}{
		{name: "nil is NULL", value: nil, want: nil},
		{name: "a typed nil pointer is NULL", value: (*string)(nil), want: nil},
		{name: "a string passes through", value: "sealed", want: "sealed"},
		{name: "a pointer passes through for pgx to read", value: ptr("sealed"), want: ptr("sealed")},
		{name: "a big.Rat becomes a decimal", value: big.NewRat(241, 2), want: decimal.RequireFromString("120.5")},
		{name: "a nil big.Rat is NULL", value: (*big.Rat)(nil), want: nil},
		{name: "a valid NullNumeric becomes a decimal", value: spanner.NullNumeric{Numeric: *big.NewRat(3, 1), Valid: true}, want: decimal.NewFromInt(3)},
		{name: "an invalid NullNumeric is NULL", value: spanner.NullNumeric{}, want: nil},
		{name: "a civil.Date passes through, for its Valuer", value: date, want: date},
		{name: "a nil civil.Date pointer is NULL", value: (*civil.Date)(nil), want: nil},
		{name: "a valid NullDate becomes its date", value: spanner.NullDate{Date: date, Valid: true}, want: date},
		{name: "a valid NullString becomes its string", value: spanner.NullString{StringVal: "a", Valid: true}, want: "a"},
		{name: "an invalid NullInt64 is NULL", value: spanner.NullInt64{}, want: nil},
		{name: "a NullJSON becomes its JSON", value: spanner.NullJSON{Value: map[string]int{"a": 1}, Valid: true}, want: []byte(`{"a":1}`)},
		{name: "an invalid NullJSON is NULL", value: spanner.NullJSON{}, want: nil},
		{name: "an Encoder's output is normalized in turn", value: id, want: id.String()},
		{name: "an Encoder over JSON ends as JSON", value: pgJSONValue{Color: "red"}, want: []byte(`{"Color":"red"}`)},
		{name: "a nil Encoder pointer is NULL", value: (*ccc.UUID)(nil), want: nil},
		{name: "an invalid NullUUID is NULL", value: ccc.NullUUID{}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := postgresValue(tt.value)
			if err != nil {
				t.Fatalf("postgresValue() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got, pgComparers...); diff != "" {
				t.Errorf("postgresValue() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPostgresCast pins the type each bound value's parameter is cast to.
func TestPostgresCast(t *testing.T) {
	t.Parallel()

	type hazard int64
	type label string

	tests := []struct {
		name  string
		value any
		want  string
	}{
		{name: "nil has no type", value: nil, want: ""},
		{name: "a string", value: "a", want: "TEXT"},
		{name: "a named string", value: label("a"), want: "TEXT"},
		{name: "a string pointer", value: ptr("a"), want: "TEXT"},
		{name: "an int64", value: int64(1), want: "BIGINT"},
		{name: "a named int64", value: hazard(1), want: "BIGINT"},
		{name: "a float64", value: 1.5, want: "DOUBLE PRECISION"},
		{name: "a bool", value: true, want: "BOOLEAN"},
		{name: "a time", value: time.Time{}, want: "TIMESTAMPTZ"},
		{name: "a civil.Date", value: civil.Date{}, want: "DATE"},
		{name: "a big.Rat, which a decimal binds as", value: big.NewRat(1, 2), want: "NUMERIC"},
		{name: "a NullNumeric", value: spanner.NullNumeric{}, want: "NUMERIC"},
		{name: "a struct has none", value: struct{}{}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := postgresCast(tt.value); got != tt.want {
				t.Errorf("postgresCast() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestParamRegistry_typedPlaceholders pins how a registry renders its placeholders: bare
// for Spanner, and for Postgres a CAST to the value's type where a lowered comparison
// renders it — strings under the byte-order collation the condition language compares by.
func TestParamRegistry_typedPlaceholders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		dbType DBType
		value  any
		want   string
	}{
		{name: "Spanner leaves a string bare", dbType: SpannerDBType, value: "a", want: "@_c1"},
		{name: "Postgres casts a string and orders it by code point", dbType: PostgresDBType, value: "a", want: `(CAST(@_c1 AS TEXT) COLLATE "C")`},
		{name: "Postgres casts a decimal to NUMERIC, so an integer column compares exactly", dbType: PostgresDBType, value: decimal.RequireFromString("1.5"), want: "CAST(@_c1 AS NUMERIC)"},
		{name: "Postgres leaves a NULL bare, for the context to type", dbType: PostgresDBType, value: nil, want: "@_c1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := newParamRegistry(tt.dbType)
			if got := registry.bindTyped(tt.value); got != tt.want {
				t.Errorf("bindTyped() = %q, want %q", got, tt.want)
			}
			if got := registry.reference("_c1"); got != tt.want {
				t.Errorf("reference() of the bound name = %q, want the same placeholder %q", got, tt.want)
			}
		})
	}

	t.Run("a filler or a cursor key binds bare on Postgres, where the column types it", func(t *testing.T) {
		t.Parallel()

		if got := newParamRegistry(PostgresDBType).bind("a"); got != "@_c1" {
			t.Errorf("bind() = %q, want %q", got, "@_c1")
		}
	})
}

// TestLoweredComparison_postgresTyping pins where a lowered comparison says a parameter's
// type on Postgres: beside a column or a subquery a string is bare, since the column types
// it and a CAST to TEXT would fail against a uuid or an enumeration, while a number keeps
// its CAST so an integer column compares exactly against a decimal; a string an ordering
// operator compares to a column keeps the byte-order collation; and beside another
// parameter, where nothing types it, every parameter carries its CAST.
func TestLoweredComparison_postgresTyping(t *testing.T) {
	t.Parallel()

	owner := columnComparand("T", "Owner")
	subquery := subqueryComparand(&scalarSubqueryNode{table: "Users", alias: "u", column: "Tier", where: &truthNode{value: true}})
	tests := []struct {
		name string
		// proposed binds a proposed value first, as the lowering binds one, which the
		// case's named comparand _c1 refers to.
		proposed bool
		node     ExpressionNode
		want     string
	}{
		{
			name: "a column equals a string: bare, the column types it",
			node: &loweredComparisonNode{left: owner, op: "=", right: valueComparand("u1")},
			want: `"T"."Owner" = @_c1`,
		},
		{
			name: "a column equals a number: the CAST holds, the column compares in the number's type",
			node: &loweredComparisonNode{left: owner, op: "=", right: valueComparand(decimal.RequireFromString("1.5"))},
			want: `"T"."Owner" = CAST(@_c1 AS NUMERIC)`,
		},
		{
			name: "a column is ordered against a string: bare under the byte-order collation",
			node: &loweredComparisonNode{left: owner, op: "<", right: valueComparand("m")},
			want: `"T"."Owner" < (@_c1 COLLATE "C")`,
		},
		{
			name: "a column is ordered against a number: the CAST holds, nothing to collate",
			node: &loweredComparisonNode{left: owner, op: sqlGreaterEq, right: valueComparand(int64(3))},
			want: `"T"."Owner" >= CAST(@_c1 AS BIGINT)`,
		},
		{
			name: "a subquery equals a string: bare, the subquery types it",
			node: &loweredComparisonNode{left: subquery, op: "=", right: valueComparand("gold")},
			want: `(SELECT "u"."Tier" FROM "Users" "u" WHERE TRUE) = @_c1`,
		},
		{
			proposed: true,
			name:     "a proposed value equals a string: both parameters, so both carry their type",
			node:     &loweredComparisonNode{left: namedComparand("_c1"), op: "=", right: valueComparand("open")},
			want:     `(CAST(@_c1 AS TEXT) COLLATE "C") = (CAST(@_c2 AS TEXT) COLLATE "C")`,
		},
		{
			proposed: true,
			name:     "a proposed value against the subject: the proposed value carries its type",
			node:     &loweredComparisonNode{left: namedComparand("_c1"), op: "=", right: namedComparand(subjectParamName)},
			want:     `(CAST(@_c1 AS TEXT) COLLATE "C") = @subject`,
		},
		{
			proposed: true,
			name:     "a proposed value against a column: a bare copy of its own, the column types it",
			node:     &loweredComparisonNode{left: owner, op: "<>", right: namedComparand("_c1")},
			want:     `"T"."Owner" <> @_c2`,
		},
		{
			// pgx sends one named parameter as one positional parameter, which Postgres
			// types once: a bare use beside a uuid column and a CAST to TEXT of the same
			// parameter would be refused with inconsistent types.
			proposed: true,
			name:     "a proposed value used typed and bare: the bare use is a copy",
			node: &LogicalOpNode{
				Left:     &loweredComparisonNode{left: namedComparand("_c1"), op: "=", right: namedComparand(subjectParamName)},
				Operator: OperatorAnd,
				Right:    &loweredComparisonNode{left: subquery, op: "=", right: namedComparand("_c1")},
			},
			want: `(CAST(@_c1 AS TEXT) COLLATE "C") = @subject AND (SELECT "u"."Tier" FROM "Users" "u" WHERE TRUE) = @_c2`,
		},
		{
			name: "a column in a list: the list is bare",
			node: &loweredInNode{left: owner, values: []any{"a", "b"}},
			want: `"T"."Owner" IN (@_c1, @_c2)`,
		},
		{
			proposed: true,
			name:     "a proposed value in a list: every parameter carries its type",
			node:     &loweredInNode{left: namedComparand("_c1"), negated: true, values: []any{"a"}},
			want:     `(CAST(@_c1 AS TEXT) COLLATE "C") NOT IN ((CAST(@_c2 AS TEXT) COLLATE "C"))`,
		},
		{
			proposed: true,
			name:     "a proposed value tested for NULL carries its type",
			node:     &loweredNullTestNode{left: namedComparand("_c1")},
			want:     `(CAST(@_c1 AS TEXT) COLLATE "C") IS NULL`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := newParamRegistry(PostgresDBType)
			if tt.proposed {
				if name := registry.bindName("proposed"); name != "_c1" {
					t.Fatalf("bindName() = %q, want _c1", name)
				}
			}
			got, err := newSQLGenerator(PostgreSQL).generateLowered(tt.node, registry)
			if err != nil {
				t.Fatalf("generateLowered() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("generateLowered() =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// scanTarget is what a column scans into, for the adapters' tests.
type scanTarget struct {
	id    ccc.UUID
	maybe *ccc.NullUUID
	attrs *pgAttrs
}

// TestScanAdapters pins what the adapters do with the driver values pgx hands a
// sql.Scanner: a Decoder is fed the value as the Spanner client would feed it, NULL
// leaves a pointer field nil, and a JSON column's bytes are handed over as its text.
func TestScanAdapters(t *testing.T) {
	t.Parallel()

	var target scanTarget
	scan := func(t *testing.T, dest any, oid uint32, src any) {
		t.Helper()

		scanner, ok := postgresDestination(reflect.ValueOf(dest), oid).(sql.Scanner)
		if !ok {
			t.Fatalf("postgresDestination(%T) is no sql.Scanner", dest)
		}
		if err := scanner.Scan(src); err != nil {
			t.Fatalf("Scan(%v) error = %v", src, err)
		}
	}

	scan(t, &target.id, 2950, "8a6570c8-1e51-4870-9def-3f68d0447d09")
	if target.id.String() != "8a6570c8-1e51-4870-9def-3f68d0447d09" {
		t.Errorf("id = %v", target.id)
	}

	scan(t, &target.maybe, 2950, "8a6570c8-1e51-4870-9def-3f68d0447d09")
	if target.maybe == nil || !target.maybe.Valid {
		t.Errorf("maybe = %v, want the UUID", target.maybe)
	}
	scan(t, &target.maybe, 2950, nil)
	if target.maybe != nil {
		t.Errorf("maybe after NULL = %v, want nil", target.maybe)
	}

	scan(t, &target.attrs, 3802, []byte(`{"color":"red","size":4}`))
	if want := (pgAttrs{Color: "red", Size: 4}); target.attrs == nil || *target.attrs != want {
		t.Errorf("attrs = %v, want %v", target.attrs, want)
	}
	scan(t, &target.attrs, 3802, nil)
	if target.attrs != nil {
		t.Errorf("attrs after NULL = %v, want nil", target.attrs)
	}

	for _, dest := range []any{new(json.RawMessage), new(civil.Date), new(*civil.Date), new(decimal.Decimal), new(*time.Time)} {
		if _, ok := postgresDestination(reflect.ValueOf(dest), 0).(*nullablePointer); ok {
			t.Errorf("postgresDestination(%T) is an adapter, want pgx to read it itself", dest)
		}
	}
}
