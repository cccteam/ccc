// migration.go is the migration command group: the schema migrations as the pipeline
// reads them, one sequence, and the renumber that keeps a branch's own files in it.

package cli

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/migration"
	"github.com/cccteam/ccc/bedrock/internal/where"
	"github.com/cccteam/ccc/impulse/app"
)

// migrationUse names the command group.
const migrationUse = "migration"

// newMigration is the command group over the schema migrations.
func newMigration(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   migrationUse,
		Short: "The schema migrations, as one sequence the pipeline applies in order",
		Long: `migration holds what keeps the schema migrations, and the seed migrations beside them, one
sequence the migrate command applies in order: six-digit indexes, one up file each, contiguous,
and a committed schema migration never changes (a seed file is development data and may). bedrock
check and the pipeline's guard refuse a directory that is not; renumber moves a branch's own
migrations back into the sequence, and the seed files after a removed one down.`,
	}
	cmd.AddCommand(newMigrationRenumber(d))

	return cmd
}

// newMigrationRenumber is migration renumber.
func newMigrationRenumber(d deps) *cobra.Command {
	var (
		appFlag   string
		dirFlag   string
		placement string
	)

	cmd := &cobra.Command{
		Use:   "renumber [--app <dir>] [--dir <dir>]",
		Short: "Move the branch's own migrations to follow the default branch's",
		Long: `renumber moves the migrations this branch added, up and down files together, to follow the
default branch's highest index with no gap, keeping their order. A schema migration the default
branch holds is never touched: git says which files are the branch's own (the ones under the
directory that the default branch's tree does not hold), and the default branch is read from
origin's copy of it when the repository has one, else from the local branch, so fetch first. A
tracked file moves with git mv, staging the rename; an untracked one is renamed on disk.

The seed directory beside the migrations (devseed) is renumbered against its own sequence, and
as its files are editable, a committed seed file the branch removed leaves no gap: the seed
files after it move down, up and down together, and the branch's own follow, around the indexes
the default branch took since the branch was cut. A committed seed file only moves down: when
the default branch's additions would push one up, or leave a gap below them, the directory is
left alone with a note that says to merge the default branch first and run the renumber again.

It closes the holes the pipeline's guard refuses a pull request for: an index the default branch
took since the branch was cut (the branch's migration moves up), a gap (the branch's migration
moves down), and a removed seed file (the seed files after it move down). Run it from anywhere
inside the repository, by hand or through go generate: the rendered cmd/generate/bedrock.go runs
it before the application's generators, so they read the migrations as the pipeline will.
Nothing to do prints nothing.

The stack directory and the application are found the way check finds them; --app, --dir and
--placement override. The placement names the default branch.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			appDir, stackDir, err := d.stack(appFlag, dirFlag)
			if err != nil {
				return err
			}
			if placement == "" {
				placement = filepath.Join(stackDir, placementFile)
			}
			p, err := derive.ReadPlacement(placement)
			if err != nil {
				return err
			}
			if p.DefaultBranch == "" {
				return errors.Newf("%s names no defaultBranch: the branch whose migrations the renumber follows", placement)
			}
			opts, err := renumberOptions(appDir, p.DefaultBranch)
			if err != nil {
				return err
			}
			result, err := migration.Renumber(cmd.Context(), *opts)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, r := range result.Renames {
				fmt.Fprintf(out, "%s: %s -> %s (%s)\n", r.Dir, r.From, r.To, strings.Join(r.Files, ", "))
			}
			if len(result.Renames) > 0 {
				fmt.Fprintf(out, "Renumbered %d migration(s) to follow %s at %s.\n", len(result.Renames), result.Ref, result.Commit)
			}
			for _, note := range result.Notes {
				fmt.Fprintf(out, "Left alone: %s\n", note)
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&appFlag, "app", "", "the application's source directory (default: the one the repository layout names, else the working directory)")
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory holding placement.json (default: the one the repository layout names)")
	cmd.Flags().StringVar(&placement, "placement", "", "the placement file (default: placement.json in the stack directory)")

	return cmd
}

// renumberOptions reads the application for its migration directories and finds the
// repository they are in: the directories are named relative to the repository root,
// where git names them.
func renumberOptions(appDir, branch string) (*migration.Options, error) {
	a, err := app.Discover(appDir)
	if err != nil {
		return nil, err
	}
	dirs, err := derive.MigrationDirs(a)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(appDir)
	if err != nil {
		return nil, errors.Wrap(err, "filepath.Abs()")
	}
	root, err := where.RepoRoot(abs)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return nil, errors.Wrap(err, "filepath.Rel()")
	}
	var editable []string
	for i, dir := range dirs {
		if rel != "." {
			dir = path.Join(filepath.ToSlash(rel), dir)
			dirs[i] = dir
		}
		if path.Base(dir) == derive.SeedDir {
			editable = append(editable, dir)
		}
	}

	return &migration.Options{Root: root, Dirs: dirs, Branch: branch, Editable: editable}, nil
}
