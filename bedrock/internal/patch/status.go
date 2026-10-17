// status.go reads what a patch line holds, for bedrock patch status and the release
// trigger's merge-back guard: the release tags the default branch cannot reach, each
// line's newest among them and its merge-back pull request, and a line's branch against
// its newest release.

package patch

import (
	"context"
	"sort"
	"strconv"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
)

// statusBehind is the comparison result that says the head commit is on the base.
const statusBehind = "behind"

// MergeBack is a patch line's newest release the default branch does not carry, and
// its merge-back pull request when one exists: merged, or open and not merged.
type MergeBack struct {
	// Line is the line's branch, patch/<major>.<minor>.x; Tag its newest release the
	// default branch cannot reach.
	Line string
	Tag  string
	// Number and URL are the merge-back pull request's (merge-back/<tag>), 0 and empty
	// when none exists; Merged says it is merged.
	Number int
	URL    string
	Merged bool
}

// MergeBacks lists, per patch line, the newest release tag the default branch cannot
// reach, with its merge-back pull request. A line's tags are never on the default
// branch, since the merge-back is a squash, so the merge-back pull request is what says a
// patch release came back; merging back a line's newest release covers the older ones,
// since the line is one branch. The merge-back guard refuses the default branch's
// release while any is not merged, and patch status prints them.
func MergeBacks(ctx context.Context, client *github.Client, owner, repo, defaultBranch string) ([]MergeBack, error) {
	tags, err := client.Tags(ctx, owner, repo)
	if err != nil {
		return nil, errors.Wrap(err, "listing tags")
	}
	newest := map[string]version{}
	for _, tag := range tags {
		v, err := parse(tag.Name)
		if err != nil {
			continue
		}
		cmp, err := client.Compare(ctx, owner, repo, defaultBranch, tag.Commit.SHA)
		if err != nil {
			return nil, errors.Wrapf(err, "comparing %s with %s", defaultBranch, tag.Name)
		}
		if cmp.Status == statusBehind || cmp.Status == statusIdentical {
			continue
		}
		if cur, ok := newest[v.line()]; !ok || v.patch > cur.patch {
			newest[v.line()] = v
		}
	}
	lines := make([]version, 0, len(newest))
	for _, v := range newest {
		lines = append(lines, v)
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].major != lines[j].major {
			return lines[i].major < lines[j].major
		}

		return lines[i].minor < lines[j].minor
	})
	backs := make([]MergeBack, 0, len(lines))
	for _, v := range lines {
		branch, _ := Branch(v.String())
		back := MergeBack{Line: branch, Tag: v.String()}
		prs, err := client.PullRequestsFrom(ctx, owner, repo, MergeBranch(v.String()))
		if err != nil {
			return nil, errors.Wrapf(err, "listing the pull requests from %s", MergeBranch(v.String()))
		}
		for i := range prs {
			pr := &prs[i]
			if pr.MergedAt != "" {
				back.Merged, back.Number, back.URL = true, pr.Number, pr.HTMLURL

				break
			}
			if back.Number == 0 {
				back.Number, back.URL = pr.Number, pr.HTMLURL
			}
		}
		backs = append(backs, back)
	}

	return backs, nil
}

// line is the version's release line, <major>.<minor>.
func (v version) line() string {
	return strconv.Itoa(v.major) + "." + strconv.Itoa(v.minor)
}

// LineState is a patch line as GitHub shows it: whether its branch exists and where its
// tip is, the line's newest release on the branch, and whether the tip has moved past
// that release, which says a patch release is pending (commits merged to the line that
// no release carries yet, or the line's release pull request in flight).
type LineState struct {
	Line   string
	Exists bool
	Tip    string
	// Tag is the line's newest release whose commit the branch carries, and TagCommit
	// its commit; the release the line started from when no patch is cut yet.
	Tag       string
	TagCommit string
	Pending   bool
}

// State reads the patch line of the release: the line of what production runs.
func State(ctx context.Context, client *github.Client, owner, repo, release string) (*LineState, error) {
	base, err := parse(release)
	if err != nil {
		return nil, err
	}
	branch, _ := Branch(release)
	st := &LineState{Line: branch}
	ref, err := client.Ref(ctx, owner, repo, "heads/"+branch)
	if err != nil {
		if github.NotFound(err) {
			return st, nil
		}

		return nil, errors.Wrapf(err, "looking for branch %s", branch)
	}
	st.Exists, st.Tip = true, ref.Object.SHA
	tags, err := client.Tags(ctx, owner, repo)
	if err != nil {
		return nil, errors.Wrap(err, "listing tags")
	}
	var newest version
	for _, tag := range tags {
		v, err := parse(tag.Name)
		if err != nil || v.major != base.major || v.minor != base.minor {
			continue
		}
		cmp, err := client.Compare(ctx, owner, repo, st.Tip, tag.Commit.SHA)
		if err != nil {
			return nil, errors.Wrapf(err, "comparing %s with %s", branch, tag.Name)
		}
		if cmp.Status != statusBehind && cmp.Status != statusIdentical {
			continue
		}
		if st.Tag == "" || v.patch > newest.patch {
			newest, st.Tag, st.TagCommit = v, tag.Name, tag.Commit.SHA
		}
	}
	st.Pending = st.Tag == "" || st.TagCommit != st.Tip

	return st, nil
}
