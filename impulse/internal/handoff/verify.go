package handoff

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/impulse/internal/app"
	"github.com/cccteam/ccc/impulse/internal/check"
)

// GuardName is the name the guardrail result reports under, beside the checks.
const GuardName = "guardrails"

// Baseline is what the tool recorded before handing off, when it stays in the process to
// verify afterwards: the commit and the staged paths, which the agent must leave alone.
// A verification in a later process has no baseline and compares against the index only.
type Baseline struct {
	Head   string
	Staged []string
}

// Record reads the baseline.
func Record(ctx context.Context, repo Repo) (*Baseline, error) {
	head, err := repo.Head(ctx)
	if err != nil {
		return nil, err
	}
	staged, err := repo.Staged(ctx)
	if err != nil {
		return nil, err
	}

	return &Baseline{Head: head, Staged: staged}, nil
}

// Verify compares the tree after the agent's work against the index: the generator
// option set and the lint configuration must read the same, and with a baseline, HEAD
// and the staged paths must not have moved. The result reports like a check.
func Verify(ctx context.Context, a *app.App, repo Repo, base *Baseline) (check.Result, error) {
	before, err := Take(a, repo.FromIndex(ctx))
	if err != nil {
		return check.Result{}, err
	}
	after, err := Take(a, FromTree(a))
	if err != nil {
		return check.Result{}, err
	}
	findings := Compare(before, after)
	if base != nil {
		moved, err := baselineFindings(ctx, repo, base)
		if err != nil {
			return check.Result{}, err
		}
		findings = append(findings, moved...)
	}
	if len(findings) > 0 {
		return check.Result{Name: GuardName, Status: check.Fail, Summary: fmt.Sprintf("%d guardrail(s) moved during the handoff", len(findings)), Details: findings}, nil
	}

	return check.Result{
		Name: GuardName, Status: check.Pass,
		Summary: fmt.Sprintf("%d generator program(s) and %d lint configuration(s) unchanged against the index", len(after.Programs), len(after.Configs)),
	}, nil
}

func baselineFindings(ctx context.Context, repo Repo, base *Baseline) ([]string, error) {
	var findings []string
	head, err := repo.Head(ctx)
	if err != nil {
		return nil, err
	}
	if head != base.Head {
		findings = append(findings, fmt.Sprintf("HEAD moved from %s to %s: the agent committed", short(base.Head), short(head)))
	}
	staged, err := repo.Staged(ctx)
	if err != nil {
		return nil, err
	}
	if strings.Join(staged, "\n") != strings.Join(base.Staged, "\n") {
		findings = append(findings, "the staged paths changed: the agent staged its work, so the index no longer holds the tree at the handoff")
	}

	return findings, nil
}

func short(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}

	return commit
}

// ErrNotClean reports a verification that found failures.
var ErrNotClean = errors.New("the handoff is not clean")
