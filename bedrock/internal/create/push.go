// push.go is the creation's GitHub side: the new organization's infrastructure
// repository, made in the GitHub organization and given the rendered files as its
// first push (which its apply trigger, made by the first apply, runs on), and the
// hand-off pull request in the root organization's repository, which records the
// folder the creation made. Both as the infrastructure GitHub App, whose installation
// covers every repository of the organization.

package create

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/org"
)

const (
	fileMode = "100644"
	blobType = "blob"
	// providersDir is where an init puts the providers, and localState the state an
	// apply without a backend would write; neither is pushed.
	providersDir = ".terraform"
	localState   = "terraform.tfstate"
)

// repository finds or makes the organization's infrastructure repository: private, with
// the default branch at an initial commit, which the push builds on.
func (f *creation) repository(ctx context.Context, h *org.Placement) (string, error) {
	full := h.GithubOrganization + "/" + h.SourceRepo
	if _, err := f.gh.Repository(ctx, h.GithubOrganization, h.SourceRepo); err == nil {
		fmt.Fprintf(f.out, "Repository %s exists.\n", full)

		return full, nil
	} else if !github.NotFound(err) {
		return "", errors.Wrapf(err, "reading repository %s", full)
	}
	_, err := f.gh.CreateRepository(ctx, h.GithubOrganization, github.RepositoryRequest{
		Name: h.SourceRepo, Description: "The infrastructure of the " + h.Prefix + " organization (bedrock)", Private: true, DefaultBranch: h.GithubDefaultBranch, AutoInit: true,
	})
	if err != nil {
		return "", errors.Wrapf(err, "creating repository %s", full)
	}
	fmt.Fprintf(f.out, "Created repository %s, private, default branch %s.\n", full, h.GithubDefaultBranch)

	return full, nil
}

// push puts the rendered files on the repository's default branch as one commit on its
// initial commit, unless the branch carries the placement already (a run that stopped
// after the push), and answers the commit.
func (f *creation) push(ctx context.Context, dir string, h *org.Placement) (string, error) {
	message := "chore: the foundation of " + h.Prefix + ", created by the root organization " + f.req.Root.Prefix + "\n\nRendered by bedrock org create from " + org.RecordsDir + "/" + h.Prefix + "/" + org.PlacementFile + " in " + f.req.Root.SourceRepo + " at " + short(f.req.Commit) + ". The apply trigger applies 0-bootstrap, then 1-org, from this push."

	return Push(ctx, f.gh, dir, h.GithubOrganization, h.SourceRepo, h.GithubDefaultBranch, message, f.out)
}

// Push puts the directory's files on the repository's branch as one commit on the
// branch's head, unless the branch carries placement.json already (a run that stopped
// after its push), and answers the commit. The first applies' inits leave each layer's
// providers under .terraform and its lock file beside them: the lock file is committed,
// as a person commits the one their first init writes, the providers never are; a local
// state is never pushed either.
func Push(ctx context.Context, gh *github.Client, dir, owner, repo, branch, message string, out io.Writer) (string, error) {
	ref, err := gh.Ref(ctx, owner, repo, "heads/"+branch)
	if err != nil {
		return "", errors.Wrapf(err, "reading %s of %s/%s", branch, owner, repo)
	}
	if _, err := gh.Contents(ctx, owner, repo, org.PlacementFile, branch); err == nil {
		fmt.Fprintf(out, "%s of %s/%s carries the foundation already (%s).\n", branch, owner, repo, short(ref.Object.SHA))

		return ref.Object.SHA, nil
	} else if !github.NotFound(err) {
		return "", errors.Wrapf(err, "reading %s on %s", org.PlacementFile, branch)
	}
	entries, err := blobs(ctx, gh, dir, owner, repo)
	if err != nil {
		return "", err
	}
	tree, err := gh.CreateTree(ctx, owner, repo, "", entries)
	if err != nil {
		return "", errors.Wrapf(err, "creating the tree of %d file(s) in %s/%s", len(entries), owner, repo)
	}
	commit, err := gh.CreateCommit(ctx, owner, repo, message, tree, []string{ref.Object.SHA})
	if err != nil {
		return "", errors.Wrapf(err, "creating the foundation commit in %s/%s", owner, repo)
	}
	if err := gh.UpdateRef(ctx, owner, repo, "heads/"+branch, commit, false); err != nil {
		return "", errors.Wrapf(err, "moving %s of %s/%s to %s", branch, owner, repo, short(commit))
	}
	fmt.Fprintf(out, "Pushed the foundation to %s/%s at %s (%d files): its apply trigger runs on it.\n", owner, repo, short(commit), len(entries))

	return commit, nil
}

// blobs stores every file of the rendered directory as a blob and answers the tree
// entries, in path order.
func blobs(ctx context.Context, gh *github.Client, dir, owner, repo string) ([]github.TreeEntry, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == providersDir || d.Name() == ".git") {
			return filepath.SkipDir
		}
		if !d.IsDir() && !strings.HasPrefix(d.Name(), localState) {
			paths = append(paths, path)
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrapf(err, "filepath.WalkDir(): %s", dir)
	}
	sort.Strings(paths)
	entries := make([]github.TreeEntry, 0, len(paths))
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.Wrapf(err, "os.ReadFile(): %s", path)
		}
		sha, err := gh.CreateBlob(ctx, owner, repo, content)
		if err != nil {
			return nil, errors.Wrapf(err, "storing %s", path)
		}
		rel, _ := filepath.Rel(dir, path)
		entries = append(entries, github.TreeEntry{Path: filepath.ToSlash(rel), Mode: fileMode, Type: blobType, SHA: sha})
	}

	return entries, nil
}

// handOff opens, in the root organization's repository, the pull request that records
// the created organization's folder (placement.json, hostingOrganizations) and the
// placement as created (organizations/<prefix>/placement.json), from a branch of its
// own; a pull request from that branch already open is left as it is. It answers the
// pull request's URL.
func (f *creation) handOff(ctx context.Context, h *org.Placement, marker *Marker) (string, error) {
	owner, repo := f.req.Root.GithubOrganization, f.req.Root.SourceRepo
	branch := handOffBranch + h.Prefix
	open, err := f.gh.PullRequestsFrom(ctx, owner, repo, branch)
	if err != nil {
		return "", errors.Wrapf(err, "listing pull requests from %s", branch)
	}
	if len(open) > 0 {
		fmt.Fprintf(f.out, "The hand-off pull request of %s exists, %s: %s. One closed unmerged is reopened by hand.\n", h.Prefix, open[0].State, open[0].HTMLURL)

		return open[0].HTMLURL, nil
	}
	root, err := org.ReadPlacement(filepath.Join(f.req.RootDir, org.PlacementFile))
	if err != nil {
		return "", err
	}
	record := root.HostingOrganization(h.Prefix)
	if record == nil {
		return "", errors.Newf("placement.json no longer records %s (hostingOrganizations)", h.Prefix)
	}
	record.Folder = marker.OrganizationFolder
	placement, err := root.JSON()
	if err != nil {
		return "", err
	}
	created, err := h.JSON()
	if err != nil {
		return "", err
	}
	base, err := f.gh.Ref(ctx, owner, repo, "heads/"+root.GithubDefaultBranch)
	if err != nil {
		return "", errors.Wrapf(err, "reading %s of %s/%s", root.GithubDefaultBranch, owner, repo)
	}
	baseCommit, err := f.gh.GetCommit(ctx, owner, repo, base.Object.SHA)
	if err != nil {
		return "", errors.Wrapf(err, "reading commit %s", short(base.Object.SHA))
	}
	var entries []github.TreeEntry
	for path, content := range map[string][]byte{org.PlacementFile: placement, org.RecordsDir + "/" + h.Prefix + "/" + org.PlacementFile: created} {
		sha, err := f.gh.CreateBlob(ctx, owner, repo, content)
		if err != nil {
			return "", errors.Wrapf(err, "storing %s", path)
		}
		entries = append(entries, github.TreeEntry{Path: path, Mode: fileMode, Type: blobType, SHA: sha})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	tree, err := f.gh.CreateTree(ctx, owner, repo, baseCommit.Tree.SHA, entries)
	if err != nil {
		return "", errors.Wrap(err, "creating the hand-off tree")
	}
	title := "chore: " + h.Prefix + " is created (folder " + marker.OrganizationFolder + ", boot project " + marker.BootProject + ")"
	body := strings.Join([]string{
		"The creation of `" + h.Prefix + "` ran on " + marker.CreatedAt + " from " + short(f.req.Commit) + ", as the creating identity, and handed the organization off:",
		"",
		"- folder `" + marker.OrganizationFolder + "` at the organization node (named `" + h.Prefix + "`), with the `terraform` folder `" + marker.TerraformFolder + "` under it",
		"- boot project `" + marker.BootProject + "` (" + marker.BootProjectNumber + "), its APIs, its billing link, the boot identity with its roles on the folder, and the state bucket `" + marker.StateBucket + "`",
		"- the first apply of its `0-bootstrap` (the connection from the recorded authorization, the repository link, the two triggers) and of its `1-org` (the folders, the projects with their identities, roles and policies), with the projects recorded in its placement (`projects`, `projectNumbers`)",
		"- repository `" + marker.Repository + "`, with the rendered foundation as its first push, which its apply trigger runs on: every layer, as its own identity, with nothing left to apply by hand",
		"- the creating identity's own grants taken off everything it made; the break-glass account holds Folder Admin on the folder",
		"",
		"This pull request records the folder in `placement.json` (`hostingOrganizations`), from which 0-bootstrap grants the new boot identity the shared secrets' policy administration, and the placement as created under `" + org.RecordsDir + "/" + h.Prefix + "/`. Nothing under the folder is the root organization's to refresh. Left to a billing administrator: the new organization's spend budget (its `0-bootstrap/README.md`, \"Created by the root organization\"); its boot and org identities hold Billing Account User already, granted by the creation.",
	}, "\n")
	commit, err := f.gh.CreateCommit(ctx, owner, repo, title+"\n\n"+body, tree, []string{base.Object.SHA})
	if err != nil {
		return "", errors.Wrap(err, "creating the hand-off commit")
	}
	if err := f.gh.CreateRef(ctx, owner, repo, "refs/heads/"+branch, commit); err != nil {
		return "", errors.Wrapf(err, "creating branch %s", branch)
	}
	pr, err := f.gh.CreatePullRequest(ctx, owner, repo, github.PullRequestRequest{Title: title, Head: branch, Base: root.GithubDefaultBranch, Body: body})
	if err != nil {
		return "", errors.Wrapf(err, "opening the hand-off pull request from %s", branch)
	}
	fmt.Fprintf(f.out, "Opened the hand-off pull request of %s: %s.\n", h.Prefix, pr.HTMLURL)

	return pr.HTMLURL, nil
}
