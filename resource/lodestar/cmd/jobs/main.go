// Command jobs is the application's job process: the work that runs apart from the
// served site, over the same data level the site opens, one command per job:
//
//	go run -tags skipAuth ./cmd/jobs <command> [flags]
//
// The root opens the data level once and registers every command over it; a command is
// a package under cmd/jobs around its work in pkg/jobs (today cleanup-files, the
// orphaned-file cleanup), and impulse add job <name> adds one. On Cloud Run the same
// binary runs as the job the scheduled methods start: Cloud Scheduler calls the service,
// and the service starts the job deployed with it, with the command as the container's
// arguments.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/cccteam/ccc/resource/lodestar/cmd/jobs/cleanupfiles"
	"github.com/cccteam/ccc/resource/lodestar/pkg/config"
	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"
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

	root := &cobra.Command{
		Use:           "jobs",
		Short:         "The application's job process: one command per job, over the data level",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		cleanupfiles.Command(data),
	)
	if err := root.ExecuteContext(ctx); err != nil {
		return errors.Wrap(err, "cobra.Command.ExecuteContext()")
	}

	return nil
}
