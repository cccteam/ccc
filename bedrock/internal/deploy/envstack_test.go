package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

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

// withSub is the substitutions with one more set.
func withSub(subs map[string]string, name, value string) map[string]string {
	subs[name] = value

	return subs
}

// tstSubs are a tag build's substitutions in tst, with stg's identity and project, so
// the lines a test expects differ from tagSubs's in the environment alone.
func tstSubs() map[string]string {
	subs := tagSubs()
	subs[envSub] = tstEnvironment

	return subs
}

// testStateBucket is the state bucket the test placement names (testPlacement).
const testStateBucket = "b"

// rollbackSubs is stg with the records bucket and the migrate command's databases the
// triggers carry, which a rollback needs.
func rollbackSubs() map[string]string {
	subs := tagSubs()
	subs[recordsBucket] = "records"
	subs[migrateDatabasesSub] = `["projects/p-spn/instances/shared-spanner/databases/p-stg-gbl-quill-db"]`

	return subs
}

// promotedSubs is stg with the promotion order the triggers carry, which names production.
func promotedSubs() map[string]string {
	subs := tagSubs()
	subs[environmentsSub] = "tst,stg,prd"

	return subs
}

// seededSubs is tst whose trigger still says _SEED=false, from its stack's last apply,
// while the facts seed (SEED, the checkout's placement): the facts decide.
func seededSubs() map[string]string {
	subs := tstSubs()
	subs[seedSub] = "false"

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
		planLine = "tofu plan -input=false -no-color -out=WS/stack.plan -var environment=stg -var database_generation=1"
		showLine = "tofu show -json WS/stack.plan"
	)
	const (
		quillDatabase     = "projects/p-spn/instances/shared-spanner/databases/p-stg-gbl-quill-db"
		releaseBackupName = "projects/p-spn/instances/shared-spanner/backups/p-stg-gbl-quill-db-pre-v1-2-3"
		generationEnv     = "export SKIP_DEPLOY=\"\"\nexport MAINTENANCE=\"true\"\nexport RESTORE=\"" + releaseBackupName + "\"\nexport RESTORE_REASON=\"v1.2.3 mangled the invoices\"\nexport RESTORE_REQUESTER=\"octocat\"\n"
		rollbackEnv       = "export SKIP_DEPLOY=\"\"\nexport ROLLBACK=\"v1.2.4\"\nexport ROLLBACK_REASON=\"v1.2.4 mangled the invoices\"\nexport RESTORE_REQUESTER=\"octocat\"\n"
		restoreEnv        = "export SKIP_DEPLOY=\"\"\nexport RESTORE=\"empty\"\nexport RESTORE_REQUESTER=\"octocat\"\n"
		stateList         = "google_spanner_database.quill[0]\ngoogle_firestore_database.firestore\ngoogle_storage_bucket.files\ngoogle_cloud_run_v2_service.app[\"uc1\"]\n"
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
		// backup is the backup the instance holds (production's for a production-backup
		// restore, the chosen one for a rollback); nil when none. creating is how many
		// reads of it answer CREATING first; pending how many backup starts Spanner
		// refuses first because it is taking another backup.
		backup   *Backup
		creating int
		pending  int
		// objects are the records bucket's objects before the run, by gs:// path.
		objects map[string]string
		// live is the maintenance variable's value on the live service the stack names
		// (_SERVICES is set with it); "" leaves the service out.
		live         string
		wantOut      []string
		wantTofu     []string
		wantFact     string
		wantReplaced string
		// wantBackup is the backup fact a production-backup restore leaves.
		wantBackup string
		// wantCreated are the backups a rollback started; wantRestored what it restored;
		// wantFacts the facts it left; wantObject the generation object it wrote.
		wantCreated  []string
		wantRestored string
		wantFacts    map[string]string
		wantObject   string
		wantErr      string
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
			env:          restoreEnv + "export SEED=\"true\"\n",
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
			backup:       &Backup{Name: "projects/p-spn/instances/shared-spanner/backups/p-prd-gbl-quill-db-20261001", VersionTime: "2026-10-01T02:00:00Z", CreateTime: "2026-10-01T02:05:00Z", State: BackupReady},
			wantOut:      []string{"=== Restore (production-backup, asked for by octocat): stg's database p-stg-gbl-quill-db is dropped and restored from production's backup p-prd-gbl-quill-db-20261001 (data as of 2026-10-01T02:00:00Z); the migrations production's backup predates then apply ===", "Dropped p-stg-gbl-quill-db.", "Restored p-stg-gbl-quill-db from p-prd-gbl-quill-db-20261001; the plan recreates its memberships.", "Tests passed"},
			wantTofu:     []string{initLine, "tofu state show google_spanner_database.quill[0]", planLine, showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantReplaced: "google_spanner_database.quill[0]",
			wantBackup:   "projects/p-spn/instances/shared-spanner/backups/p-prd-gbl-quill-db-20261001",
		},
		{
			name:         "a restore asked minutes after a release waits for the release's backup, which Spanner is still taking, and restores it",
			subs:         promotedSubs(),
			pins:         enabledPins(),
			env:          strings.Replace(restoreEnv, "empty", "production-backup", 1),
			state:        "resource \"google_spanner_database\" \"quill\" {\n    instance = \"shared-spanner\"\n    name     = \"p-stg-gbl-quill-db\"\n    project  = \"p-spn\"\n}\n",
			backup:       &Backup{Name: "projects/p-spn/instances/shared-spanner/backups/p-prd-gbl-quill-db-20261001", VersionTime: "2026-10-01T02:00:00Z", CreateTime: "2026-10-01T02:05:00Z", State: BackupReady},
			creating:     1,
			wantOut:      []string{"Waiting for p-prd-gbl-quill-db-20261001: Spanner is still taking it (CREATING after 0s); a restore needs a READY backup.", "stg's database p-stg-gbl-quill-db is dropped and restored from production's backup p-prd-gbl-quill-db-20261001 (data as of 2026-10-01T02:00:00Z)", "Restored p-stg-gbl-quill-db from p-prd-gbl-quill-db-20261001; the plan recreates its memberships.", "Tests passed"},
			wantTofu:     []string{initLine, "tofu state show google_spanner_database.quill[0]", planLine, showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantReplaced: "google_spanner_database.quill[0]",
			wantBackup:   "projects/p-spn/instances/shared-spanner/backups/p-prd-gbl-quill-db-20261001",
		},
		{
			name:         "a restore from production's backup reads production's live database from its record, the generation a rollback restored into, for the newest backup",
			subs:         promotedSubs(),
			pins:         enabledPins(),
			env:          strings.Replace(restoreEnv, "empty", "production-backup", 1) + "export RESTORE_SOURCE_DATABASE=\"projects/p-spn/instances/shared-spanner/databases/p-prd-gbl-quill-db-3\"\n",
			state:        "resource \"google_spanner_database\" \"quill\" {\n    instance = \"shared-spanner\"\n    name     = \"p-stg-gbl-quill-db\"\n    project  = \"p-spn\"\n}\n",
			backup:       &Backup{Name: "projects/p-spn/instances/shared-spanner/backups/p-prd-gbl-quill-db-3-20261001", VersionTime: "2026-10-01T02:00:00Z", CreateTime: "2026-10-01T02:05:00Z", State: BackupReady},
			wantOut:      []string{"stg's database p-stg-gbl-quill-db is dropped and restored from production's backup p-prd-gbl-quill-db-3-20261001 (data as of 2026-10-01T02:00:00Z)", "Restored p-stg-gbl-quill-db from p-prd-gbl-quill-db-3-20261001; the plan recreates its memberships.", "Tests passed"},
			wantTofu:     []string{initLine, "tofu state show google_spanner_database.quill[0]", planLine, showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantReplaced: "google_spanner_database.quill[0]",
			wantBackup:   "projects/p-spn/instances/shared-spanner/backups/p-prd-gbl-quill-db-3-20261001",
		},
		{
			name:         "the backup the maintenance step chose and waited for is restored, with no second choice",
			subs:         promotedSubs(),
			pins:         enabledPins(),
			env:          strings.Replace(restoreEnv, "empty", "production-backup", 1) + "export RESTORE_SOURCE_DATABASE=\"projects/p-spn/instances/shared-spanner/databases/p-prd-gbl-quill-db-3\"\nexport RESTORE_READY_BACKUP=\"projects/p-spn/instances/shared-spanner/backups/p-prd-gbl-quill-db-3-pre-v0-17-2\"\n",
			state:        "resource \"google_spanner_database\" \"quill\" {\n    instance = \"shared-spanner\"\n    name     = \"p-stg-gbl-quill-db\"\n    project  = \"p-spn\"\n}\n",
			backup:       &Backup{Name: "projects/p-spn/instances/shared-spanner/backups/p-prd-gbl-quill-db-3-pre-v0-17-2", VersionTime: "2026-10-06T00:11:01Z", CreateTime: "2026-10-06T00:11:02Z", State: BackupReady},
			wantOut:      []string{"stg's database p-stg-gbl-quill-db is dropped and restored from production's backup p-prd-gbl-quill-db-3-pre-v0-17-2 (data as of 2026-10-06T00:11:01Z)", "Restored p-stg-gbl-quill-db from p-prd-gbl-quill-db-3-pre-v0-17-2; the plan recreates its memberships.", "Tests passed"},
			wantTofu:     []string{initLine, "tofu state show google_spanner_database.quill[0]", planLine, showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantReplaced: "google_spanner_database.quill[0]",
			wantBackup:   "projects/p-spn/instances/shared-spanner/backups/p-prd-gbl-quill-db-3-pre-v0-17-2",
		},
		{
			name:         "a live generation with no backup of its own restores the backup the rollback restored it from",
			subs:         promotedSubs(),
			pins:         enabledPins(),
			env:          strings.Replace(restoreEnv, "empty", "production-backup", 1) + "export RESTORE_SOURCE_DATABASE=\"projects/p-spn/instances/shared-spanner/databases/p-prd-gbl-quill-db-3\"\nexport RESTORE_SOURCE_BACKUP=\"projects/p-spn/instances/shared-spanner/backups/p-prd-gbl-quill-db-2-pre-v0-16-3\"\n",
			state:        "resource \"google_spanner_database\" \"quill\" {\n    instance = \"shared-spanner\"\n    name     = \"p-stg-gbl-quill-db\"\n    project  = \"p-spn\"\n}\n",
			backup:       &Backup{Name: "projects/p-spn/instances/shared-spanner/backups/p-prd-gbl-quill-db-2-pre-v0-16-3", VersionTime: "2026-10-05T18:03:57Z", CreateTime: "2026-10-05T18:04:00Z", State: BackupReady},
			wantOut:      []string{"p-prd-gbl-quill-db-3 has no backup of its own yet (a restore put it there from p-prd-gbl-quill-db-2-pre-v0-16-3, and no release or schedule has taken one since): that backup, whose data its own began as, is restored.", "stg's database p-stg-gbl-quill-db is dropped and restored from production's backup p-prd-gbl-quill-db-2-pre-v0-16-3 (data as of 2026-10-05T18:03:57Z)", "Tests passed"},
			wantTofu:     []string{initLine, "tofu state show google_spanner_database.quill[0]", planLine, showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantReplaced: "google_spanner_database.quill[0]",
			wantBackup:   "projects/p-spn/instances/shared-spanner/backups/p-prd-gbl-quill-db-2-pre-v0-16-3",
		},
		{
			name:     "a live generation with no backup of its own and none it was restored from is refused",
			subs:     promotedSubs(),
			pins:     enabledPins(),
			env:      strings.Replace(restoreEnv, "empty", "production-backup", 1) + "export RESTORE_SOURCE_DATABASE=\"projects/p-spn/instances/shared-spanner/databases/p-prd-gbl-quill-db-3\"\n",
			state:    "resource \"google_spanner_database\" \"quill\" {\n    instance = \"shared-spanner\"\n    name     = \"p-stg-gbl-quill-db\"\n    project  = \"p-spn\"\n}\n",
			wantTofu: []string{initLine, "tofu state show google_spanner_database.quill[0]"},
			wantErr:  "_RESTORE=production-backup: shared-spanner has no backup of production's database p-prd-gbl-quill-db-3; stg keeps its database",
		},
		{
			name:     "production's live database on another instance is refused",
			subs:     promotedSubs(),
			pins:     enabledPins(),
			env:      strings.Replace(restoreEnv, "empty", "production-backup", 1) + "export RESTORE_SOURCE_DATABASE=\"projects/p-spn/instances/other/databases/p-prd-gbl-quill-db\"\n",
			state:    "resource \"google_spanner_database\" \"quill\" {\n    instance = \"shared-spanner\"\n    name     = \"p-stg-gbl-quill-db\"\n    project  = \"p-spn\"\n}\n",
			wantTofu: []string{initLine, "tofu state show google_spanner_database.quill[0]"},
			wantErr:  "_RESTORE=production-backup: production's live database projects/p-spn/instances/other/databases/p-prd-gbl-quill-db is not on stg's instance shared-spanner, and a backup is restored within one instance",
		},
		{
			name:     "a restore from production's backup is refused when production's database has no backup",
			subs:     promotedSubs(),
			pins:     enabledPins(),
			env:      strings.Replace(restoreEnv, "empty", "production-backup", 1),
			state:    "resource \"google_spanner_database\" \"quill\" {\n    instance = \"shared-spanner\"\n    name     = \"p-stg-gbl-quill-db\"\n    project  = \"p-spn\"\n}\n",
			wantTofu: []string{initLine, "tofu state show google_spanner_database.quill[0]"},
			wantErr:  "_RESTORE=production-backup: shared-spanner has no backup of production's database p-prd-gbl-quill-db; stg keeps its database",
		},
		{
			name:         "a restore to a backup finds it, starts the forensic backup, restores into generation 2, writes the generation beside the records, imports the database and plans at it, in maintenance",
			subs:         rollbackSubs(),
			pins:         enabledPins(),
			env:          generationEnv,
			backup:       &Backup{Name: releaseBackupName, Database: quillDatabase, State: BackupReady, VersionTime: "2026-10-04T02:00:00Z"},
			wantOut:      []string{"Forensic backup: p-stg-gbl-quill-db-forensic-20261005-0430 holds p-stg-gbl-quill-db as of 2026-10-05T04:30:00Z, kept thirty days; p-stg-gbl-quill-db itself stays, protected, as the forensic copy.", "=== Restore: stg is restored from p-stg-gbl-quill-db-pre-v1-2-3 (data as of 2026-10-04T02:00:00Z) into p-stg-gbl-quill-db-2, generation 2 of quill's database ===", "Restored p-stg-gbl-quill-db-2 from p-stg-gbl-quill-db-pre-v1-2-3; Spanner optimizes it in the background and it serves meanwhile.", `Imported p-stg-gbl-quill-db-2 into the stack as google_spanner_database.restored["2"]; the plan points the stack at generation 2.`, "Tests passed"},
			wantTofu:     []string{initLine, `tofu import -input=false -no-color -var environment=stg -var database_generation=2 google_spanner_database.restored["2"] ` + quillDatabase + "-2", strings.Replace(planLine, "generation=1", "generation=2", 1) + " -var maintenance=1", showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantCreated:  []string{"p-stg-gbl-quill-db-forensic-20261005-0430 of p-stg-gbl-quill-db as of 2026-10-05T04:30:00Z until 2026-11-04T04:30:00Z"},
			wantRestored: "p-stg-gbl-quill-db-2 from " + releaseBackupName,
			wantFacts: map[string]string{
				databaseGenerationFact: "2", previousGenerationFact: "1", backupFact: releaseBackupName, backupTimeFact: "2026-10-04T02:00:00Z",
				restoreForensicFact: "projects/p-spn/instances/shared-spanner/backups/p-stg-gbl-quill-db-forensic-20261005-0430", restoreIntoFact: quillDatabase + "-2", restoreKeptFact: quillDatabase,
				// The forensic backup stands as the run's release backup in the record.
				cutFact: "2026-10-05T04:30:00Z", releaseBackupFact: "projects/p-spn/instances/shared-spanner/backups/p-stg-gbl-quill-db-forensic-20261005-0430", releaseBackupTimeFact: "2026-10-05T04:30:00Z", releaseBackupExpiresFact: "2026-11-04T04:30:00Z",
			},
			wantObject: "gs://records/quill/database/stg/2.json",
		},
		{
			name:         "a restore waits for a backup Spanner is still taking, and starts the forensic backup once it is READY",
			subs:         rollbackSubs(),
			pins:         enabledPins(),
			env:          generationEnv,
			backup:       &Backup{Name: releaseBackupName, Database: quillDatabase, State: BackupReady, VersionTime: "2026-10-04T02:00:00Z"},
			creating:     2,
			wantOut:      []string{"Waiting for p-stg-gbl-quill-db-pre-v1-2-3: Spanner is still taking it (CREATING after 0s); a restore needs a READY backup.\nForensic backup: p-stg-gbl-quill-db-forensic-20261005-0430 holds", "=== Restore: stg is restored from p-stg-gbl-quill-db-pre-v1-2-3"},
			wantCreated:  []string{"p-stg-gbl-quill-db-forensic-20261005-0430 of p-stg-gbl-quill-db as of 2026-10-05T04:30:00Z until 2026-11-04T04:30:00Z"},
			wantTofu:     []string{initLine, `tofu import -input=false -no-color -var environment=stg -var database_generation=2 google_spanner_database.restored["2"] ` + quillDatabase + "-2", strings.Replace(planLine, "generation=1", "generation=2", 1) + " -var maintenance=1", showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantRestored: "p-stg-gbl-quill-db-2 from " + releaseBackupName,
		},
		{
			name:         "a restore waits to start the forensic backup while Spanner takes another backup of the database",
			subs:         rollbackSubs(),
			pins:         enabledPins(),
			env:          generationEnv,
			backup:       &Backup{Name: releaseBackupName, Database: quillDatabase, State: BackupReady, VersionTime: "2026-10-04T02:00:00Z"},
			pending:      2,
			wantOut:      []string{"Waiting to start the forensic backup p-stg-gbl-quill-db-forensic-20261005-0430: Spanner is taking another backup of p-stg-gbl-quill-db, and takes one at a time (0s so far); it starts when that one completes.\nForensic backup: p-stg-gbl-quill-db-forensic-20261005-0430 holds", "=== Restore: stg is restored from p-stg-gbl-quill-db-pre-v1-2-3"},
			wantTofu:     []string{initLine, `tofu import -input=false -no-color -var environment=stg -var database_generation=2 google_spanner_database.restored["2"] ` + quillDatabase + "-2", strings.Replace(planLine, "generation=1", "generation=2", 1) + " -var maintenance=1", showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantCreated:  []string{"p-stg-gbl-quill-db-forensic-20261005-0430 of p-stg-gbl-quill-db as of 2026-10-05T04:30:00Z until 2026-11-04T04:30:00Z"},
			wantRestored: "p-stg-gbl-quill-db-2 from " + releaseBackupName,
		},
		{
			name:         "a restore to a moment starts a backup of the live database as of it and restores that",
			subs:         rollbackSubs(),
			pins:         enabledPins(),
			env:          strings.Replace(generationEnv, releaseBackupName, "@2026-10-04T02:00:00Z", 1),
			backup:       &Backup{Name: "projects/p-spn/instances/shared-spanner/backups/p-stg-gbl-quill-db-pit-20261004-0200", Database: quillDatabase, State: BackupReady, VersionTime: "2026-10-04T02:00:00Z"},
			wantOut:      []string{"Point in time: p-stg-gbl-quill-db-pit-20261004-0200 is made of p-stg-gbl-quill-db as of 2026-10-04T02:00:00Z, kept fourteen days; the restore waits for it.", "=== Restore: stg is restored from p-stg-gbl-quill-db-pit-20261004-0200 (data as of 2026-10-04T02:00:00Z) into p-stg-gbl-quill-db-2, generation 2 of quill's database ==="},
			wantTofu:     []string{initLine, `tofu import -input=false -no-color -var environment=stg -var database_generation=2 google_spanner_database.restored["2"] ` + quillDatabase + "-2", strings.Replace(planLine, "generation=1", "generation=2", 1) + " -var maintenance=1", showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantCreated:  []string{"p-stg-gbl-quill-db-pit-20261004-0200 of p-stg-gbl-quill-db as of 2026-10-04T02:00:00Z until 2026-10-19T04:30:00Z", "p-stg-gbl-quill-db-forensic-20261005-0430 of p-stg-gbl-quill-db as of 2026-10-05T04:30:00Z until 2026-11-04T04:30:00Z"},
			wantRestored: "p-stg-gbl-quill-db-2 from projects/p-spn/instances/shared-spanner/backups/p-stg-gbl-quill-db-pit-20261004-0200",
		},
		{
			name:         "a second restore restores into generation 3 from the generation-2 database, told by the generation object",
			subs:         withSub(rollbackSubs(), migrateDatabasesSub, `["`+quillDatabase+`-2"]`),
			pins:         enabledPins(),
			env:          generationEnv,
			backup:       &Backup{Name: releaseBackupName, Database: quillDatabase, State: BackupReady, VersionTime: "2026-10-04T02:00:00Z"},
			objects:      map[string]string{"gs://records/quill/database/stg/2.json": "{}"},
			wantOut:      []string{"Generation 2: a restore put stg's database into its generation 2 (gs://records/quill/database/stg/2.json); the plan points the stack at it.", "Forensic backup: p-stg-gbl-quill-db-2-forensic-20261005-0430 holds p-stg-gbl-quill-db-2", "into p-stg-gbl-quill-db-3, generation 3 of quill's database ==="},
			wantTofu:     []string{initLine, `tofu import -input=false -no-color -var environment=stg -var database_generation=3 google_spanner_database.restored["3"] ` + quillDatabase + "-3", strings.Replace(planLine, "generation=1", "generation=3", 1) + " -var maintenance=1", showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantRestored: "p-stg-gbl-quill-db-3 from " + releaseBackupName,
			wantFacts:    map[string]string{databaseGenerationFact: "3", previousGenerationFact: "2", restoreKeptFact: quillDatabase + "-2", restoreIntoFact: quillDatabase + "-3"},
			wantObject:   "gs://records/quill/database/stg/3.json",
		},
		{
			name:      "a rollback run plans at the generation as it is: nothing restored, no backup started, no maintenance",
			subs:      rollbackSubs(),
			pins:      enabledPins(),
			env:       rollbackEnv,
			wantOut:   []string{"Plan: 2 to add, 1 to change, 1 to destroy.", "Tests passed"},
			wantTofu:  []string{initLine, planLine, showLine},
			wantFact:  "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantFacts: map[string]string{databaseGenerationFact: "1"},
		},
		{
			name:         "a restore to a moment held by an earlier generation backs that generation up and restores it into a new one, the live generation kept",
			subs:         withSub(rollbackSubs(), migrateDatabasesSub, `["`+quillDatabase+`-2"]`),
			pins:         enabledPins(),
			env:          strings.Replace(generationEnv, releaseBackupName, "@2026-10-04T02:00:00Z", 1) + "export RESTORE_SOURCE_DATABASE=\"p-stg-gbl-quill-db\"\n",
			backup:       &Backup{Name: "projects/p-spn/instances/shared-spanner/backups/p-stg-gbl-quill-db-pit-20261004-0200", Database: quillDatabase, State: BackupReady, VersionTime: "2026-10-04T02:00:00Z"},
			objects:      map[string]string{"gs://records/quill/database/stg/2.json": "{}"},
			wantOut:      []string{"Point in time: p-stg-gbl-quill-db-pit-20261004-0200 is made of p-stg-gbl-quill-db as of 2026-10-04T02:00:00Z, kept fourteen days; the restore waits for it.", "Forensic backup: p-stg-gbl-quill-db-2-forensic-20261005-0430 holds p-stg-gbl-quill-db-2", "into p-stg-gbl-quill-db-3, generation 3 of quill's database ==="},
			wantTofu:     []string{initLine, `tofu import -input=false -no-color -var environment=stg -var database_generation=3 google_spanner_database.restored["3"] ` + quillDatabase + "-3", strings.Replace(planLine, "generation=1", "generation=3", 1) + " -var maintenance=1", showLine},
			wantFact:     "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantCreated:  []string{"p-stg-gbl-quill-db-pit-20261004-0200 of p-stg-gbl-quill-db as of 2026-10-04T02:00:00Z until 2026-10-19T04:30:00Z", "p-stg-gbl-quill-db-2-forensic-20261005-0430 of p-stg-gbl-quill-db-2 as of 2026-10-05T04:30:00Z until 2026-11-04T04:30:00Z"},
			wantRestored: "p-stg-gbl-quill-db-3 from projects/p-spn/instances/shared-spanner/backups/p-stg-gbl-quill-db-pit-20261004-0200",
			wantFacts:    map[string]string{databaseGenerationFact: "3", previousGenerationFact: "2", restoreKeptFact: quillDatabase + "-2", restoreIntoFact: quillDatabase + "-3"},
		},
		{
			name:      "a release build after a restore plans at the generation the restore wrote",
			subs:      rollbackSubs(),
			pins:      enabledPins(),
			objects:   map[string]string{"gs://records/quill/database/stg/2.json": "{}"},
			wantOut:   []string{"Generation 2: a restore put stg's database into its generation 2 (gs://records/quill/database/stg/2.json); the plan points the stack at it.", "Tests passed"},
			wantTofu:  []string{initLine, strings.Replace(planLine, "generation=1", "generation=2", 1), showLine},
			wantFact:  "Plan: 2 to add, 1 to change, 1 to destroy.",
			wantFacts: map[string]string{databaseGenerationFact: "2"},
		},
		{
			name:     "a restore to a backup the instance does not hold is refused before any backup is started",
			subs:     rollbackSubs(),
			pins:     enabledPins(),
			env:      generationEnv,
			wantTofu: []string{initLine},
			wantErr:  "_RESTORE=" + releaseBackupName + ": there is no such backup on shared-spanner",
		},
		{
			name:     "a restore to a backup of another application's database is refused",
			subs:     rollbackSubs(),
			pins:     enabledPins(),
			env:      generationEnv,
			backup:   &Backup{Name: releaseBackupName, Database: "projects/p-spn/instances/shared-spanner/databases/p-stg-gbl-other-db", State: BackupReady},
			wantTofu: []string{initLine},
			wantErr:  "_RESTORE=" + releaseBackupName + " is a backup of p-stg-gbl-other-db, not of this application's database (p-stg-gbl-quill-db)",
		},
		{
			name:     "a restore to a backup on a build whose trigger names no records bucket is refused before anything",
			subs:     tagSubs(),
			pins:     enabledPins(),
			env:      generationEnv,
			backup:   &Backup{Name: releaseBackupName, Database: quillDatabase, State: BackupReady},
			wantTofu: []string{initLine},
			wantErr:  "build.json carries no _RECORDS_BUCKET: a restore writes the database's generation beside the deployment records, so the run refuses before touching anything",
		},
		{
			name:     "a run under the skip facts plans nothing (the maintenance instruction)",
			subs:     tagSubs(),
			pins:     enabledPins(),
			env:      "export SKIP_DEPLOY=\"true\"\nexport SKIP_REASON=\"Skipped: this run takes stg out of a maintenance an earlier run left on (bedrock maintenance off, asked for by octocat) and deploys nothing.\"\n",
			wantOut:  []string{"Skipped: this run takes stg out of a maintenance an earlier run left on (bedrock maintenance off, asked for by octocat) and deploys nothing."},
			wantTofu: []string{initLine},
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
			spanner := &fakeSpanner{backup: tt.backup, creating: tt.creating, pending: tt.pending}
			store := &memoryStore{objects: map[string]string{}}
			for k, v := range tt.objects {
				store.objects[k] = v
			}
			clients := &Clients{Exec: run, SecretsAs: secrets.openAs, SpannerAs: spanner.open, Storage: store.open, Sleep: noSleep, Now: func() time.Time { return time.Date(2026, 10, 5, 4, 30, 0, 0, time.UTC) }}
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
			// A generation restore's backup is checked through wantFacts; the replace path's here.
			if tt.wantRestored == "" && env[backupFact] != tt.wantBackup {
				t.Errorf("%s = %q, want %q", backupFact, env[backupFact], tt.wantBackup)
			}
			if tt.wantBackup != "" && (spanner.dropped != "projects/p-spn/instances/shared-spanner/databases/p-stg-gbl-quill-db" || spanner.restored != "p-stg-gbl-quill-db from "+tt.wantBackup) {
				t.Errorf("dropped %q, restored %q", spanner.dropped, spanner.restored)
			}
			if tt.wantCreated != nil {
				if diff := cmp.Diff(tt.wantCreated, spanner.created); diff != "" {
					t.Errorf("backups started (-want +got):\n%s", diff)
				}
			}
			if tt.wantErr != "" && len(spanner.created) > 0 {
				t.Errorf("a refused rollback started backups: %v", spanner.created)
			}
			if tt.wantRestored != "" && spanner.restored != tt.wantRestored {
				t.Errorf("restored %q, want %q", spanner.restored, tt.wantRestored)
			}
			if tt.wantRestored != "" && spanner.dropped != "" {
				t.Errorf("a rollback dropped %q; the earlier generation stays", spanner.dropped)
			}
			for fact, want := range tt.wantFacts {
				if env[fact] != want {
					t.Errorf("%s = %q, want %q", fact, env[fact], want)
				}
			}
			if tt.wantObject != "" {
				var note generationNote
				if err := json.Unmarshal([]byte(store.objects[tt.wantObject]), &note); err != nil {
					t.Fatalf("%s: %v", tt.wantObject, err)
				}
				want := generationNote{Requester: "octocat", Reason: "v1.2.3 mangled the invoices", Build: "b-1", Release: subs[tagSub], At: "2026-10-05T04:30:00Z"}
				want.Generation, want.Database, want.Backup, want.Kept = note.Generation, note.Database, note.Backup, note.Kept
				if diff := cmp.Diff(want, note); diff != "" {
					t.Errorf("%s (-want +got):\n%s", tt.wantObject, diff)
				}
				if note.Database != env[restoreIntoFact] || note.Kept != env[restoreKeptFact] || note.Backup != env[backupFact] || strconv.Itoa(note.Generation) != env[databaseGenerationFact] {
					t.Errorf("the generation note (%+v) and the facts disagree", note)
				}
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

	// The stack's substitutions output, which the step reads for the migrate command's
	// settings, and the two reads by their command lines.
	const (
		substitutionsOutput = `{"_SERVICES": "us-central1=quill-app", "_MIGRATE_ENV": "{\"APP_SERVICE_NAME\":\"quill-migrate\"}", "_MIGRATE_DATABASES": "[\"projects/p-stg/instances/i/databases/quill-db\"]"}`
		readSubstitutions   = "tofu output -json substitutions"
		readFirestore       = "tofu output -raw firestore_database"
	)
	settingsRead := "The migrate command's settings and databases are read from the stack as applied (_MIGRATE_ENV, _MIGRATE_DATABASES)."
	tests := []struct {
		name     string
		subs     map[string]string
		planJSON string
		// env replaces the environment file; outputs are what tofu output answers, by
		// command line, and fail what it refuses.
		env      string
		outputs  map[string]string
		fail     map[string]error
		wantOut  []string
		wantTofu []string
		// wantSettings are the migrate command's settings the step left (MIGRATE_ENV);
		// wantCleared is the Firestore database whose documents a restore run deleted;
		// wantHostname the hostname read back (CANONICAL_HOSTNAME).
		wantSettings string
		wantCleared  string
		wantHostname string
		// wantServices, wantJobsJob and wantArguments are the services, the job process's
		// template job and the build arguments read back from the applied stack, when a
		// case checks them.
		wantServices  string
		wantJobsJob   string
		wantArguments map[string]string
		wantErr       string
	}{
		{
			name:         "a restore run deletes the Firestore database's documents after the apply, as the apply identity",
			subs:         tstSubs(),
			planJSON:     stackPlanJSON,
			env:          applyRestoreEnv,
			outputs:      map[string]string{readSubstitutions: substitutionsOutput, readFirestore: "quill-fs\n"},
			wantOut:      []string{"Applied tst's stack: 2 added, 1 changed, 1 destroyed.", settingsRead, "Restore: every document of the Firestore database quill-fs is deleted; its documents referred to rows the restore replaced."},
			wantTofu:     []string{"tofu apply -input=false -no-color WS/stack.plan", readSubstitutions, readFirestore},
			wantSettings: `{"APP_SERVICE_NAME":"quill-migrate"}`,
			wantCleared:  "projects/p-stg/databases/quill-fs",
		},
		{
			name:         "a restore run of a stack without a Firestore database clears nothing",
			subs:         tstSubs(),
			planJSON:     stackPlanJSON,
			env:          applyRestoreEnv,
			outputs:      map[string]string{readSubstitutions: substitutionsOutput},
			fail:         map[string]error{readFirestore: errors.New("no output")},
			wantOut:      []string{"No Firestore database to clear: the stack has no firestore_database output"},
			wantTofu:     []string{"tofu apply -input=false -no-color WS/stack.plan", readSubstitutions, readFirestore},
			wantSettings: `{"APP_SERVICE_NAME":"quill-migrate"}`,
		},
		{
			name:         "the saved plan is applied as the apply identity, and the migrate command's settings read from the applied stack",
			subs:         tagSubs(),
			planJSON:     stackPlanJSON,
			outputs:      map[string]string{readSubstitutions: substitutionsOutput},
			wantOut:      []string{"Applied stg's stack: 2 added, 1 changed, 1 destroyed.", settingsRead},
			wantTofu:     []string{"tofu apply -input=false -no-color WS/stack.plan", readSubstitutions},
			wantSettings: `{"APP_SERVICE_NAME":"quill-migrate"}`,
		},
		{
			name:         "the environment's hostname is read back from the applied stack, where the trigger still names its last apply's",
			subs:         withSub(tagSubs(), hostnameSub, "quill-stg.example.dev"),
			planJSON:     stackPlanJSON,
			outputs:      map[string]string{readSubstitutions: strings.Replace(substitutionsOutput, "{", `{"_HOSTNAME": "quill.stg.example.dev", `, 1)},
			wantOut:      []string{"The environment's hostname is read from the stack as applied (_HOSTNAME): quill.stg.example.dev, where the trigger (its stack's last apply) says \"quill-stg.example.dev\".", settingsRead},
			wantTofu:     []string{"tofu apply -input=false -no-color WS/stack.plan", readSubstitutions},
			wantSettings: `{"APP_SERVICE_NAME":"quill-migrate"}`,
			wantHostname: "quill.stg.example.dev",
		},
		{
			name:         "a hostname the trigger names alike is read back without a word",
			subs:         withSub(tagSubs(), hostnameSub, "quill-stg.example.dev"),
			planJSON:     stackPlanJSON,
			outputs:      map[string]string{readSubstitutions: strings.Replace(substitutionsOutput, "{", `{"_HOSTNAME": "quill-stg.example.dev", `, 1)},
			wantOut:      []string{settingsRead},
			wantTofu:     []string{"tofu apply -input=false -no-color WS/stack.plan", readSubstitutions},
			wantSettings: `{"APP_SERVICE_NAME":"quill-migrate"}`,
			wantHostname: "quill-stg.example.dev",
		},
		{
			name:          "the services, the job process's template job and a build argument the trigger lacks are read from the applied stack",
			subs:          withSub(tagSubs(), servicesSub, "us-central1=quill-app"),
			planJSON:      stackPlanJSON,
			outputs:       map[string]string{readSubstitutions: strings.Replace(substitutionsOutput, `{"_SERVICES": "us-central1=quill-app",`, `{"_SERVICES": "us-central1=quill-app,us-central1=quill-portal", "_JOBS_JOB": "us-central1=quill-jobs", "_BUILD_ARG_THEME": "dark",`, 1)},
			wantOut:       []string{"The services read from the stack as applied (_SERVICES): us-central1=quill-app,us-central1=quill-portal, where the trigger (its stack's last apply) says \"us-central1=quill-app\".", "The job process's template job read from the stack as applied (_JOBS_JOB): us-central1=quill-jobs, where the trigger (its stack's last apply) says \"\".", "The build argument THEME is read from the stack as applied (_BUILD_ARG_THEME), for the steps after the image build, which passed what the trigger carried.", settingsRead},
			wantTofu:      []string{"tofu apply -input=false -no-color WS/stack.plan", readSubstitutions},
			wantSettings:  `{"APP_SERVICE_NAME":"quill-migrate"}`,
			wantServices:  "us-central1=quill-app,us-central1=quill-portal",
			wantJobsJob:   "us-central1=quill-jobs",
			wantArguments: map[string]string{"_BUILD_ARG_THEME": "dark"},
		},
		{
			name:          "values the trigger names alike are read back without a word",
			subs:          withSub(withSub(withSub(tagSubs(), servicesSub, "us-central1=quill-app"), jobsJobSub, "us-central1=quill-jobs"), "_BUILD_ARG_THEME", "dark"),
			planJSON:      stackPlanJSON,
			outputs:       map[string]string{readSubstitutions: strings.Replace(substitutionsOutput, `{"_SERVICES": "us-central1=quill-app",`, `{"_SERVICES": "us-central1=quill-app", "_JOBS_JOB": "us-central1=quill-jobs", "_BUILD_ARG_THEME": "dark",`, 1)},
			wantOut:       []string{settingsRead},
			wantTofu:      []string{"tofu apply -input=false -no-color WS/stack.plan", readSubstitutions},
			wantSettings:  `{"APP_SERVICE_NAME":"quill-migrate"}`,
			wantServices:  "us-central1=quill-app",
			wantJobsJob:   "us-central1=quill-jobs",
			wantArguments: map[string]string{"_BUILD_ARG_THEME": "dark"},
		},
		{
			name:         "a plan with no change applies nothing, and still reads the settings",
			subs:         tagSubs(),
			planJSON:     `{"resource_changes": []}`,
			outputs:      map[string]string{readSubstitutions: substitutionsOutput},
			wantOut:      []string{"Nothing to apply: stg's stack matches the code.", settingsRead},
			wantTofu:     []string{readSubstitutions},
			wantSettings: `{"APP_SERVICE_NAME":"quill-migrate"}`,
		},
		{
			name:     "a stack whose output names no settings for the migrate command is refused",
			subs:     tagSubs(),
			planJSON: `{"resource_changes": []}`,
			outputs:  map[string]string{readSubstitutions: `{"_SERVICES": "us-central1=quill-app"}`},
			wantErr:  "the stack's substitutions output names no _MIGRATE_ENV, the migrate command's settings",
		},
		{
			name:     "a stack whose output names no databases for the migrate command is refused",
			subs:     tagSubs(),
			planJSON: `{"resource_changes": []}`,
			outputs:  map[string]string{readSubstitutions: `{"_SERVICES": "us-central1=quill-app", "_MIGRATE_ENV": "{\"APP_SERVICE_NAME\":\"quill-migrate\"}"}`},
			wantErr:  "the stack's substitutions output names no _MIGRATE_DATABASES, the databases the migrate command reaches",
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
			run := &fakeRunner{outputs: tt.outputs, fail: tt.fail}
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
			if env[migrateEnvFact] != tt.wantSettings {
				t.Errorf("%s = %q, want %q", migrateEnvFact, env[migrateEnvFact], tt.wantSettings)
			}
			if want := `["projects/p-stg/instances/i/databases/quill-db"]`; tt.wantSettings != "" && env[migrateDatabasesFact] != want {
				t.Errorf("%s = %q, want %q", migrateDatabasesFact, env[migrateDatabasesFact], want)
			}
			if store.database != tt.wantCleared {
				t.Errorf("cleared %q, want %q", store.database, tt.wantCleared)
			}
			if env[canonicalHostnameFact] != tt.wantHostname {
				t.Errorf("%s = %q, want %q", canonicalHostnameFact, env[canonicalHostnameFact], tt.wantHostname)
			}
			if tt.wantHostname == tt.subs[hostnameSub] && strings.Contains(out.String(), "The environment's hostname is read") {
				t.Errorf("a hostname the trigger names alike is said:\n%s", out.String())
			}
			if tt.wantServices != "" && env[services] != tt.wantServices {
				t.Errorf("%s = %q, want %q", services, env[services], tt.wantServices)
			}
			if tt.wantJobsJob != "" && env[jobsJobFact] != tt.wantJobsJob {
				t.Errorf("%s = %q, want %q", jobsJobFact, env[jobsJobFact], tt.wantJobsJob)
			}
			for name, want := range tt.wantArguments {
				if env[name] != want {
					t.Errorf("%s = %q, want %q", name, env[name], want)
				}
			}
			if tt.wantServices != "" && tt.wantServices == tt.subs[servicesSub] && tt.wantJobsJob == tt.subs[jobsJobSub] && (strings.Contains(out.String(), "(_SERVICES)") || strings.Contains(out.String(), "(_JOBS_JOB)") || strings.Contains(out.String(), "(_BUILD_ARG_")) {
				t.Errorf("a value the trigger names alike is said:\n%s", out.String())
			}
			if tt.wantCleared != "" && env[clearedFact] != firestoreAddress {
				t.Errorf("%s = %q, want %q", clearedFact, env[clearedFact], firestoreAddress)
			}
		})
	}
}

// fakeSpanner answers the one backup it holds and records the drop, the restore and the
// backups it was asked to create; refuse, when set, is the error every create answers.
type fakeSpanner struct {
	backup   *Backup
	dropped  string
	restored string
	created  []string
	refuse   string
	// creating is how many reads of the backup (listed, or by name) answer CREATING before
	// READY; pending how many starts Spanner refuses first because it is taking another
	// backup.
	creating int
	pending  int
}

// pendingBackupRefusal is Spanner's refusal while it takes another backup of the database.
const pendingBackupRefusal = "Spanner answered HTTP 400 to POST /v1/projects/p-spn/instances/shared-spanner/backups?backupId=x: Cannot create backup (projects/p-spn/instances/shared-spanner/backups/x) for database (projects/p-spn/instances/shared-spanner/databases/p-stg-gbl-quill-db) because the maximum number of pending backups (1) for the database has been reached. Please retry the operation once the pending backups complete."

func (f *fakeSpanner) open(context.Context, string) (Spanner, error) {
	return f, nil
}

func (f *fakeSpanner) LatestBackup(_ context.Context, _, database string) (*Backup, error) {
	if f.backup == nil || !strings.HasSuffix(f.backup.Name, "/backups/"+path.Base(database)+"-20261001") {
		return nil, nil
	}

	return f.read(), nil
}

// read is a copy of the backup, CREATING while the creating reads last.
func (f *fakeSpanner) read() *Backup {
	b := *f.backup
	if f.creating > 0 {
		f.creating--
		b.State = BackupCreating
	}

	return &b
}

func (f *fakeSpanner) DropDatabase(_ context.Context, database string) error {
	f.dropped = database

	return nil
}

func (f *fakeSpanner) RestoreDatabase(_ context.Context, _, databaseID, backup string) error {
	f.restored = databaseID + " from " + backup

	return nil
}

func (f *fakeSpanner) CreateBackup(_ context.Context, instance, backupID, database string, versionTime, expireTime time.Time) (string, error) {
	if f.refuse != "" {
		return "", errors.New(f.refuse)
	}
	if f.pending > 0 {
		f.pending--

		return "", errors.New(pendingBackupRefusal)
	}
	f.created = append(f.created, backupID+" of "+path.Base(database)+" as of "+versionTime.Format(time.RFC3339)+" until "+expireTime.Format(time.RFC3339))

	return instance + "/operations/op-" + backupID, nil
}

func (f *fakeSpanner) Backup(_ context.Context, name string) (*Backup, error) {
	if f.backup == nil || f.backup.Name != name {
		return nil, nil
	}

	return f.read(), nil
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
		return "tofu plan -input=false -no-color -lock=false -out=WS/environment-" + env + ".plan -var environment=" + env + " -var database_generation=1"
	}
	showOf := func(env string) string {
		return "tofu show -json WS/environment-" + env + ".plan"
	}
	every := []string{initOf("tst", "p-tst"), planOf("tst"), showOf("tst"), initOf("stg", "p-stg"), planOf("stg"), showOf("stg"), initOf("prd", "p-prd"), planOf("prd"), showOf("prd")}
	tests := []struct {
		name      string
		env       string
		stackFile string
		subs      map[string]string
		pins      map[string]string
		deployer  bool
		planErr   error
		// objects are the records buckets' objects, by gs:// path: a rollback's generation note.
		objects map[string]string
		// unapplied names an environment whose stack has no state object yet.
		unapplied   string
		wantOut     []string
		wantTofu    []string
		wantComment []string
		wantErr     string
	}{
		{
			name:        "an environment whose stack was never applied is skipped with the line, the others planned, and the comment names it",
			subs:        prSubs(),
			pins:        enabledPins(),
			deployer:    true,
			unapplied:   "stg",
			wantOut:     []string{"stg: no stack yet: its first apply is by hand (the stack README, Applying; the registration sequence's step 4), and the plan waits for it.", "prd: Plan: 2 to add, 1 to change, 1 to destroy."},
			wantTofu:    []string{initOf("tst", "p-tst"), planOf("tst"), showOf("tst"), initOf("prd", "p-prd"), planOf("prd"), showOf("prd")},
			wantComment: []string{"**tst**: Plan: 2 to add, 1 to change, 1 to destroy.", "**stg**: no stack yet: its first apply is by hand, and the plan waits for it\n**prd**: Plan: 2 to add, 1 to change, 1 to destroy."},
		},
		{
			name: "an environment a rollback moved to generation 2 is planned at it, read from its records bucket as the plan identity",
			subs: func() map[string]string {
				subs := prSubs()
				subs[recordsBucketsSub] = "tst=records-tst,stg=records-stg,prd=records-prd"

				return subs
			}(),
			pins:     enabledPins(),
			objects:  map[string]string{"gs://records-prd/quill/database/prd/2.json": "{}"},
			wantOut:  []string{"Generation 2: a restore put prd's database into its generation 2 (gs://records-prd/quill/database/prd/2.json); the plan points the stack at it.", "prd: Plan: 2 to add, 1 to change, 1 to destroy."},
			wantTofu: []string{initOf("tst", "p-tst"), planOf("tst"), showOf("tst"), initOf("stg", "p-stg"), planOf("stg"), showOf("stg"), initOf("prd", "p-prd"), strings.Replace(planOf("prd"), "generation=1", "generation=2", 1), showOf("prd")},
		},
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
			w := workspaceFiles(t, map[string]string{EnvironmentFile: "export SKIP_DEPLOY=\"\"\n" + tt.env, BuildFile: buildFor(t, subs), placementPath: testPlacement("")})
			if tt.stackFile != "" {
				writeStackFile(t, w, tt.stackFile)
			}
			run := &fakeRunner{outputs: map[string]string{"tofu show": stackPlanJSON}, fail: map[string]error{"tofu plan": tt.planErr}}
			repo := &githubtest.Repo{}
			_, gh := githubStandIn(t, repo)
			// Every environment's stack has been applied (its state object exists) but the
			// one the case names.
			store := &memoryStore{objects: map[string]string{}}
			for _, e := range strings.Split(subs[environmentsSub], ",") {
				if e != "" && e != tt.unapplied {
					store.objects["gs://"+testStateBucket+"/"+statePrefix(subs[appSub], e)+"/default.tfstate"] = "{}"
				}
			}
			for k, v := range tt.objects {
				store.objects[k] = v
			}
			var out strings.Builder
			err := PlanEnvironments(t.Context(), &Clients{Exec: run, GitHub: gh, Secrets: secrets.open, SecretsAs: secrets.openAs, StorageAs: store.openAs}, w, &out)
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
