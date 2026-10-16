// releasejob.go is the release trigger's command: release-please, pinned, as the release
// GitHub App, on every push to the default branch or a patch line (the stack's
// cloud-build.tf, in the first environment, as the application's release identity). The
// command mints the app's installation token from its key's pinned version in Secret
// Manager and runs release-please twice for the pushed branch: github-release, which
// makes the tag and the GitHub Release of a release pull request the push merged, then
// release-pr, which opens or updates the release pull request from the commits since the
// last release. The token goes to the two processes as their --token argument, which is
// the one place release-please reads it (its environment is not), and the step prints
// its own line per command, never the argument list; the tag then fires the version
// trigger in every environment, as a tag always has. On the default branch the
// merge-back guard runs first: no regular release is cut while a patch release is not
// merged back (mergeBackGuard).

package deploy

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/patch"
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
	if !patchBranchRE.MatchString(req.Branch) {
		if err := mergeBackGuard(ctx, clients.GitHub(token), owner, repo, req.Branch, out); err != nil {
			return err
		}
	}
	for _, command := range releasePleaseCommands {
		c := Command{
			Name: releasePleaseBin,
			Args: []string{"--yes", "release-please@" + release.PleaseVersion, command, "--repo-url=" + req.RepoFullName, "--target-branch=" + req.Branch, "--config-file=" + release.PleaseConfig, "--manifest-file=" + release.PleaseManifest, "--token=" + token},
		}
		fmt.Fprintf(out, "=== release-please %s on %s (%s) ===\n", command, req.RepoFullName, req.Branch)
		if err := clients.Exec.Run(ctx, c, out); err != nil {
			return errors.Wrapf(err, "release-please %s", command)
		}
	}

	return nil
}

// mergeBackGuard refuses the default branch's release while a patch release is not
// merged back: every release tag the branch cannot reach (a patch line's, since the
// merge-back is a squash) must have a merged pull request from its merge-back branch,
// the line's newest covering the older ones. It runs before release-please opens or
// updates the branch's release pull request and before the merge of that pull request
// would make the tag, and stops the trigger naming the tag and the command. A further
// patch release on the line is allowed meanwhile: a push to a line never runs it.
func mergeBackGuard(ctx context.Context, gh *github.Client, owner, repo, branch string, out io.Writer) error {
	backs, err := patch.MergeBacks(ctx, gh, owner, repo, branch)
	if err != nil {
		return errors.Wrap(err, "the merge-back guard")
	}
	var owed, covered []string
	for _, b := range backs {
		if b.Merged {
			covered = append(covered, fmt.Sprintf("%s by #%d", b.Tag, b.Number))

			continue
		}
		what := "no pull request from " + patch.MergeBranch(b.Tag)
		if b.Number > 0 {
			what = fmt.Sprintf("pull request #%d from %s is open, not merged", b.Number, patch.MergeBranch(b.Tag))
		}
		owed = append(owed, fmt.Sprintf("%s on %s (%s)", b.Tag, b.Line, what))
	}
	switch {
	case len(owed) > 0:
		msg := fmt.Sprintf("no regular release is cut while a patch release is not merged back; %s owes %s. Bring each back with bedrock patch merge <tag> and merge its pull request, then push %s again; a further patch release on the line is allowed meanwhile", branch, strings.Join(owed, "; "), branch)
		fmt.Fprintf(out, "%s%s\n", rejected, msg)

		return errors.New(rejected + msg)
	case len(covered) > 0:
		fmt.Fprintf(out, "Merge-back guard passed: every patch release %s does not carry is merged back (%s).\n", branch, strings.Join(covered, ", "))
	default:
		fmt.Fprintf(out, "Merge-back guard passed: %s carries every release.\n", branch)
	}

	return nil
}
