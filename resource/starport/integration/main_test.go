// Package integration drives the starport's generated HTTP handlers against a real
// Spanner emulator to detect permission-enforcement regressions in the resource package.
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

	if err := c.Terminate(ctx); err != nil {
		fmt.Println(err)
	}

	if err := c.Close(); err != nil {
		fmt.Println(err)
	}

	os.Exit(exitCode)
}

func prepareDatabase(ctx context.Context, t *testing.T, sourceURL ...string) (*initiator.SpannerDB, error) {
	t.Helper()

	db, err := container.CreateDatabase(ctx, t.Name())
	if err != nil {
		return nil, errors.Wrapf(err, "initiator.SpannerContainer.CreateDatabase()")
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
		return nil, errors.Wrapf(err, "initiator.SpannerDB.MigrateUp()")
	}

	return db, nil
}
