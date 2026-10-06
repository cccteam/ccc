// Package main is the application's job process: work that runs to completion outside
// a request, as a Cloud Run job the site starts. It reads the core and data configuration
// levels and nothing above them.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/go-playground/errors/v5"
	"github.com/impulseframework/harbor/pkg/config"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	data, err := config.NewDataConfiguration(ctx)
	if err != nil {
		return errors.Wrap(err, "config.NewDataConfiguration()")
	}
	defer data.Close()

	log.Printf("%s: nothing to do yet", data.ServiceName())

	return nil
}
