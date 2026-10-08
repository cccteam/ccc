// main serves the application: the generated resource API behind browser sessions, and the
// console's built Angular bundle for everything else.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"

	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/app"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/config"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/outlets/pkg/router"
	"github.com/cccteam/ccc/resource/maintenance"
	"github.com/cccteam/ccc/resource/server"
	"github.com/cccteam/ccc/tracer"
	"github.com/go-playground/errors/v5"
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
	// The App's background work: the feature flags are followed until the server stops,
	// and a copy that could not be read when the App was built stops the start here.
	if err := a.Start(ctx); err != nil {
		return errors.Wrap(err, "app.Start()")
	}
	// Tracing runs ahead of the logger on every request, so the request log and the spans
	// share the trace the caller's headers name.
	hooks := router.Hooks{Outermost: []func(http.Handler) http.Handler{tracer.NewGoogleCloudHandler()}}
	if err := server.New(conf.Addr()).Start(ctx, router.New(a, hooks)); err != nil {
		return errors.Wrap(err, "server exited unexpectedly")
	}

	return nil
}
