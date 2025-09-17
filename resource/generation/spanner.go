package generation

import (
	"context"
	"fmt"
	"log"

	"cloud.google.com/go/spanner"
	initiator "github.com/cccteam/db-initiator"
	"github.com/cccteam/spxscan"
	"github.com/go-playground/errors/v5"
)

func createSpannerDB(ctx context.Context, emulatorVersion, migrationSourceURL string) (*initiator.SpannerDB, error) {
	log.Println("Starting Spanner Container...")
	spannerContainer, err := initiator.NewSpannerContainer(ctx, emulatorVersion)
	if err != nil {
		return nil, errors.Wrap(err, "initiator.NewSpannerContainer()")
	}

	db, err := spannerContainer.CreateDatabase(ctx, "resourcegeneration")
	if err != nil {
		return nil, errors.Wrap(err, "initiator.SpannerContainer.CreateDatabase()")
	}

	log.Println("Starting Spanner Migration...")
	if err := db.MigrateUp(migrationSourceURL); err != nil {
		return nil, errors.Wrap(err, "initiator.SpannerDB.MigrateUp()")
	}

	return db, nil
}

func (c *client) runSpanner(ctx context.Context, emulatorVersion, migrationSourceURL string) error {
	db, err := createSpannerDB(ctx, emulatorVersion, migrationSourceURL)
	if err != nil {
		return err
	}

	tableMap, err := createTableMapUsingQuery(ctx, db.Client)
	if err != nil {
		return errors.Wrap(err, "newTableMap()")
	}

	enumValues, err := fetchEnumValues(ctx, db.Client)
	if err != nil {
		return errors.Wrap(err, "fetchEnumValues()")
	}

	if err := db.DropDatabase(context.Background()); err != nil {
		return errors.Wrap(err, "db-initiator.SpannerDB.DropDatabase()")
	}

	if err := db.Close(); err != nil {
		return errors.Wrap(err, "db-initiator.SpannerDB.Close()")
	}

	c.tableMap = tableMap
	c.enumValues = enumValues

	return nil
}

func createTableMapUsingQuery(ctx context.Context, db *spanner.Client) (map[string]*tableMetadata, error) {
	log.Println("Creating spanner table lookup...")

	results, err := queryInformationSchema(ctx, db)
	if err != nil {
		return nil, err
	}

	schemaMetadata := make(map[string]*tableMetadata)
	viewColumns := make([]*informationSchemaResult, 0, 16)
	tokenListColumns := make([]*informationSchemaResult, 0, 16)
	for i := range results {
		table, ok := schemaMetadata[results[i].TableName]
		if !ok {
			table = &tableMetadata{
				Columns:       make(map[string]columnMeta),
				SearchIndexes: make(map[string][]*searchExpression),
				IsView:        results[i].IsView,
			}
		}

		if results[i].IsView {
			viewColumns = append(viewColumns, &results[i])
		}

		if results[i].SpannerType == "TOKENLIST" {
			tokenListColumns = append(tokenListColumns, &results[i])

			continue
		}

		table.addSchemaResult(&results[i])
		schemaMetadata[results[i].TableName] = table
	}

	nullableViews, err := viewColumnNullability(schemaMetadata, viewColumns)
	if err != nil {
		return nil, err
	}
	schemaMetadata = nullableViews

	searchIndexMetadata, err := tokenListSearchIndexes(schemaMetadata, tokenListColumns)
	if err != nil {
		return nil, err
	}
	schemaMetadata = searchIndexMetadata

	return schemaMetadata, nil
}

const tableMapQuery string = `WITH DEPENDENCIES AS (
		SELECT
			kcu1.TABLE_NAME, 
			kcu1.COLUMN_NAME, 
			(SUM(CASE tc.CONSTRAINT_TYPE WHEN 'PRIMARY KEY' THEN 1 ELSE 0 END)) AS IS_PRIMARY_KEY,
			(SUM(CASE tc.CONSTRAINT_TYPE WHEN 'FOREIGN KEY' THEN 1 ELSE 0 END)) AS IS_FOREIGN_KEY,
			SUM(CASE tc.CONSTRAINT_TYPE WHEN 'PRIMARY KEY' THEN kcu1.ORDINAL_POSITION ELSE NULL END) AS KEY_ORDINAL_POSITION,
			(CASE MIN(CASE 
					WHEN kcu4.TABLE_NAME IS NOT NULL THEN 1
					WHEN kcu2.TABLE_NAME IS NOT NULL THEN 2
					ELSE 3
					END)
			WHEN 1 THEN MAX(kcu4.TABLE_NAME)
			WHEN 2 THEN MAX(kcu2.TABLE_NAME)
			ELSE NULL
			END) AS REFERENCED_TABLE,
			(CASE MIN(CASE 
					WHEN kcu4.COLUMN_NAME IS NOT NULL THEN 1
					WHEN kcu2.COLUMN_NAME IS NOT NULL THEN 2
					ELSE 3
					END)
			WHEN 1 THEN MAX(kcu4.COLUMN_NAME)
			WHEN 2 THEN MAX(kcu2.COLUMN_NAME)
			ELSE NULL
			END) AS REFERENCED_COLUMN
		FROM INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu1 -- All columns that are Primary Key or Foreign Key
		JOIN INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc ON tc.CONSTRAINT_NAME = kcu1.CONSTRAINT_NAME -- Identify whether column is Primary Key or Foreign Key
		-- All unique constraints (e.g. PK_Persons) referenced by foreign key constraints (e.g. FK_PersonPhones_PersonId)
		LEFT JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc ON rc.CONSTRAINT_NAME = kcu1.CONSTRAINT_NAME 
		LEFT JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu2 ON kcu2.CONSTRAINT_NAME = rc.UNIQUE_CONSTRAINT_NAME -- Table & Column belonging to referenced unique constraint (e.g. Persons, Id)
			AND kcu2.ORDINAL_POSITION = kcu1.ORDINAL_POSITION
		LEFT JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu3 ON kcu3.TABLE_NAME = kcu2.TABLE_NAME AND kcu3.COLUMN_NAME = kcu2.COLUMN_NAME
		LEFT JOIN INFORMATION_SCHEMA.REFERENTIAL_CONSTRAINTS rc2 ON rc2.CONSTRAINT_NAME = kcu3.CONSTRAINT_NAME
		LEFT JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE kcu4 ON kcu4.CONSTRAINT_NAME = rc2.UNIQUE_CONSTRAINT_NAME -- Table & Column belonging to 1-jump referenced unique constraint (e.g. DoeInstitutions, Id)
			AND kcu4.ORDINAL_POSITION = kcu1.ORDINAL_POSITION
		WHERE
			kcu1.CONSTRAINT_SCHEMA != 'INFORMATION_SCHEMA'
			AND tc.CONSTRAINT_TYPE IN ('PRIMARY KEY', 'FOREIGN KEY')
		GROUP BY kcu1.TABLE_NAME, kcu1.COLUMN_NAME
	)
	SELECT
		c.TABLE_NAME,
		c.COLUMN_NAME,
		(c.IS_NULLABLE = 'YES') AS IS_NULLABLE,
		c.SPANNER_TYPE,
		(d.IS_PRIMARY_KEY > 0 and d.IS_PRIMARY_KEY IS NOT NULL) as IS_PRIMARY_KEY,
		(d.IS_FOREIGN_KEY > 0 and d.IS_FOREIGN_KEY IS NOT NULL) as IS_FOREIGN_KEY,
		d.REFERENCED_TABLE,
		d.REFERENCED_COLUMN,
		(t.TABLE_NAME IS NULL AND v.TABLE_NAME IS NOT NULL) as IS_VIEW,
		v.VIEW_DEFINITION,
		ic.INDEX_NAME IS NOT NULL AS IS_INDEX,
		MAX(COALESCE(i.IS_UNIQUE, false)) AS IS_UNIQUE_INDEX,
		c.GENERATION_EXPRESSION,
		c.ORDINAL_POSITION,
		COALESCE(d.KEY_ORDINAL_POSITION, 1) AS KEY_ORDINAL_POSITION,
		c.COLUMN_DEFAULT IS NOT NULL AS HAS_DEFAULT,
	FROM INFORMATION_SCHEMA.COLUMNS c
		LEFT JOIN INFORMATION_SCHEMA.TABLES t ON c.TABLE_NAME = t.TABLE_NAME
			AND t.TABLE_TYPE = 'BASE TABLE'
		LEFT JOIN INFORMATION_SCHEMA.VIEWS v ON c.TABLE_NAME = v.TABLE_NAME
		LEFT JOIN DEPENDENCIES d ON c.TABLE_NAME = d.TABLE_NAME
			AND c.COLUMN_NAME = d.COLUMN_NAME
		LEFT JOIN INFORMATION_SCHEMA.INDEX_COLUMNS ic ON c.COLUMN_NAME = ic.COLUMN_NAME
			AND c.TABLE_NAME = ic.TABLE_NAME
		LEFT JOIN INFORMATION_SCHEMA.INDEXES i ON ic.INDEX_NAME = i.INDEX_NAME 
	WHERE 
		c.TABLE_SCHEMA != 'INFORMATION_SCHEMA'
		AND c.COLUMN_NAME NOT LIKE '%_HIDDEN'
	GROUP BY c.TABLE_NAME, c.COLUMN_NAME, IS_NULLABLE, c.SPANNER_TYPE,
	d.IS_PRIMARY_KEY, d.IS_FOREIGN_KEY, d.REFERENCED_COLUMN, d.REFERENCED_TABLE,
	IS_VIEW, v.VIEW_DEFINITION, IS_INDEX, c.GENERATION_EXPRESSION, c.ORDINAL_POSITION, d.KEY_ORDINAL_POSITION, c.COLUMN_DEFAULT
	ORDER BY c.TABLE_NAME, c.ORDINAL_POSITION`

func queryInformationSchema(ctx context.Context, db *spanner.Client) ([]informationSchemaResult, error) {
	stmt := spanner.Statement{SQL: tableMapQuery}

	var result []informationSchemaResult
	if err := spxscan.Select(ctx, db.Single(), &result, stmt); err != nil {
		return nil, errors.Wrap(err, "spxscan.Select()")
	}

	return result, nil
}

func fetchEnumValues(ctx context.Context, db *spanner.Client) (map[string][]enumData, error) {
	qry := `
	SELECT DISTINCT
		c.TABLE_NAME
	FROM INFORMATION_SCHEMA.COLUMNS c
	LEFT JOIN INFORMATION_SCHEMA.TABLES t ON c.TABLE_NAME = t.TABLE_NAME
		AND t.TABLE_TYPE = 'BASE TABLE'
	WHERE c.COLUMN_NAME = 'Description';
	`
	stmt := spanner.Statement{SQL: qry}

	type tableNameResults struct {
		TableName string `spanner:"TABLE_NAME"`
	}

	var results []tableNameResults
	if err := spxscan.Select(ctx, db.Single(), &results, stmt); err != nil {
		return nil, errors.Wrap(err, "spxscan.Select()")
	}

	enumResults := make(map[string][]enumData, len(results))
	for _, tnr := range results {
		stmt := spanner.Statement{SQL: fmt.Sprintf("SELECT DISTINCT Id, Description FROM %s ORDER BY Id", tnr.TableName)}

		var results []enumData
		if err := spxscan.Select(ctx, db.Single(), &results, stmt); err != nil {
			return nil, errors.Wrap(err, "spxscan.Select()")
		}

		enumResults[tnr.TableName] = results
	}

	return enumResults, nil
}
