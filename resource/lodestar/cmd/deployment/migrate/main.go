// Package main is the deployment's migration step: it applies the schema migrations
// and reconciles the role configuration into the permission engine. It reads the core
// and data configuration levels and nothing above them.
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

	data, err := config.NewDataConfiguration(ctx)
	if err != nil {
		return errors.Wrap(err, "config.NewDataConfiguration()")
	}
	defer data.Close()

	// The roles are reconciled per sector: the domain universe is the Sectors table.
	domains, err := data.Domains(ctx)
	if err != nil {
		return errors.Wrap(err, "config.DataConfiguration.Domains()")
	}
	if err := deploy.MigrateRoles(ctx, data.UserManager(), crew.RolesPath, domains...); err != nil {
		return errors.Wrap(err, "deploy.MigrateRoles(crew)")
	}
	if err := deploy.MigrateRoles(ctx, data.Members().Access().UserManager(), members.RolesPath, domains...); err != nil {
		return errors.Wrap(err, "deploy.MigrateRoles(members)")
	}

	return nil
}
