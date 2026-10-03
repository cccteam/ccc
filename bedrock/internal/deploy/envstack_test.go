package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

// stackPlanJSON is a plan of a stack as tofu show -json gives it: a service updated (its
// template pinning a secret version by short name), a bucket created, a job unchanged (its
// template pinning a version by full name), a secret replaced, and a data read.
const stackPlanJSON = `{"resource_changes": [
 {"address": "google_cloud_run_v2_service.app[\"uc1\"]", "type": "google_cloud_run_v2_service", "change": {"actions": ["update"], "after": {"project": "p-stg", "template": [{"containers": [{"env": [{"name": "APP_COOKIE_KEY", "value_source": [{"secret_key_ref": [{"secret": "quill-cookie-key", "version": "3"}]}]}]}]}]}}},
 {"address": "google_storage_bucket.files", "type": "google_storage_bucket", "change": {"actions": ["create"], "after": {}}},
 {"address": "google_cloud_run_v2_job.migrate", "type": "google_cloud_run_v2_job", "change": {"actions": ["no-op"], "after": {"project": "p-stg", "template": [{"template": [{"containers": [{"env": [{"name": "APP_DB", "value_source": [{"secret_key_ref": [{"secret": "projects/p-stg/secrets/quill-db", "version": "latest"}]}]}]}]}]}]}}},
 {"address": "google_secret_manager_secret.old", "type": "google_secret_manager_secret", "change": {"actions": ["delete", "create"], "before": {}, "after": {}}},
 {"address": "data.google_project.p", "type": "google_project", "change": {"actions": ["read"]}}
]}`

// The versions the plan above pins, and a stack of pins that exist and are enabled.
const (
	cookieVersion = "projects/p-stg/secrets/quill-cookie-key/versions/3"
	dbVersion     = "projects/p-stg/secrets/quill-db/versions/latest"
)

func enabledPins() map[string]string {
	return map[string]string{cookieVersion: enabledState, dbVersion: enabledState}
}

func TestStackPlan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    string
		want    *StackPlan
		wantErr string
	}{
		{
			name: "the counts and the changes, no-ops and reads left out, a replace counted twice",
			data: stackPlanJSON,
			want: &StackPlan{Add: 2, Change: 1, Destroy: 1, Changes: []StackChange{
				{Address: `google_cloud_run_v2_service.app["uc1"]`, Actions: []string{actionUpdate}},
				{Address: "google_storage_bucket.files", Actions: []string{actionCreate}},
				{Address: "google_secret_manager_secret.old", Actions: []string{actionDelete, actionCreate}},
			}},
		},
		{name: "a plan with no change", data: `{"resource_changes": [{"address": "a.b", "type": "google_x", "change": {"actions": ["no-op"]}}]}`, want: &StackPlan{}},
		{name: "a plan that is not JSON is refused", data: "{", wantErr: "the plan's JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := stackPlan([]byte(tt.data))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("stackPlan() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("stackPlan() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("stackPlan() mismatch (-want +got):\n%s", diff)
			}
			if got.Summary() != fmt.Sprintf("Plan: %d to add, %d to change, %d to destroy.", tt.want.Add, tt.want.Change, tt.want.Destroy) {
				t.Errorf("Summary() = %q", got.Summary())
			}
		})
	}
}

func TestPlannedFileStores(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		plan string
		want []string
	}{
		{name: "the buckets the planned substitutions name", plan: plannedStoresJSON, want: []string{"google_storage_bucket.files"}},
		{name: "several, trimmed", plan: `{"output_changes": {"substitutions": {"after": {"_FILE_STORES": "google_storage_bucket.files, google_storage_bucket.files_documents"}}}}`, want: []string{"google_storage_bucket.files", "google_storage_bucket.files_documents"}},
		{name: "a plan without the output", plan: stackPlanJSON},
		{name: "an output not known yet", plan: `{"output_changes": {"substitutions": {"after": null, "after_unknown": true}}}`},
		{name: "not a plan", plan: "not json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := plannedFileStores([]byte(tt.plan)); !slices.Equal(got, tt.want) {
				t.Errorf("plannedFileStores() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlannedMounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    string
		project string
		want    []string
	}{
		{name: "the versions the planned templates pin, a short name under the resource's project, sorted", data: stackPlanJSON, project: "p-tst", want: []string{cookieVersion, dbVersion}},
		{
			name:    "a short name under the build's project when the resource names none",
			data:    `{"resource_changes": [{"address": "google_cloud_run_v2_service.app", "type": "google_cloud_run_v2_service", "change": {"actions": ["create"], "after": {"template": [{"containers": [{"env": [{"value_source": [{"secret_key_ref": [{"secret": "k", "version": "1"}]}]}]}]}]}}}]}`,
			project: "p-tst",
			want:    []string{"projects/p-tst/secrets/k/versions/1"},
		},
		{name: "a plan without a service or a job pins nothing", data: `{"resource_changes": [{"address": "google_storage_bucket.b", "type": "google_storage_bucket", "change": {"actions": ["create"], "after": {"secret_key_ref": [{"secret": "x", "version": "1"}]}}}]}`, project: "p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := plannedMounts([]byte(tt.data), tt.project)
			if err != nil {
				t.Fatalf("plannedMounts() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("plannedMounts() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// tagSubs are a tag build's substitutions for stg, for a stack with one file store.
// writeStackFile puts one .tf file, iam.tf, into the workspace's stack directory: what
// the test of the stack scans for authoritative IAM resources.
func writeStackFile(t *testing.T, w Workspace, content string) {
	t.Helper()

	dir := filepath.Join(string(w), stackDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "iam.tf"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func tagSubs() map[string]string {
	return map[string]string{appSub: "quill", envSub: stgEnvironment, projectSub: "p-stg", applyIdentitySub: "quill-apply@p-stg.iam.gserviceaccount.com", commitSub: "c9", repoFullNameSub: "acme/quill", fileStoresSub: "google_storage_bucket.files"}
}

// tstSubs are a tag build's substitutions in tst, with stg's identity and project, so
// the lines a test expects differ from tagSubs's in the environment alone.
func tstSubs() map[string]string {
	subs := tagSubs()
	subs[envSub] = tstEnvironment

	return subs
}

// promotedSubs is stg with the promotion order the triggers carry, which names production.
func promotedSubs() map[string]string {
	subs := tagSubs()
	subs[environmentsSub] = "tst,stg,prd"

	return subs
}

// seededSubs is tst on the placement's seed list: _SEED is true.
func seededSubs() map[string]string {
	subs := tstSubs()
	subs[seedSub] = trueValue

	return subs
}

// storesSubs is tst for a stack with the default file store and two named ones.
func storesSubs() map[string]string {
	subs := tstSubs()
	subs[fileStoresSub] = "google_storage_bucket.files,google_storage_bucket.files_documents,google_storage_bucket.files_client_files"

	return subs
}

// storelessTagSubs is a tag build whose trigger carries no file store yet.
func storelessTagSubs() map[string]string {
	subs := tagSubs()
	delete(subs, fileStoresSub)

	return subs
}

// plannedStoresJSON is stackPlanJSON with the substitutions output the planned stack
// writes, naming the default store's bucket the trigger does not carry yet.
var plannedStoresJSON = strings.TrimSuffix(stackPlanJSON, "}") + `, "output_changes": {"substitutions": {"actions": ["update"], "before": {"_FILE_STORES": ""}, "after": {"_FILE_STORES": "google_storage_bucket.files"}, "after_unknown": false}}}`

// storelessSubs is tst for a stack that names no file store.
func storelessSubs() map[string]string {
	subs := tstSubs()
	delete(subs, fileStoresSub)

	return subs
}

func TestPlanEnvironmentStack(t *testing.T) {
	t.Parallel()

	const (
		initLine = "tofu init -input=false -no-color -backend-config=prefix=3-app/quill/stg -backend-config=impersonate_service_account=quill-apply@p-stg.iam.gserviceaccount.com"
		planLine = "tofu plan -input=false -no-color -out=WS/stack.plan -var environment=stg"
		showLine = "tofu show -json WS/stack.plan"
	)
	const (
		restoreEnv = "export SKIP_DEPLOY=\"\"\nexport RESTORE=\"empty\"\nexport RESTORE_REQUESTER=\"octocat\"\n"
		stateList  = "google_spanner_database.quill[0]\ngoogle_firestore_database.firestore\ngoogle_storage_bucket.files\ngoogle_cloud_run_v2_service.app[\"uc1\"]\n"
	)
	tests := []struct {
		name      string
		subs      map[string]string
		pins      map[string]string
		stackFile string
		// planJSON is what tofu show -json answers; stackPlanJSON when empty.
		planJSON string
		// env is the environment file; state what tofu state list (or state show) answers.
		env   string
		state string
		// backup is the backup of production's database the instance holds; nil when none.
		backup *Backup
		// live is the maintenance variable's value on the live service the stack names
		// (_SERVICES is set with it); "" leaves the service out.
		live         string
		wantOut      []string
		wantTofu     []string
		wantFact     string
		wantReplaced string
		// wantBackup is the backup fact a production-backup restore leaves.
		wantBackup string
		wantErr    string
	}{
		{
			name:         "a restore run replaces the database, the Firestore database and, in tst, the file store's bucket",
			subs:         tstSubs(),
			pins:         enabledPins(),
			env:          restoreEnv,
			state:        stateList,
			wantOut:      []string{"=== Restore (empty, asked for by octocat): google_spanner_database.quill[0], google_storage_bucket.files is replaced in tst's stack; the migrations then apply afresh ===", "The Firestore database stays (Firestore keeps a deleted database's id unavailable for minutes); the apply deletes its documents instead.", "Tests passed"},
			wantTofu:     []string{strings.Replace(initLine, "3-app/quill/stg", "3-app/quill/tst", 1), "tofu state list", strings.Replace(planLine, "environment=stg", "environment=tst", 1) + " -replace=google_spanner_database.quill[0] -replace=google_storage_bucket.files", showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantReplaced: "google_spanner_database.quill[0],google_storage_bucket.files",
		},
		{
			name:         "a restore run in maintenance tells the plan the maintenance variable's live value, so the apply leaves the service alone",
			subs:         tstSubs(),
			pins:         enabledPins(),
			env:          restoreEnv + "export MAINTENANCE=\"true\"\n",
			state:        stateList,
			wantOut:      []string{"=== Restore (empty, asked for by octocat): google_spanner_database.quill[0], google_storage_bucket.files is replaced in tst's stack; the migrations then apply afresh ===", "Tests passed"},
			wantTofu:     []string{strings.Replace(initLine, "3-app/quill/stg", "3-app/quill/tst", 1), "tofu state list", strings.Replace(planLine, "environment=stg", "environment=tst", 1) + " -var maintenance=1 -replace=google_spanner_database.quill[0] -replace=google_storage_bucket.files", showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantReplaced: "google_spanner_database.quill[0],google_storage_bucket.files",
		},
		{
			name:     "a run that was not in maintenance keeps a service an earlier run's maintenance left so, until the release's revision clears it",
			subs:     tagSubs(),
			pins:     enabledPins(),
			live:     "1",
			wantOut:  []string{"quill-app is in maintenance from an earlier run (APP_MAINTENANCE=1 on the service): the plan keeps it so, and the release's revision clears it after the migrations.", "Tests passed"},
			wantTofu: []string{initLine, planLine + " -var maintenance=1", showLine},
			wantFact: "Plan: 2 to add, 1 to change, 1 to destroy.",
		},
		{
			name:     "a service that serves leaves the plan's maintenance variable at its default",
			subs:     tagSubs(),
			pins:     enabledPins(),
			live:     "",
			wantOut:  []string{"Tests passed"},
			wantTofu: []string{initLine, planLine, showLine},
			wantFact: "Plan: 2 to add, 1 to change, 1 to destroy.",
		},
		{
			name:         "a restore run in a seeded environment says the seed applies afresh too",
			subs:         seededSubs(),
			pins:         enabledPins(),
			env:          restoreEnv,
			state:        stateList,
			wantOut:      []string{"=== Restore (empty, asked for by octocat): google_spanner_database.quill[0], google_storage_bucket.files is replaced in tst's stack; the migrations and the seed then apply afresh ===", "Tests passed"},
			wantTofu:     []string{strings.Replace(initLine, "3-app/quill/stg", "3-app/quill/tst", 1), "tofu state list", strings.Replace(planLine, "environment=stg", "environment=tst", 1) + " -replace=google_spanner_database.quill[0] -replace=google_storage_bucket.files", showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantReplaced: "google_spanner_database.quill[0],google_storage_bucket.files",
		},
		{
			name:         "a restore run in tst replaces every file store's bucket the stack names, the named stores with the default",
			subs:         storesSubs(),
			pins:         enabledPins(),
			env:          restoreEnv,
			state:        "google_spanner_database.quill[0]\ngoogle_storage_bucket.files\ngoogle_storage_bucket.files_documents\ngoogle_storage_bucket.files_client_files\n",
			wantOut:      []string{"=== Restore (empty, asked for by octocat): google_spanner_database.quill[0], google_storage_bucket.files, google_storage_bucket.files_documents, google_storage_bucket.files_client_files is replaced in tst's stack; the migrations then apply afresh ===", "Tests passed"},
			wantTofu:     []string{strings.Replace(initLine, "3-app/quill/stg", "3-app/quill/tst", 1), "tofu state list", strings.Replace(planLine, "environment=stg", "environment=tst", 1) + " -replace=google_spanner_database.quill[0] -replace=google_storage_bucket.files -replace=google_storage_bucket.files_documents -replace=google_storage_bucket.files_client_files", showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantReplaced: "google_spanner_database.quill[0],google_storage_bucket.files,google_storage_bucket.files_documents,google_storage_bucket.files_client_files",
		},
		{
			name:         "a restore run in tst of a stack that names no file store replaces the database alone, whatever buckets the state holds",
			subs:         storelessSubs(),
			pins:         enabledPins(),
			env:          restoreEnv,
			state:        "google_spanner_database.quill[0]\ngoogle_storage_bucket.files\n",
			wantOut:      []string{"=== Restore (empty, asked for by octocat): google_spanner_database.quill[0] is replaced in tst's stack; the migrations then apply afresh ===", "Tests passed"},
			wantTofu:     []string{strings.Replace(initLine, "3-app/quill/stg", "3-app/quill/tst", 1), "tofu state list", strings.Replace(planLine, "environment=stg", "environment=tst", 1) + " -replace=google_spanner_database.quill[0]", showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantReplaced: "google_spanner_database.quill[0]",
		},
		{
			name:         "stg keeps its file store's bucket through a restore, and a stack without Firestore replaces the database alone",
			subs:         tagSubs(),
			pins:         enabledPins(),
			env:          restoreEnv,
			state:        "google_spanner_database.quill[0]\ngoogle_storage_bucket.files\n",
			wantOut:      []string{"=== Restore (empty, asked for by octocat): google_spanner_database.quill[0] is replaced in stg's stack"},
			wantTofu:     []string{initLine, "tofu state list", planLine + " -replace=google_spanner_database.quill[0]", showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantReplaced: "google_spanner_database.quill[0]",
		},
		{
			name:     "a restore of a stack without a database is refused",
			subs:     tagSubs(),
			env:      restoreEnv,
			state:    "google_cloud_run_v2_service.app[\"uc1\"]\n",
			wantTofu: []string{initLine, "tofu state list"},
			wantErr:  "_RESTORE=empty: stg's stack holds no database to replace (nothing of google_spanner_database.quill[0] in its state)",
		},
		{
			name:         "a restore from production's backup drops stg's database and restores it from the newest backup before the plan",
			subs:         promotedSubs(),
			pins:         enabledPins(),
			env:          strings.Replace(restoreEnv, "empty", "production-backup", 1),
			state:        "# google_spanner_database.quill[0]:\nresource \"google_spanner_database\" \"quill\" {\n    database_dialect = \"GOOGLE_STANDARD_SQL\"\n    instance         = \"shared-spanner\"\n    name             = \"p-stg-gbl-quill-db\"\n    project          = \"p-spn\"\n}\n",
			backup:       &Backup{Name: "projects/p-spn/instances/shared-spanner/backups/p-prd-gbl-quill-db-20261001", VersionTime: "2026-10-01T02:00:00Z", CreateTime: "2026-10-01T02:05:00Z"},
			wantOut:      []string{"=== Restore (production-backup, asked for by octocat): stg's database p-stg-gbl-quill-db is dropped and restored from production's backup p-prd-gbl-quill-db-20261001 (data as of 2026-10-01T02:00:00Z); the migrations production's backup predates then apply ===", "Dropped p-stg-gbl-quill-db.", "Restored p-stg-gbl-quill-db from p-prd-gbl-quill-db-20261001; the plan recreates its memberships.", "Tests passed"},
			wantTofu:     []string{initLine, "tofu state show google_spanner_database.quill[0]", planLine, showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantReplaced: "google_spanner_database.quill[0]",
			wantBackup:   "projects/p-spn/instances/shared-spanner/backups/p-prd-gbl-quill-db-20261001",
		},
		{
			name:     "a restore from production's backup is refused when production's database has no backup",
			subs:     promotedSubs(),
			pins:     enabledPins(),
			env:      strings.Replace(restoreEnv, "empty", "production-backup", 1),
			state:    "resource \"google_spanner_database\" \"quill\" {\n    instance = \"shared-spanner\"\n    name     = \"p-stg-gbl-quill-db\"\n    project  = \"p-spn\"\n}\n",
			wantTofu: []string{initLine, "tofu state show google_spanner_database.quill[0]"},
			wantErr:  "_RESTORE=production-backup: shared-spanner has no READY backup of production's database p-prd-gbl-quill-db; stg keeps its database",
		},
		{
			name:     "a tag build plans, tests and appends the summary",
			subs:     tagSubs(),
			pins:     enabledPins(),
			wantOut:  []string{"=== stg's stack at 3-app/quill/stg as quill-apply@p-stg.iam.gserviceaccount.com ===", "Plan: 2 to add, 1 to change, 1 to destroy.", `  update google_cloud_run_v2_service.app["uc1"]`, "  delete+create google_secret_manager_secret.old", "Tests passed: no authoritative IAM resource other than a file store's bucket policy; 2 pinned secret version(s) exist and are enabled."},
			wantTofu: []string{initLine, planLine, showLine},
			wantFact: "Plan: 2 to add, 1 to change, 1 to destroy.",
		},
		{
			name:     "a disabled version is refused before the apply",
			subs:     tagSubs(),
			pins:     map[string]string{cookieVersion: "DISABLED", dbVersion: enabledState},
			wantTofu: []string{initLine, planLine, showLine},
			wantErr:  "Build REJECTED: a planned revision template pins secret version projects/p-stg/secrets/quill-cookie-key/versions/3, which is DISABLED; a revision would fail to start on it",
		},
		{
			name:     "a version that does not exist is refused",
			subs:     tagSubs(),
			pins:     map[string]string{dbVersion: enabledState},
			wantTofu: []string{initLine, planLine, showLine},
			wantErr:  "pins secret version projects/p-stg/secrets/quill-cookie-key/versions/3, which could not be read",
		},
		{
			name:      "an authoritative IAM resource in the stack is refused",
			subs:      tagSubs(),
			pins:      enabledPins(),
			stackFile: "resource \"google_project_iam_binding\" \"all\" {}\n",
			wantTofu:  []string{initLine, planLine, showLine},
			wantErr:   "Build REJECTED: the stack declares an authoritative IAM resource, which replaces every member of its role on each apply (a file store's bucket policy, storage.tf's, is the one admitted): iam.tf:1 google_project_iam_binding.all",
		},
		{
			name:      "the file store's bucket policy, named from the trigger's buckets, passes the test",
			subs:      tagSubs(),
			pins:      enabledPins(),
			stackFile: "resource \"google_storage_bucket_iam_policy\" \"files\" {}\n",
			wantOut:   []string{"Tests passed: no authoritative IAM resource other than a file store's bucket policy; 2 pinned secret version(s) exist and are enabled."},
			wantTofu:  []string{initLine, planLine, showLine},
			wantFact:  "Plan: 2 to add, 1 to change, 1 to destroy.",
		},
		{
			name:      "the policy of a store the pull request declares first, named from the planned substitutions before any trigger carries the bucket",
			subs:      storelessTagSubs(),
			pins:      enabledPins(),
			stackFile: "resource \"google_storage_bucket_iam_policy\" \"files\" {}\n",
			planJSON:  plannedStoresJSON,
			wantOut:   []string{"Tests passed: no authoritative IAM resource other than a file store's bucket policy; 2 pinned secret version(s) exist and are enabled."},
			wantTofu:  []string{initLine, planLine, showLine},
			wantFact:  "Plan: 2 to add, 1 to change, 1 to destroy.",
		},
		{
			name:      "a policy on a bucket that is not a file store's is refused",
			subs:      tagSubs(),
			pins:      enabledPins(),
			stackFile: "resource \"google_storage_bucket_iam_policy\" \"records\" {}\n",
			wantTofu:  []string{initLine, planLine, showLine},
			wantErr:   "is the one admitted): iam.tf:1 google_storage_bucket_iam_policy.records",
		},
		{
			name:    "a build without an apply identity is refused",
			subs:    map[string]string{appSub: "quill", envSub: stgEnvironment},
			wantErr: "build.json names no apply identity (_APPLY_IDENTITY)",
		},
		{
			name:    "a pull-request build does nothing here",
			subs:    stackSubs(),
			wantOut: []string{pullRequestBuildNotice},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			envFile := tt.env
			if envFile == "" {
				envFile = "export SKIP_DEPLOY=\"\"\n"
			}
			subs := tt.subs
			if tt.live != "" || strings.Contains(tt.name, "serves") {
				subs = map[string]string{}
				for k, v := range tt.subs {
					subs[k] = v
				}
				subs["_SERVICES"] = "us-central1=quill-app"
			}
			w := workspaceFiles(t, map[string]string{EnvironmentFile: envFile, BuildFile: buildFor(t, subs)})
			if tt.stackFile != "" {
				writeStackFile(t, w, tt.stackFile)
			}
			planJSON := tt.planJSON
			if planJSON == "" {
				planJSON = stackPlanJSON
			}
			run := &fakeRunner{outputs: map[string]string{"tofu show": planJSON, "tofu state": tt.state}}
			secrets := &fakeSecrets{states: tt.pins}
			spanner := &fakeSpanner{backup: tt.backup}
			clients := &Clients{Exec: run, SecretsAs: secrets.openAs, SpannerAs: spanner.open}
			if tt.live != "" || strings.Contains(tt.name, "serves") {
				// The stack names one service, deployed, carrying the maintenance variable at
				// the live value.
				const name = "projects/p-stg/locations/us-central1/services/quill-app"
				doc := serviceDoc(name)
				template, _ := doc["template"].(map[string]any)
				container, _ := firstContainer(template)
				container["env"] = []any{map[string]any{"name": "APP_MAINTENANCE", "value": tt.live}}
				services := newFakeRun(map[string]map[string]any{name: doc})
				clients.Run = services.open
			}
			var out strings.Builder
			err := PlanEnvironmentStack(t.Context(), clients, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("PlanEnvironmentStack() error = %v, want %q; output:\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("PlanEnvironmentStack() error = %v; output:\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			lines := strings.ReplaceAll(strings.Join(run.lines(), "\n"), string(w), "WS")
			if diff := cmp.Diff(strings.Join(tt.wantTofu, "\n"), lines); diff != "" {
				t.Errorf("tofu (-want +got):\n%s", diff)
			}
			env, err := w.Environment()
			if err != nil {
				t.Fatal(err)
			}
			if env[stackPlanFact] != tt.wantFact {
				t.Errorf("%s = %q, want %q", stackPlanFact, env[stackPlanFact], tt.wantFact)
			}
			if env[restoredFact] != tt.wantReplaced {
				t.Errorf("%s = %q, want %q", restoredFact, env[restoredFact], tt.wantReplaced)
			}
			if env[backupFact] != tt.wantBackup {
				t.Errorf("%s = %q, want %q", backupFact, env[backupFact], tt.wantBackup)
			}
			if tt.wantBackup != "" && (spanner.dropped != "projects/p-spn/instances/shared-spanner/databases/p-stg-gbl-quill-db" || spanner.restored != "p-stg-gbl-quill-db from "+tt.wantBackup) {
				t.Errorf("dropped %q, restored %q", spanner.dropped, spanner.restored)
			}
			if tt.wantFact != "" {
				if _, err := os.Stat(filepath.Join(string(w), StackPlanJSONFile)); err != nil {
					t.Errorf("the plan's JSON is not in the workspace: %v", err)
				}
			}
		})
	}
}

// applyRestoreEnv is a restore run's environment file as the apply step reads it.
const applyRestoreEnv = "export SKIP_DEPLOY=\"\"\nexport RESTORE=\"empty\"\nexport RESTORE_REQUESTER=\"octocat\"\n"

func TestApplyEnvironmentStack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		subs     map[string]string
		planJSON string
		// env replaces the environment file; outputs are what tofu output answers.
		env      string
		outputs  map[string]string
		wantOut  []string
		wantTofu []string
		// wantCleared is the Firestore database whose documents a restore run deleted.
		wantCleared string
		wantErr     string
	}{
		{
			name:        "a restore run deletes the Firestore database's documents after the apply, as the apply identity",
			subs:        tstSubs(),
			planJSON:    stackPlanJSON,
			env:         applyRestoreEnv,
			outputs:     map[string]string{"tofu output": "quill-fs\n"},
			wantOut:     []string{"Applied tst's stack: 2 added, 1 changed, 1 destroyed.", "Restore: every document of the Firestore database quill-fs is deleted; its documents referred to rows the restore replaced."},
			wantTofu:    []string{"tofu apply -input=false -no-color WS/stack.plan", "tofu output -raw firestore_database"},
			wantCleared: "projects/p-stg/databases/quill-fs",
		},
		{
			name:     "a restore run of a stack without a Firestore database clears nothing",
			subs:     tstSubs(),
			planJSON: stackPlanJSON,
			env:      applyRestoreEnv,
			wantOut:  []string{"No Firestore database to clear: the stack has no firestore_database output"},
			wantTofu: []string{"tofu apply -input=false -no-color WS/stack.plan", "tofu output -raw firestore_database"},
		},
		{
			name:     "the saved plan is applied as the apply identity",
			subs:     tagSubs(),
			planJSON: stackPlanJSON,
			wantOut:  []string{"Applied stg's stack: 2 added, 1 changed, 1 destroyed."},
			wantTofu: []string{"tofu apply -input=false -no-color WS/stack.plan"},
		},
		{
			name:     "a plan with no change applies nothing",
			subs:     tagSubs(),
			planJSON: `{"resource_changes": []}`,
			wantOut:  []string{"Nothing to apply: stg's stack matches the code."},
		},
		{
			name:    "a build whose plan step did not run is refused",
			subs:    tagSubs(),
			wantErr: "stack-plan.json (deploy stack plan writes it)",
		},
		{
			name:    "a pull-request build does nothing here",
			subs:    stackSubs(),
			wantOut: []string{pullRequestBuildNotice},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := map[string]string{EnvironmentFile: "export SKIP_DEPLOY=\"\"\n", BuildFile: buildFor(t, tt.subs)}
			if tt.env != "" {
				files[EnvironmentFile] = tt.env
			}
			if tt.planJSON != "" {
				files[StackPlanJSONFile] = tt.planJSON
			}
			w := workspaceFiles(t, files)
			run := &fakeRunner{outputs: tt.outputs}
			if run.outputs == nil {
				run.outputs = map[string]string{}
				run.fail = map[string]error{"tofu output": errors.New("no output")}
			}
			store := &fakeFirestore{}
			var out strings.Builder
			err := ApplyEnvironmentStack(t.Context(), &Clients{Exec: run, FirestoreAs: store.open}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ApplyEnvironmentStack() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("ApplyEnvironmentStack() error = %v; output:\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			lines := strings.ReplaceAll(strings.Join(run.lines(), "\n"), string(w), "WS")
			if diff := cmp.Diff(strings.Join(tt.wantTofu, "\n"), lines); diff != "" {
				t.Errorf("tofu (-want +got):\n%s", diff)
			}
			env, err := w.Environment()
			if err != nil {
				t.Fatal(err)
			}
			if env[environmentApplied] != "" {
				t.Errorf("the apply appended %s", environmentApplied)
			}
			if store.database != tt.wantCleared {
				t.Errorf("cleared %q, want %q", store.database, tt.wantCleared)
			}
			if tt.wantCleared != "" && env[clearedFact] != firestoreAddress {
				t.Errorf("%s = %q, want %q", clearedFact, env[clearedFact], firestoreAddress)
			}
		})
	}
}

// fakeSpanner answers the one backup it holds and records the drop and the restore.
type fakeSpanner struct {
	backup   *Backup
	dropped  string
	restored string
}

func (f *fakeSpanner) open(context.Context, string) (Spanner, error) {
	return f, nil
}

func (f *fakeSpanner) LatestBackup(_ context.Context, _, database string) (*Backup, error) {
	if f.backup == nil || !strings.HasSuffix(f.backup.Name, "/backups/"+path.Base(database)+"-20261001") {
		return nil, nil
	}

	return f.backup, nil
}

func (f *fakeSpanner) DropDatabase(_ context.Context, database string) error {
	f.dropped = database

	return nil
}

func (f *fakeSpanner) RestoreDatabase(_ context.Context, _, databaseID, backup string) error {
	f.restored = databaseID + " from " + backup

	return nil
}

// fakeFirestore records the database whose documents were deleted, and the identity asked for.
type fakeFirestore struct {
	identity string
	database string
}

func (f *fakeFirestore) open(_ context.Context, identity string) (Firestore, error) {
	f.identity = identity

	return f, nil
}

func (f *fakeFirestore) DeleteAllDocuments(_ context.Context, database string) error {
	f.database = database

	return nil
}

// environmentApplied is a fact the apply never writes: the record reads the plan's JSON.
const environmentApplied = "STACK_APPLIED"

func TestPlanEnvironments(t *testing.T) {
	t.Parallel()

	prSubs := func() map[string]string {
		subs := stackSubs()
		subs[environmentsSub] = "tst,stg,prd"
		subs[planIdentitiesSub] = "tst=quill-plan@p-tst.iam.gserviceaccount.com,stg=quill-plan@p-stg.iam.gserviceaccount.com,prd=quill-plan@p-prd.iam.gserviceaccount.com"

		return subs
	}
	initOf := func(env, project string) string {
		return "tofu init -input=false -no-color -backend-config=prefix=3-app/quill/" + env + " -backend-config=impersonate_service_account=quill-plan@" + project + ".iam.gserviceaccount.com"
	}
	planOf := func(env string) string {
		return "tofu plan -input=false -no-color -lock=false -out=WS/environment-" + env + ".plan -var environment=" + env
	}
	showOf := func(env string) string {
		return "tofu show -json WS/environment-" + env + ".plan"
	}
	every := []string{initOf("tst", "p-tst"), planOf("tst"), showOf("tst"), initOf("stg", "p-stg"), planOf("stg"), showOf("stg"), initOf("prd", "p-prd"), planOf("prd"), showOf("prd")}
	tests := []struct {
		name        string
		env         string
		stackFile   string
		subs        map[string]string
		pins        map[string]string
		deployer    bool
		planErr     error
		wantOut     []string
		wantTofu    []string
		wantComment []string
		wantErr     string
	}{
		{
			name:      "the file stores' bucket policies, named from the triggers' buckets, pass each environment's test",
			stackFile: "resource \"google_storage_bucket_iam_policy\" \"files\" {}\nresource \"google_storage_bucket_iam_policy\" \"files_documents\" {}\n",
			subs: func() map[string]string {
				subs := prSubs()
				subs[fileStoresSub] = "google_storage_bucket.files,google_storage_bucket.files_documents"

				return subs
			}(),
			pins:     enabledPins(),
			wantOut:  []string{"prd: Plan: 2 to add, 1 to change, 1 to destroy.", "Tests passed: no authoritative IAM resource other than a file store's bucket policy; 2 pinned secret version(s) exist and are enabled."},
			wantTofu: every,
		},
		{
			name:      "a policy on a bucket that is not a file store's fails the first environment's test, said on the pull request",
			env:       "export GITHUB_TOKEN=\"test-token\"\n",
			stackFile: "resource \"google_storage_bucket_iam_policy\" \"records\" {}\n",
			subs: func() map[string]string {
				subs := prSubs()
				subs[fileStoresSub] = "google_storage_bucket.files"

				return subs
			}(),
			pins:        enabledPins(),
			wantTofu:    []string{initOf("tst", "p-tst"), planOf("tst"), showOf("tst")},
			wantComment: []string{"The plan of tst's stack failed a test (build b-1): Build REJECTED: the stack declares an authoritative IAM resource, which replaces every member of its role on each apply (a file store's bucket policy, storage.tf's, is the one admitted): iam.tf:1 google_storage_bucket_iam_policy.records"},
			wantErr:     "iam.tf:1 google_storage_bucket_iam_policy.records",
		},
		{
			name:        "every environment is planned as its plan identity, the summaries said on the pull request",
			subs:        prSubs(),
			pins:        enabledPins(),
			deployer:    true,
			wantOut:     []string{"=== tst's stack at 3-app/quill/tst as quill-plan@p-tst.iam.gserviceaccount.com ===", "stg: Plan: 2 to add, 1 to change, 1 to destroy.", "=== prd's stack at 3-app/quill/prd as quill-plan@p-prd.iam.gserviceaccount.com ==="},
			wantTofu:    every,
			wantComment: []string{"The stack's plan for each environment (build b-1), the same tests passed in each:", "**tst**: Plan: 2 to add, 1 to change, 1 to destroy.\n  - `update google_cloud_run_v2_service.app[\"uc1\"]`", "**prd**: Plan: 2 to add, 1 to change, 1 to destroy."},
		},
		{
			name:     "without a deployer app the summaries stay in the log",
			subs:     prSubs(),
			pins:     enabledPins(),
			wantOut:  []string{"prd: Plan: 2 to add, 1 to change, 1 to destroy."},
			wantTofu: every,
		},
		{
			name: "an environment without a plan identity is refused before any plan",
			subs: func() map[string]string {
				subs := prSubs()
				subs[planIdentitiesSub] = "tst=quill-plan@p-tst.iam.gserviceaccount.com,stg=,prd=quill-plan@p-prd.iam.gserviceaccount.com"

				return subs
			}(),
			wantErr: "build.json names no plan identity for stg (_PLAN_IDENTITIES): apply 2-env for stg with this bedrock",
		},
		{
			name:        "a failing test is said on the pull request with the repository's token",
			env:         "export GITHUB_TOKEN=\"test-token\"\n",
			subs:        prSubs(),
			pins:        map[string]string{cookieVersion: "DESTROYED", dbVersion: enabledState},
			wantTofu:    []string{initOf("tst", "p-tst"), planOf("tst"), showOf("tst")},
			wantComment: []string{"The plan of tst's stack failed a test (build b-1): Build REJECTED: a planned revision template pins secret version projects/p-stg/secrets/quill-cookie-key/versions/3, which is DESTROYED"},
			wantErr:     "which is DESTROYED",
		},
		{
			name:        "a failing plan is said on the pull request",
			env:         "export GITHUB_TOKEN=\"test-token\"\n",
			subs:        prSubs(),
			planErr:     errors.New("tofu failed: fake"),
			wantTofu:    []string{initOf("tst", "p-tst"), planOf("tst")},
			wantComment: []string{"The plan of tst's stack failed; the build log says why (build b-1)."},
			wantErr:     "fake",
		},
		{
			name:    "a teardown plans nothing",
			env:     "export DOWN=\"true\"\n",
			subs:    prSubs(),
			wantOut: []string{"/gcbrun down: nothing to plan for the environments."},
		},
		{
			name:    "triggers without the promotion order plan nothing, with a notice",
			subs:    stackSubs(),
			wantOut: []string{"The triggers carry no _ENVIRONMENTS: the stack has not been applied with this bedrock yet, which the first release on it does; the environments' plans wait for it."},
		},
		{
			name:    "a tag build does nothing here",
			subs:    tagSubs(),
			wantOut: []string{tagBuildNotice},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subs := tt.subs
			secrets := &fakeSecrets{states: tt.pins}
			if tt.deployer {
				subs, secrets = deployerSubs(subs), deployerSecrets(t)
				secrets.states = tt.pins
			}
			w := workspaceFiles(t, map[string]string{EnvironmentFile: "export SKIP_DEPLOY=\"\"\n" + tt.env, BuildFile: buildFor(t, subs)})
			if tt.stackFile != "" {
				writeStackFile(t, w, tt.stackFile)
			}
			run := &fakeRunner{outputs: map[string]string{"tofu show": stackPlanJSON}, fail: map[string]error{"tofu plan": tt.planErr}}
			repo := &githubtest.Repo{}
			_, gh := githubStandIn(t, repo)
			var out strings.Builder
			err := PlanEnvironments(t.Context(), &Clients{Exec: run, GitHub: gh, Secrets: secrets.open, SecretsAs: secrets.openAs}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("PlanEnvironments() error = %v, want %q; output:\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("PlanEnvironments() error = %v; output:\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			lines := strings.ReplaceAll(strings.Join(run.lines(), "\n"), string(w), "WS")
			if diff := cmp.Diff(strings.Join(tt.wantTofu, "\n"), lines); diff != "" {
				t.Errorf("tofu (-want +got):\n%s", diff)
			}
			comments := repo.Comments[7]
			if len(tt.wantComment) == 0 {
				if len(comments) != 0 {
					t.Errorf("posted %q, want nothing", comments[0].Body)
				}

				return
			}
			if len(comments) != 1 {
				t.Fatalf("posted %d comment(s), want one", len(comments))
			}
			containsAll(t, comments[0].Body, tt.wantComment...)
			if _, err := os.Stat(filepath.Join(string(w), stackDir, ".terraform")); !os.IsNotExist(err) {
				t.Errorf("the backend cache stays after the plans: %v", err)
			}
		})
	}
}
