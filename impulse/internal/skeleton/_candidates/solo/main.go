// main serves solo: the generated resource API behind browser sessions, and the
// console's built Angular bundle for everything else.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/app"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/config"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/solo/pkg/router"
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

	conf, err := config.NewServerConfiguration(ctx)
	if err != nil {
		return errors.Wrap(err, "config.NewServerConfiguration()")
	}
	defer conf.Close()

	if err := server.New(conf.Addr()).Start(ctx, router.New(app.New(conf))); err != nil {
		return errors.Wrap(err, "server exited unexpectedly")
	}

	return nil
}
