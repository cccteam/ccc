// sweep.go is the hourly sweep of closed pull requests' environments.

package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"
)

// The labels a pull request's services carry.
const (
	applicationLabel = "application"
	pullRequestLabel = "pull_request"
)

// SweepRequest names the sweep's build: its id and where it runs, as the step passes them.
type SweepRequest struct {
	BuildID, Project, Location string
}

// Sweep destroys the environments of closed pull requests. A pull request has an
// environment while its services stand (the pull_request label on the application's
// services, in every region of _SERVICES); GitHub says which of those pull requests are
// closed, asked with the repository's token minted from the Cloud Build connection the
// way resolve mints it; and each closed one's stack is destroyed as the apply identity,
// from its own state prefix, the way /gcbrun down destroys it. Closing or merging a pull
// request starts no build, so this is how an environment nobody took down goes away. An
// open pull request's environment stays, and so does one GitHub cannot answer for.
func Sweep(ctx context.Context, clients *Clients, w Workspace, req *SweepRequest, out io.Writer) error {
	if err := (&ResolveRequest{BuildID: req.BuildID, Project: req.Project, Location: req.Location}).check(); err != nil {
		return err
	}
	builds, err := clients.Builds(ctx)
	if err != nil {
		return err
	}
	data, err := builds.Get(ctx, req.Project, req.Location, req.BuildID)
	if err != nil {
		return err
	}
	build, err := parseBuild(data)
	if err != nil {
		return err
	}
	subs := build.Substitutions
	numbers, err := pullRequestEnvironments(ctx, clients, subs)
	if err != nil {
		return err
	}
	if len(numbers) == 0 {
		fmt.Fprintf(out, "No pull-request environment in %s.\n", subs[envSub])

		return nil
	}
	fmt.Fprintf(out, "Pull requests with an environment: %s\n", strings.Join(numbers, " "))
	repository := "projects/" + req.Project + "/locations/" + req.Location + "/connections/" + subs["_REPO_CONNECTION_NAME"] + "/repositories/" + subs["_REPO_NAME"]
	token, err := builds.ReadToken(ctx, repository)
	if err != nil {
		return err
	}
	owner, repo, err := splitRepo(subs["_REPO_FULL_NAME"])
	if err != nil {
		return err
	}
	gh := clients.GitHub(token)
	s := newStack(clients, w, subs, out)
	for _, number := range numbers {
		n, _ := strconv.Atoi(number)
		state, err := gh.PullRequestState(ctx, owner, repo, n)
		switch {
		case err != nil:
			fmt.Fprintf(out, "Notice: GitHub did not answer for pull request %s (%v); its environment stays.\n", number, err)

			continue
		case state != "closed":
			fmt.Fprintf(out, "Pull request %s is %s: its environment stays.\n", number, state)

			continue
		}
		fmt.Fprintf(out, "Pull request %s is closed: its environment goes.\n", number)
		if err := deleteBuildJobs(ctx, clients, subs, number, out); err != nil {
			return err
		}
		// Each pull request's stack is initialized afresh against its own prefix.
		if err := os.RemoveAll(filepath.Join(s.dir, ".terraform")); err != nil {
			return errors.Wrap(err, "os.RemoveAll()")
		}
		if err := s.init(ctx, subs, number); err != nil {
			return err
		}
		if err := s.tofu(ctx, "destroy", "-auto-approve", "-input=false", "-no-color", "-var", "environment="+subs[envSub], "-var", "pull_request="+number); err != nil {
			return err
		}
	}

	return nil
}

// deleteBuildJobs deletes the jobs the pull request's builds made for the job process,
// which its stack never owned, in the job process's region (the migrate job's): an
// application without a job process has none.
func deleteBuildJobs(ctx context.Context, clients *Clients, subs map[string]string, number string, out io.Writer) error {
	if subs["_JOBS_JOB"] == "" {
		return nil
	}
	region, _, err := target(migrateJobFact, subs["_MIGRATE_JOB"])
	if err != nil {
		return err
	}
	run, err := clients.Run(ctx)
	if err != nil {
		return err
	}

	return deletePullRequestJobs(ctx, run, subs[projectSub], region, subs[appSub], number, out)
}

// pullRequestEnvironments are the numbers of the pull requests whose services stand, in
// every region the application's services run in, sorted.
func pullRequestEnvironments(ctx context.Context, clients *Clients, subs map[string]string) ([]string, error) {
	run, err := clients.Run(ctx)
	if err != nil {
		return nil, err
	}
	found := map[string]bool{}
	for _, pair := range strings.Split(subs["_SERVICES"], ",") {
		region, _, err := target("_SERVICES", pair)
		if err != nil {
			return nil, err
		}
		list, err := run.Services(ctx, subs["_PROJECT"], region)
		if err != nil {
			return nil, err
		}
		for _, svc := range list {
			labels, _ := svc["labels"].(map[string]any)
			app, _ := labels[applicationLabel].(string)
			number, _ := labels[pullRequestLabel].(string)
			if app == subs[appSub] && number != "" {
				found[number] = true
			}
		}
	}
	numbers := make([]string, 0, len(found))
	for n := range found {
		numbers = append(numbers, n)
	}
	sort.Slice(numbers, func(i, j int) bool {
		a, _ := strconv.Atoi(numbers[i])
		b, _ := strconv.Atoi(numbers[j])

		return a < b
	})

	return numbers, nil
}
