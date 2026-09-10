// Package main is the deployment's migration step: it applies the schema migrations
// and reconciles the role configuration into the permission engine. It reads the core
// and data configuration levels and nothing above them.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/config"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/deploy"
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

	data, err := config.NewDataConfiguration(ctx)
	if err != nil {
		return errors.Wrap(err, "config.NewDataConfiguration()")
	}
	defer data.Close()

	if err := deploy.MigrateRoles(ctx, data.UserManager(), staff.RolesPath); err != nil {
		return errors.Wrap(err, "deploy.MigrateRoles()")
	}

	return nil
}
