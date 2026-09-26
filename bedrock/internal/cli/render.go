// render.go is the render command: write the application stack into a directory.

package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/render"
)

func newRender() *cobra.Command {
	var (
		appDir    string
		outDir    string
		placement string
	)

	cmd := &cobra.Command{
		Use:   "render --app <dir> --out <dir>",
		Short: "Write the application stack from the code",
		Long: `render reads the application (its config struct tags, its main packages, its auths, its
generated router) and the placement, and writes the stack into the output directory.

The stack's .tf files and its README are owned: render rewrites them every time, and each
says in a comment which declaration it comes from. terraform.tfvars is seeded: written when
absent, then a person's, holding the placement values filled in per environment. The
placement is read from --placement, or from placement.json in the output directory.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m, err := model(appDir, placement, outDir)
			if err != nil {
				return err
			}
			files, err := render.Render(m)
			if err != nil {
				return err
			}
			written, err := render.Write(files, outDir)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rendered the %s stack into %s: %d owned file(s) written.\n", m.App, outDir, written.Owned)
			if len(written.Seeded) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Seeded %s; fill in the placement per environment there.\n", strings.Join(written.Seeded, ", "))
			}
			if len(written.Kept) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Kept %s as it was.\n", strings.Join(written.Kept, ", "))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&appDir, "app", ".", "application root (the directory holding go.mod)")
	cmd.Flags().StringVar(&outDir, "out", "", "the stack directory to write (required)")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the stack directory)")
	_ = cmd.MarkFlagRequired("out")

	return cmd
}
