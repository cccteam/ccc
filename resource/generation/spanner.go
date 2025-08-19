package generation

import (
	"context"
	"log"

	initiator "github.com/cccteam/db-initiator"
	"github.com/go-playground/errors/v5"
)

func (c *client) runSpanner(ctx context.Context) error {
	if err := c.genCache.DeleteSubpath("migrations"); err != nil {
		return errors.Wrap(err, "cache.Cache.DeleteSubpath()")
	}

	log.Println("Starting Spanner Container...")
	spannerContainer, err := initiator.NewSpannerContainer(ctx, c.spannerEmulatorVersion)
	if err != nil {
		return errors.Wrap(err, "initiator.NewSpannerContainer()")
	}

	db, err := spannerContainer.CreateDatabase(ctx, "resourcegeneration")
	if err != nil {
		return errors.Wrap(err, "initiator.SpannerContainer.CreateDatabase()")
	}

	log.Println("Starting Spanner Migration...")
	if err := db.MigrateUp(c.migrationSourceURL); err != nil {
		return errors.Wrap(err, "initiator.SpannerDB.MigrateUp()")
	}

	c.db = db.Client
	if c.tableMap, err = c.newTableMap(ctx); err != nil {
		return errors.Wrap(err, "newTableMap()")
	}

	if c.enumValues, err = c.fetchEnumValues(ctx); err != nil {
		return errors.Wrap(err, "fetchEnumValues()")
	}

	c.cleanup = func() {
		if err := db.DropDatabase(ctx); err != nil {
			panic(err)
		}

		if err := db.Close(); err != nil {
			panic(err)
		}

		if err := c.populateCache(); err != nil {
			panic(err)
		}
	}

	return nil
}
