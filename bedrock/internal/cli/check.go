// check.go is the check command: the drift between the code and a committed stack.

package cli

import (
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/check"
)

// checkCommand is the name of the check commands: the application's, the organization's and
// the domain's.
const checkCommand = "check"

func newCheck(d deps) *cobra.Command {
	var (
		appFlag   string
		dirFlag   string
		placement string
	)

	cmd := &cobra.Command{
		Use:   checkCommand + " [--app <dir>] [--dir <dir>]",
		Short: "Compare a committed stack with what the code declares",
		Long: `check renders the application stack afresh and compares every owned file with the one in
the stack directory, and the files it owns at the application root (cloudbuild.yaml,
cloudbuild-sweep.yaml, and the generate-time step cmd/generate/bedrock.go) with the ones there. It exits 1 when any differs or is missing, listing each with
the first line that differs: the drift between the code and the committed infrastructure.
Seeded files (terraform.tfvars, .gitignore, the Dockerfile, release-please's files) are a
person's and are not compared.

It also refuses a schema migrations directory, or the seed directory beside it
(schema/devseed), whose files do not form the sequence the migrate command applies:
six-digit indexes, one up file per index, at most one down, contiguous from the lowest
present (a history consolidated above 000001 passes; a skipped number does not). The
pipeline repeats that rule on every build and, in a pull-request build, also refuses a
migration modified, renamed or removed against the default branch, and an index the
default branch has taken since the branch was cut.

It refuses an application root without release-please's configuration or its manifest
(release-please-config.json, .release-please-manifest.json): the release workflow reads both,
and without them no release is cut and nothing reaches an environment. bedrock render seeds
both when absent; edited, they are the application's.

The build secrets: a secret the Dockerfile mounts as required (--mount=type=secret,
id=NAME,required=true) must be declared in every environment's build_secrets in
terraform.tfvars, or a release that passed the earlier environments fails in the image
build of the one that lacks it; the check refuses a required mount naming the
environments without it. An optional mount passes with nothing said.

The maintenance windows: it warns, without failing, while production has no maintenance
setting in placement.json (a breaking release to production is refused at the start of its
run until one is written; "anytime" is a setting), when the router package holds no release
file (zz_gen_release.json, which the resource generator writes: without it no release is
breaking and the window never holds a run), when that file does not read, and when a dated
slot has passed. A malformed setting is refused, as render refuses it.

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
			m, err := d.model(appDir, placement, dir)
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
