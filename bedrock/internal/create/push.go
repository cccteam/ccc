// push.go is the creation's GitHub side: the new organization's infrastructure
// repository, made in the GitHub organization and given the rendered files as its
// first push (which its apply trigger, made by the first apply, runs on), and the
// hand-off pull request in the root organization's repository, which records the
// folder the creation made. Both as the infrastructure GitHub App, whose installation
// covers every repository of the organization.

package create

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/deploy"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/org"
	"github.com/cccteam/ccc/bedrock/internal/render"
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

		return full, DefaultBranch(ctx, f.gh, h.GithubOrganization, h.SourceRepo, h.GithubDefaultBranch, f.clients.Sleep, f.out)
	} else if github.Moved(err) {
		fmt.Fprintf(f.out, "The name %s forwards to a repository renamed away from it; a new repository takes the name.\n", full)
	} else if !github.NotFound(err) {
		return "", errors.Wrapf(err, "reading repository %s", full)
	}
	_, err := f.gh.CreateRepository(ctx, h.GithubOrganization, github.RepositoryRequest{
		Name: h.SourceRepo, Description: "The infrastructure of the " + h.Prefix + " organization (bedrock)", Private: true, AutoInit: true,
	})
	if err != nil {
		return "", errors.Wrapf(err, "creating repository %s", full)
	}
	fmt.Fprintf(f.out, "Created repository %s, private.\n", full)

	return full, DefaultBranch(ctx, f.gh, h.GithubOrganization, h.SourceRepo, h.GithubDefaultBranch, f.clients.Sleep, f.out)
}

// DefaultBranch brings the repository's default branch to the name the placement gives
// it: GitHub names a new repository's initial branch after the organization's setting,
// whatever the creation asks (main, in the lab, 2026-11-06), so the branch is renamed once
// its initial commit is there, which makes the new name the default. A repository whose
// default branch is the one named is left as it is.
func DefaultBranch(ctx context.Context, gh *github.Client, owner, repo, wanted string, sleep deploy.SleepFunc, out io.Writer) error {
	r, err := gh.Repository(ctx, owner, repo)
	if err != nil {
		return errors.Wrapf(err, "reading repository %s/%s", owner, repo)
	}
	if err := AwaitBranch(ctx, gh, owner, repo, r.DefaultBranch, sleep, out); err != nil {
		return err
	}
	if r.DefaultBranch == wanted {
		return nil
	}
	if err := gh.RenameBranch(ctx, owner, repo, r.DefaultBranch, wanted); err != nil {
		return errors.Wrapf(err, "renaming the branch %s of %s/%s to %s", r.DefaultBranch, owner, repo, wanted)
	}
	fmt.Fprintf(out, "Renamed the branch %s of %s/%s to %s, the default branch placement.json names.\n", r.DefaultBranch, owner, repo, wanted)

	return nil
}

// GitHub writes a new repository's initial commit a moment after it answers the creation,
// and the default branch's ref answers 404 until then (the lab, 2026-11-05): AwaitBranch
// reads the ref again, branchWait apart, up to branchTries.
const (
	branchWait  = 3 * time.Second
	branchTries = 20
)

// AwaitBranch waits for the branch of a repository made moments ago to exist, and answers
// the last refusal when it does not come.
func AwaitBranch(ctx context.Context, gh *github.Client, owner, repo, branch string, sleep deploy.SleepFunc, out io.Writer) error {
	for try := 1; ; try++ {
		_, err := gh.Ref(ctx, owner, repo, "heads/"+branch)
		if err == nil {
			return nil
		}
		if !github.NotFound(err) || try == branchTries {
			return errors.Wrapf(err, "waiting for %s of the new repository %s/%s", branch, owner, repo)
		}
		if try == 1 {
			fmt.Fprintf(out, "Waiting for GitHub to write the initial commit of %s/%s.\n", owner, repo)
		}
		if err := sleep(ctx, branchWait); err != nil {
			return err
		}
	}
}

// push puts the rendered files on the repository's default branch as one commit on its
// initial commit, unless the branch carries the placement already (a run that stopped
// after the push), and answers the commit.
func (f *creation) push(ctx context.Context, dir string, h *org.Placement) (string, error) {
	message := "chore: the foundation of " + h.Prefix + ", created by the root organization " + f.req.Root.Prefix + "\n\nRendered by bedrock org create from " + org.RecordsDir + "/" + h.Prefix + "/" + org.PlacementFile + " in " + f.req.Root.SourceRepo + " at " + short(f.req.Commit) + ". The apply trigger applies 0-bootstrap, then 1-org, from this push."

	return Push(ctx, f.gh, dir, h, message, f.out)
}

// Push puts the directory's files on the repository's branch as one commit on the
// branch's head, unless the branch carries this organization's placement.json already (a
// run that stopped after its push), and answers the commit. An earlier organization's
// foundation on the branch (one destroyed and started over in the same repository) is
// history, and the commit lands on it. The first applies' inits leave each layer's
// providers under .terraform and its lock file beside them: the lock file is committed,
// as a person commits the one their first init writes, the providers never are; a local
// state is never pushed either.
func Push(ctx context.Context, gh *github.Client, dir string, p *org.Placement, message string, out io.Writer) (string, error) {
	owner, repo, branch := p.GithubOrganization, p.SourceRepo, p.GithubDefaultBranch
	head, carries, earlier, err := Foundation(ctx, gh, p)
	if err != nil {
		return "", err
	}
	if carries {
		fmt.Fprintf(out, "%s of %s/%s carries the foundation already (%s).\n", branch, owner, repo, short(head))

		return head, nil
	}
	if earlier != "" {
		fmt.Fprintf(out, "%s of %s/%s carries the foundation of an earlier %s organization (state bucket %s), destroyed since; it stays as history, and this foundation is pushed on it.\n", branch, owner, repo, p.Prefix, earlier)
	}
	entries, err := blobs(ctx, gh, dir, owner, repo)
	if err != nil {
		return "", err
	}
	tree, err := gh.CreateTree(ctx, owner, repo, "", entries)
	if err != nil {
		return "", errors.Wrapf(err, "creating the tree of %d file(s) in %s/%s", len(entries), owner, repo)
	}
	commit, err := gh.CreateCommit(ctx, owner, repo, message, tree, []string{head})
	if err != nil {
		return "", errors.Wrapf(err, "creating the foundation commit in %s/%s", owner, repo)
	}
	if err := gh.UpdateRef(ctx, owner, repo, "heads/"+branch, commit, false); err != nil {
		return "", errors.Wrapf(err, "moving %s of %s/%s to %s", branch, owner, repo, short(commit))
	}
	fmt.Fprintf(out, "Pushed the foundation to %s/%s at %s (%d files): its apply trigger runs on it.\n", owner, repo, short(commit), len(entries))

	return commit, nil
}

// Foundation reads whether the branch of the repository carries this organization's
// placement, the mark of the foundation commit, and answers the branch's head either way:
// a run again pushes nothing then, and renders nothing into a clone of what it pushed. A
// placement with another state bucket on the branch is an earlier foundation of an
// organization destroyed and started over in the same repository (0-bootstrap/README.md,
// "Destroying the organization"): earlier names its state bucket, and the new foundation
// is pushed on that history.
func Foundation(ctx context.Context, gh *github.Client, p *org.Placement) (head string, carries bool, earlier string, err error) {
	owner, repo, branch := p.GithubOrganization, p.SourceRepo, p.GithubDefaultBranch
	ref, err := gh.Ref(ctx, owner, repo, "heads/"+branch)
	if err != nil {
		return "", false, "", errors.Wrapf(err, "reading %s of %s/%s", branch, owner, repo)
	}
	data, err := gh.Contents(ctx, owner, repo, org.PlacementFile, branch)
	if github.NotFound(err) {
		return ref.Object.SHA, false, "", nil
	}
	if err != nil {
		return "", false, "", errors.Wrapf(err, "reading %s on %s", org.PlacementFile, branch)
	}
	var carried struct {
		StateBucket string `json:"stateBucket"`
	}
	if err := json.Unmarshal(data, &carried); err != nil {
		return "", false, "", errors.Wrapf(err, "json.Unmarshal(): %s on %s of %s/%s", org.PlacementFile, branch, owner, repo)
	}
	if carried.StateBucket != p.StateBucket {
		return ref.Object.SHA, false, carried.StateBucket, nil
	}

	return ref.Object.SHA, true, "", nil
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
// the created organization's folder (placement.json, hostingOrganizations), the placement
// as created (organizations/<prefix>/placement.json) and the root organization's owned
// files rendered from the placement (its README.md, which bedrock org check compares),
// from a branch of its own. This creation's pull request from that branch (its title
// names the boot project) open is kept: its branch moves to this run's commit when what
// it records changed; one merged is left as it is; one closed unmerged is history and
// another is opened (a person who closes the hand-off takes the record out of
// placement.json to end it), as is one of an earlier organization by the same prefix,
// destroyed and started over; and a branch left by a run that stopped before its pull
// request is moved to this run's commit. It answers the pull request's URL. The branch
// and the pull request are the infrastructure app's, whose declaration carries the
// pull-request permission for it.
func (f *creation) handOff(ctx context.Context, h *org.Placement, marker *Marker) (string, error) {
	owner, repo := f.req.Root.GithubOrganization, f.req.Root.SourceRepo
	branch := handOffBranch + h.Prefix
	merged, open, err := f.handOffPullRequest(ctx, owner, repo, branch, h, marker)
	if err != nil || merged != "" {
		return merged, err
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
	handed := handOnward(root, h)
	files, err := handOffFiles(root, h)
	if err != nil {
		return "", err
	}
	if open != nil {
		same, err := f.sameAt(ctx, owner, repo, open.Head.SHA, files)
		if err != nil {
			return "", err
		}
		if same {
			fmt.Fprintf(f.out, "The hand-off pull request of %s is open and up to date: %s.\n", h.Prefix, open.HTMLURL)

			return open.HTMLURL, nil
		}
	}
	title, body := handOffTitle(h, marker), handOffBody(h, marker, handed, f.req.Commit)
	commit, err := f.handOffCommit(ctx, owner, repo, root.GithubDefaultBranch, files, title, body)
	if err != nil {
		return "", err
	}
	if open != nil {
		if err := f.gh.UpdateRef(ctx, owner, repo, "heads/"+branch, commit, true); err != nil {
			return "", errors.Wrapf(err, "moving branch %s to the hand-off commit", branch)
		}
		fmt.Fprintf(f.out, "The hand-off pull request of %s is open: %s; its branch %s moves to this run's commit %s, as what it records changed.\n", h.Prefix, open.HTMLURL, branch, short(commit))

		return open.HTMLURL, nil
	}
	if err := f.handOffBranch(ctx, owner, repo, branch, commit); err != nil {
		return "", err
	}
	pr, err := f.gh.CreatePullRequest(ctx, owner, repo, github.PullRequestRequest{Title: title, Head: branch, Base: root.GithubDefaultBranch, Body: body})
	if err != nil {
		return "", errors.Wrapf(err, "opening the hand-off pull request from %s", branch)
	}
	fmt.Fprintf(f.out, "Opened the hand-off pull request of %s: %s.\n", h.Prefix, pr.HTMLURL)

	return pr.HTMLURL, nil
}

// handOffFiles is what the hand-off writes, by path: the root placement with the folder
// recorded and the label handed onward, the placement as created, and the root
// organization's owned files rendered from the placement (bedrock org check compares
// them with it on the pull request); a seeded file (the journal) is a person's, left
// as it is.
func handOffFiles(root, h *org.Placement) (map[string][]byte, error) {
	placement, err := root.JSON()
	if err != nil {
		return nil, err
	}
	created, err := h.JSON()
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{org.PlacementFile: placement, org.RecordsDir + "/" + h.Prefix + "/" + org.PlacementFile: created}
	rendered, err := org.Render(root)
	if err != nil {
		return nil, errors.Wrap(err, "rendering the root organization for the hand-off")
	}
	for i := range rendered {
		if rendered[i].Tier != render.Owned {
			continue
		}
		files[rendered[i].Path] = rendered[i].Content
	}

	return files, nil
}

// sameAt says whether the files are, at ref, what the hand-off would write.
func (f *creation) sameAt(ctx context.Context, owner, repo, ref string, files map[string][]byte) (bool, error) {
	for path, content := range files {
		have, err := f.gh.Contents(ctx, owner, repo, path, ref)
		switch {
		case github.NotFound(err):
			return false, nil
		case err != nil:
			return false, errors.Wrapf(err, "reading %s at %s", path, short(ref))
		case !bytes.Equal(have, content):
			return false, nil
		}
	}

	return true, nil
}

// handOffCommit stores the files as a commit on the default branch's head and answers
// the commit's SHA.
func (f *creation) handOffCommit(ctx context.Context, owner, repo, defaultBranch string, files map[string][]byte, title, body string) (string, error) {
	base, err := f.gh.Ref(ctx, owner, repo, "heads/"+defaultBranch)
	if err != nil {
		return "", errors.Wrapf(err, "reading %s of %s/%s", defaultBranch, owner, repo)
	}
	baseCommit, err := f.gh.GetCommit(ctx, owner, repo, base.Object.SHA)
	if err != nil {
		return "", errors.Wrapf(err, "reading commit %s", short(base.Object.SHA))
	}
	entries := make([]github.TreeEntry, 0, len(files))
	for path, content := range files {
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
	commit, err := f.gh.CreateCommit(ctx, owner, repo, title+"\n\n"+body, tree, []string{base.Object.SHA})
	if err != nil {
		return "", errors.Wrap(err, "creating the hand-off commit")
	}

	return commit, nil
}

// handOffTitle is the hand-off pull request's title, which names the boot project: how
// a later run tells this creation's pull request from an earlier organization's.
func handOffTitle(h *org.Placement, marker *Marker) string {
	return "chore: " + h.Prefix + " is created (folder " + marker.OrganizationFolder + ", boot project " + marker.BootProject + ")"
}

// handOffBody is the hand-off pull request's body: what the creation made and handed off.
func handOffBody(h *org.Placement, marker *Marker, handed, commit string) string {
	return strings.Join([]string{
		"The creation of `" + h.Prefix + "` ran on " + marker.CreatedAt + " from " + short(commit) + ", as the creating identity, and handed the organization off:",
		"",
		"- folder `" + marker.OrganizationFolder + "` at the organization node (named `" + h.Prefix + "`), with the `terraform` folder `" + marker.TerraformFolder + "` under it",
		"- boot project `" + marker.BootProject + "` (" + marker.BootProjectNumber + "), its APIs, its billing link, the boot identity with its roles on the folder, and the state bucket `" + marker.StateBucket + "`",
		"- the first apply of its `0-bootstrap` (the connection from the recorded authorization, the repository link, the two triggers) and of its `1-org` (the folders, the projects with their identities and roles, the public-invoker tags), with the projects, the folders and the tags recorded in its placement (`projects`, `projectNumbers`, `folders`, `publicInvokerTags`); the organization-policy baseline on its folders is this organization's, set by this apply",
		"- the public zone of its apps domain `" + h.AppsDomain + "` in its net project (`" + zoneName(h) + "`, recorded in its placement as `appsZone`), made as the creating identity, which the root organization's apply verified as an owner of the domain in Google's webmaster verification, and adopted by its `2-net`; " + handed,
		"- repository `" + marker.Repository + "`, with the rendered foundation as its first push, which its apply trigger runs on: every layer, as its own identity, with nothing left to apply by hand",
		"- the creating identity's own grants taken off everything it made; the break-glass account holds Folder Admin on the folder",
		"",
		"This pull request records the folder in `placement.json` (`hostingOrganizations`), from which 0-bootstrap grants the new boot identity the shared secrets' policy administration, the placement as created under `" + org.RecordsDir + "/" + h.Prefix + "/`, and this organization's README.md rendered from the placement. Nothing under the folder is the root organization's to refresh. Left to a billing administrator: the new organization's spend budget (its `0-bootstrap/README.md`, \"Created by the root organization\"); its boot and org identities hold Billing Account User already, granted by the creation.",
	}, "\n")
}

// pullRequestOpen is the API's state of a pull request that is open.
const pullRequestOpen = "open"

// handOffPullRequest looks at the pull requests from the branch and answers this
// creation's (its title names the boot project): the URL of one merged, which stands,
// or one open, which the hand-off keeps up to date; neither when this creation opens
// one. One of this creation closed unmerged is history, as is any pull request of an
// earlier organization by the same prefix, destroyed and started over, which the
// branch's move would take over were it open, so an open one is refused.
func (f *creation) handOffPullRequest(ctx context.Context, owner, repo, branch string, h *org.Placement, marker *Marker) (string, *github.PullRequest, error) {
	prs, err := f.gh.PullRequestsFrom(ctx, owner, repo, branch)
	if err != nil {
		return "", nil, errors.Wrapf(err, "listing pull requests from %s", branch)
	}
	var open *github.PullRequest
	for i := range prs {
		pr := &prs[i]
		mine := strings.Contains(pr.Title, "boot project "+marker.BootProject)
		switch {
		case !mine && pr.State == pullRequestOpen:
			return "", nil, errors.Newf("the pull request %s from %s is an earlier %s organization's hand-off (its folder is gone), open: close it, and the next apply opens this creation's", pr.HTMLURL, branch, h.Prefix)
		case !mine:
			fmt.Fprintf(f.out, "The pull request %s from %s is an earlier %s organization's hand-off, %s, destroyed since; it stays as history, and this creation opens its own.\n", pr.HTMLURL, branch, h.Prefix, pullRequestState(pr))
		case pr.State == pullRequestOpen:
			open = pr
		case pr.MergedAt != "":
			fmt.Fprintf(f.out, "The hand-off pull request of %s was merged: %s.\n", h.Prefix, pr.HTMLURL)

			return pr.HTMLURL, nil, nil
		default:
			fmt.Fprintf(f.out, "The hand-off pull request of %s was closed unmerged: %s; it stays as history, and this creation opens another (to end the hand-off, take %s's record out of %s).\n", h.Prefix, pr.HTMLURL, h.Prefix, org.PlacementFile)
		}
	}

	return "", open, nil
}

// pullRequestState words a pull request's state: open, closed or merged.
func pullRequestState(pr *github.PullRequest) string {
	if pr.MergedAt != "" {
		return "merged"
	}

	return pr.State
}

// handOffBranch puts the hand-off branch at the commit: a branch a run that stopped
// between the branch and its pull request left is moved to this run's commit, and one
// that does not stand is created.
func (f *creation) handOffBranch(ctx context.Context, owner, repo, branch, commit string) error {
	stood, err := f.gh.Ref(ctx, owner, repo, "heads/"+branch)
	switch {
	case err == nil:
		if err := f.gh.UpdateRef(ctx, owner, repo, "heads/"+branch, commit, true); err != nil {
			return errors.Wrapf(err, "moving branch %s to the hand-off commit", branch)
		}
		fmt.Fprintf(f.out, "Branch %s stood at %s from an earlier run; moved to the hand-off commit %s.\n", branch, short(stood.Object.SHA), short(commit))

		return nil
	case github.NotFound(err):
		if err := f.gh.CreateRef(ctx, owner, repo, "refs/heads/"+branch, commit); err != nil {
			return errors.Wrapf(err, "creating branch %s", branch)
		}

		return nil
	default:
		return errors.Wrapf(err, "reading branch %s", branch)
	}
}

// zoneName names the apps zone the creation made, for the hand-off's body.
func zoneName(h *org.Placement) string {
	if h.AppsZone == nil {
		return "none"
	}

	return h.AppsZone.Name
}

// handOnward hands the apps domain onward in the root placement: when the apps domain is a
// label of a domain the root organization holds a zone for, the label is recorded under
// that domain with the new organization and its zone's name servers, from which the root
// organization's apply writes the NS record set. It answers the sentence the hand-off's
// body says about it.
func handOnward(root, h *org.Placement) string {
	if h.AppsZone == nil {
		return "no zone was recorded, so nothing is handed onward"
	}
	domain, label := labelOf(root, h.AppsDomain)
	if domain == "" {
		return "the apps domain is under no domain of this organization, so it is delegated where the domain is served (`bedrock domain check` in the new organization prints the records)"
	}
	d := root.Domains[domain]
	if d.Labels == nil {
		d.Labels = map[string]org.Delegation{}
	}
	d.Labels[label] = org.Delegation{Organization: h.Prefix, NameServers: h.AppsZone.NameServers}
	root.Domains[domain] = d

	return "this pull request hands the label `" + label + "` of `" + domain + "` onward to that zone (`domains` in `placement.json`), and the merge's apply writes the NS record set, so the apps domain resolves within the hour"
}

// labelOf is the domain the root organization holds a zone for that the name is under,
// the longest when several, and the name's label under it; empty when there is none.
func labelOf(root *org.Placement, name string) (domain, label string) {
	for d := range root.Domains {
		if strings.HasSuffix(name, "."+d) && len(d) > len(domain) {
			domain = d
		}
	}
	if domain == "" {
		return "", ""
	}

	return domain, strings.TrimSuffix(name, "."+domain)
}
