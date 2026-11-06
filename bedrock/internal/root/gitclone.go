// gitclone.go makes the bootstrap's directory, which holds the rendered files it pushed, a
// clone of the repository at the pushed commit, so the person goes on from it by pull
// request as the bootstrap's epilogue says.

package root

import (
	"context"
	"os/exec"
	"strings"

	"github.com/go-playground/errors/v5"
)

// CloneFunc makes dir, which holds the files of the commit, a clone of the repository at
// url with branch checked out at commit, fetching with the token.
type CloneFunc func(ctx context.Context, dir, url, branch, commit, token string) error

// GitClone is CloneFunc over git on this computer: the directory is made a repository
// with origin at the url, the branch is fetched (the token goes to git as a header
// through its environment, never on the command line) and the working tree is set to the
// commit, which it already holds file for file. A computer without git answers an error
// that says so, and the bootstrap prints the clone to make by hand.
func GitClone(ctx context.Context, dir, url, branch, commit, token string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("git is not on this computer")
	}
	const quiet = "--quiet"
	steps := [][]string{
		{"init", quiet, "--initial-branch=" + branch},
		{"remote", "add", "origin", url},
		{"fetch", quiet, "origin", branch},
		{"reset", quiet, "--hard", commit},
		{"branch", quiet, "--set-upstream-to=origin/" + branch, branch},
	}
	for _, args := range steps {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // git, with arguments the bootstrap composes from the placement, in the directory it rendered
		cmd.Env = append(cmd.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraheader", "GIT_CONFIG_VALUE_0=AUTHORIZATION: bearer "+token)
		if out, err := cmd.CombinedOutput(); err != nil {
			return errors.Newf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
		}
	}

	return nil
}
