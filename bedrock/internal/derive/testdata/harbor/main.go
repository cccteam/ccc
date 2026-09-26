// main serves the application: the generated resource API behind browser sessions, and the
// console's built Angular bundle for everything else.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/go-playground/errors/v5"
	"github.com/impulseframework/harbor/app"
	"github.com/impulseframework/harbor/pkg/config"
	"github.com/impulseframework/harbor/pkg/router"
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

	if err := server.New(conf.Addr()).Start(ctx, router.New(app.New(conf), router.Hooks{})); err != nil {
		return errors.Wrap(err, "server exited unexpectedly")
	}

	return nil
}
