package deploy

import (
	"bytes"
	"strings"
	"testing"
)

func TestStackTeardown(t *testing.T) {
	t.Parallel()

	const (
		applyIdentity = "harbor-apply@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com"
		subs          = `"substitutions": {"_APP": "harbor", "_ENV": "tst", "_APPLY_IDENTITY": "` + applyIdentity + `", "_RECORDS_BUCKET": "imp-tst-gbl-harbor-records", "_STACK_ACTION": "teardown", "_REQUESTER": "jo"}`
		approval      = `"approval": {"state": "APPROVED", "result": {"approverAccount": "lead@example.com", "approvalTime": "2026-10-15T05:00:00Z", "decision": "APPROVED", "comment": "go"}}`
		deployBuild   = `{"id": "b-1", "serviceAccount": "projects/p/serviceAccounts/harbor-deploy@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com", ` + subs + `}`
		approvedBuild = `{"id": "b-2", "serviceAccount": "projects/p/serviceAccounts/harbor-deploy@imp-tst-gbl-core-1a2b.iam.gserviceaccount.com", ` + subs + `, ` + approval + `}`
		applyBuild    = `{"id": "b-3", "serviceAccount": "projects/p/serviceAccounts/` + applyIdentity + `", ` + subs + `}`
		noRequester   = `{"id": "b-4", "serviceAccount": "projects/p/serviceAccounts/` + applyIdentity + `", "substitutions": {"_APP": "harbor", "_ENV": "tst", "_APPLY_IDENTITY": "` + applyIdentity + `", "_STACK_ACTION": "teardown"}}`
		bareBuild     = `{"id": "b-5", "substitutions": {"_APP": "harbor", "_ENV": "tst", "_REQUESTER": "jo"}}`
		initLine      = "tofu init -input=false -no-color -backend-config=prefix=3-app/harbor/tst"
		impersonated  = " -backend-config=impersonate_service_account=" + applyIdentity
		teardownPlan  = `tofu plan -destroy -input=false -no-color -out=DATA/tst/teardown.tfplan -var environment=tst -var database_generation=1 -var generated_secret_versions={APP_COOKIE_KEY="3"} -var teardown=true -exclude=google_service_account.app -exclude=google_spanner_database.harbor[0] -exclude=google_storage_bucket.files[0] -exclude=google_firestore_database.firestore -exclude=google_secret_manager_secret.harbor["APP_COOKIE_KEY"] -exclude=random_id.stack -exclude=google_storage_bucket_object.stack[0] -exclude=google_cloud_tasks_queue.tasks[0] -exclude=google_storage_bucket.migrate_logs[0]`
		destroyPlan   = `tofu plan -destroy -input=false -no-color -out=DATA/tst/teardown.tfplan -var environment=tst -var database_generation=1 -var generated_secret_versions={APP_COOKIE_KEY="3"} -var destroy=true`
	)
	tearsDown := []string{initLine + impersonated, "tofu state list", "tofu show -json", teardownPlan, "tofu show -json DATA/tst/teardown.tfplan", "tofu apply -input=false -no-color DATA/tst/teardown.tfplan"}
	tests := []struct {
		name    string
		build   string
		destroy bool
		routed  bool
		// wantIdentities are the identities the store is opened as, in order: the reads,
		// then the record's write.
		wantIdentities []string
		wantOut        []string
		wantRan        []string
		wantEnv        string
		wantRecord     []string
		wantNoRecord   bool
		wantErr        string
	}{
		{
			name:           "tears tst down as the deploy identity impersonating the apply identity: the state and the records read as it, tofu driven as it, the record written as it naming the requester",
			build:          deployBuild,
			wantIdentities: []string{applyIdentity, applyIdentity},
			wantOut: []string{
				"=== teardown of harbor's stack in tst as " + applyIdentity + ", asked for by jo ===",
				"=== tst: teardown of harbor's stack at 3-app/harbor/tst ===",
				"tst: 10 resource(s) destroyed, 9 kept.",
				"Tore harbor's stack in tst down: 10 resource(s) destroyed, 9 kept",
			},
			wantRan:    tearsDown,
			wantEnv:    "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT=" + applyIdentity,
			wantRecord: []string{`"kind": "teardown"`, `"by": "jo"`, `"release": "v0.1.4"`},
		},
		{
			name:           "an approved build's record names the approver",
			build:          approvedBuild,
			wantIdentities: []string{applyIdentity, applyIdentity},
			wantRan:        tearsDown,
			wantEnv:        "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT=" + applyIdentity,
			wantRecord:     []string{`"by": "jo"`, `"approval": {`, `"approver": "lead@example.com"`, `"at": "2026-10-15T05:00:00Z"`, `"comment": "go"`},
		},
		{
			name:       "a build running as the apply identity impersonates nothing",
			build:      applyBuild,
			wantRan:    []string{initLine, "tofu state list", "tofu show -json", teardownPlan, "tofu show -json DATA/tst/teardown.tfplan", "tofu apply -input=false -no-color DATA/tst/teardown.tfplan"},
			wantRecord: []string{`"kind": "teardown"`, `"by": "jo"`},
		},
		{
			name:           "destroys tst whole",
			build:          deployBuild,
			destroy:        true,
			wantIdentities: []string{applyIdentity, applyIdentity},
			wantOut:        []string{"=== destroy of harbor's stack in tst as " + applyIdentity + ", asked for by jo ===", "Destroyed harbor's stack in tst: 19 resource(s)."},
			wantRan:        []string{initLine + impersonated, "tofu state list", "tofu show -json", destroyPlan, "tofu show -json DATA/tst/teardown.tfplan", "tofu apply -input=false -no-color DATA/tst/teardown.tfplan"},
			wantEnv:        "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT=" + applyIdentity,
			wantRecord:     []string{`"kind": "destroy"`, `"by": "jo"`},
		},
		{
			name:           "refuses while 2-net routes a host to the environment's backend, like the hand command",
			build:          deployBuild,
			routed:         true,
			wantIdentities: []string{applyIdentity},
			wantRan:        []string{},
			wantNoRecord:   true,
			wantErr:        "2-net routes harbor-tst.impulseframework.dev to " + tstBackend,
		},
		{
			name:         "a run with no requester is refused before anything is read",
			build:        noRequester,
			wantRan:      []string{},
			wantNoRecord: true,
			wantErr:      "_REQUESTER is not set: the operations workflow's teardown action passes the GitHub actor, whom the record names",
		},
		{
			name:         "a trigger that carries less than the step needs is refused",
			build:        bareBuild,
			wantRan:      []string{},
			wantNoRecord: true,
			wantErr:      "_APPLY_IDENTITY is not set: the stack trigger carries it",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := applicationWorkspace(t, map[string]string{placementPath: teardownPlacementJSON()})
			store := teardownStore(tt.routed)
			builds := &fakeBuilds{build: tt.build}
			shown := planJSON(tstGoes...)
			if tt.destroy {
				shown = planJSON(managedAddresses(tstStateList)...)
			}
			run := &fakeRunner{outputs: map[string]string{"tofu state": tstStateList, "tofu show": shown, "tofu show -json": stateJSON(managedAddresses(tstStateList), tstDependencies)}}
			clients := &Clients{Builds: builds.open, Exec: run, Storage: store.open, StorageAs: store.openAs, StorageAsWriter: store.openAs}
			var out bytes.Buffer
			err := StackTeardown(t.Context(), clients, w, &BuildRequest{BuildID: "b", Project: "p", Location: "l"}, tt.destroy, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("StackTeardown() error = %v, wantErr %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("StackTeardown() error = %v\n%s", err, out.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if got := strings.Join(store.identities, ","); got != strings.Join(tt.wantIdentities, ",") {
				t.Errorf("the store was opened as %q, want %q", got, strings.Join(tt.wantIdentities, ","))
			}
			lines := run.lines()
			for i := range lines {
				lines[i] = dataDirRE.ReplaceAllString(lines[i], "DATA/")
			}
			if got := strings.Join(lines, "\n"); got != strings.Join(tt.wantRan, "\n") {
				t.Errorf("tofu ran:\n%s\nwant:\n%s", got, strings.Join(tt.wantRan, "\n"))
			}
			if tt.wantEnv != "" && (len(run.ran) == 0 || !strings.Contains(strings.Join(run.ran[0].Env, " "), tt.wantEnv)) {
				t.Errorf("init ran with env %v, want %q", run.ran[0].Env, tt.wantEnv)
			}
			if tt.wantEnv == "" && len(run.ran) > 0 && strings.Contains(strings.Join(run.ran[0].Env, " "), "IMPERSONATE") {
				t.Errorf("init ran with env %v, want no impersonation", run.ran[0].Env)
			}
			record, written := store.objects["gs://imp-tst-gbl-harbor-records/"+teardownObject("harbor", "tst")]
			if tt.wantNoRecord && written {
				t.Errorf("a record was written:\n%s", record)
			}
			for _, want := range tt.wantRecord {
				if !strings.Contains(record, want) {
					t.Errorf("record lacks %q; written:\n%s", want, record)
				}
			}
		})
	}
}
