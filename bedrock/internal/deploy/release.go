package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/release"
)

// The substitutions the release checks read, beyond the record step's, and the words
// the log has always carried.
const (
	tagSub           = "TAG_NAME"
	repoFullNameSub  = "REPO_FULL_NAME"
	releaseActorsSub = "_RELEASE_ACTORS"
	defaultBranchSub = "_DEFAULT_BRANCH"
	// baseBranchSub is Cloud Build's substitution for a pull request's base branch:
	// the default branch, or a hotfix line.
	baseBranchSub = "_BASE_BRANCH"
	// recordsBucketsSub names each environment's deployment-records bucket, env=bucket
	// pairs in the promotion order, for the pull-request build's hotfix preview.
	recordsBucketsSub  = "_RECORDS_BUCKETS"
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

// skipped is a step's answer to a run an earlier step ended: the reason that step left
// (SKIP_REASON: a version run, whose migrate command printed the database's migration
// version and nothing else deploys), else the pull request's environment torn down.
func skipped(env map[string]string) string {
	if reason := env[skipReasonFact]; reason != "" {
		return reason
	}

	return tornDown
}

// skippedRecord is the record step's answer to the same.
func skippedRecord(env map[string]string) string {
	if reason := env[skipReasonFact]; reason != "" {
		return reason + " Nothing to record."
	}

	return "The pull request's environment was torn down: nothing to record."
}

// hotfixLineRE reads the release line of a v<major>.<minor>.<patch> tag.
var hotfixLineRE = regexp.MustCompile(`^v(\d+)\.(\d+)\.\d+$`)

// hotfixBranchRE is a hotfix line's branch, hotfix/<major>.<minor>.x, with the line.
var hotfixBranchRE = regexp.MustCompile(`^hotfix/(\d+\.\d+)\.x$`)

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
// traffic has shifted there. Last the maintenance window (windowCheck): whether the
// release turns away the one this environment runs, read from the release file in the
// checkout's router package (routerDir) and the environment's newest live record, and
// whether the run can wait for the window; what can never proceed is refused here,
// before anything is built. A pull-request build has no release to validate, and
// previews the window instead. A tag build first says which bedrock runs it (bedrock, the
// running version), and whether that is a commit pin: a release may be deployed by a
// bedrock built from an unreleased commit, and the log says so rather than refusing it.
func ValidateRelease(ctx context.Context, clients *Clients, w Workspace, bedrock, routerDir string, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if env[skipDeploy] == trueValue {
		fmt.Fprintln(out, skipped(env))

		return nil
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	subs := build.Substitutions
	if subs[tagSub] == "" {
		if subs[prNumberSub] != "" && hotfixBranchRE.MatchString(subs[baseBranchSub]) {
			if err := hotfixPreview(ctx, clients.StorageAs, subs, w, out); err != nil {
				return err
			}
		}
		if subs[prNumberSub] != "" {
			if err := windowPreview(ctx, clients.StorageAs, w, subs, routerDir, out); err != nil {
				return err
			}
		}
		fmt.Fprintln(out, "Pull-request build: no release to validate.")

		return nil
	}
	if release.IsCommitPin(bedrock) {
		fmt.Fprintf(out, "This tag build runs bedrock %s, a commit pin: bedrock built from an unreleased commit (placement.json bedrockVersion).\n", bedrock)
	} else {
		fmt.Fprintf(out, "This tag build runs bedrock %s.\n", bedrock)
	}
	// Every build carries the repository's token: deploy resolve mints it from the
	// connection the trigger reads the repository through, and every build starts from a
	// trigger. A build without one skips no check; it is refused.
	token := env["GITHUB_TOKEN"]
	if token == "" {
		return errors.New("GITHUB_TOKEN is empty in the environment file: deploy resolve mints it from the trigger's connection, and every build starts from a trigger")
	}
	window, hotfix, err := validateTag(ctx, clients.GitHub(token), subs, out)
	if err != nil {
		return err
	}
	if window {
		fmt.Fprintf(out, "Breaking changes: the release notes of %s carry a breaking-changes section (a commit with ! after its type, or a BREAKING CHANGE footer), recorded (%s); whether the release deploys inside the maintenance window is decided below from the release file's oldest answered release, not from the notes.\n", subs[tagSub], windowReleaseFact)
		if err := w.Append(map[string]string{windowReleaseFact: trueValue}); err != nil {
			return err
		}
	}
	if err := gate(ctx, clients.Storage, subs, out); err != nil {
		return err
	}
	switch {
	case hotfix == nil:
	case env[restoreFact] != "":
		fmt.Fprintf(out, "Restore run: %s's database is replaced before %s deploys, so what it holds is not compared with the hotfix.\n", subs[envSub], subs[tagSub])
	case env[rollbackFact] != "":
		fmt.Fprintf(out, "Rollback run: %s returns to %s on a database restored from a backup, so what it holds is not compared with the hotfix.\n", subs[envSub], subs[tagSub])
	default:
		if err := hotfixGate(ctx, clients.Storage, subs, w, hotfix, out); err != nil {
			return err
		}
	}

	return windowCheck(ctx, clients, w, build, env, routerDir, out)
}

// hotfixLine is what the tag check learned of a hotfix: its tag and its release line.
type hotfixLine struct {
	tag, line string
}

// hotfixGate is the line check at production's door, then the database check a hotfix
// passes in every environment. A hotfix is built from production's release, so an
// environment that ran a later release may hold a migration or seed file the hotfix does
// not carry, or one whose content differs; the hotfix's migration would fail on it,
// and the hotfix is refused with the restore named instead. The environment's newest
// live deployment record lists what its database holds, each file with its content's
// hash, and the build's checkout carries the hotfix's files. In production, the
// hotfix's line (major.minor) must be the line production runs, read from the same
// record, and that comes first: a hotfix from another line is refused for its line,
// whatever production's files, since production is never restored by a run. An
// environment with no live record holds nothing to compare.
func hotfixGate(ctx context.Context, open StoreFunc, subs map[string]string, w Workspace, h *hotfixLine, out io.Writer) error {
	env, bucket, app := subs[envSub], subs[recordsBucket], subs[appSub]
	store, err := open(ctx)
	if err != nil {
		return err
	}
	defer store.Close()
	live, err := newestLiveRelease(ctx, store, bucket, app, env)
	if err != nil {
		return err
	}
	if live == nil {
		fmt.Fprintf(out, "Hotfix check: %s has no live deployment record; nothing for %s to be behind.\n", env, h.tag)

		return nil
	}
	if env == prdEnvironment {
		line, problem := productionLine(live)
		if problem != "" {
			return errors.Newf("%s%s, so the line hotfix %s is on cannot be checked against it.", rejected, problem, h.tag)
		}
		if line != h.line {
			return errors.Newf("%sproduction runs %s, line %s; hotfix %s is on line %s. A hotfix is based on the release production runs.", rejected, live.Version, line, h.tag, h.line)
		}
	}
	behind, err := behindRecord(w, live, env, "hotfix "+h.tag, h.tag)
	if err != nil {
		return err
	}
	if behind != "" {
		return errors.Newf("%s%s", rejected, behind)
	}
	fmt.Fprintf(out, "Hotfix check passed: %s's database holds nothing %s does not carry (%d file(s) recorded by %s, build %s)\n", env, h.tag, len(live.Migrations), live.Version, live.Build)

	return nil
}

// behindRecord says what the environment's database holds, by its live record, that the
// files under the workspace do not carry: the refusal's sentence, or nothing. The record
// names the release live when it was written, not the one that applied each file, and
// the sentence says so. what names
// the candidate ("hotfix v1.2.4", "this pull request") and restoreTo what the environment
// is restored to ("v1.2.4", "the hotfix"); production, never restored by a run, is told
// to start the line from the release it runs instead.
func behindRecord(w Workspace, live *Record, env, what, restoreTo string) (string, error) {
	advice := restoreAdvice(env, restoreTo, live.Version)
	for _, m := range upFirst(live.Migrations) {
		hash, err := hashFile(filepath.Join(string(w), filepath.FromSlash(m.Dir), m.Name))
		if err != nil {
			return "", err
		}
		file := path.Join(m.Dir, m.Name)
		switch {
		case hash == "":
			return fmt.Sprintf("%s's database holds %s (in %s's record), which %s does not carry; %s", env, file, live.Version, what, advice), nil
		case hash != m.Hash:
			return fmt.Sprintf("%s's database holds %s with other content than %s carries (by %s's record); %s", env, file, what, live.Version, advice), nil
		}
	}

	return "", nil
}

// restoreAdvice is the sentence a refusal ends with: the restore of the environment to
// restoreTo, or, for production, which no run restores, the line started from the
// release production runs.
func restoreAdvice(env, restoreTo, production string) string {
	if env == prdEnvironment {
		return "production is never restored by a run: a hotfix is based on the release production runs, so start the line from " + production + "."
	}

	return "restore " + env + " to " + restoreTo + " first: a restore run replaces the database and skips this check."
}

// productionLine is the release line (major.minor) production runs, from its live
// record's version, or the problem when the version is not a release.
func productionLine(live *Record) (line, problem string) {
	m := hotfixLineRE.FindStringSubmatch(live.Version)
	if m == nil {
		return "", fmt.Sprintf("production's live record names %s, not a v<major>.<minor>.<patch> release", live.Version)
	}

	return m[1] + "." + m[2], ""
}

// seedDirBeside is the seed directory beside the schema migrations directory,
// root-relative: where a release's build applies the development seed from, and what
// tells a seed file in a record from a schema file.
func seedDirBeside(migrationsDir string) string {
	return path.Join(path.Dir(migrationsDir), derive.SeedDir)
}

// seedGone lists the seed files the environment's live record holds that the tree under
// root no longer carries as applied: edited, renumbered or removed since. A release's
// build restores the environment itself on this list (seedChanged), and the hotfix
// preview reads it first in a seeded environment, since that restore makes what the
// database holds nothing to the hotfix. Only the files in seedDir count; a schema file
// is the hotfix check's concern.
func seedGone(live *Record, seedDir, root string) ([]string, error) {
	var gone []string
	for _, m := range live.Migrations {
		if m.Dir != seedDir {
			continue
		}
		hash, err := hashFile(filepath.Join(root, filepath.FromSlash(m.Dir), m.Name))
		if err != nil {
			return nil, err
		}
		if hash != m.Hash {
			gone = append(gone, path.Join(m.Dir, m.Name))
		}
	}

	return gone, nil
}

// seedRestoreReason is why a release's build restores the environment when the seed
// changed, in the words the record carries (RESTORE_REASON) and the hotfix preview
// promises.
func seedRestoreReason(live *Record, gone []string) string {
	return fmt.Sprintf("the seed changed since %s applied it (build %s): %s, not in the tree as applied (edited, renumbered or removed since), so the database is recreated and the migrations and the seed apply from the start", live.Version, live.Build, strings.Join(gone, ", "))
}

// hotfixPreview is a pull-request build's look ahead for a fix on a hotfix line: what
// each environment's release check will say to the line's next release, read from the
// environment's live deployment record as that environment's plan identity (the
// identity the build already plans the environment as; _RECORDS_BUCKETS and
// _PLAN_IDENTITIES name the buckets and identities). In an environment the placement's
// seed list names, the placement read from the pull request's tree, the seed rule is
// read first: a seed the tree no longer carries as applied makes the release's build
// restore the environment itself, and the hotfix is taken. It warns and never refuses:
// the developer learns before the merge that a restore comes first, and where, or that
// the release's build makes it.
func hotfixPreview(ctx context.Context, open StoreAsFunc, subs map[string]string, w Workspace, out io.Writer) error {
	placement, err := checkoutPlacement(w, "the seed list is written")
	if err != nil {
		return err
	}
	p := &hotfixPreviewer{open: open, w: w, app: subs[appSub], line: subs[baseBranchSub], seedDir: seedDirBeside(subs[migrationsSub]), seeded: placement.SeedEnvironments()}
	buckets, identities := pairs(subs[recordsBucketsSub]), pairs(subs[planIdentitiesSub])
	fmt.Fprintf(out, "Hotfix preview: this pull request is against %s, and each environment's release check will say this to the line's next release, read from the environment's live deployment record:\n", p.line)
	for _, env := range strings.Split(subs[environmentsSub], ",") {
		if env == "" {
			continue
		}
		bucket, identity := buckets[env], identities[env]
		if bucket == "" || identity == "" {
			fmt.Fprintf(out, "  %s: its records bucket or plan identity is not named (%s, %s); nothing read.\n", env, recordsBucketsSub, planIdentitiesSub)

			continue
		}
		answer, err := p.environment(ctx, env, bucket, identity)
		if err != nil {
			fmt.Fprintf(out, "  %s: its records could not be read as %s: %v\n", env, identity, err)

			continue
		}
		fmt.Fprintf(out, "  %s\n", answer)
	}

	return nil
}

// hotfixPreviewer is what every environment's answer in the hotfix preview reads: the
// records as each environment's plan identity, the pull request's tree, the line the
// pull request is against, the seed directory beside the schema migrations and the
// environments the tree's placement seeds.
type hotfixPreviewer struct {
	open      StoreAsFunc
	w         Workspace
	app, line string
	seedDir   string
	seeded    []string
}

// environment is one environment's answer. Production's door comes first: the line it
// runs. Then, in a seeded environment whose record holds a seed file the tree no longer
// carries as applied, the answer is the seed rule's: the release's build restores the
// environment itself and takes the hotfix, whatever else the database holds. Last, the
// files the database holds against the tree, as the release check compares them.
func (p *hotfixPreviewer) environment(ctx context.Context, env, bucket, identity string) (string, error) {
	live, err := previewLive(ctx, p.open, p.app, env, bucket, identity)
	if err != nil {
		return "", err
	}
	if live == nil {
		return env + ": no live deployment record; nothing to be behind.", nil
	}
	if env == prdEnvironment {
		l, problem := productionLine(live)
		if problem != "" {
			return fmt.Sprintf("%s: %s, so the line cannot be checked against it.", env, problem), nil
		}
		if "hotfix/"+l+".x" != p.line {
			return fmt.Sprintf("%s: WILL REFUSE the hotfix at production's door: production runs %s, line %s; this hotfix is on %s. A hotfix is based on the release production runs.", env, live.Version, l, p.line), nil
		}
	}
	if slices.Contains(p.seeded, env) {
		gone, err := seedGone(live, p.seedDir, string(p.w))
		if err != nil {
			return "", err
		}
		if len(gone) > 0 {
			return fmt.Sprintf("%s: would take the hotfix, after the restore the release's build decides itself: %s, and what the database holds is not compared with the hotfix.", env, seedRestoreReason(live, gone)), nil
		}
	}
	behind, err := behindRecord(p.w, live, env, "this pull request", "the hotfix")
	if err != nil {
		return "", err
	}
	if behind != "" {
		return env + ": WILL REFUSE the hotfix: " + behind, nil
	}

	return fmt.Sprintf("%s: would take the hotfix; its database holds nothing this pull request does not carry (%d file(s) recorded by %s, build %s).", env, len(live.Migrations), live.Version, live.Build), nil
}

// pairs reads a substitution of env=value pairs, comma-separated.
func pairs(list string) map[string]string {
	values := map[string]string{}
	for _, pair := range strings.Split(list, ",") {
		if key, value, ok := strings.Cut(pair, "="); ok && key != "" {
			values[key] = value
		}
	}

	return values
}

// upFirst orders a record's migrations so that up files come before down files: the
// hotfix check names the first file the hotfix does not carry, and the up file is the one
// the database applied; a down file beside it is in the same case and would be named
// first only by its name's order.
func upFirst(migrations []Migration) []Migration {
	ordered := make([]Migration, 0, len(migrations))
	for _, m := range migrations {
		if !strings.HasSuffix(m.Name, ".down.sql") {
			ordered = append(ordered, m)
		}
	}
	for _, m := range migrations {
		if strings.HasSuffix(m.Name, ".down.sql") {
			ordered = append(ordered, m)
		}
	}

	return ordered
}

// windowReleaseFact says the release's notes carry release-please's breaking-changes
// section, the designation an application repository has for a change that is not safe
// on the running service. It is recorded for a reader of the facts; whether the release
// waits for the maintenance window and deploys behind the maintenance page is decided
// from the release file's oldest answered release (windowCheck), never from the notes.
const windowReleaseFact = "WINDOW_RELEASE"

// windowRelease reads the release notes for release-please's breaking-changes heading.
func windowRelease(notes string) bool {
	for _, line := range strings.Split(notes, "\n") {
		heading := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
		heading = strings.TrimSpace(strings.TrimPrefix(heading, "⚠"))
		if strings.HasPrefix(strings.ToUpper(heading), "BREAKING CHANGES") {
			return true
		}
	}

	return false
}

// validateTag is the two GitHub checks: the release and its actor, then the commit's
// place on the default branch or a hotfix line. It answers whether the release notes
// carry a breaking-changes section, and the hotfix's line when the tag is a hotfix.
func validateTag(ctx context.Context, gh *github.Client, subs map[string]string, out io.Writer) (window bool, line *hotfixLine, err error) {
	owner, repo, err := splitRepo(subs[repoFullNameSub])
	if err != nil {
		return false, nil, err
	}
	tag, commit, branch := subs[tagSub], subs[commitSub], subs[defaultBranchSub]
	cut, err := gh.Release(ctx, owner, repo, tag)
	if err != nil {
		var apiErr *github.Error
		if errors.As(err, &apiErr) {
			return false, nil, errors.Newf("%stag %s has no GitHub Release (HTTP %d); a release is cut by release-please as the release app, never by a tag alone.", rejected, tag, apiErr.Status)
		}

		return false, nil, err
	}
	actors := strings.Split(subs[releaseActorsSub], ",")
	if !slices.Contains(actors, cut.Author.Login) {
		return false, nil, errors.Newf("%sthe GitHub Release for %s was made by %s, not by an accepted release actor (%s).", rejected, tag, cut.Author.Login, subs[releaseActorsSub])
	}
	fmt.Fprintf(out, "Release %s validated: cut by %s\n", tag, cut.Author.Login)
	window = windowRelease(cut.Body)
	comparison, err := gh.Compare(ctx, owner, repo, commit, branch)
	if err != nil {
		return false, nil, err
	}
	if comparison.Status == statusAhead || comparison.Status == statusIdentical {
		fmt.Fprintf(out, "Tag %s validated: its commit is on %s\n", tag, branch)

		return window, nil, nil
	}
	line, err = validateHotfix(ctx, gh, &hotfixCandidate{owner: owner, repo: repo, tag: tag, commit: commit, branch: branch, status: comparison.Status}, out)
	if err != nil {
		return false, nil, err
	}

	return window, line, nil
}

// hotfixCandidate is a tag whose commit is not on the default branch.
type hotfixCandidate struct {
	owner, repo, tag, commit, branch, status string
}

// validateHotfix accepts the tag as a hotfix release: a v<major>.<minor>.<patch> tag at
// the tip of hotfix/<major>.<minor>.x, a branch whose base on the default branch carries
// a v<major>.<minor>.* release tag. It answers the tag and its line.
func validateHotfix(ctx context.Context, gh *github.Client, c *hotfixCandidate, out io.Writer) (*hotfixLine, error) {
	m := hotfixLineRE.FindStringSubmatch(c.tag)
	if m == nil {
		return nil, errors.Newf("%stag %s is not on %s (compare status: %s) and is not a v<major>.<minor>.<patch> tag a hotfix line could carry.", rejected, c.tag, c.branch, c.status)
	}
	line := m[1] + "." + m[2]
	hotfixBranch := "hotfix/" + line + ".x"
	ref, err := gh.Ref(ctx, c.owner, c.repo, "heads/"+hotfixBranch)
	if err != nil {
		if github.NotFound(err) {
			return nil, errors.Newf("%stag %s is not on %s (compare status: %s) and there is no branch %s it could be a hotfix of.", rejected, c.tag, c.branch, c.status, hotfixBranch)
		}

		return nil, err
	}
	if ref.Object.SHA != c.commit {
		return nil, errors.Newf("%stag %s (commit %s) is not at the tip of %s (%s); a hotfix release is the branch's tip.", rejected, c.tag, c.commit, hotfixBranch, ref.Object.SHA)
	}
	comparison, err := gh.Compare(ctx, c.owner, c.repo, c.branch, c.commit)
	if err != nil {
		return nil, err
	}
	base := comparison.MergeBaseCommit.SHA
	tags, err := gh.Tags(ctx, c.owner, c.repo)
	if err != nil {
		return nil, err
	}
	baseTag := ""
	for _, t := range tags {
		if t.Commit.SHA == base && strings.HasPrefix(t.Name, "v"+line+".") {
			baseTag = t.Name

			break
		}
	}
	if baseTag == "" {
		return nil, errors.Newf("%s%s branches from %s on %s, which carries no v%s.* release tag; a hotfix line starts at a release.", rejected, hotfixBranch, base, c.branch, line)
	}
	fmt.Fprintf(out, "Tag %s validated as a hotfix: the tip of %s, branched from release %s on %s\n", c.tag, hotfixBranch, baseTag, c.branch)

	return &hotfixLine{tag: c.tag, line: line}, nil
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
