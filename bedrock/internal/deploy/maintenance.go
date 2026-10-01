// maintenance.go puts the application into maintenance for a run that replaces or
// interrupts its database (a restore run), and takes it out once the release serves.

package deploy

import (
	"context"
	"fmt"
	"io"
	"net/http"
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
	// maintenanceOn is the value the maintenance variable takes on a maintenance revision.
	maintenanceOn = "1"
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

// MaintenanceOn puts the application into maintenance when the run needs it: a restore
// run, whose database is replaced before the release deploys. The release's own image
// starts as a revision with the maintenance variable set, in every region, under the tag
// next, receiving no traffic; the revision is probed through the load balancer's next
// hostname and must answer 503 with the marker, else the run stops here with nothing
// moved, naming the application's missing switch; then all traffic moves to it, the
// application's task queue is paused (and, on a restore, purged), the running executions
// of the serving build's job are canceled, and the old revision's requests in flight are
// let finish: its active instances are read until none is, or until the service's request
// timeout has passed since traffic moved. Any other run keeps the application serving.
func MaintenanceOn(ctx context.Context, clients *Clients, w Workspace, out io.Writer) error {
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
	switch {
	case env[restoreFact] == "":
		fmt.Fprintln(out, "No maintenance: this run keeps the application serving (a restore run starts a maintenance revision first).")

		return nil
	case subs[prNumberSub] != "":
		fmt.Fprintln(out, "No maintenance: a pull-request build never goes into maintenance.")

		return nil
	}
	if env[imageFact] == "" || env[digestFact] == "" {
		return errors.Newf("%s names no image digest (IMAGE, IMAGE_DIGEST): the image build writes it", EnvironmentFile)
	}
	if env[services] == "" {
		return errors.Newf("%s names no services (SERVICES): the resolve step writes them", EnvironmentFile)
	}
	m := &maintenance{clients: clients, build: build, env: env, out: out, project: subs[projectSub], sleep: clients.sleep()}
	if err := m.open(ctx); err != nil {
		return err
	}
	fmt.Fprintf(out, "=== Maintenance on: %s is replaced before %s deploys, so the application serves its maintenance page meanwhile ===\n", env[restoreFact], env[versionFact])
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
	fmt.Fprintf(out, "Maintenance is on: %d revision(s) serve the maintenance page; the database may be replaced.\n", len(revisions))

	return nil
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
	if env[maintenanceFact] != trueValue {
		fmt.Fprintln(out, "No maintenance to end: the application served throughout.")

		return nil
	}
	build, err := w.Build()
	if err != nil {
		return err
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
		if serving := servingRevision(doc); serving != "" {
			previous = append(previous, Revision{Region: region, Service: service, Revision: serving})
		}
		m.timeout = requestTimeout(doc)
		revision, _, err := deployMaintenanceRevision(ctx, m.run, name, doc, image, labels)
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
	template := m.build.Substitutions["_JOBS_JOB"]
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
	live, err := newestRecordWhere(ctx, store, subs[recordsBucket], subs[appSub]+"/"+subs[envSub]+"/", func(r *Record) bool {
		return r.Status == Live
	})
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
		fmt.Fprintf(m.out, "Canceled the running execution %s of %s's job: a restore interrupts everything the application is doing.\n", shortName(name), live.Version)
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
