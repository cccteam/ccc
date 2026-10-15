// Package cleanupfiles is the cleanup-files command of the job process: the orphaned-file
// cleanup over the application's file stores (pkg/jobs, CleanupFiles), run as
//
//	go run -tags skipAuth ./cmd/jobs cleanup-files [--store <name>] [--window 48h] [--dry-run]
//
// It reads every key the rows hold through the generated holders and deletes the
// UUID-named objects older than the window that no row holds, one store at a time; a
// store named with --store (documents, or default for the default store) narrows the
// run, --window raises the age an object must reach, and --dry-run lists what would go
// and deletes nothing. On Cloud Run the scheduled CleanUpFiles method (pkg/rpc) starts
// it each day.
//
// Demonstrates: filestore.cleanup.
package cleanupfiles

import (
	"context"
	"fmt"
	"time"

	"github.com/cccteam/ccc/resource"
	"github.com/cccteam/ccc/resource/lodestar/pkg/config"
	"github.com/cccteam/ccc/resource/lodestar/pkg/jobs"
	"github.com/cccteam/ccc/resource/lodestar/pkg/resources"
	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
)

// defaultStoreArg is how --store names the default store, which has no name of its own.
const defaultStoreArg = "default"

// Command is the cleanup-files command over the data level the root opened.
func Command(data *config.DataConfiguration) *cobra.Command {
	c := &command{data: data}
	cmd := &cobra.Command{
		Use:   jobs.CleanupCommand,
		Short: "Delete the stored files no row holds",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return c.Run(cmd.Context())
		},
	}
	cmd.Flags().StringVar(&c.store, "store", "", "the store to clean: documents, or default for the default store; every store when unset")
	cmd.Flags().DurationVar(&c.window, "window", 0, "the age an object must reach before it may be deleted; the cleanup's default of 48h when unset, and under 24h is refused")
	cmd.Flags().BoolVar(&c.dryRun, "dry-run", false, "list what would be deleted and delete nothing")

	return cmd
}

// command is the cleanup-files command: the data level it runs over, and its flags.
type command struct {
	data   *config.DataConfiguration
	store  string
	window time.Duration
	dryRun bool
}

// Run runs the cleanup over the stores and prints each report: the counts, then the
// orphans deleted or, on a dry run, listed. A store that refuses stops the run after the
// reports before it.
func (c *command) Run(ctx context.Context) error {
	opts := jobs.CleanupOptions{Window: c.window, DryRun: c.dryRun}
	if c.store != "" {
		store, err := storeNamed(c.store)
		if err != nil {
			return err
		}
		opts.Stores = []resource.StoreName{store}
	}
	reports, err := jobs.CleanupFiles(ctx, c.data.ResourceClient(), opts)
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

// storeNamed reads the --store argument: the default store by the word default, a named
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
