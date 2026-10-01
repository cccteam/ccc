// sweepjobs.go deletes the builds' jobs that nothing runs any more: the job process's jobs
// whose version no revision carries, and the migrate jobs a run that did not finish left.

package deploy

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
)

// youngJob is how long a build's job stays whatever its state: the longest a run takes,
// so the job of a build still running, or one that just failed and is being looked at,
// is never swept from under it.
const youngJob = 3 * time.Hour

// SweepJobs deletes the jobs deploy jobs made that nothing runs any more. A job of the job
// process stays while a revision of the service, in any region, carries its version key: a
// revision that exists can take a traffic rollback, and then starts the job of its own
// build. A build's migrate job is deleted by deploy migrate at the end of its step; one a
// run that did not finish left behind goes here. Any job stays while an execution of it is
// still running, and while it is younger than three hours, since its build may still be
// running; the templates always stay. Nothing here retires a revision: that is Cloud Run's
// own ceiling of revisions per service. A torn-down pull-request environment has nothing to
// sweep.
func SweepJobs(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if env[skipDeploy] == trueValue {
		fmt.Fprintln(out, tornDown)

		return nil
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	project := build.Substitutions[projectSub]
	region, migrateTemplate, err := target(migrateJobFact, env[migrateJobFact])
	if err != nil {
		return err
	}
	run, err := clients.Run(ctx)
	if err != nil {
		return err
	}
	jobs, err := run.Jobs(ctx, project, region)
	if err != nil {
		return err
	}
	now := time.Now()
	deleted, err := sweepLeftovers(ctx, run, jobs, jobPrefix(project, region, migrateTemplate), now, out)
	if err != nil {
		return err
	}
	if env[jobsJobFact] != "" {
		jobsRegion, jobsTemplate, err := target(jobsJobFact, env[jobsJobFact])
		if err != nil {
			return err
		}
		if jobsRegion != region {
			if jobs, err = run.Jobs(ctx, project, jobsRegion); err != nil {
				return err
			}
		}
		kept, err := keptKeys(ctx, run, project, env[services])
		if err != nil {
			return err
		}
		n, err := sweepBuildJobs(ctx, run, jobs, jobPrefix(project, jobsRegion, jobsTemplate), kept, now, out)
		if err != nil {
			return err
		}
		deleted += n
	}
	fmt.Fprintf(out, "Swept the builds' jobs: %d deleted.\n", deleted)

	return nil
}

// sweepBuildJobs deletes the job process's jobs under the prefix whose version key no
// revision carries, unless an execution is running or the job is young, saying why each
// stays or goes, and answers how many went.
func sweepBuildJobs(ctx context.Context, run Run, jobs []map[string]any, prefix string, kept map[string]string, now time.Time, out io.Writer) (int, error) {
	deleted := 0
	for _, job := range sortedByName(jobs) {
		name := text(job, keyName)
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if by, ok := kept[strings.TrimPrefix(name, prefix)]; ok {
			fmt.Fprintf(out, "Job %s stays: revision %s carries its version.\n", shortName(name), by)

			continue
		}
		stays, err := stays(ctx, run, job, now, out)
		if err != nil || stays {
			if err != nil {
				return deleted, err
			}

			continue
		}
		if err := run.Delete(ctx, name); err != nil {
			return deleted, err
		}
		deleted++
		fmt.Fprintf(out, "Job %s deleted: no revision carries its version.\n", shortName(name))
	}

	return deleted, nil
}

// sweepLeftovers deletes the migrate jobs under the prefix, the ones a run that did not
// finish left behind (a run that finished deleted its own), unless an execution is running
// or the job is young, and answers how many went.
func sweepLeftovers(ctx context.Context, run Run, jobs []map[string]any, prefix string, now time.Time, out io.Writer) (int, error) {
	deleted := 0
	for _, job := range sortedByName(jobs) {
		name := text(job, keyName)
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		stays, err := stays(ctx, run, job, now, out)
		if err != nil || stays {
			if err != nil {
				return deleted, err
			}

			continue
		}
		if err := run.Delete(ctx, name); err != nil {
			return deleted, err
		}
		deleted++
		fmt.Fprintf(out, "Job %s deleted: a migrate job of a run that did not finish.\n", shortName(name))
	}

	return deleted, nil
}

// stays says whether a job that nothing names any more stays anyway: it is younger than
// youngJob, or an execution of it is still running; it says why.
func stays(ctx context.Context, run Run, job map[string]any, now time.Time, out io.Writer) (bool, error) {
	name := text(job, keyName)
	if created, err := time.Parse(time.RFC3339Nano, text(job, "createTime")); err == nil && now.Sub(created) < youngJob {
		fmt.Fprintf(out, "Job %s stays: made %s ago, its build may still be running.\n", shortName(name), now.Sub(created).Round(time.Minute))

		return true, nil
	}
	running, err := runningExecution(ctx, run, name)
	if err != nil {
		return false, err
	}
	if running != "" {
		fmt.Fprintf(out, "Job %s stays: execution %s is still running.\n", shortName(name), running)

		return true, nil
	}

	return false, nil
}

// keptKeys are the version keys the revisions that exist carry, in every region the
// stack's substitutions name a service in (region=name pairs), each with one revision
// that carries it. A revision without the label (deployed before the label existed) keeps
// nothing, since no job was made for it.
func keptKeys(ctx context.Context, run Run, project, pairs string) (map[string]string, error) {
	kept := map[string]string{}
	for _, pair := range strings.Split(pairs, ",") {
		region, name, err := target(services, pair)
		if err != nil {
			return nil, err
		}
		revisions, err := run.Revisions(ctx, serviceName(project, region, name))
		if err != nil {
			return nil, err
		}
		for _, revision := range revisions {
			key := text(revision, "labels."+versionLabel)
			if key == "" {
				continue
			}
			if _, ok := kept[key]; !ok {
				kept[key] = shortName(text(revision, keyName)) + " (" + region + ")"
			}
		}
	}
	if len(kept) == 0 && pairs == "" {
		return nil, errors.Newf("%s names no service: the stack's substitutions carry _SERVICES", services)
	}

	return kept, nil
}

// runningExecution is the name of an execution of the job that has not completed, or
// empty.
func runningExecution(ctx context.Context, run Run, job string) (string, error) {
	executions, err := run.Executions(ctx, job)
	if err != nil {
		return "", err
	}
	for _, execution := range executions {
		if text(execution, "completionTime") == "" {
			return shortName(text(execution, keyName)), nil
		}
	}

	return "", nil
}

// sortedByName is the documents in the order of their names.
func sortedByName(docs []map[string]any) []map[string]any {
	sorted := append([]map[string]any{}, docs...)
	sort.Slice(sorted, func(i, j int) bool {
		return text(sorted[i], keyName) < text(sorted[j], keyName)
	})

	return sorted
}

// deletePullRequestJobs deletes the jobs deploy jobs made for the pull request's builds
// (the ones named under the templates' prefixes and carrying the application, its number
// and a version key; never a template, which the stack owns) in the region, when its
// environment goes.
func deletePullRequestJobs(ctx context.Context, run Run, project, region string, prefixes []string, app, number string, out io.Writer) error {
	jobs, err := run.Jobs(ctx, project, region)
	if err != nil {
		return err
	}
	for _, job := range sortedByName(jobs) {
		name := text(job, keyName)
		if !underAny(name, prefixes) || text(job, "labels."+applicationLabel) != app || text(job, "labels."+pullRequestLabel) != number || text(job, "labels."+versionLabel) == "" {
			continue
		}
		if err := run.Delete(ctx, name); err != nil {
			return err
		}
		fmt.Fprintf(out, "Job %s deleted with pull request %s's environment.\n", shortName(name), number)
	}

	return nil
}

// underAny says whether the name starts with one of the prefixes.
func underAny(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return false
}
