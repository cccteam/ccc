// Package main is the application's generate program: it runs the resource generator
// the generate package declares (NewGenerator), and prints the schema warnings a
// successful run raised, one line each. A warning is a performance finding about the
// schema, never a refusal: the run has written its output when the lines print.
package main

import (
	"context"
	"log"

	"github.com/cccteam/ccc/resource/lodestar/cmd/generate"
	"github.com/go-playground/errors/v5"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	generator, err := generate.NewGenerator(ctx)
	if err != nil {
		return errors.Wrap(err, "generate.NewGenerator()")
	}
	defer generator.Close()

	if err := generator.Generate(); err != nil {
		return errors.Wrap(err, "generation.Generator.Generate()")
	}

	for _, warning := range generator.Warnings() {
		log.Printf("Warning: %s", warning)
	}

	return nil
}
