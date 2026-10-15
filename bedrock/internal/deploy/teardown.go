// teardown.go is an environment's stack taken down by a person at their own computer:
// bedrock teardown <env>, which destroys what a pipeline makes again (the Cloud Run
// services and jobs, the endpoint groups and backend services, the triggers, the
// schedules, the Cloud Armor policy, the request-log exclusion) and keeps what holds data
// or a name (the database and every generation of it, the file stores, the secret
// containers, the identities with their grants, the Firestore database, the Firebase key,
// the task queue, the migrate logs, the stack's id); and bedrock destroy <env>, which
// destroys the whole stack. Both drive tofu in the checkout's stack directory as the
// person (their own sign-in, no identity impersonated), against the environment's state
// prefix, the way the first apply by hand does: an apply that records the protections
// lifted where production holds them (var.teardown, var.destroy), a destroy plan with
// the kept resources excluded, the plan read back and held against what the command
// means to destroy, the apply of that plan, and a record of what was done beside the
// stack's id. Neither runs while 2-net routes a host to the environment's backend
// service: Google refuses the backend's deletion while the URL map uses it, and OpenTofu
// keeps everything an excluded resource depends on, so the services could not go either.

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

const (
	// teardownVar and destroyVar are the stack variables the two commands alone set.
	teardownVar = "teardown"
	destroyVar  = "destroy"
	// stateObjectName is the object a layer's or a stack's state is, under its prefix;
	// netLayer is the prefix of 2-net's.
	stateObjectName = "default.tfstate"
	netLayer        = "2-net"
	// hostsOutput is 2-net's output of the hosts it routes, by backend service; and
	// netHostsOutput the stack's output of the hosts it offers, the same way.
	hostsOutput    = "hosts"
	netHostsOutput = "net_hosts"
	// substitutionsOutput is the stack's output of the version trigger's substitutions,
	// where the records bucket and the project are read from the state.
	substitutionsOutput = "substitutions"
	// teardownPlanFile is the saved destroy plan, in the run's data directory.
	teardownPlanFile = "teardown.tfplan"
	// teardownObjectName is the record of a teardown or a destroy, beside the stack's id.
	teardownObjectName = "teardown.json"
	// dataPrefix starts the address of a data resource in the state list.
	dataPrefix = "data."
	// pullRequestSegment starts a pull request's state prefix under the environment's.
	pullRequestSegment = "/pr"
	// deleteAction is the plan action the command expects on everything it destroys;
	// managedMode the mode of a resource that is not a data source.
	deleteAction = "delete"
	managedMode  = "managed"
	// serviceType is the Cloud Run service's resource type, destroyed by both commands
	// and protected in production.
	serviceType = "google_cloud_run_v2_service"
	// applyCmd, destroyCmd and the flags the commands pass to tofu by hand.
	applyCmd      = "apply"
	destroyCmd    = "destroy"
	autoApprove   = "-auto-approve"
	destroyFlag   = "-destroy"
	excludeFlag   = "-exclude="
	targetFlag    = "-target="
	reconfigure   = "-reconfigure"
	backendPrefix = "-backend-config=prefix="
)

// teardownTypes are the resource types a teardown destroys: what a pipeline makes again,
// and what the load balancer and the schedules reach the services through. Every other
// type is kept, google_service_account.scheduler alone excepted (teardownNames): the
// schedules' identity goes with the schedules.
var teardownTypes = map[string]bool{
	serviceType:                                    true,
	"google_cloud_run_v2_service_iam_member":       true,
	"google_cloud_run_v2_job":                      true,
	"google_cloud_run_v2_job_iam_member":           true,
	"google_tags_location_tag_binding":             true,
	"time_sleep":                                   true,
	"google_compute_region_network_endpoint_group": true,
	"google_compute_backend_service":               true,
	"google_compute_security_policy":               true,
	"google_cloudbuild_trigger":                    true,
	"google_cloud_scheduler_job":                   true,
	"google_logging_project_exclusion":             true,
}

// teardownNames are the resources a teardown destroys by name, of a type it otherwise
// keeps.
var teardownNames = map[string]bool{
	"google_service_account.scheduler": true,
}

// protectedTypes are the resource types production protects from deletion, which a
// teardown lifts the protection of; destroyProtectedTypes the ones a destroy lifts
// besides (the database's deletion and drop protection, the file stores' force-destroy,
// the Firestore database's delete protection).
var (
	protectedTypes        = map[string]bool{serviceType: true}
	destroyProtectedTypes = map[string]bool{"google_spanner_database": true, "google_storage_bucket": true, "google_firestore_database": true}
)

// TeardownRequest is one environment's teardown, or its destroy with Destroy.
type TeardownRequest struct {
	// Dir is the stack directory of the checkout; DataDir the run's directory for what
	// tofu keeps between its commands (TF_DATA_DIR): each environment's stack gets a
	// directory of its own under it, named after the environment, and the pull-request
	// stacks share one, since a backend's prefix is recorded at init and a run prepares
	// every environment before any runs.
	Dir     string
	DataDir string
	// Placement is the application's placement beside the stack; App the application
	// and Env the environment.
	Placement *derive.Placement
	App       string
	Env       string
	// Destroy destroys the whole stack instead of tearing it down.
	Destroy bool
	// By names who runs the command, for the record; Now is the clock it is dated by.
	By  string
	Now func() time.Time
}

// command is the command's name, which is also the stack variable it sets.
func (r *TeardownRequest) command() string {
	if r.Destroy {
		return destroyVar
	}

	return teardownVar
}

// EnvironmentTeardown is one environment's teardown or destroy, prepared: the stack
// initialized at the environment's prefix, its state listed and sorted into what goes
// and what stays, the protections production holds found, and the records read for the
// plan's variables and for the record. Run does it.
type EnvironmentTeardown struct {
	req     *TeardownRequest
	clients *Clients
	s       *stack
	// destroyed and kept are the stack's managed resources sorted; protected are the
	// ones whose protection the apply before the destroy lifts; dependents are the
	// resources of a kept type that go with what they depend on, each with what it
	// depends on.
	destroyed  []string
	kept       []string
	protected  []string
	dependents []string
	// generation and pins are the plan's variables, as the pipeline's last plan had
	// them; live is the environment's live release, or nil; id the stack's id, or "".
	generation int
	pins       map[string]string
	live       *Record
	id         string
	// records is the environment's records bucket, from the stack's state.
	records string
}

// PrepareTeardown reads what the environment's teardown or destroy needs and refuses what
// cannot go: the stack's state in the state bucket (none, or one with no outputs, the
// empty state an init writes: the stack has not been applied, said on out, and nil is
// answered), 2-net's hosts (a host routed to the environment's
// backend service refuses the run), the records (the database generation, the generated
// secrets' pins, the live release), and the stack's state listed after its init.
func PrepareTeardown(ctx context.Context, clients *Clients, req *TeardownRequest, out io.Writer) (*EnvironmentTeardown, error) {
	store, err := clients.Storage(ctx)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	bucket, app, env := req.Placement.StateBucket, req.App, req.Env
	state, ok, err := readState(ctx, store, bucket, statePrefix(app, env))
	if err != nil {
		return nil, err
	}
	switch {
	case !ok:
		fmt.Fprintf(out, "No stack in %s: nothing at gs://%s/%s/%s, so the stack has not been applied there, and there is nothing to take down.\n", env, bucket, statePrefix(app, env), stateObjectName)

		return nil, nil
	case len(state.Outputs) == 0:
		fmt.Fprintf(out, "No stack in %s: gs://%s/%s/%s is empty (an init wrote it, no apply did), so there is nothing to take down.\n", env, bucket, statePrefix(app, env), stateObjectName)

		return nil, nil
	}
	if err := refuseRouted(ctx, store, bucket, state, req); err != nil {
		return nil, err
	}
	subs, err := state.stringMap(substitutionsOutput)
	if err != nil {
		return nil, err
	}
	t := &EnvironmentTeardown{req: req, clients: clients, records: subs[recordsBucket]}
	if t.records == "" {
		return nil, errors.Newf("%s's stack names no records bucket in its state (output %s, %s): it was applied by a bedrock before the records; apply it once with this one first", env, substitutionsOutput, recordsBucket)
	}
	if err := t.readRecords(ctx, store); err != nil {
		return nil, err
	}
	t.s = handStack(clients, req.Dir, req.dataDir(), out)
	fmt.Fprintf(out, "=== %s: %s of %s's stack at %s ===\n", env, req.command(), app, statePrefix(app, env))
	if err := t.s.initAt(ctx, statePrefix(app, env)); err != nil {
		return nil, err
	}
	listed, err := t.s.tofuOutput(ctx, "state", "list")
	if err != nil {
		return nil, err
	}
	t.sortResources(managedAddresses(string(listed)))
	if !req.Destroy && len(t.destroyed) > 0 {
		shown, err := t.s.tofuOutput(ctx, "show", "-json")
		if err != nil {
			return nil, err
		}
		dependencies, err := stateDependencies(shown)
		if err != nil {
			return nil, err
		}
		t.sweepDependents(dependencies)
	}

	return t, nil
}

// stateDependencies reads, off tofu show -json of the state, what each managed resource
// depends on: resource addresses without their instance keys, as the state records
// them.
func stateDependencies(shown []byte) (map[string][]string, error) {
	var state struct {
		Values map[string]json.RawMessage `json:"values"`
	}
	if err := json.Unmarshal(shown, &state); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): tofu show -json of the state")
	}
	var module struct {
		Resources []map[string]json.RawMessage `json:"resources"`
	}
	if raw, ok := state.Values["root_module"]; ok {
		if err := json.Unmarshal(raw, &module); err != nil {
			return nil, errors.Wrap(err, "json.Unmarshal(): tofu show -json of the state, root_module")
		}
	}
	dependencies := map[string][]string{}
	for _, r := range module.Resources {
		var address, mode string
		var dependsOn []string
		if err := errors.Join(unmarshalField(r, "address", &address), unmarshalField(r, "mode", &mode), unmarshalField(r, "depends_on", &dependsOn)); err != nil {
			return nil, errors.Wrap(err, "json.Unmarshal(): tofu show -json of the state, resources")
		}
		if mode == managedMode {
			dependencies[address] = dependsOn
		}
	}

	return dependencies, nil
}

// unmarshalField reads one field of a JSON object into v; an absent field leaves v as is.
func unmarshalField(object map[string]json.RawMessage, name string, v any) error {
	raw, ok := object[name]
	if !ok {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return errors.Wrap(err, name)
	}

	return nil
}

// sweepDependents moves a kept resource that depends on a destroyed one over to the
// destroyed, to the closure: OpenTofu keeps an excluded resource's dependencies with it,
// so a kept resource holding on to a destroyed one would hold the teardown short (the
// migrate-log sink names the version trigger in its filter, and the trigger names the
// services). What goes this way is of a kept type and made again by the next apply.
func (t *EnvironmentTeardown) sweepDependents(dependencies map[string][]string) {
	for swept := true; swept; {
		swept = false
		destroyed := map[string]bool{}
		for _, address := range t.destroyed {
			destroyed[resourceName(address)] = true
		}
		var kept []string
		for _, address := range t.kept {
			var on []string
			for _, dependency := range dependencies[address] {
				if destroyed[dependency] {
					on = append(on, dependency)
				}
			}
			if len(on) == 0 {
				kept = append(kept, address)

				continue
			}
			t.destroyed = append(t.destroyed, address)
			t.dependents = append(t.dependents, address+" ("+strings.Join(on, ", ")+")")
			swept = true
		}
		t.kept = kept
	}
}

// refuseRouted refuses the run while 2-net routes a host to one of the environment's
// backend services: the hosts 2-net's state says it routes, held against the backends
// the stack's state offers (its net_hosts output).
func refuseRouted(ctx context.Context, store Store, bucket string, state *stateFile, req *TeardownRequest) error {
	offered, err := state.stringMap(netHostsOutput)
	if err != nil {
		return err
	}
	net, ok, err := readState(ctx, store, bucket, netLayer)
	if err != nil || !ok {
		return err
	}
	hosts, err := net.stringMap(hostsOutput)
	if err != nil {
		return err
	}
	backends := map[string]bool{}
	for _, backend := range offered {
		backends[backend] = true
	}
	var routed []string
	for _, host := range slices.Sorted(maps.Keys(hosts)) {
		if backends[hosts[host]] {
			routed = append(routed, host+" to "+hosts[host])
		}
	}
	if len(routed) == 0 {
		return nil
	}

	return errors.Newf("2-net routes %s: the load balancer uses %s's backend service, Google refuses its deletion while it does, and the services behind it cannot go either. Take the hosts out of 2-net first, one of three ways, each through an organization pull request: bedrock org teardown, whose first step does; %s's registration set back to grants in placement.json (registration), which takes its hostnames out and leaves the rest; or %s's removal from placement.json; then run bedrock %s again", strings.Join(routed, ", "), req.Env, req.App, req.App, req.command())
}

// readRecords reads what the plan's variables and the record come from: the stack's id,
// the database generation the pipeline's last plan had, the generated secrets' pins the
// live release runs, and that release.
func (t *EnvironmentTeardown) readRecords(ctx context.Context, store Store) error {
	app, env := t.req.App, t.req.Env
	id, err := stackID(ctx, store, t.records, app, env)
	if err != nil {
		return err
	}
	t.id = id
	t.generation, _, err = databaseGeneration(ctx, store, t.records, app, env)
	if err != nil {
		return err
	}
	t.live, err = newestLiveRelease(ctx, store, t.records, app, env)
	if err != nil {
		return err
	}
	if t.live != nil {
		t.pins = t.live.GeneratedSecrets
	}

	return nil
}

// sortResources sorts the stack's managed resources into what goes and what stays, and
// finds the protected ones among what goes: a destroy takes everything, a teardown the
// types and names of a teardown; production protects its services, and against a destroy
// its database, file stores and Firestore database.
func (t *EnvironmentTeardown) sortResources(addresses []string) {
	production := t.req.Env == t.req.Placement.Production()
	for _, address := range addresses {
		typ := resourceType(address)
		goes := t.req.Destroy || teardownTypes[typ] || teardownNames[resourceName(address)]
		if !goes {
			t.kept = append(t.kept, address)

			continue
		}
		t.destroyed = append(t.destroyed, address)
		if production && (protectedTypes[typ] || (t.req.Destroy && destroyProtectedTypes[typ])) {
			t.protected = append(t.protected, address)
		}
	}
}

// Summary says what the run does in the environment, for the person to read before it
// runs: what goes and what stays, by type, the protections lifted first, and the live
// release.
func (t *EnvironmentTeardown) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d resource(s) destroyed, %d kept.\n", t.req.Env, len(t.destroyed), len(t.kept))
	if len(t.destroyed) > 0 {
		fmt.Fprintf(&b, "  goes: %s\n", countByType(t.destroyed))
	}
	if len(t.kept) > 0 {
		fmt.Fprintf(&b, "  stays: %s\n", countByType(t.kept))
	}
	if len(t.protected) > 0 {
		fmt.Fprintf(&b, "  protection lifted first: %s\n", strings.Join(t.protected, ", "))
	}
	if len(t.dependents) > 0 {
		fmt.Fprintf(&b, "  goes with what it depends on, made again by the next apply: %s\n", strings.Join(t.dependents, "; "))
	}
	if t.live != nil {
		fmt.Fprintf(&b, "  live release: %s (its records stay under %s/%s/%s/)\n", t.live.Version, t.req.App, t.req.Env, t.id)
	}

	return b.String()
}

// Run does the environment's teardown or destroy: the apply that lifts the protections
// production holds, the destroy plan with the kept resources excluded, the plan held
// against what the command means to destroy, its apply, and the record.
func (t *EnvironmentTeardown) Run(ctx context.Context, out io.Writer) error {
	env, command := t.req.Env, t.req.command()
	if len(t.destroyed) == 0 {
		fmt.Fprintf(out, "Nothing of %s's stack is for a %s to destroy: it holds %d resource(s), every one kept.\n", env, command, len(t.kept))

		return nil
	}
	if len(t.protected) > 0 {
		fmt.Fprintf(out, "Recording in %s's state that %s lifts the protection of %s.\n", env, command, strings.Join(t.protected, ", "))
		args := append([]string{applyCmd, noInputFlag, noColorFlag, autoApprove}, t.vars()...)
		for _, address := range t.protected {
			args = append(args, targetFlag+address)
		}
		if err := t.s.tofu(ctx, args...); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(t.req.dataDir(), 0o750); err != nil {
		return errors.Wrap(err, "os.MkdirAll()")
	}
	plan := filepath.Join(t.req.dataDir(), teardownPlanFile)
	args := append([]string{planCommand, destroyFlag, noInputFlag, noColorFlag, "-out=" + plan}, t.vars()...)
	for _, address := range t.kept {
		args = append(args, excludeFlag+address)
	}
	if err := t.s.tofu(ctx, args...); err != nil {
		return err
	}
	shown, err := t.s.tofuOutput(ctx, "show", "-json", plan)
	if err != nil {
		return err
	}
	planned, err := plannedDeletions(shown)
	if err != nil {
		return err
	}
	if err := t.checkPlanned(planned); err != nil {
		return err
	}
	fmt.Fprintf(out, "The plan destroys the %d resource(s) the %s names and keeps %d, as meant; applying it.\n", len(planned), command, len(t.kept))
	if err := t.s.tofu(ctx, applyCmd, noInputFlag, noColorFlag, plan); err != nil {
		return err
	}
	if err := t.writeRecord(ctx); err != nil {
		return err
	}
	fmt.Fprint(out, t.epilogue())

	return nil
}

// vars are the plan's variables: the environment, the database generation and the
// generated secrets' pins as the pipeline's last plan had them (so that the apply before
// the destroy changes nothing else), and the command's own.
func (t *EnvironmentTeardown) vars() []string {
	args := []string{varFlag, "environment=" + t.req.Env, varFlag, "database_generation=" + strconv.Itoa(t.generation)}
	args = append(args, generatedArgs(t.pins)...)

	return append(args, varFlag, t.req.command()+"=true")
}

// checkPlanned holds the plan's deletions against what the command means to destroy:
// anything beyond it, or anything left out of it, stops the run with nothing applied.
func (t *EnvironmentTeardown) checkPlanned(planned []string) error {
	beyond := difference(planned, t.destroyed)
	left := difference(t.destroyed, planned)
	if len(beyond) == 0 && len(left) == 0 {
		return nil
	}
	var reasons []string
	if len(beyond) > 0 {
		reasons = append(reasons, "it destroys what the "+t.req.command()+" keeps: "+strings.Join(beyond, ", "))
	}
	if len(left) > 0 {
		reasons = append(reasons, "it leaves out what the "+t.req.command()+" destroys (the state changed under the run): "+strings.Join(left, ", "))
	}

	return errors.Newf("the destroy plan of %s's stack is not the %s's; %s. Nothing was applied", t.req.Env, t.req.command(), strings.Join(reasons, "; "))
}

// TeardownRecord is what a teardown or a destroy leaves beside the stack's id
// (<app>/stacks/<env>/teardown.json): what was done, when, by whom, to which stack, with
// the release that was live and what went and stayed. A destroy takes the stack's id
// object with the stack (google_storage_bucket_object.stack), so a reader finding the
// record without the id knows the stack is gone; one finding both with the same id
// knows the environment is torn down; one finding an id the record does not name knows
// the record is an earlier stack's.
type TeardownRecord struct {
	Kind      string   `json:"kind"`
	App       string   `json:"app"`
	Env       string   `json:"env"`
	StackID   string   `json:"stackId,omitempty"`
	At        string   `json:"at"`
	By        string   `json:"by"`
	Release   string   `json:"release,omitempty"`
	Destroyed []string `json:"destroyed"`
	Kept      []string `json:"kept,omitempty"`
}

// teardownObject is the record's object in the records bucket.
func teardownObject(app, env string) string {
	return app + "/" + stacksSegment + "/" + env + "/" + teardownObjectName
}

// writeRecord writes the record of what was done to the records bucket.
func (t *EnvironmentTeardown) writeRecord(ctx context.Context) error {
	r := TeardownRecord{Kind: t.req.command(), App: t.req.App, Env: t.req.Env, StackID: t.id, At: t.req.Now().UTC().Format(time.RFC3339), By: t.req.By, Destroyed: t.destroyed, Kept: t.kept}
	if t.live != nil {
		r.Release = t.live.Version
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return errors.Wrap(err, "json.MarshalIndent()")
	}
	store, err := t.clients.Storage(ctx)
	if err != nil {
		return err
	}
	defer store.Close()

	return store.Write(ctx, t.records, teardownObject(t.req.App, t.req.Env), data)
}

// epilogue says what the run left and what comes next: for a teardown, how the
// environment comes back; for a destroy, what stays outside the stack.
func (t *EnvironmentTeardown) epilogue() string {
	app, env := t.req.App, t.req.Env
	var b strings.Builder
	if t.req.Destroy {
		fmt.Fprintf(&b, "Destroyed %s's stack in %s: %d resource(s). A record went to gs://%s/%s; the deployment records under %s/%s/%s/ stay, read by nothing; the Spanner backups the pipeline and the schedules took stay until their retention ends; Google keeps the names of the task queue, the Firebase key and the migrate log bucket for a while, and a stack applied again takes an id of its own.\n", app, env, len(t.destroyed), t.records, teardownObject(app, env), app, env, t.id)

		return b.String()
	}
	fmt.Fprintf(&b, "Tore %s's stack in %s down: %d resource(s) destroyed, %d kept (the database, the file stores, the secret containers, the identities, the Firestore database, the task queue, the migrate logs, the stack's id). A record went to gs://%s/%s.\n", app, env, len(t.destroyed), len(t.kept), t.records, teardownObject(app, env))
	fmt.Fprintf(&b, "To bring it back: apply the stack by hand from the checkout of the release it ran (infrastructure/README.md, \"Applying\"), which makes the services, the triggers and the rest again, then bedrock rerun %s <release> deploys the release through them.\n", env)

	return b.String()
}

// PullRequestStacks are the numbers of the pull-request stacks under the integration
// environment's prefix in the state bucket, sorted: each has a state of its own at
// 3-app/<app>/<integration>/pr<N>/default.tfstate.
func PullRequestStacks(ctx context.Context, clients *Clients, req *TeardownRequest) ([]string, error) {
	store, err := clients.Storage(ctx)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	prefix := statePrefix(req.App, req.Placement.Integration()) + pullRequestSegment
	objects, err := store.List(ctx, req.Placement.StateBucket, prefix)
	if err != nil {
		return nil, errors.Wrapf(err, "listing gs://%s/%s", req.Placement.StateBucket, prefix)
	}
	var numbers []string
	for _, object := range objects {
		number, rest, ok := strings.Cut(strings.TrimPrefix(object, prefix), "/")
		if !ok || rest != stateObjectName {
			continue
		}
		if _, err := strconv.Atoi(number); err != nil {
			continue
		}
		numbers = append(numbers, number)
	}
	sort.Slice(numbers, func(i, j int) bool {
		a, _ := strconv.Atoi(numbers[i])
		b, _ := strconv.Atoi(numbers[j])

		return a < b
	})

	return numbers, nil
}

// TeardownPullRequests destroys every pull-request stack under the integration
// environment's prefix, each the way the sweep destroys a closed pull request's: its
// builds' jobs deleted in the environment's project (which the placement records), then
// the stack from its own prefix. Nothing of a pull request's stack is kept.
func TeardownPullRequests(ctx context.Context, clients *Clients, req *TeardownRequest, numbers []string, out io.Writer) error {
	integration := req.Placement.Integration()
	project, ok := req.Placement.Project(integration)
	if !ok {
		return errors.Newf("placement.json records no project for %s (projects.%s): the jobs of a pull request's builds are deleted in it before its stack", integration, integration)
	}
	subs := map[string]string{appSub: req.App, envSub: integration, projectSub: project.ID}
	s := handStack(clients, req.Dir, filepath.Join(req.DataDir, pullRequestsDataDir), out)
	for _, number := range numbers {
		prefix := pullRequestPrefix(subs, number)
		fmt.Fprintf(out, "=== Pull request %s: stack at %s ===\n", number, prefix)
		if err := s.initAt(ctx, prefix); err != nil {
			return err
		}
		if err := destroyPullRequestStack(ctx, clients, s, subs, number, out); err != nil {
			return err
		}
	}

	return nil
}

// pullRequestsDataDir is the pull-request stacks' directory under the run's data
// directory; an environment's is named after it.
const pullRequestsDataDir = "pull-requests"

// dataDir is the environment's stack's directory under the run's data directory.
func (r *TeardownRequest) dataDir() string {
	return filepath.Join(r.DataDir, r.Env)
}

// handStack is a stack driven by hand: as the person, no identity impersonated, in the
// stack's data directory.
func handStack(clients *Clients, dir, dataDir string, out io.Writer) *stack {
	return &stack{run: clients.Exec, dir: dir, dataDir: dataDir, out: out, clients: clients}
}

// stateFile is a state object as the backend writes it: the outputs the command reads.
type stateFile struct {
	Outputs map[string]struct {
		Value json.RawMessage `json:"value"`
	} `json:"outputs"`
}

// readState reads the state object at the prefix in the state bucket; ok is false when
// there is none, the layer or the stack never having been applied.
func readState(ctx context.Context, store Store, bucket, prefix string) (state *stateFile, ok bool, err error) {
	object := prefix + "/" + stateObjectName
	data, err := store.Read(ctx, bucket, object)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}

		return nil, false, err
	}
	var s stateFile
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, false, errors.Wrapf(err, "json.Unmarshal(): gs://%s/%s", bucket, object)
	}

	return &s, true, nil
}

// stringMap is the named output as a map of strings: empty when the state lacks the
// output or it is null.
func (s *stateFile) stringMap(name string) (map[string]string, error) {
	output, ok := s.Outputs[name]
	if !ok || len(output.Value) == 0 || string(output.Value) == "null" {
		return nil, nil
	}
	var m map[string]string
	if err := json.Unmarshal(output.Value, &m); err != nil {
		return nil, errors.Wrapf(err, "json.Unmarshal(): the state's output %s", name)
	}

	return m, nil
}

// managedAddresses are the managed resources the state list names, in its order: every
// line that is not a data resource.
func managedAddresses(listed string) []string {
	var addresses []string
	for _, line := range strings.Split(listed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, dataPrefix) {
			continue
		}
		addresses = append(addresses, line)
	}

	return addresses
}

// resourceName is the address without its instance key: google_cloud_run_v2_service.app
// for google_cloud_run_v2_service.app["uc1"].
func resourceName(address string) string {
	if i := strings.Index(address, "["); i >= 0 {
		return address[:i]
	}

	return address
}

// resourceType is the address's resource type, the part before the first dot.
func resourceType(address string) string {
	name := resourceName(address)
	if i := strings.Index(name, "."); i >= 0 {
		return name[:i]
	}

	return name
}

// countByType counts the addresses by resource type, as "2 google_cloud_run_v2_service,
// 7 google_cloudbuild_trigger", sorted by type.
func countByType(addresses []string) string {
	counts := map[string]int{}
	for _, address := range addresses {
		counts[resourceType(address)]++
	}
	parts := make([]string, 0, len(counts))
	for _, typ := range slices.Sorted(maps.Keys(counts)) {
		parts = append(parts, strconv.Itoa(counts[typ])+" "+typ)
	}

	return strings.Join(parts, ", ")
}

// plannedDeletions are the managed resources the saved plan deletes, as tofu show -json
// writes it, sorted.
func plannedDeletions(shown []byte) ([]string, error) {
	var plan map[string]json.RawMessage
	if err := json.Unmarshal(shown, &plan); err != nil {
		return nil, errors.Wrap(err, "json.Unmarshal(): tofu show -json")
	}
	var changes []struct {
		Address string `json:"address"`
		Mode    string `json:"mode"`
		Change  struct {
			Actions []string `json:"actions"`
		} `json:"change"`
	}
	if raw, ok := plan["resource_changes"]; ok {
		if err := json.Unmarshal(raw, &changes); err != nil {
			return nil, errors.Wrap(err, "json.Unmarshal(): tofu show -json, resource_changes")
		}
	}
	var deleted []string
	for _, change := range changes {
		if change.Mode == managedMode && slices.Contains(change.Change.Actions, deleteAction) {
			deleted = append(deleted, change.Address)
		}
	}
	sort.Strings(deleted)

	return deleted, nil
}

// difference is what is in a and not in b, sorted.
func difference(a, b []string) []string {
	var d []string
	for _, s := range a {
		if !slices.Contains(b, s) {
			d = append(d, s)
		}
	}
	sort.Strings(d)

	return d
}

// ApplicationTeardown is one application's teardown or destroy in every environment,
// prepared, with the pull-request stacks after them: bedrock teardown --all and
// bedrock destroy --all, and bedrock org teardown's loop over the registered
// applications.
type ApplicationTeardown struct {
	req     *TeardownRequest
	clients *Clients
	// Environments are the environments with a stack, in promotion order;
	// PullRequests the numbers of the pull-request stacks under the integration
	// environment's prefix.
	Environments []*EnvironmentTeardown
	PullRequests []string
}

// PrepareApplication prepares every environment of the application in promotion order
// (a refusal in any stops the whole before anything runs) and lists the pull-request
// stacks. The request's environment is ignored.
func PrepareApplication(ctx context.Context, clients *Clients, req *TeardownRequest, out io.Writer) (*ApplicationTeardown, error) {
	a := &ApplicationTeardown{req: req, clients: clients}
	for _, env := range req.Placement.Environments {
		r := *req
		r.Env = env
		t, err := PrepareTeardown(ctx, clients, &r, out)
		if err != nil {
			return nil, err
		}
		if t != nil {
			a.Environments = append(a.Environments, t)
		}
	}
	numbers, err := PullRequestStacks(ctx, clients, req)
	if err != nil {
		return nil, err
	}
	a.PullRequests = numbers

	return a, nil
}

// Empty reports that nothing of the application stands: no environment's stack and no
// pull-request stack.
func (a *ApplicationTeardown) Empty() bool {
	return len(a.Environments) == 0 && len(a.PullRequests) == 0
}

// Summary says what the run does, environment by environment, and which pull-request
// stacks go after them.
func (a *ApplicationTeardown) Summary() string {
	var b strings.Builder
	for _, t := range a.Environments {
		b.WriteString(t.Summary())
	}
	if len(a.PullRequests) > 0 {
		fmt.Fprintf(&b, "Pull-request stacks under %s, destroyed whole with their builds' jobs: %s\n", a.req.Placement.Integration(), strings.Join(a.PullRequests, ", "))
	}

	return b.String()
}

// Run runs every environment in promotion order, then destroys the pull-request stacks.
func (a *ApplicationTeardown) Run(ctx context.Context, out io.Writer) error {
	for _, t := range a.Environments {
		if err := t.Run(ctx, out); err != nil {
			return err
		}
	}
	if len(a.PullRequests) == 0 {
		return nil
	}

	return TeardownPullRequests(ctx, a.clients, a.req, a.PullRequests, out)
}

// NetHosts are the hosts 2-net routes, by backend service, read from its state in the
// state bucket as the person: none when the layer has not applied or routes nothing.
func NetHosts(ctx context.Context, clients *Clients, bucket string) (map[string]string, error) {
	store, err := clients.Storage(ctx)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	net, ok, err := readState(ctx, store, bucket, netLayer)
	if err != nil || !ok {
		return nil, err
	}

	return net.stringMap(hostsOutput)
}

// Standing is what one environment of an application holds, read from the state bucket
// and the records as the person, for bedrock org teardown's account of the
// applications whose stacks it cannot reach (each in its own repository): whether a
// stack stands there, whether a teardown took it down, and the release that is live.
type Standing struct {
	Env string
	// Stack says the environment's stack has been applied; TornDown that a teardown of
	// that stack is recorded beside its id. Release is the live release, or "".
	Stack    bool
	TornDown bool
	Release  string
}

// ApplicationStanding reads every environment's standing.
func ApplicationStanding(ctx context.Context, clients *Clients, bucket, app string, envs []string) ([]Standing, error) {
	store, err := clients.Storage(ctx)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	standings := make([]Standing, 0, len(envs))
	for _, env := range envs {
		s, err := environmentStanding(ctx, store, bucket, app, env)
		if err != nil {
			return nil, err
		}
		standings = append(standings, s)
	}

	return standings, nil
}

// environmentStanding is one environment's standing: the stack's state says whether it
// stands, its records whether a teardown of this stack is recorded and what is live.
func environmentStanding(ctx context.Context, store Store, bucket, app, env string) (Standing, error) {
	s := Standing{Env: env}
	state, ok, err := readState(ctx, store, bucket, statePrefix(app, env))
	if err != nil || !ok || len(state.Outputs) == 0 {
		return s, err
	}
	s.Stack = true
	subs, err := state.stringMap(substitutionsOutput)
	if err != nil || subs[recordsBucket] == "" {
		return s, err
	}
	records := subs[recordsBucket]
	id, err := stackID(ctx, store, records, app, env)
	if err != nil {
		return s, err
	}
	data, err := store.Read(ctx, records, teardownObject(app, env))
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return s, err
	default:
		var r TeardownRecord
		if err := json.Unmarshal(data, &r); err != nil {
			return s, errors.Wrapf(err, "json.Unmarshal(): gs://%s/%s", records, teardownObject(app, env))
		}
		s.TornDown = r.Kind == teardownVar && r.StackID == id
	}
	live, err := newestLiveRelease(ctx, store, records, app, env)
	if err != nil {
		return s, err
	}
	if live != nil {
		s.Release = live.Version
	}

	return s, nil
}
