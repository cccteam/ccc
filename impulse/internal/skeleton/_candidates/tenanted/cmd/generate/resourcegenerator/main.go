// Package main is the application's generate program: it runs the resource generator
// generator.go declares (newGenerator) and prints what a successful run raised, one line
// each, to standard error with a fixed prefix and no timestamp, so a tool that reads the
// lines has one contract per prefix. Every run prints the schema warnings as
// "Warning: <text>": a performance finding about the schema, never a refusal, and the
// run has written its output when the lines print; warnings_test.go pins the accepted
// set. Run with -audit, it also prints the audit pass's findings as "Audit: <text>":
// advisory findings about shapes the framework handles under a stated limitation, which
// a normal generation (go generate passes no flag) never prints; impulse audit runs
// every program that way.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/go-playground/errors/v5"
)

func main() {
	audit := flag.Bool("audit", false, "print the audit pass's findings after the warnings")
	flag.Parse()

	if err := run(context.Background(), *audit); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, audit bool) error {
	generator, err := newGenerator(ctx)
	if err != nil {
		return err
	}
	defer generator.Close()

	if err := generator.Generate(); err != nil {
		return errors.Wrap(err, "generation.Generator.Generate()")
	}

	for _, warning := range generator.Warnings() {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
	}
	if audit {
		for _, finding := range generator.Audit() {
			fmt.Fprintf(os.Stderr, "Audit: %s\n", finding)
		}
	}

	return nil
}
