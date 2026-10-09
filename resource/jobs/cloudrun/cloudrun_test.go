package cloudrun

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/cccteam/ccc/resource/internal/declarationtest"
	"github.com/cccteam/ccc/resource/jobs/cloudrun/declaration"
)

const testJob = "projects/p/locations/us-central1/jobs/imp-tst-uc1-harbor-jobs-1-2-3"

// TestStart: a start posts a run of the job with the arguments as the container's
// override and answers the execution the operation names; the API's refusals and an
// operation carrying an error or no execution are errors.
func TestStart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		status   int
		answer   string
		wantBody string
		want     string
		wantErr  string
	}{
		{
			name:     "the cleanup is started with its command as the argument",
			args:     []string{"cleanup-files"},
			status:   http.StatusOK,
			answer:   `{"name":"projects/p/locations/us-central1/operations/op-1","metadata":{"@type":"type.googleapis.com/google.cloud.run.v2.Execution","name":"` + testJob + `/executions/imp-tst-uc1-harbor-jobs-1-2-3-abcde"}}`,
			wantBody: `{"overrides":{"containerOverrides":[{"args":["cleanup-files"]}]}}`,
			want:     testJob + "/executions/imp-tst-uc1-harbor-jobs-1-2-3-abcde",
		},
		{
			name:     "no arguments: the job's own command, with no override",
			status:   http.StatusOK,
			answer:   `{"name":"op","metadata":{"name":"` + testJob + `/executions/e-1"}}`,
			wantBody: `{"overrides":{"containerOverrides":null}}`,
			want:     testJob + "/executions/e-1",
		},
		{
			name:    "a refused permission is the API's message",
			args:    []string{"cleanup-files"},
			status:  http.StatusForbidden,
			answer:  `{"error":{"code":403,"message":"Permission 'run.jobs.run' denied on resource"}}`,
			wantErr: "HTTP 403 to the start of " + testJob + ": Permission 'run.jobs.run' denied",
		},
		{
			name:    "an operation carrying an error",
			status:  http.StatusOK,
			answer:  `{"name":"op","error":{"message":"the job has no container"}}`,
			wantErr: "refused the start of " + testJob + ": the job has no container",
		},
		{
			name:    "an operation naming no execution",
			status:  http.StatusOK,
			answer:  `{"name":"op"}`,
			wantErr: `no execution name (operation "op")`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotPath, gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				gotPath, gotBody = r.Method+" "+r.URL.Path, strings.TrimSpace(string(body))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_ = json.NewEncoder(w).Encode(json.RawMessage(tt.answer))
			}))
			defer srv.Close()

			d := &Driver{job: testJob, http: srv.Client(), base: srv.URL}
			defer d.Close()
			got, err := d.Start(t.Context(), tt.args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Start() error = %v, want one containing %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("Start() = %q, want %q", got, tt.want)
			}
			if want := "POST /v2/" + testJob + ":run"; gotPath != want {
				t.Errorf("request = %q, want %q", gotPath, want)
			}
			var gotJSON any
			if err := json.Unmarshal([]byte(gotBody), &gotJSON); err != nil {
				t.Fatalf("the request body is not JSON: %v\n%s", err, gotBody)
			}
			if gotBody != tt.wantBody {
				t.Errorf("request body = %s, want %s", gotBody, tt.wantBody)
			}
		})
	}
}

// TestOpen: an empty template is a driver with no job, whose start says no job is
// configured and whose Close holds nothing; a template with no version beside it, and a
// template that is not a job's resource name, are refused before anything is opened.
func TestOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
		version  string
		wantNone bool
		wantErr  string
	}{
		{name: "no template: a driver that refuses", version: "v0.1.15", wantNone: true},
		{name: "a template with no version is refused", template: "projects/p/locations/l/jobs/harbor-jobs", wantErr: "names no key to pick this build's copy by"},
		{name: "a template with a version that is no name is refused", template: "projects/p/locations/l/jobs/harbor-jobs", version: "...", wantErr: "names no key to pick this build's copy by"},
		{name: "a template that is not a job's resource name is refused", template: "not-a-job", version: "v0.1.15", wantErr: "is not a Cloud Run job's resource name"},
		{name: "a service's resource name is refused", template: "projects/p/locations/l/services/s", version: "v0.1.15", wantErr: "is not a Cloud Run job's resource name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d, err := Open(t.Context(), Settings{Template: tt.template}, tt.version)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Open() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			defer d.Close()
			if (d.Job() == "") != tt.wantNone {
				t.Fatalf("Open() job = %q, want none %v", d.Job(), tt.wantNone)
			}
			if tt.wantNone {
				if _, err := d.Start(t.Context(), "cleanup-files"); err == nil || !strings.Contains(err.Error(), "no job process is configured") {
					t.Errorf("Start() with no job error = %v, want the refusal", err)
				}
			}
		})
	}
}

// TestJobOf names a build's job from the template and the version, by the version's key.
func TestJobOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
		version  string
		want     string
	}{
		{name: "a release", template: "projects/p/locations/us-central1/jobs/harbor-jobs", version: "v0.1.15", want: "projects/p/locations/us-central1/jobs/harbor-jobs-v0-1-15"},
		{name: "a pull request's build", template: "projects/p/locations/us-central1/jobs/harbor-pr39-jobs", version: "pr39@abc1234", want: "projects/p/locations/us-central1/jobs/harbor-pr39-jobs-pr39-abc1234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := JobOf(tt.template, tt.version); got != tt.want {
				t.Errorf("JobOf() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestVersionKey pins the key the pipeline names a build's job by.
func TestVersionKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "a release", version: "v0.1.15", want: "v0-1-15"},
		{name: "a pull request's build", version: "pr39@abc1234", want: "pr39-abc1234"},
		{name: "upper case and runs of punctuation", version: "V1.0.0-RC.1", want: "v1-0-0-rc-1"},
		{name: "punctuation at the ends", version: "-v1.0.0-", want: "v1-0-0"},
		{name: "nothing", version: "", want: ""},
		{name: "punctuation alone", version: "...", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := VersionKey(tt.version); got != tt.want {
				t.Errorf("VersionKey(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}

// TestSettingsDeclaration holds the declaration the declaration package publishes to the
// struct, as the cloud driver's test does.
func TestSettingsDeclaration(t *testing.T) {
	t.Parallel()

	declarationtest.Hold(t, reflect.TypeFor[Settings](), declaration.Settings())
}
