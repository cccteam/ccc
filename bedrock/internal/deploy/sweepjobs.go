// sweepjobs.go retires the job process's jobs that no revision runs any more.

package deploy

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"
)

// keptRevisions is how many of the service's newest revisions keep their job: the depth a
// traffic rollback can reach and still start the job of the revision it lands on.
const keptRevisions = 5

// SweepJobs deletes the job process's jobs that no revision runs any more: the copies
// deploy jobs made, named after the template job. A job stays while a revision serving
// traffic or one of the five newest revisions of the service in the job's region carries
// its version key, and while an execution of it is still running; the template stays
// always. After traffic moved, so the revision that just stopped serving keeps its job
// for a rollback and the ones before it go. A torn-down pull-request environment has
// nothing to sweep.
func SweepJobs(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
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
	project := build.Substitutions[projectSub]
	template, _, err := buildJob(project, env)
	if err != nil {
		return err
	}
	region, _, err := target(jobsJobFact, env[jobsJobFact])
	if err != nil {
		return err
	}
	service, err := serviceIn(project, region, env[services])
	if err != nil {
		return err
	}
	run, err := clients.Run(ctx)
	if err != nil {
		return err
	}
	kept, err := keptKeys(ctx, run, service)
	if err != nil {
		return err
	}
	jobs, err := run.Jobs(ctx, project, region)
	if err != nil {
		return err
	}
	deleted, err := sweepBuildJobs(ctx, run, jobs, template+"-", kept, out)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Swept the jobs of %s: %d deleted.\n", shortName(template), deleted)

	return nil
}

// sweepBuildJobs deletes the jobs under the prefix whose version key is not kept and
// which have no execution running, saying why each stays or goes, and answers how many
// went.
func sweepBuildJobs(ctx context.Context, run Run, jobs []map[string]any, prefix string, kept map[string]string, out io.Writer) (int, error) {
	deleted := 0
	for _, job := range sortedByName(jobs) {
		name := text(job, keyName)
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		key := strings.TrimPrefix(name, prefix)
		if by, ok := kept[key]; ok {
			fmt.Fprintf(out, "Job %s stays: revision %s runs it.\n", shortName(name), by)

			continue
		}
		running, err := runningExecution(ctx, run, name)
		if err != nil {
			return deleted, err
		}
		if running != "" {
			fmt.Fprintf(out, "Job %s stays: execution %s is still running.\n", shortName(name), running)

			continue
		}
		if err := run.Delete(ctx, name); err != nil {
			return deleted, err
		}
		deleted++
		fmt.Fprintf(out, "Job %s deleted: no revision runs it.\n", shortName(name))
	}

	return deleted, nil
}

// serviceIn is the resource name of the service in the region, from the services the
// stack's substitutions name (region=name pairs).
func serviceIn(project, region, pairs string) (string, error) {
	for _, pair := range strings.Split(pairs, ",") {
		r, name, err := target(services, pair)
		if err != nil {
			return "", err
		}
		if r == region {
			return serviceName(project, region, name), nil
		}
	}

	return "", errors.Newf("%s names no service in %s, the job process's region", services, region)
}

// keptKeys are the version keys of the revisions whose jobs stay, each with the revision
// that keeps it: the revisions serving traffic and the newest keptRevisions, by their
// version-key label. A revision without the label (deployed before the label existed)
// keeps nothing, since no job was made for it.
func keptKeys(ctx context.Context, run Run, service string) (map[string]string, error) {
	doc, err := run.Get(ctx, service)
	if err != nil {
		return nil, err
	}
	revisions, err := run.Revisions(ctx, service)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(revisions, func(i, j int) bool {
		return text(revisions[i], "createTime") > text(revisions[j], "createTime")
	})
	serving := map[string]bool{}
	statuses, _ := doc["trafficStatuses"].([]any)
	for _, entry := range statuses {
		status, _ := entry.(map[string]any)
		if percent, _ := status[keyPercent].(float64); percent > 0 {
			serving[text(status, keyRevision)] = true
		}
	}
	kept := map[string]string{}
	for i, revision := range revisions {
		name := shortName(text(revision, keyName))
		if i >= keptRevisions && !serving[name] {
			continue
		}
		if key := text(revision, "labels."+versionLabel); key != "" {
			if _, ok := kept[key]; !ok {
				kept[key] = name
			}
		}
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
// (the ones carrying its number and a version key, never the template the stack owns) in
// the region, when its environment goes.
func deletePullRequestJobs(ctx context.Context, run Run, project, region, app, number string, out io.Writer) error {
	jobs, err := run.Jobs(ctx, project, region)
	if err != nil {
		return err
	}
	for _, job := range sortedByName(jobs) {
		if text(job, "labels."+applicationLabel) != app || text(job, "labels."+pullRequestLabel) != number || text(job, "labels."+versionLabel) == "" {
			continue
		}
		name := text(job, keyName)
		if err := run.Delete(ctx, name); err != nil {
			return err
		}
		fmt.Fprintf(out, "Job %s deleted with pull request %s's environment.\n", shortName(name), number)
	}

	return nil
}
