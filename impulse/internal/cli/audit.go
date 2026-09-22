package cli

import (
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/audit"
	"github.com/cccteam/ccc/impulse/internal/check"
)

func newAudit() *cobra.Command {
	var appDir string

	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Run the generate programs with -audit and print the schema warnings and audit findings",
		Long: `audit runs every generator program of the application with -audit, from the module
root, and prints what each raised: the schema warnings a generation always prints, and
the audit pass's findings, advisory findings about shapes the framework handles under a
stated limitation (a table storing files whose rows the database deletes by cascade),
which a normal generation never prints. Findings never fail the command; a program that
fails, or does not take -audit, does.

It regenerates the tree like go generate does, so it needs the Spanner emulator. Run it
by decision, not on every change: before a release, after a schema change to a table
that stores files, or when a stated limitation is in question.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a, err := app.Discover(appDir)
			if err != nil {
				return err
			}
			if audit.Run(cmd.Context(), a, check.OSExec{}, cmd.OutOrStdout()) {
				return exitError{code: 1}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&appDir, "app", ".", "application root (the directory holding go.mod)")

	return cmd
}
