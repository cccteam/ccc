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
