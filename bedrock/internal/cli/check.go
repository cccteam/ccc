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
the stack directory, and the files it owns at the application root (cloudbuild.yaml,
cloudbuild-sweep.yaml, and the generate-time step cmd/generate/bedrock.go) with the ones there. It exits 1 when any differs or is missing, listing each with
the first line that differs: the drift between the code and the committed infrastructure.
Seeded files (terraform.tfvars, .gitignore, the Dockerfile) are a person's and are not compared.

It also refuses a schema migrations directory, or the seed directory beside it
(schema/devseed), whose files do not form the sequence the migrate command applies:
six-digit indexes, one up file per index, at most one down, contiguous from the lowest
present (a history consolidated above 000001 passes; a skipped number does not). The
pipeline repeats that rule on every build and, in a pull-request build, also refuses a
migration modified, renamed or removed against the default branch, and an index the
default branch has taken since the branch was cut.

The build secrets: a secret the Dockerfile mounts as required (--mount=type=secret,
id=NAME,required=true) must be declared in every environment's build_secrets in
terraform.tfvars, or a release that passed the earlier environments fails in the image
build of the one that lacks it; the check refuses a required mount naming the
environments without it. An optional mount passes with nothing said.

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
			report, err := check.Run(m, dir, appDir)
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
