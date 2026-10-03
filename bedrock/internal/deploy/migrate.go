package deploy

import (
	"context"
	"fmt"
	"io"
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

// The migration operation a release build may carry, from the operations workflow
// (bedrock migration version|rerun|force): _MIGRATE_ACTION names it, _MIGRATE_TABLE the
// table a force sets (schema, or data) and _MIGRATE_VERSION the version it sets (-1 for no
// version); _REQUESTER says who asked. _MIGRATE_LOGS is the log view the job's lines are
// read through, which the environment's stack defines in every environment but
// production. The migrate command's flags are what the job runs with.
const (
	migrateActionSub  = "_MIGRATE_ACTION"
	migrateTableSub   = "_MIGRATE_TABLE"
	migrateVersionSub = "_MIGRATE_VERSION"
	migrateLogsSub    = "_MIGRATE_LOGS"
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
	versionSkipped = "The run asked for the migration version, which the migrate job printed: nothing else deploys."
	// logTries is how many times the step looks for the job's lines, logWait apart, before
	// giving up: Cloud Logging holds what a job wrote a few seconds after the job ends.
	logTries = 12
	logWait  = 5 * time.Second
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
// the migrate job; fact names the one refused.
func target(fact, pair string) (region, name string, err error) {
	region, name, ok := strings.Cut(pair, "=")
	if !ok || region == "" || name == "" {
		return "", "", errors.Newf("%s %q is not region=name", fact, pair)
	}

	return region, name, nil
}

// MigrateAction is the migration operation a build carries: what the operations workflow
// asked the migrate job for, beyond the migrations themselves.
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
		return actionVersion + ": the migrate job prints the database's migration version and nothing else deploys" + by
	case actionRerun:
		return actionRerun + ": the migrate job runs as it always does and the release continues" + by
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

// Migrate runs this build's migrate job, the copy of the template job that deploy jobs made
// on this image (<template>-<version key>), once to completion, with the seed (schema/devseed
// as data migrations after the schema) where _SEED is true: every pull request, its database
// being new, and a release build only in the environments the placement's seed list names.
// A seeded database takes nothing twice. A build carrying a migration operation does what
// it asks first: version runs the job with -version alone and the steps after do nothing;
// force runs it with -force <n> (or -force-data <n>), leaves the force for the record, and
// then runs the migrations as always; rerun is the migrations as always, by name. The lines
// the job wrote are read from Cloud Logging through the environment's log view and printed
// after each run. The job is deleted at the end of the step whether the execution succeeded
// or failed: its logs stay in Cloud Logging, and the deployment record lists the migrations
// applied. A build that runs no migrations (shared-db) has no job to run; a failed
// execution stops the build and names itself; an operation that cannot run is refused
// before any job starts. With preflight, in a run that waits for the maintenance window,
// the job is run once with -version instead, before the wait, and kept (preflight below).
func Migrate(ctx context.Context, clients *Clients, w Workspace, preflight bool, out io.Writer) error {
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
		fmt.Fprintln(out, "Skipping the migrate job: this build does not run migrations.")

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
	run, err := clients.Run(ctx)
	if err != nil {
		return err
	}
	_, name, err := buildJob(build.Substitutions[projectSub], env, migrateJobFact)
	if err != nil {
		return err
	}
	m := &migrateRun{clients: clients, run: run, name: name, job: shortName(name), view: build.Substitutions[migrateLogsSub], out: out}
	if _, err := run.Get(ctx, name); err != nil {
		if isNotFound(err) {
			return errors.Newf("this build's migrate job %s does not exist: deploy jobs makes it right after the image build", m.job)
		}

		return err
	}
	if preflight {
		return m.preflight(ctx)
	}
	outcome := m.perform(ctx, w, build, action)
	if err := run.Delete(ctx, name); err != nil {
		fmt.Fprintf(out, "Job %s was not deleted (%v): deploy sweep-jobs deletes it.\n", m.job, err)
	} else {
		fmt.Fprintf(out, "Job %s deleted: its execution's logs stay in Cloud Logging.\n", m.job)
	}

	return outcome
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

// migrateRun is one step's runs of the build's migrate job.
type migrateRun struct {
	clients *Clients
	run     Run
	// name is the job's resource name, job its short name, view the log view the job's
	// lines are read through (empty: not read here).
	name, job, view string
	out             io.Writer
}

// preflight runs the job once with -version before the run waits for the window: the job
// starts on the release's image against the environment's database and prints what the
// migrations tables say, so an image that does not start, a configuration that does not
// load or a database that cannot be reached stops the run here, with nothing changed and
// the window not entered. Nothing is applied, and the job stays for the migrations after
// the window.
func (m *migrateRun) preflight(ctx context.Context) error {
	fmt.Fprintf(m.out, "=== Pre-flight: running job [%s] once with %s before the window ===\n", m.job, versionArg)
	if err := m.once(ctx, []string{versionArg}); err != nil {
		fmt.Fprintln(m.out, "Pre-flight failed before the window: nothing changed, and the run stops here.")

		return err
	}
	fmt.Fprintln(m.out, "Pre-flight passed: the migrate job runs on this image against the environment's database; the job stays for the migrations after the window.")

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
		fmt.Fprintf(m.out, "Rerun%s: the migrate job runs as it always does, continuing a file that stopped from its failed statement, and the release continues.\n", by(action.Requester))

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

// migrations runs the job as it always runs: the schema migrations, and the seed where
// _SEED is true.
func (m *migrateRun) migrations(ctx context.Context, build *Build) error {
	fmt.Fprintf(m.out, "=== Running job [%s] once ===\n", m.job)
	var args []string
	if build.Substitutions[seedSub] == trueValue {
		args = []string{seedArg}
		fmt.Fprintln(m.out, "Seeding: the migrate job applies schema/devseed as data migrations.")
	}

	return m.once(ctx, args)
}

// version runs the job with -version alone, prints what it said, and leaves SKIP_DEPLOY
// for the steps after, with the reason: nothing in the environment changes.
func (m *migrateRun) version(ctx context.Context, w Workspace) error {
	fmt.Fprintf(m.out, "=== Running job [%s] once with %s ===\n", m.job, versionArg)
	if err := m.once(ctx, []string{versionArg}); err != nil {
		return err
	}
	fmt.Fprintln(m.out, versionSkipped)

	return w.Append(map[string]string{skipDeploy: trueValue, skipReasonFact: versionSkipped})
}

// force runs the job with the force, prints the rows it printed, and leaves the force for
// the record.
func (m *migrateRun) force(ctx context.Context, w Workspace, action *MigrateAction) error {
	flag := forceArg
	if action.Table == tableData {
		flag = forceDataArg
	}
	args := []string{flag, strconv.Itoa(action.Version)}
	fmt.Fprintf(m.out, "Force%s: the %s migrations table is set to %s; the migrations run after it and the release continues.\n", by(action.Requester), action.Table, action.versionWord())
	fmt.Fprintf(m.out, "=== Running job [%s] once with %s ===\n", m.job, strings.Join(args, " "))
	if err := m.once(ctx, args); err != nil {
		return err
	}

	return w.Append(map[string]string{forcedTableFact: action.Table, forcedVersionFact: strconv.Itoa(action.Version)})
}

// once runs the job once to completion with the arguments, prints the lines it wrote, and
// answers how it went.
func (m *migrateRun) once(ctx context.Context, args []string) error {
	execution, runErr := m.run.RunJob(ctx, m.name, args)
	outcome := executionOutcome(execution, runErr)
	if runErr == nil {
		m.lines(ctx, shortName(text(execution, keyName)))
	}
	if outcome == nil {
		fmt.Fprintf(m.out, "Migrate job done: execution %s succeeded.\n", shortName(text(execution, keyName)))
	}

	return outcome
}

// lines prints what the execution wrote, read from Cloud Logging through the environment's
// log view by the execution's name. The entries follow the execution's end by a few
// seconds, so the read is tried again until a line is there or a minute has passed. With
// no view (production, or a stack from before the view) the lines are not read here, and
// the query that finds them is printed instead.
func (m *migrateRun) lines(ctx context.Context, execution string) {
	query := loggingQuery(execution)
	if m.view == "" || m.clients.Logs == nil {
		fmt.Fprintf(m.out, "The job's lines are not read here (no log view): Cloud Logging query %s\n", query)

		return
	}
	logs, err := m.clients.Logs(ctx)
	if err != nil {
		fmt.Fprintf(m.out, "The job's lines could not be read (%v): Cloud Logging query %s\n", err, query)

		return
	}
	sleep := m.clients.Sleep
	if sleep == nil {
		sleep = sleepFor
	}
	for try := 1; ; try++ {
		lines, err := logs.Lines(ctx, m.view, execution)
		if err != nil {
			fmt.Fprintf(m.out, "The job's lines could not be read (%v): Cloud Logging query %s\n", err, query)

			return
		}
		if len(lines) > 0 {
			fmt.Fprintf(m.out, "--- %s wrote ---\n", execution)
			for _, line := range lines {
				fmt.Fprintln(m.out, line)
			}
			fmt.Fprintln(m.out, "---")

			return
		}
		if try == logTries {
			fmt.Fprintf(m.out, "No line of %s reached Cloud Logging in a minute: query %s\n", execution, query)

			return
		}
		if err := sleep(ctx, logWait); err != nil {
			return
		}
	}
}

// executionOutcome is nil when the execution succeeded, else why the migrations failed:
// the run the API refused, or the execution's failed tasks, named with the Cloud Logging
// query that finds its logs, which outlive the job.
func executionOutcome(execution map[string]any, runErr error) error {
	if runErr != nil {
		return runErr
	}
	executionName := shortName(text(execution, keyName))
	if failed, _ := execution["failedCount"].(float64); failed > 0 {
		return errors.Newf("the migrate job failed: execution %s has %d failed task(s); its logs say why (Cloud Logging: %s)", executionName, int(failed), loggingQuery(executionName))
	}

	return nil
}

// builtImage is this build's image by digest, as the image build left it in the
// environment file.
func builtImage(env map[string]string) (string, error) {
	if env[imageFact] == "" || env[digestFact] == "" {
		return "", errors.Newf("%s names no image digest (IMAGE, IMAGE_DIGEST): the image build writes it", EnvironmentFile)
	}

	return env[imageFact] + "@" + env[digestFact], nil
}
