// trigger.go is what the trigger-held jobs share. The application's stack holds the steps
// of two Cloud Build triggers in its own text (cloud-build.tf): the check-and-plan trigger,
// on every push to a pull request, and the pull-request stack trigger, on the /gcbrun
// comment beside the preview. Both run as the apply identity, with no resolve step before
// their commands, so each command reads its build through the Cloud Build API, mints the
// repository's token the way resolve does, and says when the steps it runs under are an
// older bedrock's than itself: the trigger's steps change with the stack's apply, one
// release behind the repository, while the bedrock they run is read from the checkout's
// placement at run time.

package deploy

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/impulse/app"
)

// BuildRequest names the build a trigger-held command runs in: its id and where it runs,
// as the step passes them (BUILD_ID, PROJECT_ID, LOCATION).
type BuildRequest struct {
	BuildID, Project, Location string
}

// check refuses a request that does not name the build.
func (r *BuildRequest) check() error {
	return requireBuild(r.BuildID, r.Project, r.Location)
}

// requireBuild refuses a build named without its id, project or location.
func requireBuild(id, project, location string) error {
	for _, v := range []struct{ name, value string }{{"BUILD_ID", id}, {"PROJECT_ID", project}, {"LOCATION", location}} {
		if v.value == "" {
			return errors.Newf("%s is not set: the step passes the build's id, project and location to the command", v.name)
		}
	}

	return nil
}

// readBuild reads the build the command runs in through the Cloud Build API, and answers
// the client it read it with, for the token the command mints next.
func readBuild(ctx context.Context, clients *Clients, req *BuildRequest) (*Build, Builds, error) {
	if err := req.check(); err != nil {
		return nil, nil, err
	}
	builds, err := clients.Builds(ctx)
	if err != nil {
		return nil, nil, err
	}
	data, err := builds.Get(ctx, req.Project, req.Location, req.BuildID)
	if err != nil {
		return nil, nil, err
	}
	build, err := parseBuild(data)
	if err != nil {
		return nil, nil, err
	}

	return build, builds, nil
}

// pullRequestBuild refuses a build that is not a pull request's in the first environment:
// the trigger-held jobs of a pull request run there alone.
func pullRequestBuild(build *Build) error {
	subs := build.Substitutions
	if subs[prNumberSub] == "" {
		return errors.Newf("%s is not set: this command runs in the stack's pull-request triggers, which the pull-request event sets it on", prNumberSub)
	}
	if subs[envSub] != tstEnvironment {
		return errors.Newf("a pull request's stack is applied in %s alone (this build's %s is %q)", tstEnvironment, envSub, subs[envSub])
	}

	return nil
}

// repositoryToken mints a GitHub token for the repository from the Cloud Build
// connection the trigger reads it through; the build's identity needs Read Token
// Accessor on the connection. Every build starts from a trigger, and the triggers exist
// once 2-env holds the environment's connection and the repository's link, so a build
// whose connection or repository name is empty is one no trigger started, and the
// command refuses it: nothing is submitted by hand.
func repositoryToken(ctx context.Context, builds Builds, subs map[string]string, project, location string) (string, error) {
	connection, repo := subs["_REPO_CONNECTION_NAME"], subs["_REPO_NAME"]
	if connection == "" || repo == "" {
		return "", errors.Newf("_REPO_CONNECTION_NAME=%q _REPO_NAME=%q: every build starts from a trigger, which passes the environment's Cloud Build connection and the repository's link (both exist once 2-env holds the GitHub authorization); nothing is submitted by hand", connection, repo)
	}
	repository := "projects/" + project + "/locations/" + location + "/connections/" + connection + "/repositories/" + repo
	token, err := builds.ReadToken(ctx, repository)
	if err != nil {
		return "", errors.Wrapf(err, "could not mint a GitHub token from connection %s", connection)
	}

	return token, nil
}

// repositoryFullName is the repository as GitHub names it (<organization>/<repository>),
// a built-in of a build a GitHub event started, which the branch check and the comment
// read address the repository by; an empty name is a build no trigger started.
func repositoryFullName(subs map[string]string) (string, error) {
	if subs[repoFullNameSub] == "" {
		return "", errors.Newf("%s is not set: a trigger passes it as <organization>/<repository>, and every build starts from a trigger", repoFullNameSub)
	}

	return subs[repoFullNameSub], nil
}

// staleSteps says when the build's steps are not the ones this bedrock renders for the
// trigger: the stack that holds them was applied by an older bedrock, and the steps this
// bedrock renders take effect with the next release's stack apply in the environment. A
// bedrock release runs under the previous release's steps on purpose, so this is a
// notice, never a refusal. A build the API describes without its steps is left alone.
func staleSteps(build *Build, trigger string, want []string, out io.Writer) {
	if len(build.Steps) == 0 {
		return
	}
	got := make([]string, 0, len(build.Steps))
	for _, step := range build.Steps {
		got = append(got, step.ID)
	}
	if slices.Equal(got, want) {
		return
	}
	fmt.Fprintf(out, "Notice: this build runs under the %s trigger's steps as an older bedrock rendered them (%s), where this bedrock renders %s. The steps are the stack's and take effect with the next release's stack apply in %s; this bedrock runs under either.\n", trigger, strings.Join(got, ", "), strings.Join(want, ", "), build.Substitutions[envSub])
}

// routerDirOf is the generated router's package directory in the checkout,
// root-relative, where the resource generator writes the release file the window preview
// reads; empty when the application generates no router. The pipeline file names it for
// the release check (--router-dir, rendered from the application); a trigger-held job
// reads the application in the checkout for it, as bedrock check does.
func routerDirOf(w Workspace, p *derive.Placement) (string, error) {
	a, err := app.Discover(string(w))
	if err != nil {
		return "", errors.Wrap(err, "reading the application in the checkout")
	}
	m, err := derive.Derive(a, p)
	if err != nil {
		return "", errors.Wrap(err, "deriving the application in the checkout")
	}

	return m.RouterDir, nil
}
