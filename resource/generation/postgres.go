package generation

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/cccteam/ccc/resource"
	initiator "github.com/cccteam/db-initiator"
	"github.com/go-playground/errors/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This file reads a PostgreSQL schema into the same table map the Spanner schema read
// fills (spanner.go): the migrations run in a PostgreSQL container, and the system
// catalogs are read into the facts the generator consumes — each column's nullability,
// key and foreign-key membership, default, and type, and each table's indexes. The rest
// of the generator never learns which database the schema came from.
//
// A column's type is stated in the vocabulary the generator already parses, the way
// Spanner's INFORMATION_SCHEMA spells it (postgresColumnType): a PostgreSQL varchar(64)
// is STRING(64), text is STRING(MAX), bigint is INT64, jsonb is JSON, and an array is
// ARRAY<…>, so the value limits the decoder enforces and the JSON storage methods the
// generator writes read one vocabulary.

// defaultPostgresVersion is the PostgreSQL image the generator migrates in when the run
// does not name one.
const defaultPostgresVersion = "17"

// createPostgresDB starts a PostgreSQL container and migrates the generator's database
// in it. The container is the caller's to release through releasePostgresContainer once
// the database is read, for the reason createSpannerDB's is.
func createPostgresDB(ctx context.Context, version string, migrationSourceURLs []string) (*initiator.PostgresDatabase, *initiator.PostgresContainer, error) {
	log.Println("Starting PostgreSQL Container...")
	container, err := initiator.NewPostgresContainer(ctx, version)
	if err != nil {
		return nil, nil, errors.Wrap(err, "initiator.NewPostgresContainer()")
	}

	db, err := container.CreateDatabase(ctx, "resourcegeneration")
	if err != nil {
		releasePostgresContainer(ctx, container)

		return nil, nil, errors.Wrap(err, "initiator.PostgresContainer.CreateDatabase()")
	}

	log.Println("Starting PostgreSQL Migration...")
	for _, migrationSource := range migrationSourceURLs {
		if err := db.MigrateUp(migrationSource); err != nil {
			db.Close()
			releasePostgresContainer(ctx, container)

			return nil, nil, errors.Wrap(err, "initiator.PostgresDatabase.MigrateUp()")
		}
	}

	return db, container, nil
}

// releasePostgresContainer closes the container's connections and terminates the
// container itself; a failure is logged, since the generator's output does not depend
// on it.
func releasePostgresContainer(ctx context.Context, container *initiator.PostgresContainer) {
	container.Close()
	if err := container.Terminate(ctx); err != nil {
		log.Print(errors.Wrap(err, "testcontainers.Container.Terminate()"))
	}
}

func (c *client) runPostgres(ctx context.Context, version string, migrationSourceURL []string) error {
	db, container, err := createPostgresDB(ctx, version, migrationSourceURL)
	if err != nil {
		return err
	}
	defer releasePostgresContainer(ctx, container)
	defer db.Close()

	tableMap, err := createPostgresTableMap(ctx, db.Pool)
	if err != nil {
		return errors.Wrap(err, "createPostgresTableMap()")
	}

	enumValues, err := fetchPostgresEnumValues(ctx, db.Pool)
	if err != nil {
		return errors.Wrap(err, "fetchPostgresEnumValues()")
	}

	c.tableMap = tableMap
	c.enumValues = enumValues

	return nil
}

// postgresColumnsQuery reads one row per column of every base table in the current
// schema (a view, a foreign table and an index are not resources): the column's
// nullability, its type with the length or precision it declares (a domain read as its
// base type, an enumeration as text), whether it has a default or is generated, and
// the generation expression.
const postgresColumnsQuery = `SELECT
		c.relname AS table_name,
		a.attname AS column_name,
		NOT a.attnotnull AS is_nullable,
		CASE
			WHEN t.typtype = 'd' THEN format_type(t.typbasetype, t.typtypmod)
			WHEN t.typtype = 'e' THEN 'text'
			WHEN et.typtype = 'e' THEN 'text[]'
			ELSE format_type(a.atttypid, a.atttypmod)
		END AS column_type,
		a.attnum::bigint AS ordinal_position,
		((a.atthasdef AND a.attgenerated = '') OR a.attidentity <> '') AS has_default,
		CASE WHEN a.attgenerated <> '' THEN pg_get_expr(ad.adbin, ad.adrelid) END AS generation_expression
	FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_catalog.pg_type t ON t.oid = a.atttypid
		LEFT JOIN pg_catalog.pg_type et ON et.oid = t.typelem AND t.typcategory = 'A'
		LEFT JOIN pg_catalog.pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
	WHERE n.nspname = current_schema()
		AND c.relkind IN ('r', 'p')
		AND a.attnum > 0
		AND NOT a.attisdropped
	ORDER BY c.relname, a.attnum`

// postgresKeysQuery reads one row per column of every primary key and foreign key in
// the current schema: the constraint's kind, the column's position in it, and for a
// foreign key the column it references and the delete rule.
const postgresKeysQuery = `SELECT
		c.relname AS table_name,
		a.attname AS column_name,
		con.contype::text AS constraint_type,
		k.ord::bigint AS position,
		rc.relname AS referenced_table,
		ra.attname AS referenced_column,
		(con.confdeltype = 'c') AS delete_cascades
	FROM pg_catalog.pg_constraint con
		JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		CROSS JOIN LATERAL unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord)
		JOIN pg_catalog.pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
		LEFT JOIN LATERAL unnest(con.confkey) WITH ORDINALITY AS rk(attnum, ord) ON con.contype = 'f' AND rk.ord = k.ord
		LEFT JOIN pg_catalog.pg_class rc ON rc.oid = con.confrelid
		LEFT JOIN pg_catalog.pg_attribute ra ON ra.attrelid = con.confrelid AND ra.attnum = rk.attnum
	WHERE n.nspname = current_schema()
		AND con.contype IN ('p', 'f')
	ORDER BY c.relname, con.conname, k.ord`

// postgresIndexesQuery reads one row per column of every B-tree index of a table in the
// current schema, the primary key included: the key columns in order with their
// direction, then the columns the index includes. An index with an expression in its
// key, one that is not valid, and one of another access method serve no ordering or
// seek over a column, so they are not read. A partial index skips rows, as a
// null-filtered Spanner index skips the rows with a NULL key.
const postgresIndexesQuery = `SELECT
		c.relname AS table_name,
		ic.relname AS index_name,
		ix.indisprimary AS is_primary,
		ix.indisunique AS is_unique,
		(ix.indpred IS NOT NULL) AS is_partial,
		a.attname AS column_name,
		k.ord::bigint AS position,
		(k.ord <= ix.indnkeyatts) AS is_key,
		(k.ord <= ix.indnkeyatts AND (ix.indoption[k.ord - 1] & 1) = 1) AS is_descending
	FROM pg_catalog.pg_index ix
		JOIN pg_catalog.pg_class c ON c.oid = ix.indrelid
		JOIN pg_catalog.pg_class ic ON ic.oid = ix.indexrelid
		JOIN pg_catalog.pg_am am ON am.oid = ic.relam
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		CROSS JOIN LATERAL unnest(ix.indkey::int2[]) WITH ORDINALITY AS k(attnum, ord)
		JOIN pg_catalog.pg_attribute a ON a.attrelid = ix.indrelid AND a.attnum = k.attnum
	WHERE n.nspname = current_schema()
		AND c.relkind IN ('r', 'p')
		AND am.amname = 'btree'
		AND ix.indisvalid
		AND NOT EXISTS (SELECT 1 FROM unnest(ix.indkey::int2[]) AS e(attnum) WHERE e.attnum = 0)
	ORDER BY c.relname, ic.relname, k.ord`

// postgresKeyRow is one column of a primary key or foreign key.
type postgresKeyRow struct {
	table, column, kind string
	position            int64
	referencedTable     *string
	referencedColumn    *string
	deleteCascades      bool
}

func createPostgresTableMap(ctx context.Context, pool *pgxpool.Pool) (map[string]*tableMetadata, error) {
	log.Println("Creating postgres table lookup...")

	columns, err := queryPostgresColumns(ctx, pool)
	if err != nil {
		return nil, err
	}
	keys, err := queryPostgresKeys(ctx, pool)
	if err != nil {
		return nil, err
	}

	results, err := postgresSchemaResults(columns, keys)
	if err != nil {
		return nil, err
	}

	schemaMetadata := make(map[string]*tableMetadata)
	for i := range results {
		table, ok := schemaMetadata[results[i].TableName]
		if !ok {
			table = &tableMetadata{Columns: make(map[string]columnMeta)}
		}

		table.addSchemaResult(&results[i])
		schemaMetadata[results[i].TableName] = table
	}

	indexResults, err := queryPostgresIndexes(ctx, pool)
	if err != nil {
		return nil, err
	}
	for i := range indexResults {
		table, ok := schemaMetadata[indexResults[i].TableName]
		if !ok {
			continue
		}
		table.addIndexResult(&indexResults[i])
	}

	for _, table := range schemaMetadata {
		table.deriveIndexFlags()
	}

	return schemaMetadata, nil
}

// postgresColumnRow is one column of the columns query.
type postgresColumnRow struct {
	table, column, columnType string
	nullable, hasDefault      bool
	ordinal                   int64
	generationExpression      *string
}

func queryPostgresColumns(ctx context.Context, pool *pgxpool.Pool) ([]postgresColumnRow, error) {
	rows, err := pool.Query(ctx, postgresColumnsQuery)
	if err != nil {
		return nil, errors.Wrap(err, "pgxpool.Pool.Query()")
	}
	defer rows.Close()

	var columns []postgresColumnRow
	for rows.Next() {
		var r postgresColumnRow
		if err := rows.Scan(&r.table, &r.column, &r.nullable, &r.columnType, &r.ordinal, &r.hasDefault, &r.generationExpression); err != nil {
			return nil, errors.Wrap(err, "pgx.Rows.Scan()")
		}
		columns = append(columns, r)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "pgx.Rows.Err()")
	}

	return columns, nil
}

func queryPostgresKeys(ctx context.Context, pool *pgxpool.Pool) ([]postgresKeyRow, error) {
	rows, err := pool.Query(ctx, postgresKeysQuery)
	if err != nil {
		return nil, errors.Wrap(err, "pgxpool.Pool.Query()")
	}
	defer rows.Close()

	var keys []postgresKeyRow
	for rows.Next() {
		var r postgresKeyRow
		if err := rows.Scan(&r.table, &r.column, &r.kind, &r.position, &r.referencedTable, &r.referencedColumn, &r.deleteCascades); err != nil {
			return nil, errors.Wrap(err, "pgx.Rows.Scan()")
		}
		keys = append(keys, r)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "pgx.Rows.Err()")
	}

	return keys, nil
}

func queryPostgresIndexes(ctx context.Context, pool *pgxpool.Pool) ([]indexSchemaResult, error) {
	rows, err := pool.Query(ctx, postgresIndexesQuery)
	if err != nil {
		return nil, errors.Wrap(err, "pgxpool.Pool.Query()")
	}
	defer rows.Close()

	var results []indexSchemaResult
	for rows.Next() {
		var (
			table, index, column               string
			primary, unique, partial, key, des bool
			position                           int64
		)
		if err := rows.Scan(&table, &index, &primary, &unique, &partial, &column, &position, &key, &des); err != nil {
			return nil, errors.Wrap(err, "pgx.Rows.Scan()")
		}

		result := indexSchemaResult{
			TableName:      table,
			IndexName:      index,
			IndexType:      "INDEX",
			IsUnique:       unique,
			IsNullFiltered: partial,
			ColumnName:     column,
		}
		if primary {
			// Spanner names every table's key index PRIMARY_KEY; the generator reads
			// the type, and names the index in its warnings by this.
			result.IndexName = "PRIMARY_KEY"
			result.IndexType = primaryKeyIndexType
		}
		if key {
			result.OrdinalPosition = &position
			if des {
				ordering := descendingOrdering
				result.ColumnOrdering = &ordering
			}
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, errors.Wrap(err, "pgx.Rows.Err()")
	}

	return results, nil
}

// postgresSchemaResults folds the columns and the keys into the rows the Spanner query
// yields, so addSchemaResult reads both alike: a column's primary-key and foreign-key
// membership, its position in the key, and the table and column its foreign key
// references. The referenced column is followed one more jump when it is itself a
// foreign key, as the Spanner query does, so a column that refers to a table through an
// intermediate one names the table the value finally belongs to.
func postgresSchemaResults(columns []postgresColumnRow, keys []postgresKeyRow) ([]informationSchemaResult, error) {
	type columnKey struct{ table, column string }

	primary := make(map[columnKey]int64)
	foreign := make(map[columnKey]postgresKeyRow)
	for _, key := range keys {
		id := columnKey{key.table, key.column}
		switch key.kind {
		case "p":
			primary[id] = key.position
		case "f":
			// A column in two foreign keys reads CASCADE when either cascades.
			if existing, ok := foreign[id]; ok && existing.deleteCascades && !key.deleteCascades {
				continue
			}
			foreign[id] = key
		default:
		}
	}

	results := make([]informationSchemaResult, 0, len(columns))
	for _, column := range columns {
		id := columnKey{column.table, column.column}
		columnType, err := postgresColumnType(column.columnType)
		if err != nil {
			return nil, errors.Wrapf(err, "%s.%s", column.table, column.column)
		}

		result := informationSchemaResult{
			TableName:            column.table,
			ColumnName:           column.column,
			SpannerType:          columnType,
			IsNullable:           column.nullable,
			GenerationExpression: column.generationExpression,
			OrdinalPosition:      column.ordinal,
			KeyOrdinalPosition:   1,
			HasDefault:           column.hasDefault,
		}
		if position, ok := primary[id]; ok {
			result.IsPrimaryKey = true
			result.KeyOrdinalPosition = position
		}
		if fk, ok := foreign[id]; ok && fk.referencedTable != nil && fk.referencedColumn != nil {
			result.IsForeignKey = true
			table, col := *fk.referencedTable, *fk.referencedColumn
			if next, jumps := foreign[columnKey{table, col}]; jumps && next.referencedTable != nil && next.referencedColumn != nil {
				table, col = *next.referencedTable, *next.referencedColumn
			}
			result.ReferencedTable, result.ReferencedColumn = &table, &col
			rule := "NO ACTION"
			if fk.deleteCascades {
				rule = cascadeDeleteAction
			}
			result.DeleteRule = &rule
		}
		results = append(results, result)
	}

	return results, nil
}

// postgresTypeLength matches the length or precision a type declares: varchar(64),
// numeric(12,2).
var postgresTypeLength = regexp.MustCompile(`^([a-z ]+?)(?:\((\d+)(?:,\s*\d+)?\))?(\[\])?$`)

// postgresColumnType states a PostgreSQL column's type as format_type spells it in the
// vocabulary the generator parses (see the file's comment). A type with no counterpart
// there (a tsvector, an inet) is passed through under its own name in upper case: the
// generator sizes no rule against it and the resource's field type is the author's to
// choose.
func postgresColumnType(formatted string) (string, error) {
	m := postgresTypeLength.FindStringSubmatch(strings.TrimSpace(strings.ToLower(formatted)))
	if m == nil {
		return "", errors.Newf("column type %q is not one the schema read understands", formatted)
	}
	name, length, array := m[1], m[2], m[3] != ""

	var scalar string
	switch name {
	case "character varying", "character", "bpchar":
		if length == "" {
			scalar = "STRING(MAX)"
		} else {
			scalar = fmt.Sprintf("STRING(%s)", length)
		}
	case "text", "citext", "name":
		scalar = "STRING(MAX)"
	case "uuid":
		scalar = "UUID"
	case "smallint", "integer", "bigint":
		scalar = "INT64"
	case "real":
		scalar = "FLOAT32"
	case "double precision":
		scalar = "FLOAT64"
	case "numeric":
		scalar = "NUMERIC"
	case "boolean":
		scalar = "BOOL"
	case "timestamp with time zone", "timestamp without time zone":
		scalar = "TIMESTAMP"
	case "date":
		scalar = "DATE"
	case "bytea":
		scalar = "BYTES(MAX)"
	case "json", "jsonb":
		scalar = jsonSpannerType
	default:
		scalar = strings.ToUpper(name)
	}

	if array {
		return "ARRAY<" + scalar + ">", nil
	}

	return scalar, nil
}

// fetchPostgresEnumValues reads the rows of every table that carries a Description
// column, the shape of an enumeration table (an Id and a Description), as Spanner's
// fetchEnumValues does. The feature flags table carries a Description column too and is
// never an enumeration.
func fetchPostgresEnumValues(ctx context.Context, pool *pgxpool.Pool) (map[string][]*enumData, error) {
	tables, err := pool.Query(ctx, `SELECT DISTINCT c.relname
		FROM pg_catalog.pg_attribute a
			JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema()
			AND c.relkind IN ('r', 'p')
			AND a.attname = 'Description'
			AND a.attnum > 0
			AND NOT a.attisdropped
		ORDER BY c.relname`)
	if err != nil {
		return nil, errors.Wrap(err, "pgxpool.Pool.Query()")
	}
	names, err := pgx.CollectRows(tables, pgx.RowTo[string])
	if err != nil {
		return nil, errors.Wrap(err, "pgx.CollectRows()")
	}

	enumResults := make(map[string][]*enumData, len(names))
	for _, name := range names {
		if name == string(resource.FeatureFlagsResource) {
			continue
		}

		rows, err := pool.Query(ctx, fmt.Sprintf(`SELECT DISTINCT CAST("Id" AS TEXT), CAST("Description" AS TEXT) FROM %s ORDER BY 1`, pgx.Identifier{name}.Sanitize()))
		if err != nil {
			return nil, errors.Wrapf(err, "pgxpool.Pool.Query(%s)", name)
		}
		values, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (*enumData, error) {
			var e enumData
			if err := row.Scan(&e.ID, &e.Description); err != nil {
				return nil, errors.Wrap(err, "pgx.Row.Scan()")
			}

			return &e, nil
		})
		if err != nil {
			return nil, errors.Wrapf(err, "pgx.CollectRows(%s)", name)
		}

		enumResults[name] = values
	}

	return enumResults, nil
}
