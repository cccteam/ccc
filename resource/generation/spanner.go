package generation

import (
	"context"
	"log"

	initiator "github.com/cccteam/db-initiator"
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

	c.db = db
	c.tableMap = tableMap
	c.enumValues = enumValues

	return nil
}
