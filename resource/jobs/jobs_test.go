package jobs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testJob = "projects/p/locations/us-central1/jobs/imp-tst-uc1-harbor-jobs-1-2-3"

// TestCloudRunStart: a start posts a run of the job with the arguments as the
// container's override and answers the execution the operation names; the API's
// refusals and an operation carrying an error or no execution are errors.
func TestCloudRunStart(t *testing.T) {
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
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.answer)
			}))
			defer srv.Close()

			c := &CloudRun{job: testJob, http: srv.Client(), base: srv.URL}
			got, err := c.Start(t.Context(), tt.args...)
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
			var gotJSON, wantJSON any
			if err := json.Unmarshal([]byte(gotBody), &gotJSON); err != nil {
				t.Fatalf("the request body is not JSON: %v\n%s", err, gotBody)
			}
			_ = json.Unmarshal([]byte(tt.wantBody), &wantJSON)
			if gotBody != tt.wantBody {
				t.Errorf("request body = %s, want %s", gotBody, tt.wantBody)
			}
		})
	}
}

// TestNewCloudRun refuses a name that is not a job's.
func TestNewCloudRun(t *testing.T) {
	t.Parallel()

	for _, job := range []string{"", "harbor-jobs", "projects/p/locations/l/services/s", "projects/p/jobs/j"} {
		if _, err := NewCloudRun(t.Context(), job); err == nil || !strings.Contains(err.Error(), "is not a Cloud Run job's resource name") {
			t.Errorf("NewCloudRun(%q) error = %v, want the name refused", job, err)
		}
	}
}

// TestFromEnvironment: the variable unset is None, whose start says no job is
// configured; a name that is not a job's is refused.
func TestFromEnvironment(t *testing.T) {
	t.Setenv(JobVariable, "")
	starter, err := FromEnvironment(t.Context())
	if err != nil {
		t.Fatalf("FromEnvironment() error = %v", err)
	}
	if _, ok := starter.(None); !ok {
		t.Fatalf("FromEnvironment() = %T, want None", starter)
	}
	if _, err := starter.Start(context.Background(), "cleanup-files"); err == nil || !strings.Contains(err.Error(), "no job process is configured") {
		t.Errorf("None.Start() error = %v, want the refusal", err)
	}

	t.Setenv(JobVariable, "not-a-job")
	if _, err := FromEnvironment(t.Context()); err == nil || !strings.Contains(err.Error(), "is not a Cloud Run job's resource name") {
		t.Errorf("FromEnvironment() with a bad name error = %v, want the name refused", err)
	}
}

// TestFake records the starts in order and answers its error when set.
func TestFake(t *testing.T) {
	t.Parallel()

	f := NewFake()
	first, err := f.Start(t.Context(), "cleanup-files", "-dry-run")
	if err != nil || first != "projects/p/locations/l/jobs/j/executions/j-1" {
		t.Fatalf("Start() = %q, %v", first, err)
	}
	if _, err := f.Start(t.Context(), "cleanup-files"); err != nil {
		t.Fatal(err)
	}
	if got := f.Started(); len(got) != 2 || strings.Join(got[0], " ") != "cleanup-files -dry-run" || strings.Join(got[1], " ") != "cleanup-files" {
		t.Errorf("Started() = %v", got)
	}
}
