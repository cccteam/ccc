// main serves the console site: its generated resource API behind browser sessions,
// and its built Angular bundle for everything else.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/apps/console/app"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/apps/console/pkg/router"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/sites/pkg/config"
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

	conf, err := config.NewSiteConfiguration(ctx)
	if err != nil {
		return errors.Wrap(err, "config.NewSiteConfiguration()")
	}
	defer conf.Close()

	if err := server.New(conf.Addr()).Start(ctx, router.New(app.New(conf))); err != nil {
		return errors.Wrap(err, "server exited unexpectedly")
	}

	return nil
}
