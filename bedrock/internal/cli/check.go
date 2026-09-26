// check.go is the check command: the drift between the code and a committed stack.

package cli

import (
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/check"
)

func newCheck(d deps) *cobra.Command {
	var (
		appFlag   string
		dirFlag   string
		placement string
	)

	cmd := &cobra.Command{
		Use:   "check [--app <dir>] [--dir <dir>]",
		Short: "Compare a committed stack with what the code declares",
		Long: `check renders the application stack afresh and compares every owned file with the one in
the stack directory. It exits 1 when any differs or is missing, listing each with the first
line that differs: the drift between the code and the committed infrastructure. Seeded
files (terraform.tfvars) are a person's and are not compared.

Run from anywhere inside the repository, it finds both directories: the stack is the
application repository's infrastructure directory, or the one application layer under
3-app of an infrastructure root (the repository root, or its infrastructure directory);
--dir overrides. The application is read from the repository root when the stack is in its
infrastructure directory, else from the working directory; --app overrides. The placement
is read from --placement, or from placement.json in the stack directory.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			appDir, dir, err := d.stack(appFlag, dirFlag)
			if err != nil {
				return err
			}
			m, err := model(appDir, placement, dir)
			if err != nil {
				return err
			}
			report, err := check.Run(m, dir)
			if err != nil {
				return err
			}
			report.Write(cmd.OutOrStdout())
			if !report.Clean() {
				return exitError{code: 1}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&appFlag, "app", "", "application root, the directory holding go.mod (default: the repository root when the stack is in its infrastructure directory, else the working directory)")
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the committed stack directory (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the stack directory)")
	_ = cmd.MarkFlagDirname("app")
	_ = cmd.MarkFlagDirname("dir")

	return cmd
}
