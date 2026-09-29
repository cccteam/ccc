package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/release"
)

// The substitutions the release checks read, beyond the record step's, and the words
// the log has always carried.
const (
	tagSub             = "TAG_NAME"
	repoFullNameSub    = "REPO_FULL_NAME"
	releaseActorsSub   = "_RELEASE_ACTORS"
	defaultBranchSub   = "_DEFAULT_BRANCH"
	previousEnvSub     = "_PREVIOUS_ENV"
	previousRecordsSub = "_PREVIOUS_RECORDS_BUCKET"
	// rejected starts every refusal a release check makes.
	rejected = "Build REJECTED: "
	// tornDown is a step's answer to a pull-request environment torn down earlier.
	tornDown = "The pull request's environment was torn down: nothing to do."
	// The comparison statuses that put a commit on a branch: the branch is at the commit,
	// or has moved past it.
	statusAhead     = "ahead"
	statusIdentical = "identical"
)

// hotfixLineRE reads the release line of a v<major>.<minor>.<patch> tag.
var hotfixLineRE = regexp.MustCompile(`^v(\d+)\.(\d+)\.\d+$`)

// GitHubFunc opens the GitHub client with the token the resolve step minted: the public
// API, or a stand-in in tests.
type GitHubFunc func(token string) *github.Client

// PublicGitHub is the client over the public API.
func PublicGitHub(token string) *github.Client {
	return github.New(github.DefaultBase, token)
}

// splitRepo reads <organization>/<repository>.
func splitRepo(fullName string) (owner, repo string, err error) {
	owner, repo, ok := strings.Cut(fullName, "/")
	if !ok || owner == "" || repo == "" {
		return "", "", errors.Newf("REPO_FULL_NAME %q is not <organization>/<repository>", fullName)
	}

	return owner, repo, nil
}

// ValidateRelease is what stands between a tag and a build. Two checks through the
// GitHub API: the tag belongs to a GitHub Release cut by an accepted release actor
// (_RELEASE_ACTORS, comma-separated logins: the release app as <slug>[bot]), which is
// how release-please, and nothing else, makes a release; and the tagged commit is on the
// default branch, or it is a hotfix, the tip of the line's branch hotfix/<major>.<minor>.x
// whose base on the default branch carries a release tag of the same line. Then the
// record gate: this environment follows the previous one in the promotion order
// (_PREVIOUS_ENV, empty in the first), and a release runs here only after the previous
// environment holds a live deployment record of it, which its record step writes once
// traffic has shifted there. A pull-request build has no release to validate; a
// hand-submitted build without a connection (no token) skips the GitHub checks. A tag
// build first says which bedrock runs it (bedrock, the running version), and whether that
// is a commit pin: a release may be deployed by a bedrock built from an unreleased commit,
// and the log says so rather than refusing it.
func ValidateRelease(ctx context.Context, clients *Clients, w Workspace, bedrock string, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if env[skipDeploy] == trueValue {
		fmt.Fprintln(out, tornDown)

		return nil
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	subs := build.Substitutions
	if subs[tagSub] == "" {
		fmt.Fprintln(out, "Pull-request build: no release to validate.")

		return nil
	}
	if release.IsCommitPin(bedrock) {
		fmt.Fprintf(out, "This tag build runs bedrock %s, a commit pin: bedrock built from an unreleased commit (placement.json bedrockVersion).\n", bedrock)
	} else {
		fmt.Fprintf(out, "This tag build runs bedrock %s.\n", bedrock)
	}
	if token := env["GITHUB_TOKEN"]; token != "" {
		if err := validateTag(ctx, clients.GitHub(token), subs, out); err != nil {
			return err
		}
	}

	return gate(ctx, clients.Storage, subs, out)
}

// validateTag is the two GitHub checks: the release and its actor, then the commit's
// place on the default branch or a hotfix line.
func validateTag(ctx context.Context, gh *github.Client, subs map[string]string, out io.Writer) error {
	owner, repo, err := splitRepo(subs[repoFullNameSub])
	if err != nil {
		return err
	}
	tag, commit, branch := subs[tagSub], subs[commitSub], subs[defaultBranchSub]
	cut, err := gh.Release(ctx, owner, repo, tag)
	if err != nil {
		var apiErr *github.Error
		if errors.As(err, &apiErr) {
			return errors.Newf("%stag %s has no GitHub Release (HTTP %d); a release is cut by release-please as the release app, never by a tag alone.", rejected, tag, apiErr.Status)
		}

		return err
	}
	actors := strings.Split(subs[releaseActorsSub], ",")
	if !slices.Contains(actors, cut.Author.Login) {
		return errors.Newf("%sthe GitHub Release for %s was made by %s, not by an accepted release actor (%s).", rejected, tag, cut.Author.Login, subs[releaseActorsSub])
	}
	fmt.Fprintf(out, "Release %s validated: cut by %s\n", tag, cut.Author.Login)
	comparison, err := gh.Compare(ctx, owner, repo, commit, branch)
	if err != nil {
		return err
	}
	if comparison.Status == statusAhead || comparison.Status == statusIdentical {
		fmt.Fprintf(out, "Tag %s validated: its commit is on %s\n", tag, branch)

		return nil
	}

	return validateHotfix(ctx, gh, &hotfixCandidate{owner: owner, repo: repo, tag: tag, commit: commit, branch: branch, status: comparison.Status}, out)
}

// hotfixCandidate is a tag whose commit is not on the default branch.
type hotfixCandidate struct {
	owner, repo, tag, commit, branch, status string
}

// validateHotfix accepts the tag as a hotfix release: a v<major>.<minor>.<patch> tag at
// the tip of hotfix/<major>.<minor>.x, a branch whose base on the default branch carries
// a v<major>.<minor>.* release tag.
func validateHotfix(ctx context.Context, gh *github.Client, c *hotfixCandidate, out io.Writer) error {
	m := hotfixLineRE.FindStringSubmatch(c.tag)
	if m == nil {
		return errors.Newf("%stag %s is not on %s (compare status: %s) and is not a v<major>.<minor>.<patch> tag a hotfix line could carry.", rejected, c.tag, c.branch, c.status)
	}
	line := m[1] + "." + m[2]
	hotfixBranch := "hotfix/" + line + ".x"
	ref, err := gh.Ref(ctx, c.owner, c.repo, "heads/"+hotfixBranch)
	if err != nil {
		if github.NotFound(err) {
			return errors.Newf("%stag %s is not on %s (compare status: %s) and there is no branch %s it could be a hotfix of.", rejected, c.tag, c.branch, c.status, hotfixBranch)
		}

		return err
	}
	if ref.Object.SHA != c.commit {
		return errors.Newf("%stag %s (commit %s) is not at the tip of %s (%s); a hotfix release is the branch's tip.", rejected, c.tag, c.commit, hotfixBranch, ref.Object.SHA)
	}
	comparison, err := gh.Compare(ctx, c.owner, c.repo, c.branch, c.commit)
	if err != nil {
		return err
	}
	base := comparison.MergeBaseCommit.SHA
	tags, err := gh.Tags(ctx, c.owner, c.repo)
	if err != nil {
		return err
	}
	baseTag := ""
	for _, t := range tags {
		if t.Commit.SHA == base && strings.HasPrefix(t.Name, "v"+line+".") {
			baseTag = t.Name

			break
		}
	}
	if baseTag == "" {
		return errors.Newf("%s%s branches from %s on %s, which carries no v%s.* release tag; a hotfix line starts at a release.", rejected, hotfixBranch, base, c.branch, line)
	}
	fmt.Fprintf(out, "Tag %s validated as a hotfix: the tip of %s, branched from release %s on %s\n", c.tag, hotfixBranch, baseTag, c.branch)

	return nil
}

// gate is the record gate: the previous environment's records bucket holds a live record
// of the release, or the build stops.
func gate(ctx context.Context, open StoreFunc, subs map[string]string, out io.Writer) error {
	previous, tag := subs[previousEnvSub], subs[tagSub]
	if previous == "" {
		return nil
	}
	store, err := open(ctx)
	if err != nil {
		return err
	}
	defer store.Close()
	bucket := subs[previousRecordsSub]
	prefix := subs[appSub] + "/" + previous + "/" + tag + "/"
	objects, err := store.List(ctx, bucket, prefix)
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		return errors.Newf("%srelease %s has no deployment record in %s (nothing under gs://%s/%s); a release reaches %s after it is live in %s.", rejected, tag, previous, bucket, prefix, subs[envSub], previous)
	}
	var live *Record
	for _, object := range objects {
		data, err := store.Read(ctx, bucket, object)
		if err != nil {
			return err
		}
		var record Record
		if err := json.Unmarshal(data, &record); err != nil {
			return errors.Wrapf(err, "json.Unmarshal(): gs://%s/%s", bucket, object)
		}
		if record.Status == Live {
			live = &record
		}
	}
	if live == nil {
		return errors.Newf("%srelease %s has deployment records in %s but none is live: traffic never shifted to it there.", rejected, tag, previous)
	}
	fmt.Fprintf(out, "Gate passed: %s is live in %s (since %s, build %s)\n", tag, previous, live.Timestamp, live.Build)

	return nil
}
