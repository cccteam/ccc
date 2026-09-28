package deploy

import (
	"context"
	"fmt"
	"io"

	"github.com/go-playground/errors/v5"
)

// Jobs updates the job process's Cloud Run job to this build's image and the pipeline's
// labels and does not run it: the application runs its job process (the site through the
// Cloud Run API, or a schedule), and the pipeline only keeps the job on the image every
// other process of the build runs. After the migrations, so a run the application starts
// from here on sees the migrated schema. A torn-down pull-request environment has nothing
// to update.
func Jobs(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if env[skipDeploy] == trueValue {
		fmt.Fprintln(out, tornDown)

		return nil
	}
	if env[jobsJobFact] == "" {
		return errors.Newf("%s names no job: the stack's substitutions carry _JOBS_JOB when the application has a job process (cmd/jobs), which this step is for; render and apply the stack", jobsJobFact)
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	image, err := builtImage(env)
	if err != nil {
		return err
	}
	run, err := clients.Run(ctx)
	if err != nil {
		return err
	}
	name, err := updateJob(ctx, run, build, jobsJobFact, env[jobsJobFact], image, out)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Job %s updated: its next run is on this build's image; the pipeline does not run it.\n", shortName(name))

	return nil
}
