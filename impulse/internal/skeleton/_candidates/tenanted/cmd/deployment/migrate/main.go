// Package main is the deployment's migration step: it applies the schema migrations
// and checks the release's role policy against what the permission engine's store
// holds. It reads the core and data configuration levels and nothing above them.
//
// Three flags make it the instrument for the states the migration runner refuses to
// guess at. -version prints what each migrations table (schema, data) says about the
// database and exits; -force <n> and -force-data <n> set a table to a version, clean
// (-1 for no version), print the row before and after, and exit. None of them applies a
// migration, so a force refuses -seed and -version. The pipeline passes them from the
// operations workflow (bedrock deploy migrate); they work anywhere the job runs.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"

	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/tenanted/pkg/auth/staff"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/tenanted/pkg/config"
	"github.com/cccteam/ccc/impulse/internal/skeleton/_candidates/tenanted/pkg/deploy"
	"github.com/go-playground/errors/v5"
)

// The exit codes: the arguments refused before anything is opened (as the flag package
// exits), and a run that failed.
const (
	exitRefused = 2
	exitFailed  = 1
)

// errRefused marks arguments the flag package refused and reported itself.
var errRefused = errors.New("the arguments were refused")

func main() {
	os.Exit(execute(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// execute runs the command with its arguments and answers the exit code; a refusal of
// the arguments and a failed run say why on stderr.
func execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	o, err := parse(args, stderr)
	switch {
	case errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errRefused):
		return exitRefused
	case err != nil:
		fmt.Fprintln(stderr, err)

		return exitRefused
	}
	if err := run(ctx, o, stdout); err != nil {
		fmt.Fprintln(stderr, err)

		return exitFailed
	}

	return 0
}

// options are the flags, parsed.
type options struct {
	seed      bool
	version   bool
	force     versionFlag
	forceData versionFlag
}

// versionFlag is a version to force: set once given, -1 meaning no version.
type versionFlag struct {
	set bool
	n   int
}

func (f *versionFlag) String() string {
	if !f.set {
		return ""
	}

	return strconv.Itoa(f.n)
}

// Set reads the version: an integer 0 or above, or -1 for no version.
func (f *versionFlag) Set(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil || n < -1 {
		return errors.Newf("%q is not a version: an integer 0 or above, or -1 for no version", s)
	}
	f.set, f.n = true, n

	return nil
}

// parse reads the arguments and refuses, before anything is opened, what cannot run
// together: a force applies no migration, so neither -seed nor -version goes with it,
// and -version applies none either, so -seed does not go with it.
func parse(args []string, stderr io.Writer) (*options, error) {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o options
	fs.BoolVar(&o.seed, "seed", false, "apply the development seed (schema/devseed) after the schema migrations, as data migrations; the pipeline passes it in test environments and never in production")
	fs.BoolVar(&o.version, "version", false, "print what the schema and data migrations tables say about the database (no version, a version, or dirty at one with the progress recorded) and exit; nothing is applied")
	fs.Var(&o.force, "force", "set the schema migrations table to this `version`, clean, print the row before and after, and exit; -1 means no version; the next run continues from it")
	fs.Var(&o.forceData, "force-data", "the same for the data migrations table")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, errors.Wrap(err, "flag.FlagSet.Parse()")
		}

		return nil, errRefused
	}
	if fs.NArg() > 0 {
		return nil, errors.Newf("the command takes no argument: %q", fs.Arg(0))
	}
	forcing := o.force.set || o.forceData.set
	switch {
	case forcing && o.seed:
		return nil, errors.New("-force and -force-data apply no migration: -seed cannot go with them")
	case forcing && o.version:
		return nil, errors.New("-version reports and -force changes: give one or the other")
	case o.version && o.seed:
		return nil, errors.New("-version applies no migration: -seed cannot go with it")
	}

	return &o, nil
}

// run does what the flags ask: the report, the force, or the migrations.
func run(ctx context.Context, o *options, out io.Writer) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	settings, err := config.LoadDatabaseSettings(ctx)
	if err != nil {
		return errors.Wrap(err, "config.LoadDatabaseSettings()")
	}
	switch {
	case o.version:
		return report(ctx, settings, out)
	case o.force.set || o.forceData.set:
		return force(ctx, settings, o, out)
	}

	return migrate(ctx, settings, o.seed)
}

// report prints one line per migrations table: what it says about the database.
func report(ctx context.Context, settings config.DatabaseSettings, out io.Writer) error {
	schema, data, err := deploy.Versions(ctx, settings)
	if err != nil {
		return errors.Wrap(err, "deploy.Versions()")
	}
	fmt.Fprintf(out, "%s: %s\n%s: %s\n", deploy.SchemaTable, schema, deploy.DataTable, data)

	return nil
}

// force sets each table asked for to its version, the schema's first, printing the row
// before and after each.
func force(ctx context.Context, settings config.DatabaseSettings, o *options, out io.Writer) error {
	forces := []struct {
		table deploy.Table
		flag  versionFlag
	}{{deploy.SchemaTable, o.force}, {deploy.DataTable, o.forceData}}
	for _, f := range forces {
		if !f.flag.set {
			continue
		}
		before, err := row(ctx, settings, f.table)
		if err != nil {
			return err
		}
		if err := deploy.Force(ctx, settings, f.table, f.flag.n); err != nil {
			return errors.Wrap(err, "deploy.Force()")
		}
		after, err := row(ctx, settings, f.table)
		if err != nil {
			return err
		}
		to := "no version"
		if f.flag.n >= 0 {
			to = "version " + strconv.Itoa(f.flag.n)
		}
		fmt.Fprintf(out, "%s: %s\nforced %s to %s\n%s: %s\n", f.table, before, f.table, to, f.table, after)
	}

	return nil
}

// row is what one migrations table says about the database.
func row(ctx context.Context, settings config.DatabaseSettings, table deploy.Table) (string, error) {
	schema, data, err := deploy.Versions(ctx, settings)
	if err != nil {
		return "", errors.Wrap(err, "deploy.Versions()")
	}
	if table == deploy.DataTable {
		return data.String(), nil
	}

	return schema.String(), nil
}

// migrate is the deployment's migration step itself.
func migrate(ctx context.Context, settings config.DatabaseSettings, seed bool) error {
	// The schema goes first, before any client opens: the tables the data level's
	// clients read may not exist yet.
	if err := deploy.MigrateSchema(ctx, settings); err != nil {
		return errors.Wrap(err, "deploy.MigrateSchema()")
	}

	// The seed, when this deployment asks for it: the development data as data
	// migrations, tracked apart from the schema, so a seeded database takes nothing
	// twice and a new seed file reaches it. Before the roles, as cmd/bootstrap does.
	if seed {
		if err := deploy.SeedDevelopmentData(ctx, settings); err != nil {
			return errors.Wrap(err, "deploy.SeedDevelopmentData()")
		}
	}

	data, err := config.NewDataConfiguration(ctx)
	if err != nil {
		return errors.Wrap(err, "config.NewDataConfiguration()")
	}
	defer data.Close()

	// The default roles travel with the release: opening the data level validated the
	// staff role file against the collection, so a release whose roles are wrong stopped
	// above. A domain role is held in every tenant domain, so a tenant created after this
	// deployment needs no step of its own. What is left is to print what the store holds
	// that this release cannot use as written.
	if err := deploy.CheckRoles(ctx, data.Staff().Access(), staff.Name); err != nil {
		return errors.Wrap(err, "deploy.CheckRoles()")
	}

	// The feature flags the release declares: a new flag is written off, a known one keeps
	// its state and takes the release's description, and one the release no longer
	// declares is deleted, so every instance of this release lists the same flags.
	if err := deploy.MigrateFeatures(ctx, data.ResourceClient()); err != nil {
		return errors.Wrap(err, "deploy.MigrateFeatures()")
	}

	return nil
}
