// Package main is the deployment's migration step: it applies the schema migrations
// and checks the release's role policy against what the permission engine's store
// holds. It reads the core and data configuration levels and nothing above them.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"

	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/config"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/deploy"
	"github.com/go-playground/errors/v5"
)

func main() {
	seed := flag.Bool("seed", false, "apply the development seed (schema/devseed) after the schema migrations, as data migrations; the pipeline passes it in test environments and never in production")
	flag.Parse()
	if err := run(context.Background(), *seed); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, seed bool) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	// The schema goes first, before any client opens: the tables the data level's
	// clients read may not exist yet.
	settings, err := config.LoadSpannerSettings(ctx)
	if err != nil {
		return errors.Wrap(err, "config.LoadSpannerSettings()")
	}
	if err := deploy.MigrateSchema(ctx, settings); err != nil {
		return errors.Wrap(err, "deploy.MigrateSchema()")
	}

	// The seed, when this deployment asks for it: the development data as data
	// migrations, tracked apart from the schema, so a seeded database takes nothing
	// twice and a new seed file reaches it. Before the roles, as cmd/bootstrap does.
	if seed {
		if err := deploy.SeedDevelopmentData(ctx, settings); err != nil {
			return errors.Wrap(err, "deploy.SeedDevelopmentData()")
		}
	}

	data, err := config.NewDataConfiguration(ctx)
	if err != nil {
		return errors.Wrap(err, "config.NewDataConfiguration()")
	}
	defer data.Close()

	// The default roles travel with the release: opening the data level validated the
	// staff role file against the collection, so a release whose roles are wrong stopped
	// above. What is left is to print what the store holds that this release cannot use
	// as written.
	if err := deploy.CheckRoles(ctx, data.Staff().Access(), staff.Name); err != nil {
		return errors.Wrap(err, "deploy.CheckRoles()")
	}

	return nil
}
