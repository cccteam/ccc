// git.go is what the renumber asks git: where the default branch is, what it holds under
// a directory, which files are tracked, and the renames of the tracked ones.

package migration

import (
	"context"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"
)

// git runs one git command in the repository and returns its output; a failure carries
// what git said.
func git(ctx context.Context, root string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return "", errors.Newf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exit.Stderr)))
		}

		return "", errors.Wrapf(err, "git %s", strings.Join(args, " "))
	}

	return string(out), nil
}

// defaultRef is the ref holding the default branch's tree: origin's copy of the branch
// when the repository has one (what a pull-request build compares against), else the
// local branch. Neither is an error: the committed sequence has to come from somewhere.
func defaultRef(ctx context.Context, root, branch string) (ref, commit string, err error) {
	for _, ref := range []string{"refs/remotes/origin/" + branch, "refs/heads/" + branch} {
		out, err := git(ctx, root, "rev-parse", "--verify", "--quiet", "--short", ref)
		if err == nil {
			return ref, strings.TrimSpace(out), nil
		}
	}

	return "", "", errors.Newf("no %s branch to read the committed migrations from: neither origin/%s nor a local %s exists here (fetch first, or the placement names the wrong default branch)", branch, branch, branch)
}

// treeFiles names the files directly under the directory in the ref's tree; none when
// the tree has no such directory.
func treeFiles(ctx context.Context, root, ref, dir string) (map[string]bool, error) {
	out, err := git(ctx, root, "ls-tree", "-r", "--name-only", ref, "--", dir)
	if err != nil {
		return nil, err
	}

	return under(out, dir), nil
}

// trackedFiles names the files directly under the directory that the index tracks:
// those are moved with git mv, so the index follows; an untracked file is renamed on
// disk alone.
func trackedFiles(ctx context.Context, root, dir string) (map[string]bool, error) {
	out, err := git(ctx, root, "ls-files", "--", dir)
	if err != nil {
		return nil, err
	}

	return under(out, dir), nil
}

// under reads a listing of root-relative paths and keeps the base names of the ones
// directly under the directory.
func under(listing, dir string) map[string]bool {
	names := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(listing), "\n") {
		if line != "" && path.Dir(line) == dir {
			names[path.Base(line)] = true
		}
	}

	return names
}

// gitMove renames a tracked file, staging the rename.
func gitMove(ctx context.Context, root, from, to string) error {
	_, err := git(ctx, root, "mv", "--", from, to)

	return err
}

// mergeBase is the commit the working tree's branch was cut from the ref at: where the
// committed files it still holds are told from the ones the default branch added since.
// A tree with no history in common with the ref (git says so with exit status 1 and
// nothing else) is read at the ref.
func mergeBase(ctx context.Context, root, ref string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", root, "merge-base", "HEAD", ref)
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 && len(exit.Stderr) == 0 {
		return ref, nil
	}
	if err != nil {
		return "", errors.Wrapf(err, "git merge-base HEAD %s", ref)
	}

	return strings.TrimSpace(string(out)), nil
}

// lineRE is a hotfix line: hotfix/<major>.<minor>.x.
var lineRE = regexp.MustCompile(`^hotfix/\d+\.\d+\.x$`)

// followedBranch is the branch whose committed sequence the renumber follows: the
// default branch, or a hotfix line nearer to HEAD in the history, since a fix for a
// line is cut from the line, which is behind the default branch on purpose. Nearness
// is the number of commits HEAD has beyond the branch; a branch that is not an
// ancestor of HEAD is not followed, and a tie goes to the default branch.
func followedBranch(ctx context.Context, root, defaultBranch string) (string, error) {
	out, err := git(ctx, root, "for-each-ref", "--format=%(refname:short)", "refs/remotes/origin/hotfix/", "refs/heads/hotfix/")
	if err != nil {
		return "", errors.Wrap(err, "git for-each-ref")
	}
	followed, nearest := defaultBranch, -1
	if ref, _, err := defaultRef(ctx, root, defaultBranch); err == nil {
		nearest = distance(ctx, root, ref)
	}
	seen := map[string]bool{}
	for _, name := range strings.Fields(out) {
		line := strings.TrimPrefix(name, "origin/")
		if !lineRE.MatchString(line) || seen[line] {
			continue
		}
		seen[line] = true
		ref, _, err := defaultRef(ctx, root, line)
		if err != nil {
			continue
		}
		if d := distance(ctx, root, ref); d >= 0 && (nearest < 0 || d < nearest) {
			followed, nearest = line, d
		}
	}

	return followed, nil
}

// distance is the number of commits HEAD has beyond the ref, or -1 when the ref is not
// an ancestor of HEAD.
func distance(ctx context.Context, root, ref string) int {
	if _, err := git(ctx, root, "merge-base", "--is-ancestor", ref, "HEAD"); err != nil {
		return -1
	}
	out, err := git(ctx, root, "rev-list", "--count", ref+"..HEAD")
	if err != nil {
		return -1
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return -1
	}

	return n
}
