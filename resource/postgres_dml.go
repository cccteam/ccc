package resource

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"cloud.google.com/go/spanner"
	"github.com/go-playground/errors/v5"
)

// This file renders a buffered patch as the Postgres statement that applies it. Spanner
// buffers a patch as a mutation and applies the buffer at commit; Postgres has no
// mutations, so each patch is one INSERT, UPDATE or DELETE the transaction runs as the
// patch is buffered.

// postgresKeyColumner is implemented by a patch that names its primary key by Go field
// rather than by column (PatchSet): it resolves the key's columns for a database type.
// A patch that does not names its key by column already (the feature flag patches).
type postgresKeyColumner interface {
	dbKeyColumns(dbType DBType) ([]string, error)
}

// postgresKeyColumns returns the primary key's columns, in key order.
func postgresKeyColumns(patch PatchSetMetadata) ([]string, error) {
	if resolver, ok := patch.(postgresKeyColumner); ok {
		return resolver.dbKeyColumns(PostgresDBType)
	}

	parts := patch.PrimaryKey().Parts()
	columns := make([]string, 0, len(parts))
	for _, part := range parts {
		columns = append(columns, string(part.Key))
	}

	return columns, nil
}

// postgresMutation is one rendered write: the statement, and whether a write that
// changes no row means the row it addressed does not exist.
type postgresMutation struct {
	// what is the write as an error names it: the patch type and the row it addresses.
	what string
	stmt *Statement
	// mustAffectRow is set for an update: Spanner refuses an update of a row that does
	// not exist at commit, so Postgres refuses an UPDATE that matched no row.
	mustAffectRow bool
}

// describe names the write for an error.
func (m *postgresMutation) describe() string {
	return m.what
}

// isCommitTimestamp reports whether the value is the placeholder a Spanner commit
// timestamp column is written with; Postgres has no such placeholder, so the statement
// writes the transaction's own timestamp, the same instant for every row of the
// transaction as the commit timestamp is.
func isCommitTimestamp(value any) bool {
	t, ok := value.(time.Time)

	return ok && t.Equal(spanner.CommitTimestamp)
}

// commitTimestampExpression is what a commit timestamp column is written with.
const commitTimestampExpression = "now()"

// renderPostgresMutation renders the patch as its statement. The patch's column values
// are the map the buffering calls hand BufferMap (the resolved patch, the primary key's
// columns included); a delete carries none.
func renderPostgresMutation(patch PatchSetMetadata, values map[string]any) (*postgresMutation, error) {
	g := newSQLGenerator(PostgreSQL)
	table := g.quoteIdentifier(string(patch.Resource()))

	keyColumns, err := postgresKeyColumns(patch)
	if err != nil {
		return nil, err
	}
	keyValues := patch.PrimaryKey().Parts()
	if len(keyColumns) != len(keyValues) {
		return nil, errors.Newf("%s: the primary key names %d columns for %d values", patch.Resource(), len(keyColumns), len(keyValues))
	}

	what := fmt.Sprintf("%s %s (%s)", patch.PatchType(), patch.Resource(), patch.PrimaryKey().RowID())
	params := make(map[string]any, len(values)+len(keyValues))
	switch patch.PatchType() {
	case CreatePatchType:
		columns, valuesSQL := renderInsertColumns(g, values, params)

		return &postgresMutation{what: what, stmt: &Statement{
			SQL:    fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(columns, ", "), strings.Join(valuesSQL, ", ")),
			Params: params,
		}}, nil
	case CreateOrUpdatePatchType:
		if len(keyColumns) == 0 {
			return nil, errors.Newf("%s: a create-or-update needs the primary key to name the conflict", patch.Resource())
		}
		columns, valuesSQL := renderInsertColumns(g, values, params)
		conflict := fmt.Sprintf("ON CONFLICT (%s) DO NOTHING", strings.Join(quoteAll(g, keyColumns), ", "))
		if assignments := excludedAssignments(g, values, keyColumns); len(assignments) > 0 {
			conflict = fmt.Sprintf("ON CONFLICT (%s) DO UPDATE SET %s", strings.Join(quoteAll(g, keyColumns), ", "), strings.Join(assignments, ", "))
		}

		return &postgresMutation{what: what, stmt: &Statement{
			SQL:    fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s) %s", table, strings.Join(columns, ", "), strings.Join(valuesSQL, ", "), conflict),
			Params: params,
		}}, nil
	case UpdatePatchType:
		assignments := setAssignments(g, values, keyColumns, params)
		where, err := renderKeyPredicate(g, keyColumns, keyValues, params)
		if err != nil {
			return nil, err
		}
		if len(assignments) == 0 {
			// A patch of key columns alone still has to find its row.
			assignments = []string{fmt.Sprintf("%[1]s = %[1]s", g.quoteIdentifier(keyColumns[0]))}
		}

		return &postgresMutation{
			what: what,
			stmt: &Statement{
				SQL:    fmt.Sprintf("UPDATE %s SET %s WHERE %s", table, strings.Join(assignments, ", "), where),
				Params: params,
			},
			mustAffectRow: true,
		}, nil
	case DeletePatchType:
		where, err := renderKeyPredicate(g, keyColumns, keyValues, params)
		if err != nil {
			return nil, err
		}

		return &postgresMutation{what: what, stmt: &Statement{
			SQL:    fmt.Sprintf("DELETE FROM %s WHERE %s", table, where),
			Params: params,
		}}, nil
	default:
		return nil, errors.Newf("unsupported operation: %s", patch.PatchType())
	}
}

// sortedColumns returns the column names of values in a fixed order, so the same patch
// always renders the same statement.
func sortedColumns(values map[string]any) []string {
	columns := make([]string, 0, len(values))
	for column := range values {
		columns = append(columns, column)
	}
	slices.Sort(columns)

	return columns
}

// quoteAll quotes each identifier.
func quoteAll(g *sqlGenerator, identifiers []string) []string {
	quoted := make([]string, len(identifiers))
	for i, identifier := range identifiers {
		quoted[i] = g.quoteIdentifier(identifier)
	}

	return quoted
}

// valueExpression returns the SQL a column's value is written with: the transaction
// timestamp for a commit timestamp placeholder, else a bound parameter named by its
// position, since a column name need not be a legal parameter name.
func valueExpression(value any, params map[string]any) string {
	if isCommitTimestamp(value) {
		return commitTimestampExpression
	}
	name := fmt.Sprintf("p%d", len(params))
	params[name] = value

	return "@" + name
}

// renderInsertColumns renders an INSERT's column list and the matching value expressions.
func renderInsertColumns(g *sqlGenerator, values, params map[string]any) (columns, valuesSQL []string) {
	for _, column := range sortedColumns(values) {
		columns = append(columns, g.quoteIdentifier(column))
		valuesSQL = append(valuesSQL, valueExpression(values[column], params))
	}

	return columns, valuesSQL
}

// excludedAssignments renders the SET list of an upsert's DO UPDATE: every column but the
// key takes the value the INSERT proposed.
func excludedAssignments(g *sqlGenerator, values map[string]any, keyColumns []string) []string {
	var assignments []string
	for _, column := range sortedColumns(values) {
		if slices.Contains(keyColumns, column) {
			continue
		}
		quoted := g.quoteIdentifier(column)
		assignments = append(assignments, fmt.Sprintf("%s = EXCLUDED.%s", quoted, quoted))
	}

	return assignments
}

// setAssignments renders an UPDATE's SET list: every column but the key.
func setAssignments(g *sqlGenerator, values map[string]any, keyColumns []string, params map[string]any) []string {
	var assignments []string
	for _, column := range sortedColumns(values) {
		if slices.Contains(keyColumns, column) {
			continue
		}
		assignments = append(assignments, fmt.Sprintf("%s = %s", g.quoteIdentifier(column), valueExpression(values[column], params)))
	}

	return assignments
}

// renderKeyPredicate renders the WHERE locating a row by its primary key.
func renderKeyPredicate(g *sqlGenerator, keyColumns []string, keyValues []KeyPart, params map[string]any) (string, error) {
	if len(keyColumns) == 0 {
		return "", errors.New("a write that addresses a row needs the primary key")
	}
	terms := make([]string, len(keyColumns))
	for i, column := range keyColumns {
		name := fmt.Sprintf("p%d", len(params))
		params[name] = keyValues[i].Value
		terms[i] = fmt.Sprintf("%s = @%s", g.quoteIdentifier(column), name)
	}

	return strings.Join(terms, " AND "), nil
}

// structColumnValues reads the columns a struct patch writes: each field with a
// `postgres` tag, by its column name, as spanner.InsertStruct reads the `spanner` tags.
func structColumnValues(patch PatchSetMetadata) (map[string]any, error) {
	v := reflect.ValueOf(patch)
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, errors.Newf("%T: a struct patch must be a struct, found kind %s", patch, v.Kind())
	}

	fields, _ := dbStructTags(v.Type(), PostgresDBType)
	values := make(map[string]any, len(fields))
	for _, field := range fields {
		values[field.ColumnName] = v.Field(field.index).Interface()
	}

	return values, nil
}
