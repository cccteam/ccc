// check.go is the check command: the drift between the code and a committed stack.

package cli

import (
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/check"
)

func newCheck() *cobra.Command {
	var (
		appDir    string
		dir       string
		placement string
	)

	cmd := &cobra.Command{
		Use:   "check --app <dir> --dir <dir>",
		Short: "Compare a committed stack with what the code declares",
		Long: `check renders the application stack afresh and compares every owned file with the one in
the directory. It exits 1 when any differs or is missing, listing each with the first line
that differs: the drift between the code and the committed infrastructure. Seeded files
(terraform.tfvars) are a person's and are not compared. The placement is read from
--placement, or from placement.json in the directory.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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

	cmd.Flags().StringVar(&appDir, "app", ".", "application root (the directory holding go.mod)")
	cmd.Flags().StringVar(&dir, "dir", "", "the committed stack directory (required)")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the stack directory)")
	_ = cmd.MarkFlagRequired("dir")

	return cmd
}
