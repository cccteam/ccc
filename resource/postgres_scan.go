package resource

import (
	"context"
	"iter"
	"reflect"
	"sync"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This file is the Postgres counterpart of the Spanner reader's row handling: running a
// statement through pgx, and scanning each row into the Row envelope — the resource's
// columns into its struct by the `postgres` tag, and the reserved columns a statement
// carries (masked names, the cursor's sort-key copies, the capability checks) beside it.

// queryPostgres runs the statement and returns its rows; the caller closes them.
func queryPostgres(ctx context.Context, querier PostgresQuerier, stmt *Statement) (pgx.Rows, error) {
	sql, args, err := stmt.PostgresStatement()
	if err != nil {
		return nil, err
	}
	rows, err := querier.Query(ctx, sql, args)
	if err != nil {
		return nil, errors.Wrap(err, "PostgresQuerier.Query()")
	}

	return rows, nil
}

// targetKind is what one result column is scanned into.
type targetKind int

const (
	// targetField is a resource column, scanned into the struct field at index.
	targetField targetKind = iota
	// targetMasked is the reserved masked-names column.
	targetMasked
	// targetCursor is a cursor copy, the statement's cursorColumns entry at index.
	targetCursor
	// targetChecks is the reserved capability-checks column.
	targetChecks
	// targetSkip is a column the statement selected that the envelope does not read.
	targetSkip
)

// postgresTarget is the destination of one result column.
type postgresTarget struct {
	kind  targetKind
	index int
}

// postgresColumnIndexes caches, per resource struct type, the field index of each column
// name its `postgres` tags declare.
var postgresColumnIndexes sync.Map

// postgresColumnIndex returns the field index by column name for the resource struct.
func postgresColumnIndex(t reflect.Type) map[string]int {
	if cached, ok := postgresColumnIndexes.Load(t); ok {
		return cached.(map[string]int) //nolint:forcetypeassert // only this function stores into the cache
	}

	fields, _ := dbStructTags(t, PostgresDBType)
	indexes := make(map[string]int, len(fields))
	for _, field := range fields {
		indexes[field.ColumnName] = field.index
	}
	cached, _ := postgresColumnIndexes.LoadOrStore(t, indexes)

	return cached.(map[string]int) //nolint:forcetypeassert // the cache holds only these maps
}

// planPostgresColumns maps the statement's result columns to their destinations. A
// column neither the struct nor the statement's reserved columns name is an error for a
// plain statement, as the Spanner scan is, and skipped for an envelope statement, which
// selects columns the data struct does not carry.
func planPostgresColumns[Resource Resourcer](columns []pgconn.FieldDescription, stmt *Statement) ([]postgresTarget, error) {
	indexes := postgresColumnIndex(reflect.TypeFor[Resource]())
	lenient := envelopeScan(stmt)

	targets := make([]postgresTarget, len(columns))
	for i, column := range columns {
		switch {
		case stmt.maskedNamesColumn != "" && column.Name == stmt.maskedNamesColumn:
			targets[i] = postgresTarget{kind: targetMasked}
		case stmt.capabilityPlan != nil && stmt.capabilityPlan.checksColumn != "" && column.Name == stmt.capabilityPlan.checksColumn:
			targets[i] = postgresTarget{kind: targetChecks}
		default:
			if cursor := cursorColumnIndex(stmt, column.Name); cursor >= 0 {
				targets[i] = postgresTarget{kind: targetCursor, index: cursor}

				continue
			}
			if field, ok := indexes[column.Name]; ok {
				targets[i] = postgresTarget{kind: targetField, index: field}

				continue
			}
			if !lenient {
				return nil, errors.Newf("column %q has no field with a postgres tag on %s", column.Name, reflect.TypeFor[Resource]())
			}
			targets[i] = postgresTarget{kind: targetSkip}
		}
	}

	return targets, nil
}

// cursorCopyType is the type a cursor copy is scanned into: the field's own type, or a
// pointer to it where the copy can be NULL and the type cannot hold that (a concealing
// key's CASE), so NULL scans as a nil pointer, which the cursor writes as the NULL key.
func cursorCopyType(column cursorColumn) reflect.Type {
	if column.nullable && !isNullableType(column.fieldType) {
		return reflect.PointerTo(column.fieldType)
	}

	return column.fieldType
}

// cursorColumnIndex returns the index of the statement's cursor copy selected under
// alias, -1 when none is.
func cursorColumnIndex(stmt *Statement, alias string) int {
	for i, column := range stmt.cursorColumns {
		if column.alias == alias {
			return i
		}
	}

	return -1
}

// scanPostgresRow scans the current row into a Row envelope: the resource columns into
// the data, the reserved masked-names column into the mask list, the cursor copies into
// the cursor values, and the capability checks through the plan into the capability
// answers. A NULL check boolean reads as false — a condition permits only on TRUE.
func scanPostgresRow[Resource Resourcer](rows pgx.Rows, targets []postgresTarget, stmt *Statement) (*Row[Resource], error) {
	columns := rows.FieldDescriptions()
	row := new(Row[Resource])
	data := reflect.ValueOf(&row.Data).Elem()
	cursors := make([]reflect.Value, len(stmt.cursorColumns))
	var checks []*bool

	dests := make([]any, len(targets))
	oids := make([]uint32, len(columns))
	for i, column := range columns {
		oids[i] = column.DataTypeOID
	}
	for i, target := range targets {
		switch target.kind {
		case targetField:
			dests[i] = postgresDestination(data.Field(target.index).Addr(), oids[i])
		case targetMasked:
			dests[i] = &row.masked
		case targetCursor:
			cursors[target.index] = reflect.New(cursorCopyType(stmt.cursorColumns[target.index]))
			dests[i] = postgresDestination(cursors[target.index], oids[i])
		case targetChecks:
			dests[i] = &checks
		case targetSkip:
			dests[i] = new(any)
		}
	}
	if err := rows.Scan(dests...); err != nil {
		return nil, errors.Wrap(err, "pgx.Rows.Scan()")
	}

	for i, column := range stmt.cursorColumns {
		if !cursors[i].IsValid() {
			continue
		}
		if row.cursorValues == nil {
			row.cursorValues = make(map[accesstypes.Field]reflect.Value, len(stmt.cursorColumns))
		}
		row.cursorValues[column.field] = cursors[i].Elem()
	}
	if plan := stmt.capabilityPlan; plan != nil {
		var held []bool
		if plan.checksColumn != "" {
			if len(checks) != plan.groups {
				return nil, errors.Newf("capability checks column carries %d booleans, plan expects %d", len(checks), plan.groups)
			}
			held = make([]bool, len(checks))
			for i, b := range checks {
				held[i] = b != nil && *b
			}
		}
		row.capabilities = plan.assemble(held)
	}

	return row, nil
}

// readPostgresRow reads the first row of the statement; a nil row with no error means no
// row matched.
func readPostgresRow[Resource Resourcer](ctx context.Context, querier PostgresQuerier, stmt *Statement) (*Row[Resource], error) {
	rows, err := queryPostgres(ctx, querier, stmt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, errors.Wrap(err, "pgx.Rows.Next()")
		}

		return nil, nil
	}
	targets, err := planPostgresColumns[Resource](rows.FieldDescriptions(), stmt)
	if err != nil {
		return nil, err
	}

	return scanPostgresRow[Resource](rows, targets, stmt)
}

// listPostgresRows iterates the statement's rows.
func listPostgresRows[Resource Resourcer](ctx context.Context, querier PostgresQuerier, stmt *Statement) iter.Seq2[*Row[Resource], error] {
	return func(yield func(*Row[Resource], error) bool) {
		rows, err := queryPostgres(ctx, querier, stmt)
		if err != nil {
			yield(nil, err)

			return
		}
		defer rows.Close()

		var targets []postgresTarget
		for rows.Next() {
			if targets == nil {
				if targets, err = planPostgresColumns[Resource](rows.FieldDescriptions(), stmt); err != nil {
					yield(nil, err)

					return
				}
			}
			row, err := scanPostgresRow[Resource](rows, targets, stmt)
			if err != nil {
				yield(nil, err)

				return
			}
			if !yield(row, nil) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			yield(nil, errors.Wrap(err, "pgx.Rows.Next()"))
		}
	}
}

// countPostgresRows runs a statement whose single row and column is a count.
func countPostgresRows(ctx context.Context, querier PostgresQuerier, stmt *Statement) (int64, error) {
	rows, err := queryPostgres(ctx, querier, stmt)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var total int64
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return 0, errors.Wrap(err, "pgx.Rows.Next()")
		}

		return 0, errors.New("the count statement returned no row")
	}
	if err := rows.Scan(&total); err != nil {
		return 0, errors.Wrap(err, "pgx.Rows.Scan()")
	}

	return total, nil
}

// queryPostgresCheckRow reads a check-SELECT's first row: its leading boolean columns, a
// NULL read as false.
func queryPostgresCheckRow(ctx context.Context, querier PostgresQuerier, stmt *Statement, columns int) (checks []bool, found bool, err error) {
	rows, err := queryPostgres(ctx, querier, stmt)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, false, errors.Wrap(err, "pgx.Rows.Next()")
		}

		return nil, false, nil
	}

	scanned := make([]*bool, columns)
	dests := make([]any, columns)
	for i := range scanned {
		dests[i] = &scanned[i]
	}
	if err := rows.Scan(dests...); err != nil {
		return nil, false, errors.Wrap(err, "pgx.Rows.Scan()")
	}

	checks = make([]bool, columns)
	for i, check := range scanned {
		checks[i] = check != nil && *check
	}

	return checks, true, nil
}

// selectPostgresStrings runs a statement that selects one text column and returns the
// column of every row.
func selectPostgresStrings(ctx context.Context, querier PostgresQuerier, stmt *Statement) ([]string, error) {
	rows, err := queryPostgres(ctx, querier, stmt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, errors.Wrap(err, "pgx.Rows.Scan()")
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "pgx.Rows.Next()")
	}

	return values, nil
}
