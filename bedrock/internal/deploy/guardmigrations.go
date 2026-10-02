// guardmigrations.go is the migration guard: the deploy gate for the rule bedrock check
// and the schema-protection workflow apply before the merge.

package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/migration"
)

// GuardMigrations refuses a build whose migrations the migrate command could not apply
// in order, before anything is touched. The schema migrations directory and the seed
// directory beside it (schema/devseed) are each one sequence by the rule
// (migration.Sequence). In a pull-request build two more things hold for the schema
// migrations: every one the branch started from (the merge base with the default branch)
// is still in the tree, unchanged, since a committed schema migration never changes; and
// the sequence is read over the tree together with the default branch's, so an index the
// default branch has taken since the branch was cut is refused now, not after the merge.
// Seed files are development data: a pull request may edit or remove one, and the seed
// directory keeps only the sequence rule (a removal renumbers the files after it), read
// together with the files the default branch added since the branch was cut. A changed
// seed applies from the start by recreating the database, which the resolve step does
// for a pull request's on its next build (staleDatabase). A refusal lists every problem,
// names the fix (bedrock migration renumber, which go generate runs) and is posted on the
// pull request. A torn-down environment, a teardown and an application without a schema
// have nothing to guard.
func GuardMigrations(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	subs := build.Substitutions
	switch {
	case env[skipDeploy] == trueValue:
		fmt.Fprintln(out, tornDown)

		return nil
	case env[downFact] == trueValue:
		fmt.Fprintln(out, "Teardown: nothing to check.")

		return nil
	case subs[migrationsSub] == "":
		fmt.Fprintln(out, "No schema migrations: nothing to check.")

		return nil
	}
	// A pull request's migrations are compared with its base branch's: the default
	// branch, or the hotfix line the fix is for, which is behind the default branch on
	// purpose. A tag build has no base branch and reads the default branch's.
	branch := subs[baseBranchSub]
	if branch == "" {
		branch = subs[defaultBranchSub]
	}
	g := &migrationGuard{w: w, branch: branch}
	if subs[prNumberSub] != "" {
		if err := g.branchedFrom(ctx, clients, subs, env); err != nil {
			return err
		}
	}
	dir := subs[migrationsSub]
	for _, d := range []string{dir, path.Join(path.Dir(dir), derive.SeedDir)} {
		if err := g.guard(ctx, d, d != dir, out); err != nil {
			return err
		}
	}
	if len(g.problems) == 0 {
		against := ""
		if g.base != "" {
			against = ", the committed schema migrations unchanged against " + g.branch
		}
		fmt.Fprintf(out, "Guard passed: the migrations form one sequence%s.\n", against)

		return nil
	}

	return g.refuse(ctx, clients, build, env, out)
}

// migrationGuard is one guard's reading: the branch the migrations are compared with
// (the pull request's base branch, or the default branch in a tag build), the commit
// the pull request branched from (empty in a tag build) and the problems found so far.
type migrationGuard struct {
	w           Workspace
	gh          *github.Client
	owner, repo string
	branch      string
	base        string
	problems    []string
}

// The guard's refusal, and its fix, as the log and the pull request read them.
const (
	sequenceRule   = "the migrations are not one sequence the migrate command can apply in order (six-digit indexes, one up file each, contiguous; a committed schema migration never changes)"
	renumberFixFmt = "The fix for an index %[1]s has taken, or a gap: `bedrock migration renumber` on the branch (`go generate ./...` runs it) moves the pull request's own migrations, up and down together, to follow %[1]s's highest index with no gap, and the seed files after a removed one down; a committed schema migration is never renumbered."
)

// branchedFrom finds the commit the pull request branched from, through the GitHub API
// with the repository's token.
func (g *migrationGuard) branchedFrom(ctx context.Context, clients *Clients, subs, env map[string]string) error {
	owner, repo, err := splitRepo(subs[repoFullNameSub])
	if err != nil {
		return err
	}
	g.gh, g.owner, g.repo = clients.GitHub(env[githubTokenFact]), owner, repo
	cmp, err := g.gh.Compare(ctx, owner, repo, g.branch, subs[commitSub])
	if err != nil {
		return err
	}
	if cmp.MergeBaseCommit.SHA == "" {
		return errors.Newf("%sGitHub gave no merge base for %s...%s", rejected, g.branch, subs[commitSub])
	}
	g.base = cmp.MergeBaseCommit.SHA

	return nil
}

// guard reads one directory: for the schema migrations of a pull request, the committed
// files still there unchanged; then the sequence over the tree and, for a pull request,
// the default branch's names. seed says the directory is the seed's: its files may change
// or go, so only the names the default branch added since the merge base join the
// sequence, not the ones the pull request removed.
func (g *migrationGuard) guard(ctx context.Context, dir string, seed bool, out io.Writer) error {
	local := filepath.Join(string(g.w), filepath.FromSlash(dir))
	info, err := os.Stat(local)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return nil
	case err != nil:
		return errors.Wrap(err, "os.Stat()")
	case !info.IsDir():
		return nil
	}
	names, err := fileNames(local)
	if err != nil {
		return err
	}
	entries := make([]migration.Entry, 0, len(names))
	for _, name := range names {
		entries = append(entries, migration.Entry{Name: name})
	}
	if g.base != "" {
		if !seed {
			if err := g.unchanged(ctx, dir, local); err != nil {
				return err
			}
		}
		committed, err := g.listing(ctx, dir, g.branch)
		if err != nil {
			return err
		}
		removed := map[string]bool{}
		if seed {
			atBase, err := g.listing(ctx, dir, g.base)
			if err != nil {
				return err
			}
			for _, e := range atBase {
				removed[e.Name] = !slices.Contains(names, e.Name)
			}
		}
		for _, e := range committed {
			if slices.Contains(names, e.Name) || removed[e.Name] {
				continue
			}
			entries = append(entries, migration.Entry{Name: e.Name, Note: "on " + g.branch + ", not in this pull request"})
		}
	}
	problems, span := migration.Sequence(entries)
	for _, p := range problems {
		where := dir
		if p.File != "" {
			where = path.Join(dir, p.File)
			if p.Note != "" {
				where += " (" + p.Note + ")"
			}
		}
		g.problems = append(g.problems, where+": "+p.Text)
	}
	switch {
	case span.Count == 0 && span.High == 0:
		fmt.Fprintf(out, "%s: no migrations.\n", dir)
	case g.base != "":
		fmt.Fprintf(out, "%s: %d migration(s), %06d to %06d, read together with %s.\n", dir, span.Count, span.Low, span.High, g.branch)
	default:
		fmt.Fprintf(out, "%s: %d migration(s), %06d to %06d.\n", dir, span.Count, span.Low, span.High)
	}

	return nil
}

// unchanged holds every schema migration the pull request branched from against the
// tree: still there, and the same content (by git's blob name, which the API lists).
func (g *migrationGuard) unchanged(ctx context.Context, dir, local string) error {
	committed, err := g.listing(ctx, dir, g.base)
	if err != nil {
		return err
	}
	for _, e := range committed {
		data, err := os.ReadFile(filepath.Join(local, e.Name))
		switch {
		case errors.Is(err, os.ErrNotExist):
			g.problems = append(g.problems, path.Join(dir, e.Name)+": removed or renamed; a committed migration never changes")
		case err != nil:
			return errors.Wrap(err, "os.ReadFile()")
		case migration.BlobSHA(data) != e.SHA:
			g.problems = append(g.problems, path.Join(dir, e.Name)+": modified; a committed migration never changes, a new one follows it")
		}
	}

	return nil
}

// listing is the files of the directory at ref; none when the ref has no such directory.
func (g *migrationGuard) listing(ctx context.Context, dir, ref string) ([]github.DirEntry, error) {
	entries, err := g.gh.Directory(ctx, g.owner, g.repo, dir, ref)
	if err != nil {
		if github.NotFound(err) {
			return nil, nil
		}

		return nil, err
	}
	files := make([]github.DirEntry, 0, len(entries))
	for _, e := range entries {
		if e.Type == "file" {
			files = append(files, e)
		}
	}

	return files, nil
}

// refuse lists the problems, names the fix, posts both on the pull request and stops
// the build.
func (g *migrationGuard) refuse(ctx context.Context, clients *Clients, build *Build, env map[string]string, out io.Writer) error {
	list := "  " + strings.Join(g.problems, "\n  ") + "\n"
	fmt.Fprintf(out, "Build REJECTED: %s:\n%s", sequenceRule, list)
	if build.Substitutions[prNumberSub] == "" {
		return errors.New(rejected + sequenceRule)
	}
	fix := fmt.Sprintf(renumberFixFmt, g.branch)
	fmt.Fprintln(out, strings.ReplaceAll(fix, "`", ""))
	pr, err := speaker(ctx, clients, build, env, true)
	if err != nil {
		return err
	}
	if pr != nil {
		body := fmt.Sprintf("The pull-request build stopped: %s. Build %s:\n\n```\n%s```\n%s", sequenceRule, build.ID, list, fix)
		if err := pr.comment(ctx, body, out); err != nil {
			return err
		}
	}

	return errors.New(rejected + sequenceRule)
}

// fileNames are the names of the regular files directly in dir, sorted.
func fileNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.Wrapf(err, "os.ReadDir(): %s", dir)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}

	return names, nil
}
