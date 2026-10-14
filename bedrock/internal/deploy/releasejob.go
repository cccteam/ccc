// releasejob.go is the release trigger's command: release-please, pinned, as the release
// GitHub App, on every push to the default branch or a hotfix line (the stack's
// cloud-build.tf, in the first environment, as the application's release identity). The
// command mints the app's installation token from its key's pinned version in Secret
// Manager and runs release-please twice for the pushed branch: github-release, which
// makes the tag and the GitHub Release of a release pull request the push merged, then
// release-pr, which opens or updates the release pull request from the commits since the
// last release. The token lives in the two processes' environment and nowhere else; the
// tag then fires the version trigger in every environment, as a tag always has.

package deploy

import (
	"context"
	"fmt"
	"io"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/release"
)

// The variables the release trigger's step passes the command, from the trigger's
// substitutions (2-env's outputs: the release app's App ID and its key's pinned version)
// and the build's own (the repository and the pushed branch).
const (
	releaseAppVar    = "RELEASE_APP_ID"
	releaseKeyVar    = "RELEASE_KEY_SECRET"
	repoFullNameVar  = "REPO_FULL_NAME"
	branchVar        = "BRANCH_NAME"
	releasePleaseBin = "npx"
)

// releasePleaseCommands are release-please's two commands, in the order the step runs
// them: the release of a merged release pull request first, then the next release pull
// request.
var releasePleaseCommands = []string{"github-release", "release-pr"}

// ReleaseRequest is what the release trigger's step passes: the release app, its key's
// pinned version (a Secret Manager version's resource name), the repository and the
// pushed branch.
type ReleaseRequest struct {
	AppID        string
	KeyVersion   string
	RepoFullName string
	Branch       string
}

// check refuses a request missing any of the four.
func (r *ReleaseRequest) check() error {
	for _, v := range []struct{ name, value string }{{releaseAppVar, r.AppID}, {releaseKeyVar, r.KeyVersion}, {repoFullNameVar, r.RepoFullName}, {branchVar, r.Branch}} {
		if v.value == "" {
			return errors.Newf("%s is not set: the release trigger's step passes the release app, its key's pinned version, the repository and the pushed branch to the command", v.name)
		}
	}

	return nil
}

// RunRelease cuts the releases a push calls for: release-please, pinned, as the release
// app, github-release then release-pr for the pushed branch.
func RunRelease(ctx context.Context, clients *Clients, req *ReleaseRequest, out io.Writer) error {
	if err := req.check(); err != nil {
		return err
	}
	owner, repo, err := splitRepo(req.RepoFullName)
	if err != nil {
		return err
	}
	token, err := appToken(ctx, clients, req.AppID, req.KeyVersion, owner, repo, "the release app")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Release app %s: a token minted for its installation on %s; release-please %s runs for %s.\n", req.AppID, req.RepoFullName, release.PleaseVersion, req.Branch)
	for _, command := range releasePleaseCommands {
		c := Command{
			Name: releasePleaseBin,
			Args: []string{"--yes", "release-please@" + release.PleaseVersion, command, "--repo-url=" + req.RepoFullName, "--target-branch=" + req.Branch, "--config-file=" + release.PleaseConfig, "--manifest-file=" + release.PleaseManifest},
			Env:  []string{githubTokenFact + "=" + token},
		}
		fmt.Fprintf(out, "=== release-please %s on %s (%s) ===\n", command, req.RepoFullName, req.Branch)
		if err := clients.Exec.Run(ctx, c, out); err != nil {
			return errors.Wrapf(err, "release-please %s", command)
		}
	}

	return nil
}
