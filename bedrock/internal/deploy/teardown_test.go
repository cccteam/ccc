package deploy

import (
	"context"
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// The lab's harbor in the teardown tests: its state bucket, the records bucket of each
// environment, and the backends 2-net may route to.
const (
	teardownStateBucket = "imp-boot-gbl-state"
	tstBackend          = "projects/imp-tst-gbl-core-1a2b/global/backendServices/imp-tst-gbl-harbor-backend"
	tstNextBackend      = "projects/imp-tst-gbl-core-1a2b/global/backendServices/imp-tst-gbl-harbor-next-backend"
	teardownStackID     = "9f3c"
	readAction          = "read"
)

// teardownPlacement is harbor's placement as the commands read it.
func teardownPlacement() *derive.Placement {
	return &derive.Placement{
		Prefix: "imp", Environments: []string{"tst", "stg", "prd"}, StateBucket: teardownStateBucket,
		Projects: map[string]derive.Project{"tst": {ID: "imp-tst-gbl-core-1a2b", Number: "1"}},
	}
}

// teardownPlacementJSON is teardownPlacement as the checkout's placement.json.
func teardownPlacementJSON() string {
	return `{"prefix": "imp", "environments": ["tst", "stg", "prd"], "regions": [{"name": "us-central1", "code": "uc1"}], "appsDomain": "impulseframework.dev", "hostedDomain": "example.com", "stateBucket": "` + teardownStateBucket + `", "placeholderImage": "i", "defaultBranch": "master", "repository": "harbor", "releaseApp": "release-app", "projects": {"tst": {"id": "imp-tst-gbl-core-1a2b", "number": "1"}}}` + "\n"
}

// dataDirRE matches the temporary data directory a stack trigger's teardown makes, in
// tofu's lines.
var dataDirRE = regexp.MustCompile(`/(?:[^\s/]+/)*bedrock-stack-(?:teardown|destroy)-\d+/`)

// stackState is an environment's state object with the outputs the commands read.
func stackState(env string, backends ...string) string {
	hosts := map[string]string{}
	for i, backend := range backends {
		host := "harbor-" + env + ".impulseframework.dev"
		if i > 0 {
			host = "harbor-" + env + "-next.impulseframework.dev"
		}
		hosts[host] = backend
	}
	subs := map[string]string{recordsBucket: "imp-" + env + "-gbl-harbor-records", projectSub: "imp-" + env + "-gbl-core-1a2b", "_SERVICES": "uc1=imp-" + env + "-uc1-harbor-app"}
	data, _ := json.Marshal(map[string]any{"outputs": map[string]any{netHostsOutput: map[string]any{"value": hosts}, substitutionsOutput: map[string]any{"value": subs}}})

	return string(data)
}

// netState is 2-net's state object routing the hosts.
func netState(hosts map[string]string) string {
	data, _ := json.Marshal(map[string]any{"outputs": map[string]any{hostsOutput: map[string]any{"value": hosts}}})

	return string(data)
}

// liveRecord is a live release's record in the environment.
func liveRecord(version string, pins map[string]string) string {
	data, _ := json.Marshal(Record{App: "harbor", Version: version, Status: Live, Timestamp: "2026-10-14T20:00:00Z", GeneratedSecrets: pins})

	return string(data)
}

// tstStateList is tst's stack as tofu state list prints it: the resources a teardown
// destroys among the ones it keeps, and a data resource.
const tstStateList = `data.terraform_remote_state.env
google_cloud_run_v2_service.app["uc1"]
google_cloud_run_v2_service.app["uw3"]
google_cloud_run_v2_service_iam_member.invoker["uc1"]
google_cloudbuild_trigger.version[0]
google_cloudbuild_trigger.sweep[0]
google_cloud_scheduler_job.sweep[0]
google_compute_region_network_endpoint_group.app["uc1"]
google_compute_backend_service.app[0]
google_service_account.scheduler[0]
google_service_account.app
google_spanner_database.harbor[0]
google_storage_bucket.files[0]
google_firestore_database.firestore
google_secret_manager_secret.harbor["APP_COOKIE_KEY"]
random_id.stack
google_storage_bucket_object.stack[0]
google_cloud_tasks_queue.tasks[0]
google_storage_bucket.migrate_logs[0]
google_logging_project_sink.migrate_logs[0]
`

// tstDependencies is what tst's resources depend on, as the state records it: the
// migrate-log sink names the version trigger and the services in its filter, and its
// bucket as its destination.
var tstDependencies = map[string][]string{
	"google_logging_project_sink.migrate_logs[0]": {"google_cloud_run_v2_service.app", "google_cloudbuild_trigger.version", "google_storage_bucket.migrate_logs"},
	"google_cloudbuild_trigger.version[0]":        {"google_cloud_run_v2_service.app"},
	"google_storage_bucket_object.stack[0]":       {"random_id.stack"},
}

var (
	tstGoes = []string{
		`google_cloud_run_v2_service.app["uc1"]`, `google_cloud_run_v2_service.app["uw3"]`, `google_cloud_run_v2_service_iam_member.invoker["uc1"]`,
		"google_cloudbuild_trigger.version[0]", "google_cloudbuild_trigger.sweep[0]", "google_cloud_scheduler_job.sweep[0]",
		`google_compute_region_network_endpoint_group.app["uc1"]`, "google_compute_backend_service.app[0]", "google_service_account.scheduler[0]",
		"google_logging_project_sink.migrate_logs[0]",
	}
	tstStays = []string{
		"google_service_account.app", "google_spanner_database.harbor[0]", "google_storage_bucket.files[0]", "google_firestore_database.firestore",
		`google_secret_manager_secret.harbor["APP_COOKIE_KEY"]`, "random_id.stack", "google_storage_bucket_object.stack[0]", "google_cloud_tasks_queue.tasks[0]",
		"google_storage_bucket.migrate_logs[0]",
	}
)

// stateJSON is tofu show -json of a state holding the addresses, each with what it
// depends on, and a data resource beside them.
func stateJSON(addresses []string, dependencies map[string][]string) string {
	resources := []map[string]any{{"address": "data.terraform_remote_state.env", "mode": "data", "depends_on": []string{"random_id.stack"}}}
	for _, address := range addresses {
		resources = append(resources, map[string]any{"address": address, "mode": "managed", "depends_on": dependencies[address]})
	}
	data, _ := json.Marshal(map[string]any{"values": map[string]any{"root_module": map[string]any{"resources": resources}}})

	return string(data)
}

// planJSON is tofu show -json of a destroy plan deleting the addresses, with a data
// resource read beside them.
func planJSON(deleted ...string) string {
	changes := []map[string]any{{"address": "data.terraform_remote_state.env", "mode": "data", "change": map[string]any{"actions": []string{readAction}}}}
	for _, address := range deleted {
		changes = append(changes, map[string]any{"address": address, "mode": "managed", "change": map[string]any{"actions": []string{deleteAction}}})
	}
	data, _ := json.Marshal(map[string]any{"resource_changes": changes})

	return string(data)
}

// teardownStore is the state bucket and the records buckets as the tests start: tst
// applied and routed only when routed, prd applied with a restored generation and a
// live release, stg never applied.
func teardownStore(routed bool) *memoryStore {
	m := &memoryStore{objects: map[string]string{
		"gs://" + teardownStateBucket + "/3-app/harbor/tst/default.tfstate":                                  stackState("tst", tstBackend, tstNextBackend),
		"gs://" + teardownStateBucket + "/3-app/harbor/prd/default.tfstate":                                  stackState("prd", "projects/imp-prd-gbl-core-5e6f/global/backendServices/imp-prd-gbl-harbor-backend"),
		stackKey("imp-tst-gbl-harbor-records", "harbor", "tst"):                                              stackJSON(teardownStackID),
		"gs://imp-tst-gbl-harbor-records/" + releaseObject("harbor", "tst", teardownStackID, "v0.1.4", "b1"): liveRecord("v0.1.4", map[string]string{"APP_COOKIE_KEY": "3"}),
		stackKey("imp-prd-gbl-harbor-records", "harbor", "prd"):                                              stackJSON("77aa"),
		"gs://imp-prd-gbl-harbor-records/" + releaseObject("harbor", "prd", "77aa", "v0.1.3", "b2"):          liveRecord("v0.1.3", nil),
		"gs://imp-prd-gbl-harbor-records/" + generationObject("harbor", "prd", "77aa", 2):                    `{"generation": 2}`,
	}}
	hosts := map[string]string{"other.impulseframework.dev": "projects/x/global/backendServices/other"}
	if routed {
		hosts["harbor-tst.impulseframework.dev"] = tstBackend
	}
	m.objects["gs://"+teardownStateBucket+"/2-net/default.tfstate"] = netState(hosts)

	return m
}

func TestTeardown(t *testing.T) {
	t.Parallel()

	const prdStateList = `google_cloud_run_v2_service.app["uc1"]
google_spanner_database.harbor[0]
google_spanner_database.restored["2"]
google_storage_bucket.files[0]
google_firestore_database.firestore
google_service_account.app
`
	prdAll := managedAddresses(prdStateList)
	tests := []struct {
		name     string
		env      string
		destroy  bool
		routed   bool
		recorded bool
		empty    bool
		// backup asks the destroy for the final backups; shownState is tofu show -json of
		// the state when the case needs the databases' values; spanner the fake the
		// backups are taken on.
		backup      bool
		shownState  string
		spanner     *finalSpanner
		wantCreated []string
		stateList   string
		shown       string
		wantErr     string
		wantSummary []string
		wantOut     []string
		wantRan     []string
		wantRecord  []string
		wantNil     bool
	}{
		{
			name:    "refuses while 2-net routes a host to the environment's backend",
			env:     "tst",
			routed:  true,
			wantErr: "2-net routes harbor-tst.impulseframework.dev to " + tstBackend + ": the load balancer uses tst's backend service, Google refuses its deletion while it does, and the services behind it cannot go either. Take the hosts out of 2-net first, one of three ways, each through an organization pull request: bedrock org teardown harbor tst, which takes the environment's hostnames out and leaves the rest; bedrock org teardown, whose first step takes every application's out; or harbor's removal from placement.json; then run bedrock teardown again",
		},
		{
			name:    "says so when the stack has not been applied",
			env:     "stg",
			wantNil: true,
			wantOut: []string{"No stack in stg: nothing at gs://imp-boot-gbl-state/3-app/harbor/stg/default.tfstate, so the stack has not been applied there, and there is nothing to take down."},
		},
		{
			name:    "says so when the stack's state is the empty one an init writes",
			env:     "stg",
			empty:   true,
			wantNil: true,
			wantOut: []string{"No stack in stg: gs://imp-boot-gbl-state/3-app/harbor/stg/default.tfstate is empty (an init wrote it, no apply did), so there is nothing to take down."},
		},
		{
			name:        "tears tst down: nothing to unprotect, the kept resources excluded but the sink that depends on destroyed ones, the plan held against the teardown, the record written",
			env:         "tst",
			stateList:   tstStateList,
			shown:       planJSON(tstGoes...),
			wantSummary: []string{"tst: 10 resource(s) destroyed, 9 kept.", "goes with what it depends on, made again by the next apply: google_logging_project_sink.migrate_logs[0] (google_cloud_run_v2_service.app, google_cloudbuild_trigger.version)"},
			wantOut: []string{
				"=== tst: teardown of harbor's stack at 3-app/harbor/tst ===",
				"The plan destroys the 10 resource(s) the teardown names and keeps 9, as meant; applying it.",
				"Tore harbor's stack in tst down: 10 resource(s) destroyed, 9 kept (the database, the file stores, the secret containers, the identities, the Firestore database, the task queue, the migrate logs, the stack's id). A record went to gs://imp-tst-gbl-harbor-records/harbor/stacks/tst/teardown.json.",
				"To bring it back: the operations workflow's apply action in tst runs the stack trigger, which applies the stack again (the services, the triggers and the rest), then bedrock rerun tst <release> deploys the release through them.",
			},
			wantRan: []string{
				"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/harbor/tst",
				"tofu state list",
				"tofu show -json",
				`tofu plan -destroy -input=false -no-color -out=DATA/tst/teardown.tfplan -var environment=tst -var database_generation=1 -var generated_secret_versions={APP_COOKIE_KEY="3"} -var teardown=true -exclude=google_service_account.app -exclude=google_spanner_database.harbor[0] -exclude=google_storage_bucket.files[0] -exclude=google_firestore_database.firestore -exclude=google_secret_manager_secret.harbor["APP_COOKIE_KEY"] -exclude=random_id.stack -exclude=google_storage_bucket_object.stack[0] -exclude=google_cloud_tasks_queue.tasks[0] -exclude=google_storage_bucket.migrate_logs[0]`,
				"tofu show -json DATA/tst/teardown.tfplan",
				"tofu apply -input=false -no-color DATA/tst/teardown.tfplan",
			},
			wantRecord: []string{`"kind": "teardown"`, `"stackId": "9f3c"`, `"by": "jo@lab"`, `"at": "2026-10-15T04:00:00Z"`, `"release": "v0.1.4"`, `"google_cloud_run_v2_service.app[\"uc1\"]"`, `"google_logging_project_sink.migrate_logs[0]"`, `"kept": [`, `"random_id.stack"`},
		},
		{
			name:      "destroys prd: the protections lifted first with the pipeline's variables, everything planned, the id named",
			env:       "prd",
			destroy:   true,
			stateList: prdStateList,
			shown:     planJSON(prdAll...),
			wantOut: []string{
				`Recording in prd's state that destroy lifts the protection of google_cloud_run_v2_service.app["uc1"], google_spanner_database.harbor[0], google_spanner_database.restored["2"], google_storage_bucket.files[0], google_firestore_database.firestore.`,
				"The plan destroys the 6 resource(s) the destroy names and keeps 0, as meant; applying it.",
				"Destroyed harbor's stack in prd: 6 resource(s). A record went to gs://imp-prd-gbl-harbor-records/harbor/stacks/prd/teardown.json; the deployment records under harbor/prd/77aa/ stay, read by nothing; the Spanner backups the pipeline and the schedules took stay until their retention ends; Google keeps the names of the task queue, the Firebase key and the migrate log bucket for a while, and a stack applied again takes an id of its own.",
			},
			wantRan: []string{
				"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/harbor/prd",
				"tofu state list",
				`tofu apply -input=false -no-color -auto-approve -var environment=prd -var database_generation=2 -var destroy=true -target=google_cloud_run_v2_service.app["uc1"] -target=google_spanner_database.harbor[0] -target=google_spanner_database.restored["2"] -target=google_storage_bucket.files[0] -target=google_firestore_database.firestore`,
				"tofu plan -destroy -input=false -no-color -out=DATA/prd/teardown.tfplan -var environment=prd -var database_generation=2 -var destroy=true",
				"tofu show -json DATA/prd/teardown.tfplan",
				"tofu apply -input=false -no-color DATA/prd/teardown.tfplan",
			},
			wantRecord: []string{`"kind": "destroy"`, `"stackId": "77aa"`, `"release": "v0.1.3"`, `"google_service_account.app"`},
		},
		{
			name:        "destroys prd with the final backups first: each database backed up as of the run, kept 366 days and waited for, named in the record",
			env:         "prd",
			destroy:     true,
			backup:      true,
			stateList:   prdStateList,
			shown:       planJSON(prdAll...),
			shownState:  stateJSONWith(prdAll, nil, map[string]map[string]any{"google_spanner_database.harbor[0]": {"project": "imp-prd-gbl-core-5e6f", "instance": "shared-spanner", "name": "harbor-db"}, `google_spanner_database.restored["2"]`: {"project": "imp-prd-gbl-core-5e6f", "instance": "shared-spanner", "name": "harbor-db-2"}}),
			spanner:     &finalSpanner{fakeSpanner: &fakeSpanner{creating: 1}},
			wantSummary: []string{"prd: 6 resource(s) destroyed, 0 kept.", "final backup first, kept 366 days and waited for: harbor-db, harbor-db-2"},
			wantOut: []string{
				"=== Final backup: harbor-db-final-20261015-0400 holds harbor-db as of 2026-10-15T04:00:00Z and is kept until 2027-10-16T04:00:00Z (operation projects/imp-prd-gbl-core-5e6f/instances/shared-spanner/operations/op-harbor-db-final-20261015-0400); the destroy waits for it ===",
				"Waiting for the backup harbor-db-final-20261015-0400: Spanner is taking it (0s so far).",
				"The backup harbor-db-final-20261015-0400 is ready (",
				"=== Final backup: harbor-db-2-final-20261015-0400 holds harbor-db-2",
				"The plan destroys the 6 resource(s) the destroy names and keeps 0, as meant; applying it.",
				"The final backup(s) projects/imp-prd-gbl-core-5e6f/instances/shared-spanner/backups/harbor-db-final-20261015-0400, projects/imp-prd-gbl-core-5e6f/instances/shared-spanner/backups/harbor-db-2-final-20261015-0400 hold the database(s) as of the destroy and stay on the instance until 2027-10-16; the record names them.",
			},
			wantCreated: []string{
				"harbor-db-final-20261015-0400 of harbor-db as of 2026-10-15T04:00:00Z until 2027-10-16T04:00:00Z",
				"harbor-db-2-final-20261015-0400 of harbor-db-2 as of 2026-10-15T04:00:00Z until 2027-10-16T04:00:00Z",
			},
			wantRan: []string{
				"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/harbor/prd",
				"tofu state list",
				"tofu show -json",
				`tofu apply -input=false -no-color -auto-approve -var environment=prd -var database_generation=2 -var destroy=true -target=google_cloud_run_v2_service.app["uc1"] -target=google_spanner_database.harbor[0] -target=google_spanner_database.restored["2"] -target=google_storage_bucket.files[0] -target=google_firestore_database.firestore`,
				"tofu plan -destroy -input=false -no-color -out=DATA/prd/teardown.tfplan -var environment=prd -var database_generation=2 -var destroy=true",
				"tofu show -json DATA/prd/teardown.tfplan",
				"tofu apply -input=false -no-color DATA/prd/teardown.tfplan",
			},
			wantRecord: []string{`"kind": "destroy"`, `"backups": [`, `"projects/imp-prd-gbl-core-5e6f/instances/shared-spanner/backups/harbor-db-final-20261015-0400"`, `"projects/imp-prd-gbl-core-5e6f/instances/shared-spanner/backups/harbor-db-2-final-20261015-0400"`},
		},
		{
			name:        "says so when a destroy is asked for without the final backup",
			env:         "prd",
			destroy:     true,
			stateList:   prdStateList,
			shown:       planJSON(prdAll...),
			wantSummary: []string{"no final backup of the database (--no-backup): nothing of its data is kept beyond the backups already taken"},
			wantOut:     []string{"Destroyed harbor's stack in prd: 6 resource(s)."},
		},
		{
			name:      "refuses a plan that destroys what the teardown keeps, and one short of it",
			env:       "tst",
			stateList: tstStateList,
			shown:     planJSON(append(tstGoes[1:], "google_spanner_database.harbor[0]")...),
			wantErr:   `the destroy plan of tst's stack is not the teardown's; it destroys what the teardown keeps: google_spanner_database.harbor[0]; it leaves out what the teardown destroys (the state changed under the run): google_cloud_run_v2_service.app["uc1"]. Nothing was applied`,
			wantRan: []string{
				"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/harbor/tst",
				"tofu state list",
				"tofu show -json",
				`tofu plan -destroy -input=false -no-color -out=DATA/tst/teardown.tfplan -var environment=tst -var database_generation=1 -var generated_secret_versions={APP_COOKIE_KEY="3"} -var teardown=true -exclude=google_service_account.app -exclude=google_spanner_database.harbor[0] -exclude=google_storage_bucket.files[0] -exclude=google_firestore_database.firestore -exclude=google_secret_manager_secret.harbor["APP_COOKIE_KEY"] -exclude=random_id.stack -exclude=google_storage_bucket_object.stack[0] -exclude=google_cloud_tasks_queue.tasks[0] -exclude=google_storage_bucket.migrate_logs[0]`,
				"tofu show -json DATA/tst/teardown.tfplan",
			},
		},
		{
			name:      "says so when nothing of the stack is a teardown's to destroy, the teardown recorded",
			env:       "tst",
			recorded:  true,
			stateList: strings.Join(tstStays, "\n") + "\n",
			wantOut:   []string{"Nothing of tst's stack is for a teardown to destroy: it holds 9 resource(s), every one kept."},
			wantRan:   []string{"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/harbor/tst", "tofu state list"},
		},
		{
			name:       "records a teardown an earlier run applied and could not record: nothing to destroy, the record written",
			env:        "tst",
			stateList:  strings.Join(tstStays, "\n") + "\n",
			wantOut:    []string{"Nothing of tst's stack is for a teardown to destroy: it holds 9 resource(s), every one kept; its teardown has no record yet (an earlier run's write failed after its apply), so one goes to gs://imp-tst-gbl-harbor-records/harbor/stacks/tst/teardown.json."},
			wantRan:    []string{"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/harbor/tst", "tofu state list"},
			wantRecord: []string{`"kind": "teardown"`, `"stackId": "9f3c"`, `"release": "v0.1.4"`, `"destroyed": []`, `"kept": [`, `"google_storage_bucket.migrate_logs[0]"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := teardownStore(tt.routed)
			if tt.recorded {
				store.objects["gs://imp-tst-gbl-harbor-records/harbor/stacks/tst/teardown.json"] = `{"kind": "teardown", "stackId": "9f3c", "at": "2026-10-15T03:00:00Z", "destroyed": []}`
			}
			if tt.empty {
				store.objects["gs://"+teardownStateBucket+"/3-app/harbor/stg/default.tfstate"] = `{"version": 4, "outputs": {}, "resources": []}`
			}
			shownState := stateJSON(managedAddresses(tt.stateList), tstDependencies)
			if tt.shownState != "" {
				shownState = tt.shownState
			}
			tofu := &fakeRunner{outputs: map[string]string{"tofu state": tt.stateList, "tofu show": tt.shown, "tofu show -json": shownState}}
			clients := &Clients{Storage: store.open, Exec: tofu, Sleep: func(context.Context, time.Duration) error { return nil }}
			if tt.spanner != nil {
				clients.Spanner = tt.spanner.openDirect
			}
			req := &TeardownRequest{Dir: "/checkout/infrastructure", DataDir: "DATA", Placement: teardownPlacement(), App: "harbor", Env: tt.env, Destroy: tt.destroy, Backup: tt.backup, By: "jo@lab", Now: func() time.Time { return time.Date(2026, 10, 15, 4, 0, 0, 0, time.UTC) }}
			var out strings.Builder
			prepared, err := PrepareTeardown(context.Background(), clients, req, &out)
			if err == nil && prepared != nil {
				got := prepared.Summary()
				if !strings.Contains(got, tt.env+": ") {
					t.Errorf("Summary() = %q, want it to start with the environment", got)
				}
				for _, want := range tt.wantSummary {
					if !strings.Contains(got, want) {
						t.Errorf("Summary() = %q, want it to contain %q", got, want)
					}
				}
				err = prepared.Run(context.Background(), &out)
			}
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("teardown = %v, want no error; printed:\n%s", err, out.String())
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("teardown = %v, want an error containing %q", err, tt.wantErr)
			case tt.wantNil && prepared != nil:
				t.Fatalf("PrepareTeardown() = %+v, want nil", prepared)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q; printed:\n%s", want, out.String())
				}
			}
			if tt.wantRan != nil {
				if got := tofu.lines(); strings.Join(got, "\n") != strings.Join(tt.wantRan, "\n") {
					t.Errorf("ran:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.wantRan, "\n"))
				}
			}
			if tt.wantCreated != nil {
				if got := tt.spanner.created; strings.Join(got, "\n") != strings.Join(tt.wantCreated, "\n") {
					t.Errorf("backups taken:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.wantCreated, "\n"))
				}
				if env := tofu.ran[0].Env; len(env) != 1 || env[0] != "TF_DATA_DIR=DATA/"+tt.env {
					t.Errorf("init ran with env %v, want the environment's data directory alone", env)
				}
			}
			if tt.wantRecord != nil {
				record := store.objects["gs://imp-"+tt.env+"-gbl-harbor-records/"+teardownObject("harbor", tt.env)]
				for _, want := range tt.wantRecord {
					if !strings.Contains(record, want) {
						t.Errorf("record lacks %q; written:\n%s", want, record)
					}
				}
			}
		})
	}
}

func TestTeardownPullRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		objects  []string
		projects map[string]derive.Project
		// empty says every pull request's state holds no resources.
		empty    bool
		wantErr  string
		wantNums []string
		wantRan  []string
		wantOut  []string
		// wantKept are the state objects still in the bucket after the run.
		wantKept []string
	}{
		{
			name:     "finds the pull-request stacks by their state objects, in number order, and destroys each the sweep's way",
			objects:  []string{"3-app/harbor/tst/pr12/default.tfstate", "3-app/harbor/tst/pr7/default.tfstate", "3-app/harbor/tst/pr7/other.json", "3-app/harbor/tst/prx/default.tfstate", "3-app/harbor/tst/default.tfstate"},
			projects: map[string]derive.Project{"tst": {ID: "imp-tst-gbl-core-1a2b"}},
			wantNums: []string{"7", "12"},
			wantRan: []string{
				"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/harbor/tst/pr7",
				"tofu state list",
				"tofu output -json",
				"tofu destroy -auto-approve -input=false -no-color -var environment=tst -var pull_request=7",
				"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/harbor/tst/pr12",
				"tofu state list",
				"tofu output -json",
				"tofu destroy -auto-approve -input=false -no-color -var environment=tst -var pull_request=12",
			},
			wantOut:  []string{"=== Pull request 7: stack at 3-app/harbor/tst/pr7 ===", "Pull request 7's stack names no template for a job process; its builds made no job.", "Pull request 7's state object gs://imp-boot-gbl-state/3-app/harbor/tst/pr7/default.tfstate is deleted: nothing of its stack is kept.", "=== Pull request 12: stack at 3-app/harbor/tst/pr12 ===", "Pull request 12's state object gs://imp-boot-gbl-state/3-app/harbor/tst/pr12/default.tfstate is deleted"},
			wantKept: []string{"3-app/harbor/tst/default.tfstate", "3-app/harbor/tst/pr7/other.json", "3-app/harbor/tst/prx/default.tfstate"},
		},
		{
			name:     "a state that holds nothing is skipped before any output is read, and its object goes",
			objects:  []string{"3-app/harbor/tst/pr7/default.tfstate", "3-app/harbor/tst/default.tfstate"},
			projects: map[string]derive.Project{"tst": {ID: "imp-tst-gbl-core-1a2b"}},
			empty:    true,
			wantNums: []string{"7"},
			wantRan: []string{
				"tofu init -input=false -no-color -reconfigure -backend-config=prefix=3-app/harbor/tst/pr7",
				"tofu state list",
			},
			wantOut:  []string{"Pull request 7's stack holds nothing (never applied, or destroyed already); skipped.", "Pull request 7's state object gs://imp-boot-gbl-state/3-app/harbor/tst/pr7/default.tfstate is deleted"},
			wantKept: []string{"3-app/harbor/tst/default.tfstate"},
		},
		{
			name:     "finds none",
			objects:  []string{"3-app/harbor/tst/default.tfstate"},
			projects: map[string]derive.Project{"tst": {ID: "imp-tst-gbl-core-1a2b"}},
			wantNums: nil,
			wantKept: []string{"3-app/harbor/tst/default.tfstate"},
		},
		{
			name:     "refuses to destroy a pull request's stack without the project its jobs are in",
			objects:  []string{"3-app/harbor/tst/pr7/default.tfstate"},
			wantNums: []string{"7"},
			wantErr:  "placement.json records no project for tst (projects.tst): the jobs of a pull request's builds are deleted in it before its stack",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &memoryStore{objects: map[string]string{}}
			for _, object := range tt.objects {
				store.objects["gs://"+teardownStateBucket+"/"+object] = "{}"
			}
			listed := "google_cloud_run_v2_service.app[0]\n"
			if tt.empty {
				listed = ""
			}
			tofu := &fakeRunner{outputs: map[string]string{"tofu output": `{"substitutions": {"value": {"_SERVICES": "uc1=harbor-pr7"}}}`, "tofu state list": listed}}
			clients := &Clients{Storage: store.open, Exec: tofu}
			p := teardownPlacement()
			p.Projects = tt.projects
			req := &TeardownRequest{Dir: "/checkout/infrastructure", DataDir: "DATA", Placement: p, App: "harbor", Env: "tst"}
			numbers, err := PullRequestStacks(context.Background(), clients, req)
			if err != nil {
				t.Fatalf("PullRequestStacks() = %v", err)
			}
			if strings.Join(numbers, ",") != strings.Join(tt.wantNums, ",") {
				t.Fatalf("PullRequestStacks() = %v, want %v", numbers, tt.wantNums)
			}
			var out strings.Builder
			err = TeardownPullRequests(context.Background(), clients, req, numbers, &out)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("TeardownPullRequests() = %v, want no error; printed:\n%s", err, out.String())
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("TeardownPullRequests() = %v, want an error containing %q", err, tt.wantErr)
			}
			if got := tofu.lines(); strings.Join(got, "\n") != strings.Join(tt.wantRan, "\n") {
				t.Errorf("ran:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.wantRan, "\n"))
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q; printed:\n%s", want, out.String())
				}
			}
			if tt.wantErr == "" {
				kept, _ := store.List(context.Background(), teardownStateBucket, "")
				if strings.Join(kept, ",") != strings.Join(tt.wantKept, ",") {
					t.Errorf("the bucket keeps %v, want %v", kept, tt.wantKept)
				}
			}
		})
	}
}

func TestApplicationStanding(t *testing.T) {
	t.Parallel()

	// The live records are of 2026-10-14T20:00:00Z: torn is a teardown after it, back
	// one before it (a release run since the teardown), earlier one of another stack.
	torn, _ := json.Marshal(TeardownRecord{Kind: teardownVar, StackID: teardownStackID, At: "2026-10-15T09:00:00Z"})
	back, _ := json.Marshal(TeardownRecord{Kind: teardownVar, StackID: teardownStackID, At: "2026-10-13T09:00:00Z"})
	earlier, _ := json.Marshal(TeardownRecord{Kind: teardownVar, StackID: "0000", At: "2026-10-15T09:00:00Z"})
	tests := []struct {
		name    string
		objects map[string]string
		want    []Standing
	}{
		{
			name: "never applied, applied by an init alone, standing with a live release, torn down, and a teardown of an earlier stack",
			objects: map[string]string{
				"gs://" + teardownStateBucket + "/3-app/harbor/stg/default.tfstate":                                  `{"outputs": {}}`,
				"gs://" + teardownStateBucket + "/3-app/harbor/prd/default.tfstate":                                  stackState("prd"),
				stackKey("imp-prd-gbl-harbor-records", "harbor", "prd"):                                              stackJSON(teardownStackID),
				"gs://imp-prd-gbl-harbor-records/" + releaseObject("harbor", "prd", teardownStackID, "v0.1.3", "b2"): liveRecord("v0.1.3", nil),
				"gs://imp-prd-gbl-harbor-records/" + teardownObject("harbor", "prd"):                                 string(torn),
				"gs://" + teardownStateBucket + "/3-app/harbor/tst/default.tfstate":                                  stackState("tst"),
				stackKey("imp-tst-gbl-harbor-records", "harbor", "tst"):                                              stackJSON(teardownStackID),
				"gs://imp-tst-gbl-harbor-records/" + releaseObject("harbor", "tst", teardownStackID, "v0.1.4", "b1"): liveRecord("v0.1.4", nil),
				"gs://imp-tst-gbl-harbor-records/" + teardownObject("harbor", "tst"):                                 string(earlier),
			},
			want: []Standing{{Env: "tst", Stack: true, Release: "v0.1.4"}, {Env: "stg"}, {Env: "prd", Stack: true, TornDown: true, Release: "v0.1.3"}},
		},
		{
			name: "a release run since the teardown ends it, and a teardown with no release stands",
			objects: map[string]string{
				"gs://" + teardownStateBucket + "/3-app/harbor/prd/default.tfstate":                                  stackState("prd"),
				stackKey("imp-prd-gbl-harbor-records", "harbor", "prd"):                                              stackJSON(teardownStackID),
				"gs://imp-prd-gbl-harbor-records/" + releaseObject("harbor", "prd", teardownStackID, "v0.1.3", "b2"): liveRecord("v0.1.3", nil),
				"gs://imp-prd-gbl-harbor-records/" + teardownObject("harbor", "prd"):                                 string(back),
				"gs://" + teardownStateBucket + "/3-app/harbor/tst/default.tfstate":                                  stackState("tst"),
				stackKey("imp-tst-gbl-harbor-records", "harbor", "tst"):                                              stackJSON(teardownStackID),
				"gs://imp-tst-gbl-harbor-records/" + teardownObject("harbor", "tst"):                                 string(torn),
			},
			want: []Standing{{Env: "tst", Stack: true, TornDown: true}, {Env: "prd", Stack: true, Release: "v0.1.3"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &memoryStore{objects: tt.objects}
			envs := make([]string, 0, len(tt.want))
			for _, w := range tt.want {
				envs = append(envs, w.Env)
			}
			got, err := ApplicationStanding(context.Background(), &Clients{Storage: store.open}, teardownStateBucket, "harbor", envs)
			if err != nil {
				t.Fatalf("ApplicationStanding() = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("ApplicationStanding() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestNetHosts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		objects map[string]string
		want    map[string]string
	}{
		{name: "2-net never applied", objects: map[string]string{}, want: nil},
		{name: "2-net routes nothing", objects: map[string]string{"gs://" + teardownStateBucket + "/2-net/default.tfstate": netState(map[string]string{})}, want: map[string]string{}},
		{name: "2-net routes a host", objects: map[string]string{"gs://" + teardownStateBucket + "/2-net/default.tfstate": netState(map[string]string{"harbor-tst.impulseframework.dev": tstBackend})}, want: map[string]string{"harbor-tst.impulseframework.dev": tstBackend}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &memoryStore{objects: tt.objects}
			got, err := NetHosts(context.Background(), &Clients{Storage: store.open}, teardownStateBucket)
			if err != nil {
				t.Fatalf("NetHosts() = %v", err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("NetHosts() = %v, want %v", got, tt.want)
			}
		})
	}
}

// stateJSONWith is stateJSON with each resource's type and, for the ones named, its
// values: what a destroy's final backups read the databases from.
func stateJSONWith(addresses []string, dependencies map[string][]string, values map[string]map[string]any) string {
	resources := []map[string]any{{"address": "data.terraform_remote_state.env", "mode": "data", "type": "terraform_remote_state", "depends_on": []string{"random_id.stack"}}}
	for _, address := range addresses {
		r := map[string]any{"address": address, "mode": "managed", "type": resourceType(address), "depends_on": dependencies[address]}
		if v, ok := values[address]; ok {
			r["values"] = v
		}
		resources = append(resources, r)
	}
	data, _ := json.Marshal(map[string]any{"values": map[string]any{"root_module": map[string]any{"resources": resources}}})

	return string(data)
}

// finalSpanner is the fake as a destroy's final backups see it: every backup it started
// reads READY once the creating reads are spent.
type finalSpanner struct {
	*fakeSpanner
}

func (f *finalSpanner) openDirect(context.Context) (Spanner, error) {
	return f, nil
}

func (f *finalSpanner) Backup(_ context.Context, name string) (*Backup, error) {
	b := &Backup{Name: name, State: BackupReady}
	if f.creating > 0 {
		f.creating--
		b.State = BackupCreating
	}

	return b, nil
}
