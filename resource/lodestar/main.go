// main serves the application: the generated resource API behind browser sessions, and the
// console's built Angular bundle for everything else.
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
