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

// The labels a deploy stamps on the jobs and the services' revisions: who deployed, what
// and for which build, the build's version as a name (version-key, what the job process's
// job of the build is named after, so the sweep can tell which job a revision runs); the
// pull request's number on its own service.
const (
	managedByLabel   = "managed-by"
	managedByValue   = "cloudbuild"
	commitLabel      = "commit-sha"
	sourceRepoLabel  = "source_repo"
	environmentLabel = "environment"
	prNumberLabel    = "pr-number"
	versionLabel     = "version-key"
)

// pipelineLabels are the pipeline's labels for this build of the version; an empty value
// removes the label (a release build carries no pull request number).
func pipelineLabels(build *Build, version string) map[string]string {
	subs := build.Substitutions

	return map[string]string{
		managedByLabel:   managedByValue,
		commitLabel:      subs[commitSub],
		buildIDLabel:     build.ID,
		sourceRepoLabel:  subs[repoNameSub],
		environmentLabel: subs[envSub],
		prNumberLabel:    subs[prNumberSub],
		versionLabel:     versionKey(version),
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

// Migrate runs this build's migrate job, the copy of the template job that deploy jobs made
// on this image (<template>-<version key>), once to completion, with the seed (schema/devseed
// as data migrations after the schema) where _SEED is true: every pull request, its database
// being new, and a release build only in the environments the placement's seed list names.
// A seeded database takes nothing twice. The job is deleted at the end of the step whether
// the execution succeeded or failed: its logs stay in Cloud Logging, and the deployment record
// lists the migrations applied. A build that runs no migrations (shared-db) has no job to
// run; a failed execution stops the build and names itself.
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
	run, err := clients.Run(ctx)
	if err != nil {
		return err
	}
	_, name, err := buildJob(build.Substitutions[projectSub], env, migrateJobFact)
	if err != nil {
		return err
	}
	jobName := shortName(name)
	if _, err := run.Get(ctx, name); err != nil {
		if isNotFound(err) {
			return errors.Newf("this build's migrate job %s does not exist: deploy jobs makes it right after the image build", jobName)
		}

		return err
	}
	fmt.Fprintf(out, "=== Running job [%s] once ===\n", jobName)
	var args []string
	if build.Substitutions[seedSub] == trueValue {
		args = []string{seedArg}
		fmt.Fprintln(out, "Seeding: the migrate job applies schema/devseed as data migrations.")
	}
	execution, runErr := run.RunJob(ctx, name, args)
	outcome := executionOutcome(execution, runErr)
	if outcome == nil {
		fmt.Fprintf(out, "Migrate job done: execution %s succeeded.\n", shortName(text(execution, keyName)))
	}
	if err := run.Delete(ctx, name); err != nil {
		fmt.Fprintf(out, "Job %s was not deleted (%v): deploy sweep-jobs deletes it.\n", jobName, err)
	} else {
		fmt.Fprintf(out, "Job %s deleted: its execution's logs stay in Cloud Logging.\n", jobName)
	}

	return outcome
}

// executionOutcome is nil when the execution succeeded, else why the migrations failed:
// the run the API refused, or the execution's failed tasks, named with the Cloud Logging
// query that finds its logs, which outlive the job.
func executionOutcome(execution map[string]any, runErr error) error {
	if runErr != nil {
		return runErr
	}
	executionName := shortName(text(execution, keyName))
	if failed, _ := execution["failedCount"].(float64); failed > 0 {
		return errors.Newf("the migrate job failed: execution %s has %d failed task(s); its logs say why (Cloud Logging: resource.type=\"cloud_run_job\" AND labels.\"run.googleapis.com/execution_name\"=\"%s\")", executionName, int(failed), executionName)
	}

	return nil
}

// builtImage is this build's image by digest, as the image build left it in the
// environment file.
func builtImage(env map[string]string) (string, error) {
	if env[imageFact] == "" || env[digestFact] == "" {
		return "", errors.Newf("%s names no image digest (IMAGE, IMAGE_DIGEST): the image build writes it", EnvironmentFile)
	}

	return env[imageFact] + "@" + env[digestFact], nil
}
