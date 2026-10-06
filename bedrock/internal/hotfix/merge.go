package hotfix

import (
	"context"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
)

// mergeBranchPrefix names the merge-back branches: merge-back/<release>.
const mergeBranchPrefix = "merge-back/"

// MergeRequest names the released hotfix to bring to the default branch.
type MergeRequest struct {
	Owner         string
	Repo          string
	DefaultBranch string
	// Tag is the patch release on a hotfix line, v<major>.<minor>.<patch>.
	Tag string
}

// MergeResult is what Merge found and did: the merge-back branch at the release's
// commit, and the pull request from it into the default branch.
type MergeResult struct {
	Tag    string
	Line   string
	Commit string
	Branch string
	// BranchExisted is true when the merge-back branch was already there; BranchCommit
	// is then its tip, and the branch was left as it was.
	BranchExisted bool
	BranchCommit  string
	// Number and URL are the pull request's. Existed is true when a pull request from
	// the branch was already there (Merged says whether it was merged), and none was
	// opened.
	Number  int
	URL     string
	Existed bool
	Merged  bool
	// Title is the pull request's title: the conventional-commit line the squash merge
	// carries and release-please reads.
	Title string
}

// MergeBranch is the merge-back branch of a release: merge-back/<tag>.
func MergeBranch(tag string) string {
	return mergeBranchPrefix + tag
}

// Merge brings a released hotfix to the default branch: it creates the branch
// merge-back/<tag> at the release's commit (the tag's, so the merge-back carries exactly
// what shipped, not unreleased commits on the line) and opens a pull request from it
// into the default branch, titled fix: <tag> with the release's notes as its body. The
// hotfix line is never the pull request's head: conflicts are resolved by commits on the
// merge-back branch, which the squash merge deletes. Merge refuses a tag that is not a
// release tag, a release without a hotfix line, a release that is not on its line, one
// the default branch already carries, and one without a GitHub Release; an existing
// merge-back branch or pull request for the release is reported, not made again.
func Merge(ctx context.Context, client *github.Client, req MergeRequest) (*MergeResult, error) {
	if _, err := parse(req.Tag); err != nil {
		return nil, err
	}
	line, _ := Branch(req.Tag)
	lineRef, err := client.Ref(ctx, req.Owner, req.Repo, "heads/"+line)
	if err != nil {
		if github.NotFound(err) {
			return nil, errors.Newf("no hotfix line %s in %s/%s: %s is not a release on a hotfix line (hotfix start makes the line, and release-please releases its fixes)", line, req.Owner, req.Repo, req.Tag)
		}

		return nil, errors.Wrapf(err, "looking for branch %s", line)
	}
	commit, err := client.TagCommit(ctx, req.Owner, req.Repo, req.Tag)
	if err != nil {
		if github.NotFound(err) {
			return nil, errors.Newf("no tag %s in %s/%s: a merge-back brings a release that exists", req.Tag, req.Owner, req.Repo)
		}

		return nil, errors.Wrapf(err, "resolving tag %s", req.Tag)
	}
	if err := onBranch(ctx, client, req, commit, line, lineRef.Object.SHA); err != nil {
		return nil, err
	}
	release, err := client.Release(ctx, req.Owner, req.Repo, req.Tag)
	if err != nil {
		if github.NotFound(err) {
			return nil, errors.Newf("no release for tag %s in %s/%s: the merge-back carries the release's notes, which release-please writes when it cuts the release", req.Tag, req.Owner, req.Repo)
		}

		return nil, errors.Wrapf(err, "reading the release of %s", req.Tag)
	}
	branch := MergeBranch(req.Tag)
	result := &MergeResult{Tag: req.Tag, Line: line, Commit: commit, Branch: branch, Title: "fix: " + req.Tag}
	ref, err := client.Ref(ctx, req.Owner, req.Repo, "heads/"+branch)
	switch {
	case err == nil:
		result.BranchExisted, result.BranchCommit = true, ref.Object.SHA
	case github.NotFound(err):
		if err := client.CreateRef(ctx, req.Owner, req.Repo, "refs/heads/"+branch, commit); err != nil {
			return nil, errors.Wrapf(err, "creating branch %s", branch)
		}
	default:
		return nil, errors.Wrapf(err, "looking for branch %s", branch)
	}
	prs, err := client.PullRequestsFrom(ctx, req.Owner, req.Repo, branch)
	if err != nil {
		return nil, errors.Wrapf(err, "listing the pull requests from %s", branch)
	}
	if len(prs) > 0 {
		result.Existed, result.Number, result.URL, result.Merged = true, prs[0].Number, prs[0].HTMLURL, prs[0].MergedAt != ""

		return result, nil
	}
	pr, err := client.CreatePullRequest(ctx, req.Owner, req.Repo, github.PullRequestRequest{Title: result.Title, Body: release.Body, Head: branch, Base: req.DefaultBranch})
	if err != nil {
		return nil, errors.Wrapf(err, "opening the pull request from %s into %s", branch, req.DefaultBranch)
	}
	result.Number, result.URL = pr.Number, pr.HTMLURL

	return result, nil
}

// onBranch refuses a release whose commit is not on its hotfix line, and one the
// default branch already carries.
func onBranch(ctx context.Context, client *github.Client, req MergeRequest, commit, line, lineTip string) error {
	cmp, err := client.Compare(ctx, req.Owner, req.Repo, commit, lineTip)
	if err != nil {
		return errors.Wrapf(err, "comparing %s with %s", req.Tag, line)
	}
	if cmp.Status != statusAhead && cmp.Status != statusIdentical {
		return errors.Newf("tag %s (commit %s) is not on %s (%s): a merge-back brings a release made on a hotfix line", req.Tag, short(commit), line, cmp.Status)
	}
	cmp, err = client.Compare(ctx, req.Owner, req.Repo, commit, req.DefaultBranch)
	if err != nil {
		return errors.Wrapf(err, "comparing %s with %s", req.Tag, req.DefaultBranch)
	}
	if cmp.Status == statusAhead || cmp.Status == statusIdentical {
		return errors.Newf("tag %s (commit %s) is already on %s: nothing to merge back", req.Tag, short(commit), req.DefaultBranch)
	}

	return nil
}
