// render.go is the render command: write the application stack into a directory.

package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/render"
)

// renderCommand is the name of the render commands, the application's and the organization's.
const renderCommand = "render"

func newRender(d deps) *cobra.Command {
	var (
		appFlag   string
		outFlag   string
		placement string
	)

	cmd := &cobra.Command{
		Use:   renderCommand + " [--app <dir>] [--out <dir>]",
		Short: "Write the application stack from the code",
		Long: `render reads the application (its config struct tags, its main packages, its auths, its
generated router) and the placement, and writes the stack into the stack directory.

Run from anywhere inside the repository, it finds both directories: the stack is the
application repository's infrastructure directory, or the one application layer under
3-app of an infrastructure root (the repository root, or its infrastructure directory);
--out overrides. The application is read from the repository root when the stack is in its
infrastructure directory, else from the working directory; --app overrides.

The stack's .tf files and its README are owned: render rewrites them every time, and each
says in a comment which declaration it comes from. So are the pipeline files at the
application root, cloudbuild.yaml and cloudbuild-sweep.yaml, which Cloud Build reads there:
the deploy sequence is bedrock's, and an application customizes it through hooks
(infrastructure/hooks/<stage>.sh) and the substitutions declared in its placement values,
never by editing the file. terraform.tfvars is seeded: written when absent, then a person's,
holding the placement values filled in per environment; so are the stack's .gitignore, which
keeps the per-environment backend caches and saved plans out of the repository, and the
Dockerfile at the application root, the image build in its first shape. The placement is
read from --placement, or from placement.json in the stack directory.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			appDir, outDir, err := d.stack(appFlag, outFlag)
			if err != nil {
				return err
			}
			m, err := model(appDir, placement, outDir)
			if err != nil {
				return err
			}
			files, err := render.Render(m)
			if err != nil {
				return err
			}
			written, err := render.Write(files, outDir, appDir)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Rendered the %s stack into %s: %d owned file(s) written.\n", m.App, outDir, written.Owned)
			if len(written.Pipeline) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Rendered the pipeline into %s: %s.\n", appDir, strings.Join(written.Pipeline, ", "))
			}
			for _, name := range written.Seeded {
				switch name {
				case "Dockerfile":
					fmt.Fprintf(cmd.OutOrStdout(), "Seeded %s into %s; the image build is yours from here.\n", name, appDir)
				case ".gitignore":
					fmt.Fprintf(cmd.OutOrStdout(), "Seeded %s; the backend caches and saved plans stay out of the repository.\n", name)
				default:
					fmt.Fprintf(cmd.OutOrStdout(), "Seeded %s; fill in the placement per environment there.\n", name)
				}
			}
			if len(written.Kept) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Kept %s as it was.\n", strings.Join(written.Kept, ", "))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&appFlag, "app", "", "application root, the directory holding go.mod (default: the repository root when the stack is in its infrastructure directory, else the working directory)")
	cmd.Flags().StringVar(&outFlag, "out", "", "the stack directory to write (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the stack directory)")
	_ = cmd.MarkFlagDirname("app")
	_ = cmd.MarkFlagDirname("out")

	return cmd
}
