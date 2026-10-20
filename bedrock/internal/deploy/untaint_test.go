package deploy

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// The states tofu show -json answers before a plan: an empty one (before the first
// apply: no values), one whose service and job are tainted (their create failed, the
// first revision's start) beside an untainted bucket and a tainted one (a half-created
// bucket, replaced as before), and one with nothing tainted.
const (
	emptyStateJSON   = `{"format_version": "1.0"}`
	taintedStateJSON = `{"format_version": "1.0", "values": {"root_module": {"resources": [
 {"address": "google_storage_bucket.files", "mode": "managed", "type": "google_storage_bucket", "name": "files", "values": {}},
 {"address": "google_cloud_run_v2_service.app[\"console-uc1\"]", "mode": "managed", "type": "google_cloud_run_v2_service", "name": "app", "index": "console-uc1", "values": {}, "tainted": true},
 {"address": "google_cloud_run_v2_job.jobs", "mode": "managed", "type": "google_cloud_run_v2_job", "name": "jobs", "values": {}, "tainted": true},
 {"address": "google_storage_bucket.files_documents", "mode": "managed", "type": "google_storage_bucket", "name": "files_documents", "values": {}, "tainted": true}
]}}}`
	untaintedStateJSON = `{"format_version": "1.0", "values": {"root_module": {"resources": [
 {"address": "google_storage_bucket.files", "mode": "managed", "type": "google_storage_bucket", "name": "files", "values": {}},
 {"address": "google_cloud_run_v2_service.app[\"console-uc1\"]", "mode": "managed", "type": "google_cloud_run_v2_service", "name": "app", "index": "console-uc1", "values": {}},
 {"address": "google_cloud_run_v2_job.jobs", "mode": "managed", "type": "google_cloud_run_v2_job", "name": "jobs", "values": {}}
]}}}`
	bucketTaintedStateJSON = `{"format_version": "1.0", "values": {"root_module": {"resources": [
 {"address": "google_cloud_run_v2_service.app[\"console-uc1\"]", "mode": "managed", "type": "google_cloud_run_v2_service", "name": "app", "index": "console-uc1", "values": {}},
 {"address": "google_storage_bucket.files", "mode": "managed", "type": "google_storage_bucket", "name": "files", "values": {}, "tainted": true}
]}}}`
	moduleTaintedStateJSON = `{"format_version": "1.0", "values": {"root_module": {"resources": [
 {"address": "google_storage_bucket.files", "mode": "managed", "type": "google_storage_bucket", "name": "files", "values": {}}
], "child_modules": [{"address": "module.site", "resources": [
 {"address": "module.site.google_cloud_run_v2_service.app[\"uc1\"]", "mode": "managed", "type": "google_cloud_run_v2_service", "name": "app", "index": "uc1", "values": {}, "tainted": true}
]}]}}}`
)

// The lines the plan prints for the tainted service and job it untaints.
const (
	untaintedServiceLine = `google_cloud_run_v2_service.app["console-uc1"] is tainted (its create failed) but exists: untainted, so the apply creates a new revision in place of the one whose start failed and does not replace it (which production's deletion protection would refuse).`
	untaintedJobLine     = `google_cloud_run_v2_job.jobs is tainted (its create failed) but exists: untainted, so the apply creates a new revision in place of the one whose start failed and does not replace it (which production's deletion protection would refuse).`
)

// TestTaintedCloudRun: off tofu show -json of the state, the tainted Cloud Run services
// and jobs are named in the state's order, a child module's among them; a tainted
// resource of another kind, an untainted state and an empty state name none; what is not
// the state's JSON is refused.
func TestTaintedCloudRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		shown   string
		want    []string
		wantErr string
	}{
		{
			name:  "a tainted service and job are named in the state's order; a tainted bucket is not",
			shown: taintedStateJSON,
			want:  []string{`google_cloud_run_v2_service.app["console-uc1"]`, "google_cloud_run_v2_job.jobs"},
		},
		{
			name:  "a tainted resource of another kind alone names nothing",
			shown: bucketTaintedStateJSON,
		},
		{
			name:  "nothing tainted names nothing",
			shown: untaintedStateJSON,
		},
		{
			name:  "an empty state, before the first apply, names nothing",
			shown: emptyStateJSON,
		},
		{
			name:  "a child module's tainted service is named",
			shown: moduleTaintedStateJSON,
			want:  []string{`module.site.google_cloud_run_v2_service.app["uc1"]`},
		},
		{
			name:    "what is not the state's JSON is refused",
			shown:   "not json",
			wantErr: "json.Unmarshal(): tofu show -json of the state",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := taintedCloudRun([]byte(tt.shown))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("taintedCloudRun() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("taintedCloudRun() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("taintedCloudRun() (-want +got):\n%s", diff)
			}
		})
	}
}
