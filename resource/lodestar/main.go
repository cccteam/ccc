// main is the entrypoint for the Lodestar demo application: it serves the generated
// resource API (three outlets: the crew console, the client portal, and the droid
// channel) and both Angular applications against a bootstrapped Spanner emulator
// database (see cmd/bootstrap).
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

	conf, err := config.New(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get config")
	}
	defer conf.Close()

	if err := server.New(conf.Addr()).Start(ctx, router.New(app.New(conf))); err != nil {
		return errors.Wrap(err, "server exited unexpectedly")
	}

	return nil
}
