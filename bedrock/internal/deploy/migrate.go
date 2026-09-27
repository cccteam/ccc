package deploy

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/go-playground/errors/v5"
)

// The facts and substitutions the deploy steps read, beyond the earlier steps'.
const (
	projectSub   = "_PROJECT"
	repoNameSub  = "REPO_NAME"
	seedSub      = "_SEED"
	buildIDLabel = "gcb-build-id"
	// seedArg is the migrate command's flag that applies the seed after the schema.
	seedArg = "-seed"
)

// The labels a deploy stamps on the job and the services: who deployed, what and for
// which build; the pull request's number on its own service.
const (
	managedByLabel   = "managed-by"
	managedByValue   = "cloudbuild"
	commitLabel      = "commit-sha"
	sourceRepoLabel  = "source_repo"
	environmentLabel = "environment"
	prNumberLabel    = "pr-number"
)

// pipelineLabels are the pipeline's labels for this build; an empty value removes the
// label (a release build carries no pull request number).
func pipelineLabels(build *Build) map[string]string {
	subs := build.Substitutions

	return map[string]string{
		managedByLabel:   managedByValue,
		commitLabel:      subs[commitSub],
		buildIDLabel:     build.ID,
		sourceRepoLabel:  subs[repoNameSub],
		environmentLabel: subs[envSub],
		prNumberLabel:    subs[prNumberSub],
	}
}

// target reads a region=name pair, as the stack's substitutions name the services and
// the migrate job; fact names the one refused.
func target(fact, pair string) (region, name string, err error) {
	region, name, ok := strings.Cut(pair, "=")
	if !ok || region == "" || name == "" {
		return "", "", errors.Newf("%s %q is not region=name", fact, pair)
	}

	return region, name, nil
}

// Migrate runs the migrate job with this build's image: the job is updated to the
// image and the pipeline's labels (its variables, identity, resources and retry policy
// are the application layer's), then run to completion, with the seed (schema/devseed as
// data migrations after the schema) where _SEED is true: every pull request, its
// database being new, and a release build only in the environments the placement's
// seed list names. A seeded database takes nothing twice. A build that runs no
// migrations (shared-db) skips the job.
func Migrate(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if env[skipDeploy] == trueValue {
		fmt.Fprintln(out, tornDown)

		return nil
	}
	if env[runMigrationsFact] != trueValue {
		fmt.Fprintln(out, "Skipping the migrate job: this build does not run migrations.")

		return nil
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	region, jobName, err := target(migrateJobFact, env[migrateJobFact])
	if err != nil {
		return err
	}
	image := env[imageFact] + "@" + env[digestFact]
	if env[imageFact] == "" || env[digestFact] == "" {
		return errors.Newf("%s names no image digest (IMAGE, IMAGE_DIGEST): the image build writes it", EnvironmentFile)
	}
	run, err := clients.Run(ctx)
	if err != nil {
		return err
	}
	name := "projects/" + build.Substitutions[projectSub] + "/locations/" + region + "/jobs/" + jobName
	fmt.Fprintf(out, "=== Updating job [%s] in [%s] to this image ===\n", jobName, region)
	job, err := run.Get(ctx, name)
	if err != nil {
		return err
	}
	task, _ := field(job, "template.template").(map[string]any)
	container, err := firstContainer(task)
	if err != nil {
		return errors.Wrapf(err, "job %s", jobName)
	}
	container["image"] = image
	setLabels(job, pipelineLabels(build))
	if _, err := run.Patch(ctx, name, job); err != nil {
		return err
	}
	fmt.Fprintf(out, "=== Running job [%s] ===\n", jobName)
	var args []string
	if build.Substitutions[seedSub] == trueValue {
		args = []string{seedArg}
		fmt.Fprintln(out, "Seeding: the migrate job applies schema/devseed as data migrations.")
	}
	execution, err := run.RunJob(ctx, name, args)
	if err != nil {
		return err
	}
	if failed, _ := execution["failedCount"].(float64); failed > 0 {
		return errors.Newf("the migrate job failed: execution %s has %d failed task(s); its logs say why", shortName(text(execution, "name")), int(failed))
	}
	fmt.Fprintf(out, "Migrate job done: execution %s succeeded.\n", shortName(text(execution, "name")))

	return nil
}
