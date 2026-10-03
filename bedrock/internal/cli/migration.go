// migration.go is the migration command group: the schema migrations as the pipeline
// reads them, one sequence, the renumber that keeps a branch's own files in it, and the
// three operations on an environment's migrations started from GitHub (version, rerun,
// force) for the states the migration runner refuses to guess at.

package cli

import (
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/migration"
	"github.com/cccteam/ccc/bedrock/internal/where"
	"github.com/cccteam/ccc/impulse/app"
)

// migrationUse names the command group; the two tables a force sets are named as the
// migrate command names them, and noneWord is the word for an empty value in an
// argument or a message.
const (
	migrationUse = "migration"
	tableSchema  = "schema"
	tableData    = "data"
	noneWord     = "none"
)

// newMigration is the command group over the schema migrations.
func newMigration(d deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   migrationUse,
		Short: "The schema migrations: their sequence, and the operations on an environment's migrations",
		Long: `migration holds what keeps the schema migrations, and the seed migrations beside them, one
sequence the migrate command applies in order: six-digit indexes, one up file each, contiguous,
and a committed schema migration never changes (a seed file is development data and may). bedrock
check and the pipeline's guard refuse a directory that is not; renumber moves a branch's own
migrations back into the sequence, and the seed files after a removed one down.

It also holds the three operations on an environment's migrations, started from GitHub through the
operations workflow and done by the environment's pipeline as the deploy identity: version prints
what each migrations table says about the database, rerun runs the release again so the migrate
job continues a file that stopped from its failed statement, and force sets a migrations table to
the version the database is really at and lets the release continue. They are for the states the
migration runner refuses to guess at, and none reaches production, which is the platform
operator's (the README, When the migrate job fails).`,
	}
	cmd.AddCommand(newMigrationRenumber(d), newMigrationVersion(d), newMigrationRerun(d), newMigrationForce(d))

	return cmd
}

// operationFlags are the flags the three operations share: the release, and where the
// placement is.
type operationFlags struct {
	release, dir, placement string
}

// add puts the flags on the command; the release is required.
func (f *operationFlags) add(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.release, releaseInput, "", "the release tag the environment runs, or is to run (v<major>.<minor>.<patch>); required")
	cmd.Flags().StringVar(&f.dir, "dir", "", "the stack directory holding the placement (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&f.placement, "placement", "", "placement file (default: placement.json in the stack directory)")
	_ = cmd.MarkFlagRequired(releaseInput)
}

// migrationTarget is the environment checked for an operation on its migrations: one of
// the placement's, and not production, whose door does not exist.
func (f *operationFlags) migrationTarget(d deps, env string) (*repositoryContext, error) {
	rc, err := d.operationTarget(f.dir, f.placement, env)
	if err != nil {
		return nil, err
	}
	if env == rc.placement.Production() {
		return nil, errors.Newf("%s is production: no migration operation runs there, since production's migrations are the platform operator's (the README, When the migrate job fails); a release is run again in production with bedrock rerun", env)
	}

	return rc, nil
}

// newMigrationVersion is migration version <env> --release <tag>.
func newMigrationVersion(d deps) *cobra.Command {
	var flags operationFlags
	cmd := &cobra.Command{
		Use:   actionVersion + " <env> --release <tag>",
		Short: "Print an environment's migration version, from GitHub",
		Long: `version starts the operations workflow's migration job for the environment and the release with the
version action: the environment's version trigger runs the release with _MIGRATE_ACTION=version,
and after the usual steps (the release check, the record gate, the image) the migrate job runs once
with -version and prints what each migrations table says about the database (no version, a version,
or dirty at one with the progress recorded); the run then stops before the service, the traffic
shift and the record, so nothing in the environment changes. The lines print in the build log and in
the workflow run's summary. The command refuses production and checks the release exists; it
dispatches as the person signed in to gh (or GITHUB_TOKEN).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env := args[0]
			rc, err := flags.migrationTarget(d, env)
			if err != nil {
				return err
			}
			login, err := dispatchOperation(cmd.Context(), d, rc, env, flags.release, map[string]string{actionInput: actionVersion})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Asked, as %s, for %s's migration version at %s: the operations workflow of %s/%s runs it (%s). The migrate job prints what each migrations table says about the database, in the build log and in the run's summary; nothing else deploys.\n",
				login, env, flags.release, rc.owner, rc.repo, workflowURL(rc))

			return nil
		},
	}
	flags.add(cmd)

	return cmd
}

// newMigrationRerun is migration rerun <env> --release <tag>.
func newMigrationRerun(d deps) *cobra.Command {
	var flags operationFlags
	cmd := &cobra.Command{
		Use:   actionRerun + " <env> --release <tag>",
		Short: "Run a release again so its migrate job continues from where it stopped, from GitHub",
		Long: `rerun starts the operations workflow's migration job for the environment and the release with the
rerun action: the environment's version trigger runs the release again, and the migrate job runs as
it always does. A migration file that stopped at a statement, its progress recorded, continues from
that statement once the cause is fixed (usually rows the statement validated), applies the rest of
the file and the files after it, and the release deploys; nothing applied is repeated. It is the
everyday case after a data fix. The command refuses production and checks the release exists; it
dispatches as the person signed in to gh (or GITHUB_TOKEN).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env := args[0]
			rc, err := flags.migrationTarget(d, env)
			if err != nil {
				return err
			}
			login, err := dispatchOperation(cmd.Context(), d, rc, env, flags.release, map[string]string{actionInput: actionRerun})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Asked, as %s, for %s to run %s again: the operations workflow of %s/%s runs it (%s). The migrate job continues a file that stopped from its failed statement once the cause is fixed, and the release continues to the service, the traffic shift and the record.\n",
				login, env, flags.release, rc.owner, rc.repo, workflowURL(rc))

			return nil
		},
	}
	flags.add(cmd)

	return cmd
}

// newMigrationForce is migration force <env> <version> --release <tag> [--table data].
func newMigrationForce(d deps) *cobra.Command {
	var (
		flags operationFlags
		table string
	)
	cmd := &cobra.Command{
		Use:   actionForce + " <env> <version|none> --release <tag> [--table " + tableData + "]",
		Short: "Set an environment's migration version and let the release continue, from GitHub",
		Long: `force starts the operations workflow's migration job for the environment and the release with the
force action: the environment's version trigger runs the release with _MIGRATE_ACTION=force, the
table and the version, and after the usual steps the migrate job runs once with -force <version> (or
-force-data <version> for the data migrations table), sets the table to that version, clean, and
prints the row before and after; then the job runs as it always does, the migrations continue from
the forced version, and the release continues to the service, the traffic shift and the record,
which carries the table, the version and who asked. It is for the states the runner refuses to
guess at: a database the old library left dirty with no progress recorded, an in-flight operation
Spanner no longer has, a file changed in its applied part. A person reads the database's state
(migration version), decides what it really holds, and forces that version: none (or -1, after --,
since a bare -1 reads as a flag) means no version. The command refuses production, a version that
is neither an integer 0 or above nor none, and a table other than schema and data, and checks the
release exists; it dispatches as the person signed in to gh (or GITHUB_TOKEN).`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, version := args[0], args[1]
			if version == noneWord {
				version = "-1"
			}
			n, err := strconv.Atoi(version)
			if err != nil || n < -1 {
				return errors.Newf("%q is not a version: an integer 0 or above, or none for no version", args[1])
			}
			if table != tableSchema && table != tableData {
				return errors.Newf("unknown table %q (the tables are %s and %s)", table, tableSchema, tableData)
			}
			rc, err := flags.migrationTarget(d, env)
			if err != nil {
				return err
			}
			inputs := map[string]string{actionInput: actionForce, tableInput: table, versionInput: strconv.Itoa(n)}
			login, err := dispatchOperation(cmd.Context(), d, rc, env, flags.release, inputs)
			if err != nil {
				return err
			}
			to := "no version"
			if n >= 0 {
				to = "version " + strconv.Itoa(n)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Asked, as %s, for %s's %s migrations table to be set to %s and %s to continue: the operations workflow of %s/%s runs it (%s). The migrate job sets the version and prints the row before and after, then the migrations run from it and the release continues to the service, the traffic shift and the record, which names you.\n",
				login, env, table, to, flags.release, rc.owner, rc.repo, workflowURL(rc))

			return nil
		},
	}
	flags.add(cmd)
	cmd.Flags().StringVar(&table, tableInput, tableSchema, "the migrations table to set: schema, or data")

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
--placement override. The placement names the default branch; a branch cut from a hotfix line
(hotfix/<major>.<minor>.x, nearer to the branch in the history than the default branch is) follows
the line instead, since a line is behind the default branch on purpose.`,
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
