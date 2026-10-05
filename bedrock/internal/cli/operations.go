// operations.go is what the commands that start the operations workflow share (restore,
// rerun, rollback, migration version, rerun and force): the repository and its placement, the
// checks on the environment and the release, and the dispatch as the signed-in person
// with the inputs the workflow declares.

package cli

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/render"
)

// The operations workflow's inputs (its environment input is named like the label) and
// the actions its action input takes: the restore, the run (bedrock rerun: the release's
// tag build again, production included; the migration job's rerun option was named
// first, so the release's action is run), and the three operations on an environment's
// migrations.
const (
	actionInput    = "action"
	releaseInput   = "release"
	tableInput     = "table"
	versionInput   = "version"
	reasonInput    = "reason"
	backupInput    = "backup"
	actionRestore  = "restore"
	actionRun      = "run"
	actionRollback = "rollback"
	actionVersion  = "version"
	actionRerun    = "rerun"
	actionForce    = "force"
)

// releaseTagRE is a release tag, v<major>.<minor>.<patch>.
var releaseTagRE = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// operationTarget finds the repository and reads its placement, and checks that the
// environment is one of the placement's. Production is each command's own refusal, with
// its own reason, before anything is dispatched.
func (d deps) operationTarget(dirFlag, placementFlag, env string) (*repositoryContext, error) {
	rc, err := d.repository(dirFlag, placementFlag)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(rc.placement.Environments, env) {
		return nil, errors.Newf("%q is not one of the environments (%s)", env, strings.Join(rc.placement.Environments, ", "))
	}

	return rc, nil
}

// dispatchOperation checks the release (a tag of the right shape that exists; an empty
// one is passed as it is, for the workflow to resolve where the action allows it) and that
// the environment is wired for operations (the placement records its project), then starts
// the operations workflow with the inputs, the environment and the release among them, as
// the person signed in to gh (or GITHUB_TOKEN), and answers that person's login.
func dispatchOperation(ctx context.Context, d deps, rc *repositoryContext, env, tag string, inputs map[string]string) (string, error) {
	p := rc.placement
	if tag != "" && !releaseTagRE.MatchString(tag) {
		return "", errors.Newf("%q is not a release tag (v<major>.<minor>.<patch>, such as v1.4.0)", tag)
	}
	if _, ok := p.Project(env); !ok {
		return "", errors.Newf("%s is not wired for operations: placement.json records no project for it (projects.%s, the id and the number, which bedrock org register prints); record it, render, and merge the rendered workflow first", env, env)
	}
	client, err := d.github(ctx)
	if err != nil {
		return "", err
	}
	login, err := client.User(ctx)
	if err != nil {
		return "", errors.Wrap(err, "reading who the token belongs to")
	}
	if tag != "" {
		if _, err := client.TagCommit(ctx, rc.owner, rc.repo, tag); err != nil {
			if github.NotFound(err) {
				return "", errors.Newf("no release %s in %s/%s: an operation names a release that exists", tag, rc.owner, rc.repo)
			}

			return "", errors.Wrapf(err, "resolving %s", tag)
		}
	}
	inputs[environmentLabel], inputs[releaseInput] = env, tag
	if err := client.DispatchWorkflow(ctx, rc.owner, rc.repo, render.OperationsWorkflow, p.DefaultBranch, inputs); err != nil {
		return "", errors.Wrapf(err, "starting the operations workflow of %s/%s on %s", rc.owner, rc.repo, p.DefaultBranch)
	}

	return login, nil
}

// workflowURL is where the operations workflow's runs are watched.
func workflowURL(rc *repositoryContext) string {
	return "https://github.com/" + rc.owner + "/" + rc.repo + "/actions/workflows/" + render.OperationsWorkflow
}
