// render.go is the render command: write the application stack into a directory.

package cli

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/check"
	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/render"
)

// renderCommand is the name of the render commands, the application's and the organization's.
const renderCommand = "render"

// dockerfileName is the image build at the application root, a seeded file.
const dockerfileName = "Dockerfile"

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
says in a comment which declaration it comes from. So are the files at the application
root: the pipeline, cloudbuild.yaml and cloudbuild-sweep.yaml, which Cloud Build reads there,
the release and operations workflows, and the generate-time step, cmd/generate/bedrock.go,
which runs the migration renumber before the application's generators. A file bedrock owned
once and renders no more (the infrastructure workflow, .github/workflows/infrastructure.yml,
whose check runs in Cloud Build from the stack's check trigger now) is removed when it is
found, and render says so. The render ends by reading the schema migrations by the sequence
rule (bedrock migration check), so a gap a generator left is refused before the first push
and never reaches the default branch. The pipeline is where
the deploy sequence is bedrock's, and an application customizes it through hooks
(infrastructure/hooks/<stage>.sh) and the substitutions declared in its placement values,
never by editing the file. terraform.tfvars is seeded: written when absent, then a person's,
holding the placement values filled in per environment; so are the stack's .gitignore, which
keeps the per-environment backend caches and saved plans out of the repository, the
Dockerfile at the application root, the image build in its first shape, with its
.dockerignore, and release-please's two files there, release-please-config.json (the
first release 0.1.0, a feature on the minor below 1.0) and .release-please-manifest.json
(at 0.0.0, which release-please then moves), which the stack's release trigger reads. In the
sites layout, a kept Dockerfile whose image still sets an ENTRYPOINT (seeded before each
site's service passed its binary as the container's arguments, which replace only a default
command) is noted after the render with the lines to end the image with; bedrock check
refuses the image until they are written. The
placement is read from --placement, or from placement.json in the stack directory.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			appDir, outDir, err := d.stack(appFlag, outFlag)
			if err != nil {
				return err
			}
			m, err := d.model(appDir, placement, outDir)
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
			if err := reportWritten(cmd.OutOrStdout(), m, written, outDir, appDir); err != nil {
				return err
			}
			// The migrations are read by the sequence rule last, so a gap a generator left
			// is refused here, before the first push, and never reaches the default branch.
			findings, _, err := check.Migrations(appDir, m)
			if err != nil {
				return err
			}

			return refuseMigrations(cmd.OutOrStdout(), findings, check.MigrationsFix(cmd.Context(), appDir), "fix them before the first push; bedrock check refuses them in every build, and the CI migrations job on every pull request")
		},
	}

	cmd.Flags().StringVar(&appFlag, "app", "", "application root, the directory holding go.mod (default: the repository root when the stack is in its infrastructure directory, else the working directory)")
	cmd.Flags().StringVar(&outFlag, "out", "", "the stack directory to write (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placement, "placement", "", "placement file (default: placement.json in the stack directory)")
	_ = cmd.MarkFlagDirname("app")
	_ = cmd.MarkFlagDirname("out")

	return cmd
}

// reportWritten says what the render wrote: the owned files of the stack and the
// pipeline, the retired files removed, each seeded file created, and the seeded files
// kept, with the note on a kept Dockerfile the stack's services cannot start.
func reportWritten(w io.Writer, m *derive.Model, written *render.Written, outDir, appDir string) error {
	fmt.Fprintf(w, "Rendered the %s stack into %s: %d owned file(s) written.\n", m.App, outDir, written.Owned)
	if len(written.Pipeline) > 0 {
		fmt.Fprintf(w, "Rendered the pipeline into %s: %s.\n", appDir, strings.Join(written.Pipeline, ", "))
	}
	for _, name := range written.Removed {
		fmt.Fprintf(w, "Removed %s from %s: an earlier bedrock rendered it, and the stack's release trigger cuts the releases now (cloud-build.tf).\n", name, appDir)
	}
	for _, name := range written.Seeded {
		switch name {
		case dockerfileName:
			fmt.Fprintf(w, "Seeded %s into %s; the image build is yours from here.\n", name, appDir)
		case ".dockerignore":
			fmt.Fprintf(w, "Seeded %s into %s; what a local image build leaves out of its context.\n", name, appDir)
		case ".gitignore":
			fmt.Fprintf(w, "Seeded %s; the backend caches and saved plans stay out of the repository.\n", name)
		case render.ReleasePleaseConfig:
			fmt.Fprintf(w, "Seeded %s into %s; release-please's configuration, yours from here.\n", name, appDir)
		case render.ReleasePleaseManifest:
			fmt.Fprintf(w, "Seeded %s into %s at 0.0.0, so the first release pull request proposes 0.1.0; release-please moves it from then on.\n", name, appDir)
		default:
			fmt.Fprintf(w, "Seeded %s; fill in the placement per environment there.\n", name)
		}
	}
	if len(written.Kept) == 0 {
		return nil
	}
	fmt.Fprintf(w, "Kept %s as it was.\n", strings.Join(written.Kept, ", "))

	return noteImageStart(w, appDir, m, written.Kept)
}

// noteImageStart says, after a render that kept the Dockerfile, when the kept image
// starts in a way the stack's services cannot start it: a sites-layout image seeded
// before each site's service passed its own binary as the container's arguments keeps
// an ENTRYPOINT, under which every site's service runs the entrypoint with its binary
// as an argument and serves the first site. The note is bedrock check's refusal, with
// the lines to write; the render goes through, since the author edits the file next,
// and the check refuses the image until then. A flat image, a seeded Dockerfile and one
// that follows the contract draw nothing.
func noteImageStart(w io.Writer, appDir string, m *derive.Model, kept []string) error {
	if !slices.Contains(kept, dockerfileName) {
		return nil
	}
	start, err := check.ScanStart(appDir, m)
	if err != nil {
		return err
	}
	if start == nil || start.Problem == "" {
		return nil
	}
	lines := start.Refusal("The kept " + filepath.Join(appDir, dockerfileName))
	fmt.Fprintln(w, lines[0])
	for _, line := range lines[1:] {
		fmt.Fprintf(w, "    %s\n", line)
	}
	fmt.Fprintln(w, "bedrock check refuses the image until then, and the stack's check trigger runs it on every push to a pull request.")

	return nil
}
