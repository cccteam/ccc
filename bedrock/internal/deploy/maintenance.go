// maintenance.go puts the application into maintenance for a run that replaces or
// interrupts its database (a restore run before its stack is planned, a breaking release
// once its maintenance window is open), and takes it out once the release serves.

package deploy

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// The facts a maintenance step leaves: that the run is in maintenance, the maintenance
// revisions (region=revision, comma-separated), the queue it paused (its full name, empty
// when the application has none), whether it purged the queue, how many job executions it
// canceled, and how the wait for the old revision's requests ended.
const (
	maintenanceFact          = "MAINTENANCE"
	maintenanceRevisionsFact = "MAINTENANCE_REVISIONS"
	maintenanceQueueFact     = "MAINTENANCE_QUEUE"
	maintenancePurgedFact    = "MAINTENANCE_PURGED"
	maintenanceCanceledFact  = "MAINTENANCE_CANCELED"
	maintenanceWaitedFact    = "MAINTENANCE_WAITED"
	// tasksQueueSub is the application's task queue, named in full by the stack when the
	// application declares one.
	tasksQueueSub = "_TASKS_QUEUE"
	// servicesSub names the application's services per region (region=name, comma-separated).
	servicesSub = "_SERVICES"
	jobsJobSub  = "_JOBS_JOB"
	// maintenanceOn is the value the maintenance variable takes on a maintenance revision.
	maintenanceOn = "1"
	// queuePaused is the state of a Cloud Tasks queue that holds its tasks.
	queuePaused = "PAUSED"
	// maintenanceHeader is the marker a maintenance answer carries, as the framework's
	// maintenance package sets it; the probe requires it.
	maintenanceHeader      = "X-Maintenance"
	maintenanceHeaderValue = "1"
	// probeWindow is how long the maintenance revision gets to answer through the load
	// balancer with the marker before the run stops; probeEvery is the time between tries.
	probeWindow = 3 * time.Minute
	probeEvery  = 5 * time.Second
	// waitEvery is the time between reads of the old revision's active instances; the
	// wait itself is bounded by the service's request timeout (defaultRequestTimeout when
	// the service names none). metricWindow is how far back a read looks for a point.
	waitEvery             = 15 * time.Second
	defaultRequestTimeout = 300 * time.Second
	metricWindow          = 5 * time.Minute
)

// SleepFunc waits, or returns the context's error; tests substitute one that does not wait.
type SleepFunc func(ctx context.Context, d time.Duration) error

// sleepFor waits for d.
func sleepFor(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "waiting")
	case <-timer.C:
		return nil
	}
}

// maintenance is one run's maintenance: its clients, build and facts.
type maintenance struct {
	clients *Clients
	build   *Build
	env     map[string]string
	out     io.Writer
	project string
	sleep   SleepFunc
	run     Run
	timeout time.Duration
}

// restoreReadyBackupFact names, for the plan step, the backup of production's live
// database the maintenance step chose and waited for before the maintenance page went up
// (a production-backup restore).
const restoreReadyBackupFact = "RESTORE_READY_BACKUP"

// displacedLabel is the label a maintenance revision carries naming the revision that
// served when it took the traffic, so that a run which only ends the maintenance (bedrock
// maintenance off) knows where the traffic goes back to.
const displacedLabel = "bedrock-displaced"

// MaintenanceOn puts the application into maintenance when the run needs it. The step
// stands at two places in the pipeline: before the stack is planned, where a restore run
// (whose database is replaced by the plan) goes into maintenance; and after the gate
// (window), where a breaking release does, once the window is open, with a second look
// at the window first (windowStillOpen). The release's own image starts as a revision
// with the maintenance variable set, in every region, under the tag next, receiving no
// traffic; the revision is probed through the load balancer's next hostname and must
// answer 503 with the marker, else the run stops here with nothing moved, naming the
// application's missing switch; then all traffic moves to it, the application's task
// queue is paused (and, on a restore, purged), the running executions of the serving
// build's job are canceled, and the old revision's requests in flight are let finish:
// its active instances are read until none is, or until the service's request timeout
// has passed since traffic moved. What can wait, waits before any of that, with the
// application serving (waitBeforeMaintenance): a restore from production's backup waits
// for production's newest backup to be READY, and a breaking release that takes a
// release backup waits for Spanner to be free of another backup of the database, since
// either wait would otherwise run behind the maintenance page. Any other run keeps the application serving: an
// ordinary release inside a window under releases all deploys the rolling way, with no
// maintenance revision, no probe, no pause and no cancel.
func MaintenanceOn(ctx context.Context, clients *Clients, w Workspace, window bool, out io.Writer) error {
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
	if subs[prNumberSub] != "" {
		fmt.Fprintln(out, "No maintenance: a pull-request build never goes into maintenance.")

		return nil
	}
	heading, ok := maintenanceCause(env, window, subs[envSub], out)
	if !ok {
		return nil
	}
	if err := beforeMaintenance(ctx, clients, w, subs, env, window, out); err != nil {
		return err
	}
	if window {
		slot, err := windowStillOpen(ctx, clients, w, build, env)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Second look at the window: %s's window is open (%s); maintenance begins.\n", subs[envSub], slot)
	}
	m := &maintenance{clients: clients, build: build, env: env, out: out, project: subs[projectSub], sleep: clients.sleep()}
	if err := m.open(ctx); err != nil {
		return err
	}
	fmt.Fprintf(out, "=== Maintenance on: %s ===\n", heading)
	revisions, previous, err := m.deployRevisions(ctx, env[imageFact]+"@"+env[digestFact], pipelineLabels(build, env[versionFact]))
	if err != nil {
		return err
	}
	if err := m.probe(ctx, nextURL(subs[hostnameSub], subs[prNumberSub])); err != nil {
		return err
	}
	moved := time.Now()
	pairs := make([]string, 0, len(revisions))
	for _, r := range revisions {
		if err := shiftAll(ctx, m.run, serviceName(m.project, r.Region, r.Service), r.Revision); err != nil {
			return err
		}
		fmt.Fprintf(out, "Traffic in [%s] moved to the maintenance revision [%s]: the application answers 503 with its maintenance page.\n", r.Region, r.Revision)
		pairs = append(pairs, r.Region+"="+r.Revision)
	}
	facts := map[string]string{maintenanceFact: trueValue, maintenanceRevisionsFact: strings.Join(pairs, ",")}
	if err := m.quiesce(ctx, facts, previous, moved); err != nil {
		return err
	}
	if err := w.Append(facts); err != nil {
		return err
	}
	fmt.Fprintf(out, "Maintenance is on: %d revision(s) serve the maintenance page; the database may be replaced or migrated.\n", len(revisions))

	return nil
}

// maintenanceCause says whether this step puts the application into maintenance and,
// when it does, the heading that says why: at the restore position a restore run; at the
// window position a breaking release, unless the run is in maintenance already (a restore
// run that is also breaking). Every other case is said on out.
func maintenanceCause(env map[string]string, window bool, environment string, out io.Writer) (string, bool) {
	version := env[versionFact]
	switch {
	case !window && env[restoreFact] != "":
		return fmt.Sprintf("%s's database is replaced (%s) before %s deploys, so the application serves its maintenance page meanwhile", environment, env[restoreFact], version), true
	case !window:
		fmt.Fprintln(out, "No maintenance: this run keeps the application serving (a restore run starts its maintenance revision here, and a breaking release starts its own once its window is open).")

		return "", false
	case env[maintenanceFact] == trueValue:
		fmt.Fprintln(out, "Maintenance is on already: this run put the application into maintenance before its database was replaced; nothing more to start.")

		return "", false
	case env[windowBreakingFact] == trueValue:
		return fmt.Sprintf("%s is a breaking release for %s (%s), so the application serves its maintenance page while the database migrates", version, environment, env[windowReasonFact]), true
	case env[windowNeededFact] == trueValue:
		fmt.Fprintf(out, "No maintenance: %s is not a breaking release and deploys the rolling way inside %s's window; no maintenance revision, no probe, no queue pause and no cancel.\n", version, environment)

		return "", false
	default:
		fmt.Fprintln(out, "No maintenance: this run keeps the application serving.")

		return "", false
	}
}

// beforeMaintenance is what comes before the maintenance page goes up: the facts the
// step needs (the image digest the image build wrote, the services the resolve step
// wrote), then the waits (waitBeforeMaintenance), whose facts are appended.
func beforeMaintenance(ctx context.Context, clients *Clients, w Workspace, subs, env map[string]string, window bool, out io.Writer) error {
	if env[imageFact] == "" || env[digestFact] == "" {
		return errors.Newf("%s names no image digest (IMAGE, IMAGE_DIGEST): the image build writes it", EnvironmentFile)
	}
	if env[services] == "" {
		return errors.Newf("%s names no services (SERVICES): the resolve step writes them", EnvironmentFile)
	}
	waited, err := waitBeforeMaintenance(ctx, clients, subs, env, window, out)
	if err != nil {
		return err
	}
	if len(waited) == 0 {
		return nil
	}

	return w.Append(waited)
}

// waitBeforeMaintenance is what the run waits for before the maintenance page goes up,
// with the application still serving: at the restore position, a restore from
// production's backup waits for the backup it will restore (readyProductionBackup) and
// names it for the plan step; at the window position, a breaking release that takes a
// release backup after maintenance waits for Spanner to be free of another backup of
// the database (freeOfBackups), so the backup step starts at once. The facts answered
// are appended to the environment; nothing else waits here.
func waitBeforeMaintenance(ctx context.Context, clients *Clients, subs, env map[string]string, window bool, out io.Writer) (map[string]string, error) {
	switch {
	case !window && env[restoreFact] == restoreBackup:
		return readyProductionBackup(ctx, clients, subs, env, out)
	case window && noReleaseBackup(env) == "":
		return nil, freeOfBackups(ctx, clients, subs, env, out)
	}

	return nil, nil
}

// readyProductionBackup is a production-backup restore's wait before the maintenance
// page goes up: production's live database by its record (RESTORE_SOURCE_DATABASE), the
// backup the restore takes (productionBackup: the newest, or the one a rollback restored
// the generation from), waited for until READY as the apply identity, and named for the
// plan step (RESTORE_READY_BACKUP), which restores that one. A record that names no
// database (written before database generations) leaves the choice and the wait to the
// plan step, which reads production's first database from the stack's state; said on
// out. No backup at all is refused here, before anything stops serving.
func readyProductionBackup(ctx context.Context, clients *Clients, subs, env map[string]string, out io.Writer) (map[string]string, error) {
	productionDB := env[restoreSourceDatabaseFact]
	if productionDB == "" {
		fmt.Fprintln(out, "Production's record names no database (written before database generations): the plan step reads production's first database from the stack's state and waits for its backup there, if it must.")

		return nil, nil
	}
	i := strings.Index(productionDB, "/databases/")
	if i < 0 {
		return nil, errors.Newf("%s=%s: production's live database %s is not a database's resource name", restoreSub, restoreBackup, productionDB)
	}
	instance := productionDB[:i]
	store, err := spannerAsApplyIdentity(ctx, clients, subs)
	if err != nil {
		return nil, err
	}
	backup, err := productionBackup(ctx, store, env, instance, productionDB, out)
	if err != nil {
		return nil, err
	}
	if backup == nil {
		return nil, errors.Newf("%s=%s: %s has no backup of production's database %s; %s keeps its database and stays serving", restoreSub, restoreBackup, path.Base(instance), path.Base(productionDB), subs[envSub])
	}
	if backup.State != BackupReady {
		fmt.Fprintf(out, "Before maintenance: production's newest backup %s is still being taken; the wait is here, with %s serving.\n", path.Base(backup.Name), subs[envSub])
	}
	if backup, err = readyBackup(ctx, clients, store, backup, out); err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "Production's backup %s (data as of %s) is READY; the plan step restores it.\n", path.Base(backup.Name), backup.VersionTime)

	return map[string]string{restoreReadyBackupFact: backup.Name}, nil
}

// freeOfBackups is a breaking release's wait before the maintenance page goes up, when
// the run takes a release backup after it: Spanner takes one backup of a database at a
// time, so one it is still taking (the last release's, or the schedule's) would hold the
// backup step, and the maintenance page with it; the wait is here instead, with the
// application serving, and the backup step then starts at once.
func freeOfBackups(ctx context.Context, clients *Clients, subs, env map[string]string, out io.Writer) error {
	database, instance, err := spannerDatabase(env)
	if err != nil {
		return err
	}
	store, err := spannerAsApplyIdentity(ctx, clients, subs)
	if err != nil {
		return err
	}
	backup, err := store.LatestBackup(ctx, instance, database)
	if err != nil {
		return errors.Wrapf(err, "listing the backups of %s", path.Base(database))
	}
	if backup == nil || backup.State == BackupReady {
		return nil
	}
	fmt.Fprintf(out, "Before maintenance: Spanner is taking another backup of %s (%s); the release backup can start when it completes, so the wait is here, with %s serving.\n", path.Base(database), path.Base(backup.Name), subs[envSub])
	_, err = readyBackup(ctx, clients, store, backup, out)

	return err
}

// spannerAsApplyIdentity opens Spanner as the apply identity the build's trigger names
// (_APPLY_IDENTITY), which holds database admin on the environment's instance.
func spannerAsApplyIdentity(ctx context.Context, clients *Clients, subs map[string]string) (Spanner, error) {
	identity := subs[applyIdentitySub]
	if identity == "" {
		return nil, errors.Newf("%s names no apply identity (%s): the stack's triggers carry it", BuildFile, applyIdentitySub)
	}

	return clients.SpannerAs(ctx, identity)
}

// quiesce stops what the application does on its own once its traffic is on the
// maintenance revisions: the task queue is paused (and purged, on a restore), the serving
// build's running job executions are canceled, and the old revisions' requests in flight
// are let finish. What it did goes into the facts.
func (m *maintenance) quiesce(ctx context.Context, facts map[string]string, previous []Revision, moved time.Time) error {
	restore := m.env[restoreFact] != ""
	if queue := m.build.Substitutions[tasksQueueSub]; queue != "" {
		if err := m.pauseQueue(ctx, queue, restore); err != nil {
			return err
		}
		facts[maintenanceQueueFact] = queue
		facts[maintenancePurgedFact] = strconv.FormatBool(restore)
	} else {
		fmt.Fprintf(m.out, "No task queue to pause: the stack names none (%s).\n", tasksQueueSub)
	}
	canceled, err := m.cancelExecutions(ctx)
	if err != nil {
		return err
	}
	facts[maintenanceCanceledFact] = strconv.Itoa(canceled)
	facts[maintenanceWaitedFact] = m.waitForInFlight(ctx, previous, moved)

	return nil
}

// MaintenanceOff takes the application out of maintenance once the release's revision
// serves: the task queue is resumed, against the new release. A run that was not in
// maintenance has nothing to end. The maintenance revisions stay, as any old revision does.
func MaintenanceOff(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
	env, err := w.Environment()
	if err != nil {
		return err
	}
	build, err := w.Build()
	if err != nil {
		return err
	}
	if env[maintenanceOffFact] == trueValue {
		return endLeftMaintenance(ctx, clients, build, env, out)
	}
	if env[maintenanceFact] != trueValue {
		// The queue the trigger names is the environment's, which a pull-request build
		// shares with the release the environment serves. A pull-request build has its own
		// stack and never goes into maintenance, so the queue is not its to resume: a
		// restore of the environment may be in maintenance while the pull request builds.
		if build.Substitutions[prNumberSub] != "" {
			fmt.Fprintln(out, "No maintenance to end: a pull-request build never goes into maintenance, and the environment's queue is left as it is.")

			return nil
		}
		// A run that was not in maintenance still ends one an earlier run began and did
		// not end (a restore run that failed after maintenance on): the release this run
		// deployed serves now, and the queue must deliver again.
		if err := resumeLeftPaused(ctx, clients, build.Substitutions[tasksQueueSub], env[versionFact], out); err != nil {
			return err
		}
		fmt.Fprintln(out, "No maintenance to end: the application served throughout.")

		return nil
	}
	if queue := env[maintenanceQueueFact]; queue != "" {
		tasks, err := clients.Tasks(ctx)
		if err != nil {
			return err
		}
		state, err := tasks.Resume(ctx, queue)
		if err != nil {
			return errors.Wrapf(err, "resuming the queue %s", queue)
		}
		fmt.Fprintf(out, "Queue %s resumed (%s): tasks are delivered again, to %s.\n", shortName(queue), state, env[versionFact])
	}
	fmt.Fprintf(out, "=== Maintenance off: %s serves %s; the maintenance revision(s) %s take no traffic ===\n", build.Substitutions[envSub], env[versionFact], env[maintenanceRevisionsFact])

	return nil
}

// endLeftMaintenance is the whole of a run under the maintenance instruction (bedrock
// maintenance off): for every service the trigger names, when a maintenance revision
// serves (the maintenance variable set on it), all traffic goes back to the revision it
// displaced, named by the label the maintenance step put on it, or, for a maintenance
// revision from a build before the label, to the service's latest ready revision when
// that is another one; then the queue an earlier run left paused is resumed. A service
// not in maintenance is left as it is, and said.
func endLeftMaintenance(ctx context.Context, clients *Clients, build *Build, env map[string]string, out io.Writer) error {
	run, err := clients.Run(ctx)
	if err != nil {
		return err
	}
	subs := build.Substitutions
	fmt.Fprintf(out, "=== Maintenance off: %s, asked for by %s ===\n", subs[envSub], subs[requesterSub])
	for _, entry := range strings.Split(env[services], ",") {
		region, service, err := target(services, entry)
		if err != nil {
			return err
		}
		name := serviceName(subs[projectSub], region, service)
		doc, err := run.Get(ctx, name)
		if err != nil {
			return err
		}
		serving := servingRevision(doc)
		if serving == "" {
			fmt.Fprintf(out, "%s in %s serves no revision; nothing to move.\n", service, region)

			continue
		}
		revision, err := run.Get(ctx, name+"/revisions/"+serving)
		if err != nil {
			return errors.Wrapf(err, "reading the revision %s of %s", serving, service)
		}
		if !revisionInMaintenance(revision) {
			fmt.Fprintf(out, "%s in %s is not in maintenance: %s serves, with %s unset.\n", service, region, serving, derive.MaintenanceVariable)

			continue
		}
		back := text(revision, "labels."+displacedLabel)
		switch {
		case back != "":
			fmt.Fprintf(out, "%s in %s: the maintenance revision %s displaced %s (its %s label); traffic goes back to it.\n", service, region, serving, back, displacedLabel)
		case shortName(text(doc, "latestReadyRevision")) != "" && shortName(text(doc, "latestReadyRevision")) != serving:
			back = shortName(text(doc, "latestReadyRevision"))
			fmt.Fprintf(out, "%s in %s: the maintenance revision %s names no revision it displaced (a build before the label); traffic goes back to the latest ready revision, %s.\n", service, region, serving, back)
		default:
			return errors.Newf("%s in %s: the maintenance revision %s names no revision it displaced and is the latest ready revision itself; run the release again instead (bedrock rerun), which deploys and ends the maintenance", service, region, serving)
		}
		if err := shiftAll(ctx, run, name, back); err != nil {
			return errors.Wrapf(err, "moving %s's traffic in %s back to %s", service, region, back)
		}
		fmt.Fprintf(out, "%s in %s serves %s again; the maintenance revision %s takes no traffic.\n", service, region, back, serving)
	}
	if err := sayDatabaseState(ctx, clients, subs, out); err != nil {
		return err
	}

	return resumeLeftPaused(ctx, clients, subs[tasksQueueSub], env[versionFact], out)
}

// sayDatabaseState says what the application comes back to: a restore canceled after it
// dropped the environment's database leaves Spanner restoring the backup for twenty
// minutes or so, and a restored database has no memberships until the run's stack
// applies, so the revision traffic went back to cannot start on it (the lab's staging
// failed its startup probe for an hour after its database was READY) until bedrock
// rerun finishes the run; the step says so rather than ending on traffic alone. The
// database is the one the triggers name for the migrate command; a trigger that names
// none says nothing.
func sayDatabaseState(ctx context.Context, clients *Clients, subs map[string]string, out io.Writer) error {
	if subs[migrateDatabasesSub] == "" {
		return nil
	}
	database, _, err := currentDatabase(subs)
	if err != nil {
		return err
	}
	store, err := spannerAsApplyIdentity(ctx, clients, subs)
	if err != nil {
		return err
	}
	d, err := store.Database(ctx, database)
	if err != nil {
		return errors.Wrapf(err, "reading the database %s", path.Base(database))
	}
	switch {
	case d == nil:
		fmt.Fprintf(out, "The database %s is gone: a restore dropped it and stopped before the backup was restored; the application answers errors until bedrock rerun runs the release again or a restore makes it.\n", path.Base(database))
	case (d.State == DatabaseReady || d.State == DatabaseReadyOptimizing) && d.RestoredFrom == "":
		fmt.Fprintf(out, "The database %s is %s; the application serves on it.\n", path.Base(database), d.State)
	case d.State == DatabaseReady || d.State == DatabaseReadyOptimizing:
		fmt.Fprintf(out, "The database %s is %s, restored from %s. A restore run stopped before its stack applied leaves it without its memberships, and the application cannot start on it (its instances fail their startup probe): bedrock rerun finishes the run, whose apply gives the database its memberships, runs the migrations where the backup's schema is behind the release, and deploys.\n", path.Base(database), d.State, path.Base(d.RestoredFrom))
	default:
		from := ""
		if d.RestoredFrom != "" {
			from = " (Spanner is restoring it from " + path.Base(d.RestoredFrom) + ")"
		}
		fmt.Fprintf(out, "The database %s is %s%s: the application answers errors until Spanner has finished, about twenty minutes for a restore here, and cannot start on it until the run is finished, since a restored database has no memberships until the stack applies; bedrock rerun finishes the run, whose apply gives the database its memberships, runs the migrations where the backup's schema is behind the release, and deploys.\n", path.Base(database), d.State, from)
	}

	return nil
}

// revisionInMaintenance says whether a revision runs with the maintenance variable set:
// its first container's environment names it with the on value.
func revisionInMaintenance(revision map[string]any) bool {
	containers, _ := revision["containers"].([]any)
	if len(containers) == 0 {
		return false
	}
	container, _ := containers[0].(map[string]any)
	vars, _ := container["env"].([]any)
	for _, entry := range vars {
		v, _ := entry.(map[string]any)
		if text(v, keyName) == derive.MaintenanceVariable && text(v, keyValue) == maintenanceOn {
			return true
		}
	}

	return false
}

// resumeLeftPaused resumes the application's queue when an earlier run's maintenance
// left it paused; a queue the stack does not name, or one that delivers, is left alone.
func resumeLeftPaused(ctx context.Context, clients *Clients, queue, version string, out io.Writer) error {
	if queue == "" {
		return nil
	}
	tasks, err := clients.Tasks(ctx)
	if err != nil {
		return err
	}
	state, err := tasks.State(ctx, queue)
	if err != nil {
		return errors.Wrapf(err, "reading the queue %s", queue)
	}
	if state != queuePaused {
		return nil
	}
	state, err = tasks.Resume(ctx, queue)
	if err != nil {
		return errors.Wrapf(err, "resuming the queue %s", queue)
	}
	fmt.Fprintf(out, "Queue %s was left paused by an earlier run's maintenance; resumed (%s): tasks are delivered again, to %s.\n", shortName(queue), state, version)

	return nil
}

// sleep is the clients' waiting, the real one when none is set.
func (c *Clients) sleep() SleepFunc {
	if c.Sleep != nil {
		return c.Sleep
	}

	return sleepFor
}

// deployRevisions puts a maintenance revision in every region, with no traffic, and
// answers them with the revisions that serve now (the ones whose requests are let finish).
func (m *maintenance) deployRevisions(ctx context.Context, image string, labels map[string]string) (revisions, previous []Revision, err error) {
	for _, entry := range strings.Split(m.env[services], ",") {
		region, service, err := target(services, entry)
		if err != nil {
			return nil, nil, err
		}
		name := serviceName(m.project, region, service)
		doc, err := repair(ctx, m.run, name, service, m.out)
		if err != nil {
			return nil, nil, err
		}
		labeled := maps.Clone(labels)
		if serving := servingRevision(doc); serving != "" {
			previous = append(previous, Revision{Region: region, Service: service, Revision: serving})
			labeled[displacedLabel] = serving
		}
		m.timeout = requestTimeout(doc)
		revision, _, err := deployMaintenanceRevision(ctx, m.run, name, doc, image, labeled)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "the maintenance revision in region %s", region)
		}
		fmt.Fprintf(m.out, "Maintenance revision [%s] deployed to [%s] in [%s] under the tag [%s], with %s=%s and no traffic.\n", revision, service, region, nextTag, derive.MaintenanceVariable, maintenanceOn)
		revisions = append(revisions, Revision{Region: region, Service: service, Revision: revision})
	}

	return revisions, previous, nil
}

// open opens Cloud Run.
func (m *maintenance) open(ctx context.Context) error {
	run, err := m.clients.Run(ctx)
	if err != nil {
		return err
	}
	m.run = run
	if m.timeout == 0 {
		m.timeout = defaultRequestTimeout
	}

	return nil
}

// probe requires the maintenance revision, now the one the load balancer's next hostname
// serves, to answer 503 with the marker, trying for the probe window. Without a hostname
// (no load balancer) there is nothing to probe through, and the run stops: a maintenance
// revision that is not proven would serve the database while it is replaced.
func (m *maintenance) probe(ctx context.Context, url string) error {
	if url == "" {
		return errors.Newf("Build REJECTED: the maintenance revision cannot be probed: the stack names no hostname (%s) for the load balancer's next backend. Traffic did not move.", hostnameSub)
	}
	client := m.clients.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	var last string
	for try := 0; try < int(probeWindow/probeEvery); try++ {
		if try > 0 {
			if err := m.sleep(ctx, probeEvery); err != nil {
				return err
			}
		}
		status, marker, err := probeOnce(ctx, client, url)
		switch {
		case err != nil:
			last = err.Error()
		case status == http.StatusServiceUnavailable && marker == maintenanceHeaderValue:
			fmt.Fprintf(m.out, "Probe passed: %s answered 503 with %s: %s from the maintenance revision.\n", url, maintenanceHeader, marker)

			return nil
		default:
			last = fmt.Sprintf("answered %d with %s: %q", status, maintenanceHeader, marker)
		}
	}

	return errors.Newf("Build REJECTED: the maintenance revision ignores %s: %s %s after %s; the application's main does not check maintenance.Requested() before it builds its configuration (impulse check maintenance-switch names the fix). Traffic did not move.", derive.MaintenanceVariable, url, last, probeWindow)
}

// probeOnce asks the URL once, as a navigation, and answers the status and the marker.
func probeOnce(ctx context.Context, client *http.Client, url string) (status int, marker string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return 0, "", errors.Wrap(err, "http.NewRequestWithContext()")
	}
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", errors.Wrap(err, "http.Client.Do()")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxAnswer))

	return resp.StatusCode, resp.Header.Get(maintenanceHeader), nil
}

// pauseQueue pauses the application's queue, and purges it when the database is replaced:
// every queued task refers to rows that are about to disappear.
func (m *maintenance) pauseQueue(ctx context.Context, queue string, purge bool) error {
	tasks, err := m.clients.Tasks(ctx)
	if err != nil {
		return err
	}
	state, err := tasks.Pause(ctx, queue)
	if err != nil {
		return errors.Wrapf(err, "pausing the queue %s", queue)
	}
	fmt.Fprintf(m.out, "Queue %s paused (%s): tasks keep queuing up and none is delivered while the application is in maintenance.\n", shortName(queue), state)
	if !purge {
		return nil
	}
	if _, err := tasks.Purge(ctx, queue); err != nil {
		return errors.Wrapf(err, "purging the queue %s", queue)
	}
	fmt.Fprintf(m.out, "Queue %s purged: its tasks referred to rows the restore replaces.\n", shortName(queue))

	return nil
}

// cancelExecutions cancels the running executions of the serving build's job, the copy of
// the job process's template named by the live deployment's version; nothing new can
// start, since the server that starts jobs now answers 503. An application without a job
// process, or an environment with no live deployment, has nothing to cancel.
func (m *maintenance) cancelExecutions(ctx context.Context) (int, error) {
	template := m.build.Substitutions[jobsJobSub]
	if template == "" {
		fmt.Fprintln(m.out, "No job executions to cancel: the application has no job process (_JOBS_JOB).")

		return 0, nil
	}
	region, templateName, err := target("_JOBS_JOB", template)
	if err != nil {
		return 0, err
	}
	store, err := m.clients.Storage(ctx)
	if err != nil {
		return 0, err
	}
	defer store.Close()
	subs := m.build.Substitutions
	live, err := newestLiveRelease(ctx, store, subs[recordsBucket], subs[appSub], subs[envSub])
	if err != nil {
		return 0, err
	}
	if live == nil {
		fmt.Fprintln(m.out, "No job executions to cancel: the environment has no live deployment record.")

		return 0, nil
	}
	job := jobPrefix(m.project, region, templateName) + versionKey(live.Version)
	executions, err := m.run.Executions(ctx, job)
	if err != nil {
		if isNotFound(err) {
			fmt.Fprintf(m.out, "No job executions to cancel: %s has no job.\n", live.Version)

			return 0, nil
		}

		return 0, err
	}
	canceled := 0
	for _, e := range executions {
		if text(e, "completionTime") != "" {
			continue
		}
		name := text(e, keyName)
		if err := m.run.CancelExecution(ctx, name); err != nil {
			return canceled, errors.Wrapf(err, "canceling %s", shortName(name))
		}
		fmt.Fprintf(m.out, "Canceled the running execution %s of %s's job: the run interrupts everything the application is doing.\n", shortName(name), live.Version)
		canceled++
	}
	if canceled == 0 {
		fmt.Fprintf(m.out, "No running execution of %s's job to cancel.\n", live.Version)
	}

	return canceled, nil
}

// waitForInFlight lets the revisions that served before maintenance finish the requests
// they hold: Cloud Run sends them no new requests, and each request is bounded by the
// service's timeout. The old revisions' active instances are read until none is, or
// until the timeout has passed since traffic moved, whichever is first; a metric that
// cannot be read leaves the timeout as the guarantee. It answers how the wait ended.
func (m *maintenance) waitForInFlight(ctx context.Context, previous []Revision, moved time.Time) string {
	if len(previous) == 0 {
		fmt.Fprintln(m.out, "No revision served before maintenance; nothing in flight to wait for.")

		return "nothing served before"
	}
	deadline := moved.Add(m.timeout)
	metrics, err := m.clients.Metrics(ctx)
	if err != nil {
		fmt.Fprintf(m.out, "The active-instance metric cannot be opened (%v); waiting the request timeout (%s) instead.\n", errors.Cause(err), m.timeout)
		metrics = nil
	}
	for {
		if metrics != nil {
			active, unknown, err := activeAcross(ctx, metrics, m.project, previous)
			switch {
			case err != nil:
				fmt.Fprintf(m.out, "The active-instance metric cannot be read (%v); waiting the request timeout (%s) instead.\n", errors.Cause(err), m.timeout)
				metrics = nil
			case active == 0:
				waited := time.Since(moved).Round(time.Second)
				how := "no active instance"
				if unknown {
					how = "no active instance reported"
				}
				fmt.Fprintf(m.out, "Requests in flight finished: %s on the old revision(s) after %s.\n", how, waited)

				return fmt.Sprintf("%s: %s", waited, how)
			default:
				fmt.Fprintf(m.out, "%d active instance(s) still finish requests on the old revision(s); waiting.\n", active)
			}
		}
		if remaining := time.Until(deadline); remaining <= 0 {
			fmt.Fprintf(m.out, "The request timeout (%s) has passed since traffic moved: every request the old revision(s) held is over.\n", m.timeout)

			return fmt.Sprintf("%s: request timeout", m.timeout)
		}
		if err := m.sleep(ctx, min(waitEvery, time.Until(deadline))); err != nil {
			return "interrupted"
		}
	}
}

// activeAcross sums the active instances of the revisions; unknown is true when no
// revision reported a point.
func activeAcross(ctx context.Context, metrics Metrics, project string, revisions []Revision) (active int, unknown bool, err error) {
	unknown = true
	for _, r := range revisions {
		count, known, err := metrics.ActiveInstances(ctx, project, r.Revision, metricWindow)
		if err != nil {
			return 0, false, err
		}
		if known {
			unknown = false
		}
		active += count
	}

	return active, unknown, nil
}

// servingRevision is the revision the service's traffic serves now, by its statuses.
func servingRevision(doc map[string]any) string {
	serving, _ := doc["trafficStatuses"].([]any)
	for _, entry := range serving {
		status, _ := entry.(map[string]any)
		if percent, _ := status[keyPercent].(float64); percent > 0 {
			if revision := text(status, keyRevision); revision != "" {
				return revision
			}

			return shortName(text(doc, "latestReadyRevision"))
		}
	}

	return ""
}

// requestTimeout is the service's request timeout as its template names it ("300s"),
// or the default.
func requestTimeout(doc map[string]any) time.Duration {
	if d, err := time.ParseDuration(text(doc, "template.timeout")); err == nil && d > 0 {
		return d
	}

	return defaultRequestTimeout
}

// deployMaintenanceRevision is deployRevision with the maintenance variable set on the
// container: the first variable the stack declares, set to 1 here (added when the stack
// of an older render does not declare it).
func deployMaintenanceRevision(ctx context.Context, run Run, name string, doc map[string]any, image string, labels map[string]string) (revision, uri string, err error) {
	template, _ := doc[keyTemplate].(map[string]any)
	container, err := firstContainer(template)
	if err != nil {
		return "", "", err
	}
	vars, _ := container["env"].([]any)
	set := false
	for _, entry := range vars {
		v, _ := entry.(map[string]any)
		if text(v, keyName) == derive.MaintenanceVariable {
			v[keyValue] = maintenanceOn
			delete(v, "valueSource")
			set = true
		}
	}
	if !set {
		vars = append(vars, map[string]any{keyName: derive.MaintenanceVariable, keyValue: maintenanceOn})
	}
	container["env"] = vars

	return deployRevision(ctx, run, name, doc, image, labels, nextTag)
}

// shiftAll moves all of the service's traffic to the revision, keeping the tags other
// revisions carry, the way shift-traffic does.
func shiftAll(ctx context.Context, run Run, name, revision string) error {
	doc, err := run.Get(ctx, name)
	if err != nil {
		return err
	}
	traffic := []any{map[string]any{keyType: targetRevision, keyRevision: revision, keyPercent: fullTraffic}}
	current, _ := doc[keyTraffic].([]any)
	for _, entry := range current {
		t, _ := entry.(map[string]any)
		if text(t, keyTag) != "" && text(t, keyRevision) != revision {
			traffic = append(traffic, map[string]any{keyType: t[keyType], keyRevision: t[keyRevision], keyPercent: float64(0), keyTag: t[keyTag]})
		}
	}
	if _, err := run.Patch(ctx, name, map[string]any{keyName: name, keyTraffic: traffic}, "traffic"); err != nil {
		return err
	}

	return nil
}
