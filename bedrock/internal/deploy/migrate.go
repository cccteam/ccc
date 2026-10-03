// migrate.go runs the migrations: the release's own migrate command, taken out of the
// environment's image by the image build, run on the build worker as the deploy identity,
// with its lines in the build log.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
)

// The facts and substitutions the deploy steps read, beyond the earlier steps'.
const (
	projectSub   = "_PROJECT"
	repoNameSub  = "REPO_NAME"
	seedSub      = "_SEED"
	buildIDLabel = "gcb-build-id"
)

// The migrate command's settings and the migration operation a release build may carry.
// _MIGRATE_ENV is the stack's: the variables the migrate command runs with, as a JSON
// object of name to value (the levels the command constructs, as the stack derives them
// for the environment or the pull request; no secret, the command never had one). The
// stack steps read it back from the applied stack's substitutions output into MIGRATE_ENV,
// so a release that changes the variables migrates with its own, where the trigger's copy
// is the last apply's. The operation comes from the operations workflow (bedrock migration
// version|rerun|force): _MIGRATE_ACTION names it, _MIGRATE_TABLE the table a force sets
// (schema, or data) and _MIGRATE_VERSION the version it sets (-1 for no version);
// _REQUESTER says who asked. The migrate command's flags are what it runs with.
const (
	migrateEnvSub     = "_MIGRATE_ENV"
	migrateEnvFact    = "MIGRATE_ENV"
	migrateActionSub  = "_MIGRATE_ACTION"
	migrateTableSub   = "_MIGRATE_TABLE"
	migrateVersionSub = "_MIGRATE_VERSION"
	actionVersion     = "version"
	actionRerun       = "rerun"
	actionForce       = "force"
	tableSchema       = "schema"
	tableData         = "data"
	seedArg           = "-seed"
	versionArg        = "-version"
	forceArg          = "-force"
	forceDataArg      = "-force-data"
	// The facts the step leaves: the force it applied, for the record, and why the steps
	// after a version run do nothing, beside SKIP_DEPLOY.
	forcedTableFact   = "MIGRATE_FORCED_TABLE"
	forcedVersionFact = "MIGRATE_FORCED_VERSION"
	skipReasonFact    = "SKIP_REASON"
	// versionSkipped is the reason a version run leaves for the steps after it.
	versionSkipped = "The run asked for the migration version, which the migrate command printed: nothing else deploys."
)

// The labels a deploy stamps on the jobs and the services' revisions: who deployed, what
// and for which build, the build's version as a name (version-key, what the job process's
// job of the build is named after, so the sweep can tell which job a revision runs); the
// pull request's number on its own service.
const (
	managedByLabel   = "managed-by"
	managedByValue   = "cloudbuild"
	commitLabel      = "commit-sha"
	sourceRepoLabel  = "source_repo"
	environmentLabel = "environment"
	prNumberLabel    = "pr-number"
	versionLabel     = "version-key"
)

// pipelineLabels are the pipeline's labels for this build of the version; an empty value
// removes the label (a release build carries no pull request number).
func pipelineLabels(build *Build, version string) map[string]string {
	subs := build.Substitutions

	return map[string]string{
		managedByLabel:   managedByValue,
		commitLabel:      subs[commitSub],
		buildIDLabel:     build.ID,
		sourceRepoLabel:  subs[repoNameSub],
		environmentLabel: subs[envSub],
		prNumberLabel:    subs[prNumberSub],
		versionLabel:     versionKey(version),
	}
}

// target reads a region=name pair, as the stack's substitutions name the services and
// the job process's job; fact names the one refused.
func target(fact, pair string) (region, name string, err error) {
	region, name, ok := strings.Cut(pair, "=")
	if !ok || region == "" || name == "" {
		return "", "", errors.Newf("%s %q is not region=name", fact, pair)
	}

	return region, name, nil
}

// MigrateAction is the migration operation a build carries: what the operations workflow
// asked the migrate command for, beyond the migrations themselves.
type MigrateAction struct {
	// Action is version, rerun or force.
	Action string
	// Table is the migrations table a force sets: schema, or data.
	Table string
	// Version is the version a force sets; -1 for no version.
	Version int
	// Requester is who asked (_REQUESTER); a force names one.
	Requester string
}

// migrateAction reads the operation off the build's substitutions, nil for a build that
// carries none, and refuses one that cannot run: an action that is none of the three, a
// table or a version with no action, a version with an action that takes none, a force
// whose version is missing, not an integer or below -1, a force without a requester, a
// table that is neither schema nor data.
func migrateAction(subs map[string]string) (*MigrateAction, error) {
	a := &MigrateAction{Action: subs[migrateActionSub], Table: subs[migrateTableSub], Version: -1, Requester: subs[requesterSub]}
	version := subs[migrateVersionSub]
	if a.Action == "" {
		if a.Table != "" || version != "" {
			return nil, errors.Newf("%s and %s go with a %s (version, rerun or force), and the build names none", migrateTableSub, migrateVersionSub, migrateActionSub)
		}

		return nil, nil
	}
	switch a.Action {
	case actionVersion, actionRerun:
		if version != "" {
			return nil, errors.Newf("%s=%s with %s=%s: a version goes with %s", migrateVersionSub, version, migrateActionSub, a.Action, actionForce)
		}
	case actionForce:
		if version == "" {
			return nil, errors.Newf("%s=%s names no version (%s): a force says which version the database is at, or -1 for no version", migrateActionSub, actionForce, migrateVersionSub)
		}
		n, err := strconv.Atoi(version)
		if err != nil || n < -1 {
			return nil, errors.Newf("%s %q is not a version: an integer 0 or above, or -1 for no version", migrateVersionSub, version)
		}
		a.Version = n
		if a.Requester == "" {
			return nil, errors.Newf("%s=%s names no requester (%s): a force says who asked for it", migrateActionSub, actionForce, requesterSub)
		}
	default:
		return nil, errors.Newf("unknown %s %q (the actions are %s, %s and %s)", migrateActionSub, a.Action, actionVersion, actionRerun, actionForce)
	}
	switch a.Table {
	case "":
		a.Table = tableSchema
	case tableSchema, tableData:
	default:
		return nil, errors.Newf("unknown %s %q (the tables are %s and %s)", migrateTableSub, a.Table, tableSchema, tableData)
	}

	return a, nil
}

// String says what the operation does, for the build's log.
func (a *MigrateAction) String() string {
	by := ""
	if a.Requester != "" {
		by = ", asked for by " + a.Requester
	}
	switch a.Action {
	case actionVersion:
		return actionVersion + ": the migrate command prints the database's migration version and nothing else deploys" + by
	case actionRerun:
		return actionRerun + ": the migrate command runs as it always does and the release continues" + by
	default:
		return fmt.Sprintf("%s: the %s migrations table is set to %s, then the migrations run and the release continues%s", actionForce, a.Table, a.versionWord(), by)
	}
}

// versionWord is the version a force sets, in words.
func (a *MigrateAction) versionWord() string {
	if a.Version < 0 {
		return "no version"
	}

	return "version " + strconv.Itoa(a.Version)
}

// Migrate runs the migrations on the build worker: the release's own migrate command,
// which the image build took out of the environment's image to program, run in the
// checkout (where it reads the schema directory, as it does in the image) as the deploy
// identity, with the variables the stack derived for it (MIGRATE_ENV, the levels it
// constructs; the version variable set to the build's version, as the image sets it for
// the processes it runs) and the flags the facts decide: the seed (schema/devseed as data
// migrations after the schema) where _SEED is true, every pull request, its database being
// new, and a release build only in the environments the placement's seed list names. A
// seeded database takes nothing twice. The command reaches Spanner and Firestore through
// their APIs as the deploy identity, which the application stack grants database admin
// on the application's own database and nothing wider; its lines go straight into the
// build log. A build carrying a migration operation does what it asks first: version runs
// the command with -version alone and the steps after do nothing; force runs it with
// -force <n> (or -force-data <n>), leaves the force for the record, and then runs the
// migrations as always; rerun is the migrations as always, by name. A build that runs no
// migrations (shared-db) runs nothing; a failed run stops the build with the command's
// lines above; an operation that cannot run is refused before anything runs. With
// preflight, in a run that waits for the maintenance window, the command is run once with
// -version instead, before the wait (preflight below).
func Migrate(ctx context.Context, clients *Clients, w Workspace, program, versionVariable string, preflight bool, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	if env[skipDeploy] == trueValue {
		fmt.Fprintln(out, skipped(env))

		return nil
	}
	if preflight && !preflightDue(env, out) {
		return nil
	}
	if env[runMigrationsFact] != trueValue {
		fmt.Fprintln(out, "Skipping the migrations: this build does not run them.")

		return nil
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	action, err := migrateAction(build.Substitutions)
	if err != nil {
		return err
	}
	vars, err := migrateSettings(env, versionVariable)
	if err != nil {
		return err
	}
	if _, err := os.Stat(program); err != nil {
		return errors.Newf("no migrate command at %s: the image build takes %s out of the image (deploy build-image), and the Dockerfile builds it (go build -o /build/migrate ./cmd/deployment/migrate)", program, migrateInImage)
	}
	m := &migrateRun{exec: clients.Exec, command: Command{Dir: string(w), Env: vars, Name: program}, out: out}
	fmt.Fprintf(out, "The migrate command %s runs on this worker as the deploy identity with %d variables from the stack.\n", program, len(vars))
	if preflight {
		return m.preflight(ctx)
	}

	return m.perform(ctx, w, build, action)
}

// migrateSettings are the variables the migrate command runs with, NAME=value sorted by
// name: the stack's (MIGRATE_ENV, which the stack steps read from the applied stack's
// substitutions output, the levels the command constructs for this environment or pull
// request) and the version variable, when the pipeline names one, set to the build's
// version, which the image sets for the processes it runs and the worker does not.
func migrateSettings(env map[string]string, versionVariable string) ([]string, error) {
	raw := env[migrateEnvFact]
	if raw == "" {
		return nil, errors.Newf("%s names no settings for the migrate command (%s): the stack steps write them from the stack's substitutions output (%s)", EnvironmentFile, migrateEnvFact, migrateEnvSub)
	}
	var settings map[string]string
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): %s", migrateEnvFact)
	}
	if versionVariable != "" {
		settings[versionVariable] = env[versionFact]
	}
	vars := make([]string, 0, len(settings))
	for _, name := range slices.Sorted(maps.Keys(settings)) {
		vars = append(vars, name+"="+settings[name])
	}

	return vars, nil
}

// preflightDue reports whether the pre-flight has anything to do: a run that waits for
// the maintenance window and does not replace its database; the others are said on out.
func preflightDue(env map[string]string, out io.Writer) bool {
	switch {
	case env[windowNeededFact] != trueValue:
		fmt.Fprintln(out, "No pre-flight: this run waits for no maintenance window.")

		return false
	case env[restoreFact] != "":
		fmt.Fprintln(out, "No pre-flight: a restore run replaced the database, which the migrations fill from the start.")

		return false
	default:
		return true
	}
}

// migrateRun is one step's runs of the migrate command: the command with its directory
// and variables, run with the arguments of each run.
type migrateRun struct {
	exec    Runner
	command Command
	out     io.Writer
}

// preflight runs the command once with -version before the run waits for the window: the
// release's own command loads its configuration against the environment's database and
// prints what the migrations tables say, so a configuration that does not load or a
// database that cannot be reached stops the run here, with nothing changed and the window
// not entered. Nothing is applied.
func (m *migrateRun) preflight(ctx context.Context) error {
	fmt.Fprintf(m.out, "=== Pre-flight: running the migrate command once with %s before the window ===\n", versionArg)
	if err := m.once(ctx, []string{versionArg}); err != nil {
		fmt.Fprintln(m.out, "Pre-flight failed before the window: nothing changed, and the run stops here.")

		return err
	}
	fmt.Fprintln(m.out, "Pre-flight passed: the release's migrate command loads its configuration and reaches the environment's database; the migrations run after the window.")

	return nil
}

// perform does what the operation asks: the version report alone, a force and then the
// migrations, or the migrations, which a rerun is by name.
func (m *migrateRun) perform(ctx context.Context, w Workspace, build *Build, action *MigrateAction) error {
	switch {
	case action == nil:
		return m.migrations(ctx, build)
	case action.Action == actionVersion:
		return m.version(ctx, w)
	case action.Action == actionForce:
		if err := m.force(ctx, w, action); err != nil {
			return err
		}

		return m.migrations(ctx, build)
	default:
		fmt.Fprintf(m.out, "Rerun%s: the migrate command runs as it always does, continuing a file that stopped from its failed statement, and the release continues.\n", by(action.Requester))

		return m.migrations(ctx, build)
	}
}

// by names who asked, when someone did.
func by(requester string) string {
	if requester == "" {
		return ""
	}

	return ", asked for by " + requester
}

// migrations runs the command as it always runs: the schema migrations, and the seed
// where _SEED is true.
func (m *migrateRun) migrations(ctx context.Context, build *Build) error {
	var args []string
	if build.Substitutions[seedSub] == trueValue {
		args = []string{seedArg}
		fmt.Fprintln(m.out, "Seeding: the migrate command applies schema/devseed as data migrations.")
	}

	return m.once(ctx, args)
}

// version runs the command with -version alone and leaves SKIP_DEPLOY for the steps
// after, with the reason: nothing in the environment changes.
func (m *migrateRun) version(ctx context.Context, w Workspace) error {
	if err := m.once(ctx, []string{versionArg}); err != nil {
		return err
	}
	fmt.Fprintln(m.out, versionSkipped)

	return w.Append(map[string]string{skipDeploy: trueValue, skipReasonFact: versionSkipped})
}

// force runs the command with the force, whose rows it prints, and leaves the force for
// the record.
func (m *migrateRun) force(ctx context.Context, w Workspace, action *MigrateAction) error {
	flag := forceArg
	if action.Table == tableData {
		flag = forceDataArg
	}
	fmt.Fprintf(m.out, "Force%s: the %s migrations table is set to %s; the migrations run after it and the release continues.\n", by(action.Requester), action.Table, action.versionWord())
	if err := m.once(ctx, []string{flag, strconv.Itoa(action.Version)}); err != nil {
		return err
	}

	return w.Append(map[string]string{forcedTableFact: action.Table, forcedVersionFact: strconv.Itoa(action.Version)})
}

// once runs the command once with the arguments, its lines on the build log as it writes
// them, and says how long it took; a command that exits with an error fails the step, its
// message above.
func (m *migrateRun) once(ctx context.Context, args []string) error {
	c := m.command
	c.Args = args
	fmt.Fprintf(m.out, "=== Running the migrate command%s ===\n", withArgs(args))
	started := time.Now()
	if err := m.exec.Run(ctx, c, m.out); err != nil {
		return errors.Newf("the migration failed after %s: the migrate command's lines are above (%v)", time.Since(started).Round(time.Second), errors.Cause(err))
	}
	fmt.Fprintf(m.out, "Migrate command done in %s.\n", time.Since(started).Round(time.Second))

	return nil
}

// withArgs spells the arguments of a run for its heading, nothing for none.
func withArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return " with " + strings.Join(args, " ")
}

// builtImage is this build's image by digest, as the image build left it in the
// environment file.
func builtImage(env map[string]string) (string, error) {
	if env[imageFact] == "" || env[digestFact] == "" {
		return "", errors.Newf("%s names no image digest (IMAGE, IMAGE_DIGEST): the image build writes it", EnvironmentFile)
	}

	return env[imageFact] + "@" + env[digestFact], nil
}
