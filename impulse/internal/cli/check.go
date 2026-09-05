package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

func newCheck() *cobra.Command {
	var (
		appDir       string
		skipGenerate bool
		fix          bool
		only         []string
		list         bool
	)

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Verify an application's invariants: code against code",
		Long: `check reads the application from its own files (the generator program, go.mod, the
browser apps, the process files, the test harnesses, the config struct tags) and verifies
the agreements between them. It exits non-zero when any check fails.

The regen check runs go generate, which needs the Spanner emulator and rewrites generated
files in the working tree; pass --skip-generate to leave it out.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if list {
				for _, c := range check.All() {
					fmt.Fprintf(cmd.OutOrStdout(), "%-18s %s\n", c.Name(), c.Describe())
				}

				return nil
			}

			checks, err := check.Select(only)
			if err != nil {
				return err
			}
			a, err := app.Discover(appDir)
			if err != nil {
				return err
			}

			env := &check.Env{
				App:          a,
				Exec:         check.OSExec{},
				SkipGenerate: skipGenerate,
				Fix:          fix,
				Out:          cmd.ErrOrStderr(),
			}
			results := check.Run(cmd.Context(), env, checks)
			check.Report(cmd.OutOrStdout(), results)
			if check.Failed(results) {
				return exitError{code: 1}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&appDir, "app", ".", "application root (the directory holding go.mod)")
	cmd.Flags().BoolVar(&skipGenerate, "skip-generate", false, "skip the regen check (no emulator, no working-tree rewrite)")
	cmd.Flags().BoolVar(&fix, "fix", false, "apply the mechanical remedies (.prettierignore entries, env template lines)")
	cmd.Flags().StringSliceVar(&only, "only", nil, "run only these checks (see --list)")
	cmd.Flags().BoolVar(&list, "list", false, "list the checks and exit")

	return cmd
}

func asExit(err error, target *exitError) bool {
	return errors.As(err, target)
}
