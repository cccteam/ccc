// Package hotfix starts a hotfix line: the branch hotfix/<major>.<minor>.x at the
// commit of a release on the default branch, on which fixes are released by
// release-please as the line's next patch versions.
//
// release-please numbers a release from the manifest file as it stands on the branch
// it releases and from the commits since that version's release in the branch's
// history. At the release's commit the manifest names that release, so the line's first
// hotfix is the next patch, whatever the default branch has released since. One case
// needs help: when the default branch has already cut a later patch of the same line
// (v0.1.22 cut while production still runs v0.1.21), the branch would compute v0.1.22
// again. Start then creates the branch at a commit on the release's commit whose message
// carries release-please's Release-As footer naming the patch after the highest one cut,
// so the line's first release skips to it. The manifest is left as the release wrote it:
// set to a version whose release the line does not hold, release-please would count the
// line's whole history as unreleased, and the feature commits in it would bump the minor.
package hotfix

import (
	"context"
	"regexp"
	"strconv"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
)

const (
	// statusAhead and statusIdentical are the comparison results that say the base
	// commit is on the head branch.
	statusAhead     = "ahead"
	statusIdentical = "identical"
	// ManifestFile is release-please's manifest: the version of each package, at the
	// repository root the one package ".".
	ManifestFile = ".release-please-manifest.json"
)

// tagRE is a release tag: v<major>.<minor>.<patch>.
var tagRE = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// Request names the release the hotfix line starts from.
type Request struct {
	Owner         string
	Repo          string
	DefaultBranch string
	// Tag is the release the line starts from, v<major>.<minor>.<patch>.
	Tag string
}

// Result is what Start found and did.
type Result struct {
	Tag    string
	Commit string
	Branch string
	// Existed is true when the line already had its branch; BranchCommit is then the
	// commit at its tip, and nothing was created.
	Existed      bool
	BranchCommit string
	// Skipped is the line's highest patch the default branch had already cut beyond
	// the tag, when there was one: the branch then starts at a commit that sets the
	// manifest to it, so the next release is Next.
	Skipped string
	// Next is the version release-please will give the line's first hotfix.
	Next string
	// Latest is the repository's latest release by version, among its v<major>.<minor>.
	// <patch> tags: when it is not Tag, the hotfix may not be based on the release
	// production runs, which GitHub cannot tell.
	Latest string
}

// Branch is the hotfix branch of the tag's release line: hotfix/<major>.<minor>.x.
func Branch(tag string) (string, error) {
	line, err := parse(tag)
	if err != nil {
		return "", err
	}

	return "hotfix/" + strconv.Itoa(line.major) + "." + strconv.Itoa(line.minor) + ".x", nil
}

// version is a parsed release tag.
type version struct {
	major, minor, patch int
}

func (v version) String() string {
	return "v" + strconv.Itoa(v.major) + "." + strconv.Itoa(v.minor) + "." + strconv.Itoa(v.patch)
}

// bare is the version without its v, as the manifest writes it.
func (v version) bare() string {
	return v.String()[1:]
}

func parse(tag string) (version, error) {
	m := tagRE.FindStringSubmatch(tag)
	if m == nil {
		return version{}, errors.Newf("tag %q is not a release tag (v<major>.<minor>.<patch>, such as v0.1.4)", tag)
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch, _ := strconv.Atoi(m[3])

	return version{major: major, minor: minor, patch: patch}, nil
}

// Start creates the line's branch at the tag's commit. It refuses a tag that is not a
// release tag, a tag the repository lacks, and a release that is not on the default
// branch; a line whose branch already exists is reported, not recreated.
func Start(ctx context.Context, client *github.Client, req Request) (*Result, error) {
	base, err := parse(req.Tag)
	if err != nil {
		return nil, err
	}
	branch, _ := Branch(req.Tag)
	commit, err := client.TagCommit(ctx, req.Owner, req.Repo, req.Tag)
	if err != nil {
		if github.NotFound(err) {
			return nil, errors.Newf("no tag %s in %s/%s: a hotfix line starts from a release that exists", req.Tag, req.Owner, req.Repo)
		}

		return nil, errors.Wrapf(err, "resolving tag %s", req.Tag)
	}
	tags, err := client.Tags(ctx, req.Owner, req.Repo)
	if err != nil {
		return nil, errors.Wrap(err, "listing tags")
	}
	result := &Result{Tag: req.Tag, Commit: commit, Branch: branch, Latest: latestRelease(tags, base).String()}
	ref, err := client.Ref(ctx, req.Owner, req.Repo, "heads/"+branch)
	if err == nil {
		result.Existed, result.BranchCommit = true, ref.Object.SHA

		return result, nil
	}
	if !github.NotFound(err) {
		return nil, errors.Wrapf(err, "looking for branch %s", branch)
	}
	if err := onDefaultBranch(ctx, client, req, commit); err != nil {
		return nil, err
	}
	highest := highestPatch(tags, base)
	tip := commit
	next := version{major: base.major, minor: base.minor, patch: highest.patch + 1}
	result.Next = next.String()
	if highest.patch > base.patch {
		result.Skipped = highest.String()
		tip, err = continuationCommit(ctx, client, req, commit, highest, next)
		if err != nil {
			return nil, err
		}
	}
	if err := client.CreateRef(ctx, req.Owner, req.Repo, "refs/heads/"+branch, tip); err != nil {
		return nil, errors.Wrapf(err, "creating branch %s", branch)
	}

	return result, nil
}

// onDefaultBranch refuses a release whose commit is not on the default branch.
func onDefaultBranch(ctx context.Context, client *github.Client, req Request, commit string) error {
	cmp, err := client.Compare(ctx, req.Owner, req.Repo, commit, req.DefaultBranch)
	if err != nil {
		return errors.Wrapf(err, "comparing %s with %s", req.Tag, req.DefaultBranch)
	}
	if cmp.Status != statusAhead && cmp.Status != statusIdentical {
		return errors.Newf("tag %s (commit %s) is not on %s (%s): a hotfix line starts from a release on the default branch", req.Tag, short(commit), req.DefaultBranch, cmp.Status)
	}

	return nil
}

// highestPatch is the line's highest patch among the repository's tags, the base
// itself when none is higher.
func highestPatch(tags []github.Tag, base version) version {
	highest := base
	for _, tag := range tags {
		v, err := parse(tag.Name)
		if err != nil || v.major != base.major || v.minor != base.minor {
			continue
		}
		if v.patch > highest.patch {
			highest = v
		}
	}

	return highest
}

// latestRelease is the highest release among the repository's tags, the base itself
// when none is higher.
func latestRelease(tags []github.Tag, base version) version {
	latest := base
	for _, tag := range tags {
		v, err := parse(tag.Name)
		if err != nil {
			continue
		}
		if v.major > latest.major || (v.major == latest.major && (v.minor > latest.minor || (v.minor == latest.minor && v.patch > latest.patch))) {
			latest = v
		}
	}

	return latest
}

// continuationCommit makes a commit on the release's commit, with the release's own
// tree, whose message tells release-please the line's next release (its Release-As
// footer), and returns it. The manifest is left as the release wrote it: release-please
// counts a line's unreleased commits from the manifest's release on the branch, which
// the line holds, and the footer alone decides the number, so the line's first release
// is the patch after the one the default branch has already cut, with a changelog of
// the line's own commits.
func continuationCommit(ctx context.Context, client *github.Client, req Request, parent string, highest, next version) (string, error) {
	parentCommit, err := client.GetCommit(ctx, req.Owner, req.Repo, parent)
	if err != nil {
		return "", errors.Wrapf(err, "reading commit %s", short(parent))
	}
	message := "chore(hotfix): the " + strconv.Itoa(highest.major) + "." + strconv.Itoa(highest.minor) + " line continues after " + highest.String() + ", which " + req.DefaultBranch + " has already cut\n\nRelease-As: " + next.bare()
	commit, err := client.CreateCommit(ctx, req.Owner, req.Repo, message, parentCommit.Tree.SHA, []string{parent})
	if err != nil {
		return "", errors.Wrap(err, "creating the continuation commit")
	}

	return commit, nil
}

// short is the first seven characters of a commit, as git prints them.
func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}

	return sha
}
