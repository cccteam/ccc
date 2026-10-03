// Command jobs is the application's job process: the work that runs on a schedule
// beside the served site, over the same data level the site opens. Its one command
// today is cleanup-files, the orphaned-file cleanup over the application's file stores:
//
//	go run -tags skipAuth ./cmd/jobs cleanup-files [-store <name>] [-window 48h] [-dry-run]
//
// The command opens the data level (the database, the file stores, the live service),
// reads every key the rows hold through the generated holders, and deletes the
// UUID-named objects older than the window that no row holds, one store at a time; a
// store named with -store (documents, or default for the default store) narrows the run,
// -window raises the age an object must reach, and -dry-run lists what would go and
// deletes nothing. On Cloud Run the same binary runs as the job the schedule starts.
//
// Demonstrates: filestore.cleanup.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/config"
	"github.com/cccteam/ccc/resource/lodestar/pkg/jobs"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/go-playground/errors/v5"
)

// cleanupCommand names the one command.
const cleanupCommand = "cleanup-files"

// defaultStoreArg is how -store names the default store, which has no name of its own.
const defaultStoreArg = "default"

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	if len(args) == 0 || args[0] != cleanupCommand {
		return errors.Newf("usage: jobs %s [-store <name>] [-window <duration>] [-dry-run]", cleanupCommand)
	}
	flags := flag.NewFlagSet(cleanupCommand, flag.ContinueOnError)
	storeName := flags.String("store", "", "the store to clean: documents, or default for the default store; every store when unset")
	window := flags.Duration("window", 0, "the age an object must reach before it may be deleted; the cleanup's default of 48h when unset, and under 24h is refused")
	dryRun := flags.Bool("dry-run", false, "list what would be deleted and delete nothing")
	if err := flags.Parse(args[1:]); err != nil {
		return errors.Wrap(err, "flag.FlagSet.Parse()")
	}
	opts := jobs.CleanupOptions{Window: *window, DryRun: *dryRun}
	if *storeName != "" {
		store, err := storeNamed(*storeName)
		if err != nil {
			return err
		}
		opts.Stores = []resource.StoreName{store}
	}

	data, err := config.NewDataConfiguration(ctx)
	if err != nil {
		return errors.Wrap(err, "config.NewDataConfiguration()")
	}
	defer data.Close()

	reports, err := jobs.CleanupFiles(ctx, data.ResourceClient(), opts)
	for _, report := range reports {
		fmt.Println(report)
		for _, key := range report.Orphans {
			fmt.Printf("  %s\n", key)
		}
	}
	if err != nil {
		return errors.Wrap(err, "jobs.CleanupFiles()")
	}

	return nil
}

// storeNamed reads the -store argument: the default store by the word default, a named
// store by its name.
func storeNamed(name string) (resource.StoreName, error) {
	switch resource.StoreName(name) {
	case defaultStoreArg:
		return resource.DefaultStore, nil
	case resource.StoreNameFor[resources.Documents]():
		return resource.StoreNameFor[resources.Documents](), nil
	default:
		return "", errors.Newf("unknown store %q; the stores are %s and %s", name, defaultStoreArg, resource.StoreNameFor[resources.Documents]())
	}
}
