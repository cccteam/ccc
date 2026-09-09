// Package integration drives the served application end to end against a real Spanner
// emulator: the real router, the real session manager, and the real permission engine,
// provisioned through the deployment's own steps.
package integration

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	initiator "github.com/cccteam/db-initiator"
	"github.com/go-playground/errors/v5"
)

var container *initiator.SpannerContainer

func TestMain(m *testing.M) {
	ctx := context.Background()

	c, err := initiator.NewSpannerContainer(ctx, "1.5.56")
	if err != nil {
		log.Fatal(err)
	}
	container = c

	exitCode := m.Run()
	closeSharedWorld()
	closeSharedDocuments()

	if err := c.Terminate(ctx); err != nil {
		fmt.Println(err)
	}

	if err := c.Close(); err != nil {
		fmt.Println(err)
	}

	os.Exit(exitCode)
}

// prepareDatabase creates a database migrated with the application schema followed by
// any extra fixture sources, dropping it when the test completes.
func prepareDatabase(ctx context.Context, t *testing.T, sourceURL ...string) (*initiator.SpannerDB, error) {
	t.Helper()

	db, err := container.CreateDatabase(ctx, t.Name())
	if err != nil {
		return nil, errors.Wrap(err, "initiator.SpannerContainer.CreateDatabase()")
	}
	t.Cleanup(func() {
		if err := db.DropDatabase(context.Background()); err != nil {
			panic(err)
		}
		if err := db.Close(); err != nil {
			panic(err)
		}
	})

	if err := db.MigrateUp(sourceURL...); err != nil {
		return nil, errors.Wrap(err, "initiator.SpannerDB.MigrateUp()")
	}

	return db, nil
}
