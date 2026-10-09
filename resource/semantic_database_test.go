package resource

// The semantic differential's databases: the harness draws a world, writes it,
// reads it back through the package's statements, and compares every answer with
// the reference evaluator; what differs between Spanner and PostgreSQL — how the
// world is written and removed, and how a check statement is run — is behind
// semanticDatabase.

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"cloud.google.com/go/spanner"
	initiator "github.com/cccteam/db-initiator"
	"github.com/go-playground/errors/v5"
	"github.com/jackc/pgx/v5"
)

// semanticDatabase is the database a differential run executes against.
type semanticDatabase interface {
	// dbType is the database's type: the dialect the statements render in.
	dbType() DBType
	// client is the resource client over the database.
	client() Client
	// insertWorld writes the case's rows, and removeWorld removes them, referencing
	// tables first.
	insertWorld(ctx context.Context, w *semanticWorld) error
	removeWorld(ctx context.Context, w *semanticWorld) error
	// runCheck runs a check statement outside any request transaction and returns
	// the first row's leading boolean columns, a NULL read as false.
	runCheck(ctx context.Context, stmt *Statement, columns int) (checks []bool, found bool, err error)
}

// spannerSemanticDatabase is the differential's emulator database.
type spannerSemanticDatabase struct {
	db *initiator.SpannerDB
}

// newSpannerSemanticDatabase creates the emulator database migrated with the fixture
// world, dropped when the test ends.
func newSpannerSemanticDatabase(t *testing.T) semanticDatabase {
	t.Helper()

	container := spannerEmulator(t)
	ctx := context.Background()
	db, err := container.CreateDatabase(ctx, fmt.Sprintf("semantic-%d", testDatabases.Add(1)))
	if err != nil {
		t.Fatalf("initiator.SpannerContainer.CreateDatabase() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.DropDatabase(context.Background()); err != nil {
			t.Error(err)
		}
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.MigrateUp("file://testdata/semantic/schema"); err != nil {
		t.Fatalf("initiator.SpannerDB.MigrateUp() error = %v", err)
	}

	return &spannerSemanticDatabase{db: db}
}

func (*spannerSemanticDatabase) dbType() DBType { return SpannerDBType }

func (d *spannerSemanticDatabase) client() Client { return NewSpannerClient(d.db.Client) }

func (d *spannerSemanticDatabase) insertWorld(ctx context.Context, w *semanticWorld) error {
	if _, err := d.db.Apply(ctx, w.mutations()); err != nil {
		return errors.Wrap(err, "spanner.Client.Apply()")
	}

	return nil
}

func (d *spannerSemanticDatabase) removeWorld(ctx context.Context, w *semanticWorld) error {
	if _, err := d.db.Apply(ctx, w.deletions()); err != nil {
		return errors.Wrap(err, "spanner.Client.Apply(deletions)")
	}

	return nil
}

func (d *spannerSemanticDatabase) runCheck(ctx context.Context, stmt *Statement, columns int) (checks []bool, found bool, err error) {
	it := d.db.Single().Query(ctx, stmt.SpannerStatement())
	defer it.Stop()

	row, err := it.Next()
	if err != nil {
		return nil, false, errors.Wrap(err, "spanner.RowIterator.Next()")
	}
	checks = make([]bool, columns)
	for i := range checks {
		var passed spanner.NullBool
		if err := row.Column(i, &passed); err != nil {
			return nil, false, errors.Wrapf(err, "spanner.Row.Column(%d)", i)
		}
		checks[i] = passed.Valid && passed.Bool
	}

	return checks, true, nil
}

// postgresSemanticDatabase is the differential's Postgres database.
type postgresSemanticDatabase struct {
	db     *initiator.PostgresDatabase
	pgxCli *PostgresClient
}

// newPostgresSemanticDatabase creates the Postgres database migrated with the fixture
// world.
func newPostgresSemanticDatabase(t *testing.T) semanticDatabase {
	t.Helper()

	db, err := postgresContainer(t).CreateDatabase(t.Context(), fmt.Sprintf("semantic-differential-%d", testDatabases.Add(1)))
	if err != nil {
		t.Fatalf("initiator.PostgresContainer.CreateDatabase() error = %v", err)
	}
	t.Cleanup(db.Close)
	if err := db.MigrateUp("file://testdata/semantic/postgres"); err != nil {
		t.Fatalf("initiator.PostgresDatabase.MigrateUp() error = %v", err)
	}

	return &postgresSemanticDatabase{db: db, pgxCli: NewPostgresClient(db.Pool)}
}

func (*postgresSemanticDatabase) dbType() DBType { return PostgresDBType }

func (d *postgresSemanticDatabase) client() Client { return d.pgxCli }

// worldRows lists the world's rows by table, in the order they are inserted: the
// referenced tables first.
func (w *semanticWorld) worldRows() []worldTable {
	return []worldTable{
		{table: "Hubs", rows: asAny(maps.Values(w.hubs))},
		{table: "Carriers", rows: asAny(maps.Values(w.carriers))},
		{table: "Routes", rows: asAny(maps.Values(w.routes))},
		{table: string(semanticResource), rows: asAny(slices.Values(w.parcels))},
		{table: "Memberships", rows: asAny(slices.Values(w.memberships))},
		{table: "Profiles", rows: asAny(slices.Values(w.profiles))},
	}
}

// worldTable is one table's rows of the world.
type worldTable struct {
	table string
	rows  []any
}

// asAny collects the sequence's values as any.
func asAny[T any](values iter.Seq[T]) []any {
	var rows []any
	for v := range values {
		rows = append(rows, v)
	}

	return rows
}

func (d *postgresSemanticDatabase) insertWorld(ctx context.Context, w *semanticWorld) error {
	batch := &pgx.Batch{}
	for _, table := range w.worldRows() {
		for _, row := range table.rows {
			v := reflect.ValueOf(row).Elem()
			fields, _ := dbStructTags(v.Type(), PostgresDBType)
			columns := make([]string, 0, len(fields))
			placeholders := make([]string, 0, len(fields))
			params := make(map[string]any, len(fields))
			for _, field := range fields {
				name := "c" + field.ColumnName
				columns = append(columns, `"`+field.ColumnName+`"`)
				placeholders = append(placeholders, "@"+name)
				params[name] = v.Field(field.index).Interface()
			}
			stmt := &Statement{
				SQL:    `INSERT INTO "` + table.table + `" (` + strings.Join(columns, ", ") + `) VALUES (` + strings.Join(placeholders, ", ") + `)`,
				Params: params,
			}
			sql, args, err := stmt.PostgresStatement()
			if err != nil {
				return err
			}
			batch.Queue(sql, args)
		}
	}

	return d.sendBatch(ctx, batch)
}

func (d *postgresSemanticDatabase) removeWorld(ctx context.Context, w *semanticWorld) error {
	// The tables empty in the reverse of their insertion order; a case owns its
	// partition, but the anchors it wrote are its own too, so every row goes.
	tables := w.worldRows()
	batch := &pgx.Batch{}
	for i := len(tables) - 1; i >= 0; i-- {
		if len(tables[i].rows) == 0 {
			continue
		}
		batch.Queue(`DELETE FROM "` + tables[i].table + `"`)
	}

	return d.sendBatch(ctx, batch)
}

// sendBatch runs the batch's statements in one round trip and reports the first failure.
func (d *postgresSemanticDatabase) sendBatch(ctx context.Context, batch *pgx.Batch) error {
	results := d.db.SendBatch(ctx, batch)
	defer results.Close()
	for range batch.Len() {
		if _, err := results.Exec(); err != nil {
			return errors.Wrap(err, "pgx.BatchResults.Exec()")
		}
	}

	return nil
}

func (d *postgresSemanticDatabase) runCheck(ctx context.Context, stmt *Statement, columns int) (checks []bool, found bool, err error) {
	return queryPostgresCheckRow(ctx, d.db.Pool, stmt, columns)
}
