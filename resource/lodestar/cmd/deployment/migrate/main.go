// Package main is the deployment's migration step: it applies the schema migrations
// and checks each auth's policy store against the release's role file, printing what a
// deploy should hear about before the release takes traffic. It reads the core and
// data configuration levels and nothing above them.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/crew"
	"github.com/cccteam/ccc/resource/lodestar/pkg/auth/members"
	"github.com/cccteam/ccc/resource/lodestar/pkg/config"
	"github.com/cccteam/ccc/resource/lodestar/pkg/deploy"
	"github.com/go-playground/errors/v5"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
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

	// Opening the data level validates each auth's role file against the generated
	// collection: a release whose roles are wrong fails here, before it takes traffic.
	data, err := config.NewDataConfiguration(ctx)
	if err != nil {
		return errors.Wrap(err, "config.NewDataConfiguration()")
	}
	defer data.Close()

	// The roles are the release's and the store holds no row for them, so there is
	// nothing to write; the check reports the role file's warnings and what the store
	// holds that this release cannot use as written.
	if err := deploy.CheckRoles(ctx, data.Crew().Access(), crew.Name); err != nil {
		return errors.Wrap(err, "deploy.CheckRoles(crew)")
	}
	if err := deploy.CheckRoles(ctx, data.Members().Access(), members.Name); err != nil {
		return errors.Wrap(err, "deploy.CheckRoles(members)")
	}

	return nil
}
