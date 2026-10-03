// main serves the application: the generated resource API behind browser sessions, the
// console's and the portal's built Angular bundles under /console/ and /portal/, and the
// root alone redirecting to the console.
//
// Demonstrates: impulse.bootstrapped, ci-stub.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/cccteam/ccc/resource/lodestar/app"
	"github.com/cccteam/ccc/resource/lodestar/pkg/config"
	"github.com/cccteam/ccc/resource/lodestar/pkg/router"
	"github.com/cccteam/ccc/resource/maintenance"
	"github.com/go-playground/errors/v5"
	"github.com/jtwatson/server"
)

func main() {
	if err := Main(); err != nil {
		log.Fatal(err)
	}
}

func Main() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// A maintenance revision serves the maintenance page in place of the application: no
	// database, no sessions, no secrets are opened. The deploy pipeline starts one before
	// a release that replaces or interrupts the database.
	if maintenance.Requested() {
		if err := maintenance.Serve(ctx); err != nil {
			return errors.Wrap(err, "maintenance.Serve()")
		}

		return nil
	}

	conf, err := config.NewSiteConfiguration(ctx)
	if err != nil {
		return errors.Wrap(err, "config.NewSiteConfiguration()")
	}
	defer conf.Close()

	a := app.New(conf)
	if err := a.Start(ctx); err != nil {
		return errors.Wrap(err, "app.Start()")
	}
	if err := server.New(conf.Addr()).Start(ctx, router.New(a, router.AppHooks(a))); err != nil {
		return errors.Wrap(err, "server exited unexpectedly")
	}

	return nil
}
