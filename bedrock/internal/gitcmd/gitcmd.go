// Package gitcmd runs git in a repository for the commands that read the committed
// state beside the working tree: the default branch's ref, a file as the branch holds
// it, the tracked files under a directory, and a rename the index follows.
package gitcmd

import (
	"context"
	"os/exec"
	"strings"

	"github.com/go-playground/errors/v5"
)

// Output runs one git command in the repository at root and returns its standard
// output; a failure carries what git said.
func Output(ctx context.Context, root string, args ...string) (string, error) {
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

// DefaultRef is the ref holding the default branch's tree: origin's copy of the branch
// when the repository has one (what a pull-request build compares against), else the
// local branch. Neither is an error: the committed state has to come from somewhere.
func DefaultRef(ctx context.Context, root, branch string) (ref, commit string, err error) {
	for _, ref := range []string{"refs/remotes/origin/" + branch, "refs/heads/" + branch} {
		out, err := Output(ctx, root, "rev-parse", "--verify", "--quiet", "--short", ref)
		if err == nil {
			return ref, strings.TrimSpace(out), nil
		}
	}

	return "", "", errors.Newf("no %s branch to read here: neither origin/%s nor a local %s exists (fetch first, or the placement names the wrong default branch)", branch, branch, branch)
}

// InWorkTree reports whether dir is inside a git working tree: a directory that is not
// has no committed state to compare with, and a check over it says nothing.
func InWorkTree(ctx context.Context, dir string) bool {
	out, err := Output(ctx, dir, "rev-parse", "--is-inside-work-tree")

	return err == nil && strings.TrimSpace(out) == "true"
}

// Prefix is dir's path from the repository's root, with a trailing slash, and empty
// at the root: what a path under dir is prefixed with to name it in a tree.
func Prefix(ctx context.Context, dir string) (string, error) {
	out, err := Output(ctx, dir, "rev-parse", "--show-prefix")
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(out), nil
}

// Show reads the file at path (from the repository's root) as the ref's tree holds it.
// A ref whose tree has no such file is not an error: ok is false and nothing is read.
func Show(ctx context.Context, root, ref, path string) (content []byte, ok bool, err error) {
	listed, err := Output(ctx, root, "ls-tree", "--full-tree", "--name-only", ref, "--", path)
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(listed) == "" {
		return nil, false, nil
	}
	out, err := Output(ctx, root, "show", ref+":"+path)
	if err != nil {
		return nil, false, err
	}

	return []byte(out), true, nil
}
