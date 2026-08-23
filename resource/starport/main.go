// main is the entrypoint for the starport demo application: it serves the generated
// resource API and the Angular frontend against a bootstrapped Spanner emulator
// database (see cmd/bootstrap).
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/cccteam/ccc/resource/starport/app"
	"github.com/cccteam/ccc/resource/starport/pkg/config"
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

	if err := server.New(conf.Addr()).Start(ctx, app.NewServer(conf)); err != nil {
		return errors.Wrap(err, "server exited unexpectedly")
	}

	return nil
}
