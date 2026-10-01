// Package hotfix starts a hotfix line: the branch hotfix/<major>.<minor>.x at the
// commit of a release on the default branch, on which fixes are released by
// release-please as the line's next patch versions.
//
// release-please numbers a release from the manifest file as it stands on the branch
// it releases and from the commits since the last release in that branch's history. At
// the release's commit the manifest names that release, so the line's first hotfix is
// the next patch, whatever the default branch has released since. One case needs help:
// when the default branch has already cut a later patch of the same line (v0.1.22 cut
// while production still runs v0.1.21), the branch would compute v0.1.22 again. Start
// then creates the branch at a commit that sets the manifest to the line's highest
// existing patch, so the next release skips to the one after it.
package hotfix

import (
	"context"
	"encoding/json"
	"regexp"
	"strconv"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
)

const (
	// ManifestFile is release-please's manifest: the version of each package, at the
	// repository root the one package ".".
	ManifestFile = ".release-please-manifest.json"
	// rootPackage is the manifest's key for the repository root.
	rootPackage = "."
	// fileMode is a regular file in a git tree.
	fileMode = "100644"
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
	result.Next = version{major: base.major, minor: base.minor, patch: highest.patch + 1}.String()
	if highest.patch > base.patch {
		result.Skipped = highest.String()
		tip, err = manifestCommit(ctx, client, req, commit, highest)
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
	if cmp.Status != "ahead" && cmp.Status != "identical" {
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

// manifestCommit makes a commit on the release's commit that sets the manifest's root
// version to the line's highest patch, and returns it.
func manifestCommit(ctx context.Context, client *github.Client, req Request, parent string, highest version) (string, error) {
	data, err := client.Contents(ctx, req.Owner, req.Repo, ManifestFile, parent)
	if err != nil {
		if github.NotFound(err) {
			return "", errors.Newf("no %s at %s: release-please needs its manifest to number the line's releases, and %s has already been cut beyond %s", ManifestFile, req.Tag, highest, req.Tag)
		}

		return "", errors.Wrapf(err, "reading %s at %s", ManifestFile, req.Tag)
	}
	manifest := map[string]string{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", errors.Wrapf(err, "%s at %s", ManifestFile, req.Tag)
	}
	manifest[rootPackage] = highest.bare()
	content, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", errors.Wrap(err, "json.MarshalIndent()")
	}
	content = append(content, '\n')
	blob, err := client.CreateBlob(ctx, req.Owner, req.Repo, content)
	if err != nil {
		return "", errors.Wrap(err, "storing the manifest")
	}
	parentCommit, err := client.GetCommit(ctx, req.Owner, req.Repo, parent)
	if err != nil {
		return "", errors.Wrapf(err, "reading commit %s", short(parent))
	}
	tree, err := client.CreateTree(ctx, req.Owner, req.Repo, parentCommit.Tree.SHA, []github.TreeEntry{{Path: ManifestFile, Mode: fileMode, Type: "blob", SHA: blob}})
	if err != nil {
		return "", errors.Wrap(err, "building the tree")
	}
	message := "chore(hotfix): the " + strconv.Itoa(highest.major) + "." + strconv.Itoa(highest.minor) + " line continues after " + highest.String() + ", which " + req.DefaultBranch + " has already cut"
	commit, err := client.CreateCommit(ctx, req.Owner, req.Repo, message, tree, []string{parent})
	if err != nil {
		return "", errors.Wrap(err, "creating the manifest commit")
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
