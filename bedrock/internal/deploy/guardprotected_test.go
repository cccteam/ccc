package deploy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// protectedPlanJSON is a plan with one change per case, as tofu show -json writes it.
func protectedPlanJSON(address, typ string, index any, actions []string, before map[string]any) string {
	change := map[string]any{"address": address, "type": typ, "mode": "managed", "change": map[string]any{"actions": actions, "before": before, "after": nil}}
	if index != nil {
		change["index"] = index
	}
	data, _ := json.Marshal(map[string]any{"resource_changes": []any{change}})

	return string(data)
}

func TestProtectedDeletions(t *testing.T) {
	t.Parallel()

	const retire = `a release that stops declaring it is refused until the release before it retired it: keep the declaration, name %s in retired in terraform.tfvars (retired = ["%s"]) and release, which lifts the protection; the next release removes the declaration and the name together`
	tests := []struct {
		name    string
		plan    string
		want    []string
		wantErr string
	}{
		{
			name: "a secret container destroyed while the state records its protection is refused, naming the variable to retire",
			plan: protectedPlanJSON(`google_secret_manager_secret.quill["APP_LEGACY_KEY"]`, "google_secret_manager_secret", "APP_LEGACY_KEY", []string{"delete"}, map[string]any{"deletion_protection": true}),
			want: []string{`google_secret_manager_secret.quill["APP_LEGACY_KEY"]: the plan destroys it, and the state records its protection (deletion_protection = true); a secret container holds every version of its value, so ` + strings.ReplaceAll(strings.ReplaceAll(retire, "%s in", "APP_LEGACY_KEY in"), `["%s"]`, `["APP_LEGACY_KEY"]`)},
		},
		{
			name: "a build secret's container is named by its name",
			plan: protectedPlanJSON(`google_secret_manager_secret.build["LICENSE"]`, "google_secret_manager_secret", "LICENSE", []string{"delete"}, map[string]any{"deletion_protection": true}),
			want: []string{`google_secret_manager_secret.build["LICENSE"]: the plan destroys it, and the state records its protection (deletion_protection = true); a secret container holds every version of its value, so ` + strings.ReplaceAll(strings.ReplaceAll(retire, "%s in", "LICENSE in"), `["%s"]`, `["LICENSE"]`)},
		},
		{
			name: "a secret container whose protection the release before lifted passes",
			plan: protectedPlanJSON(`google_secret_manager_secret.quill["APP_LEGACY_KEY"]`, "google_secret_manager_secret", "APP_LEGACY_KEY", []string{"delete"}, map[string]any{"deletion_protection": false}),
		},
		{
			name: "a file store's bucket is named by its variable, from the resource's name",
			plan: protectedPlanJSON("google_storage_bucket.files_exports[0]", "google_storage_bucket", 0, []string{"delete"}, map[string]any{"force_destroy": false}),
			want: []string{`google_storage_bucket.files_exports[0]: the plan destroys it, and the state records its protection (force_destroy = false); a file store's bucket holds its objects, so ` + strings.ReplaceAll(strings.ReplaceAll(retire, "%s in", "APP_FILE_STORE_EXPORTS in"), `["%s"]`, `["APP_FILE_STORE_EXPORTS"]`)},
		},
		{
			name: "the default file store is APP_FILE_STORE",
			plan: protectedPlanJSON("google_storage_bucket.files[0]", "google_storage_bucket", 0, []string{"delete"}, map[string]any{"force_destroy": false}),
			want: []string{`google_storage_bucket.files[0]: the plan destroys it, and the state records its protection (force_destroy = false); a file store's bucket holds its objects, so ` + strings.ReplaceAll(strings.ReplaceAll(retire, "%s in", "APP_FILE_STORE in"), `["%s"]`, `["APP_FILE_STORE"]`)},
		},
		{
			name: "a bucket that is not a file store's is bedrock destroy's alone",
			plan: protectedPlanJSON("google_storage_bucket.migrate_logs[0]", "google_storage_bucket", 0, []string{"delete"}, map[string]any{"force_destroy": false}),
			want: []string{"google_storage_bucket.migrate_logs[0]: the plan destroys it, and the state records its protection (force_destroy = false); a file store's bucket holds its objects, so bedrock destroy alone destroys it, lifting the protection with an apply of its own first"},
		},
		{
			name: "a bucket whose force-destroy the release before lifted passes",
			plan: protectedPlanJSON("google_storage_bucket.files_exports[0]", "google_storage_bucket", 0, []string{"delete"}, map[string]any{"force_destroy": true}),
		},
		{
			name: "the database replaced is a destroy, refused: a restore run and bedrock destroy lift its protection themselves",
			plan: protectedPlanJSON("google_spanner_database.quill[0]", "google_spanner_database", 0, []string{"delete", "create"}, map[string]any{"deletion_protection": true}),
			want: []string{"google_spanner_database.quill[0]: the plan destroys it, and the state records its protection (deletion_protection = true); the database holds the environment's rows, so bedrock destroy alone destroys it, and a restore run alone replaces it, each lifting the protection with an apply of its own first"},
		},
		{
			name: "the database replaced after a restore run's lift passes",
			plan: protectedPlanJSON("google_spanner_database.quill[0]", "google_spanner_database", 0, []string{"delete", "create"}, map[string]any{"deletion_protection": false}),
		},
		{
			name: "the Firestore database destroyed while protected is refused, naming its variable",
			plan: protectedPlanJSON("google_firestore_database.firestore", "google_firestore_database", nil, []string{"delete"}, map[string]any{"delete_protection_state": "DELETE_PROTECTION_ENABLED"}),
			want: []string{`google_firestore_database.firestore: the plan destroys it, and the state records its protection (delete_protection_state = DELETE_PROTECTION_ENABLED); the Firestore database holds its documents, so ` + strings.ReplaceAll(strings.ReplaceAll(retire, "%s in", "APP_FIRESTORE_DATABASE in"), `["%s"]`, `["APP_FIRESTORE_DATABASE"]`)},
		},
		{
			name: "an update of a protected resource passes",
			plan: protectedPlanJSON(`google_secret_manager_secret.quill["APP_LEGACY_KEY"]`, "google_secret_manager_secret", "APP_LEGACY_KEY", []string{"update"}, map[string]any{"deletion_protection": true}),
		},
		{
			name: "a destroy of a type the stack does not protect passes",
			plan: protectedPlanJSON(`google_cloud_run_v2_service.app["uc1"]`, "google_cloud_run_v2_service", "uc1", []string{"delete"}, map[string]any{"deletion_protection": true}),
		},
		{
			name:    "a plan that is not JSON is refused",
			plan:    "{",
			wantErr: "the plan's JSON",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := protectedDeletions([]byte(tt.plan))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("protectedDeletions() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("protectedDeletions() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("protectedDeletions() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGuardProtected(t *testing.T) {
	t.Parallel()

	plan := protectedPlanJSON(`google_secret_manager_secret.quill["APP_LEGACY_KEY"]`, "google_secret_manager_secret", "APP_LEGACY_KEY", []string{"delete"}, map[string]any{"deletion_protection": true})
	err := guardProtected([]byte(plan), "stg")
	if err == nil {
		t.Fatal("guardProtected() = nil, want the refusal")
	}
	for _, want := range []string{"Build REJECTED: the plan of stg's stack destroys what the state protects, and nothing was applied:\n  google_secret_manager_secret.quill[\"APP_LEGACY_KEY\"]: the plan destroys it", `retired = ["APP_LEGACY_KEY"]`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("guardProtected() error lacks %q:\n%v", want, err)
		}
	}
	if err := guardProtected([]byte(stackPlanJSON), "stg"); err != nil {
		t.Errorf("guardProtected() on a plan destroying nothing protected = %v", err)
	}
}
