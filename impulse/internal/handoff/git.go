package handoff

import (
	"context"
	"os"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// Repo is the application's git repository, which holds the state the agent's work is
// compared against: the index is the tree at the handoff, and HEAD must not move.
type Repo struct {
	Root string
	Exec check.Execer
}

// Check reports whether the root is inside a git work tree.
func (r Repo) Check(ctx context.Context) error {
	if _, err := r.Exec.Run(ctx, r.Root, nil, "git", "rev-parse", "--is-inside-work-tree"); err != nil {
		return errors.Newf("%s is not in a git repository; the agent's work is verified against the index, so the handoff needs one", r.Root)
	}

	return nil
}

// Dirty lists the paths git reports as modified, staged, or untracked, the brief itself
// excepted.
func (r Repo) Dirty(ctx context.Context) ([]string, error) {
	out, err := r.Exec.Run(ctx, r.Root, nil, "git", "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return nil, errors.Wrap(err, "git status")
	}
	var paths []string
	for line := range strings.Lines(string(out)) {
		line = strings.TrimRight(line, "\n")
		if len(line) < 4 {
			continue
		}
		p := line[3:]
		if p == File {
			continue
		}
		paths = append(paths, p)
	}

	return paths, nil
}

// Head returns the commit HEAD names.
func (r Repo) Head(ctx context.Context) (string, error) {
	out, err := r.Exec.Run(ctx, r.Root, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", errors.Wrap(err, "git rev-parse HEAD")
	}

	return strings.TrimSpace(string(out)), nil
}

// Staged lists the paths with staged changes.
func (r Repo) Staged(ctx context.Context) ([]string, error) {
	out, err := r.Exec.Run(ctx, r.Root, nil, "git", "diff", "--cached", "--name-only")
	if err != nil {
		return nil, errors.Wrap(err, "git diff --cached")
	}

	return strings.Fields(string(out)), nil
}

// StageAll stages every change in the tree, so the index holds the tree at the handoff.
func (r Repo) StageAll(ctx context.Context) error {
	if _, err := r.Exec.Run(ctx, r.Root, nil, "git", "add", "-A"); err != nil {
		return errors.Wrap(err, "git add -A")
	}

	return nil
}

// FromIndex reads files as the index holds them: the tree at the handoff, when the
// agent has staged nothing. A path not in the index reads as absent.
func (r Repo) FromIndex(ctx context.Context) Reader {
	return func(rel string) ([]byte, error) {
		out, err := r.Exec.Run(ctx, r.Root, nil, "git", "show", ":"+rel)
		if err != nil {
			return nil, os.ErrNotExist
		}

		return out, nil
	}
}

// For builds the repository helper for an application.
func For(a *app.App, exec check.Execer) Repo {
	return Repo{Root: a.Root, Exec: exec}
}
