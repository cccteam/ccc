package deploy

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// jobsDoc is a job process's Cloud Run job as the API answers it, the parts the step
// touches.
func jobsDoc() map[string]any {
	return map[string]any{
		keyName:  "projects/tst-project/locations/us-central1/jobs/harbor-jobs",
		"labels": map[string]any{"terraform": "true"},
		"template": map[string]any{
			"taskCount": float64(1),
			"template": map[string]any{
				"containers": []any{map[string]any{"image": "reg/harbor@sha256:old", "command": []any{"/jobs"}}},
				"timeout":    "1800s",
			},
		},
	}
}

func TestJobs(t *testing.T) {
	t.Parallel()

	const (
		jobName     = "projects/tst-project/locations/us-central1/jobs/harbor-jobs"
		environment = "export SKIP_DEPLOY=\"\"\nexport JOBS_JOB=\"us-central1=harbor-jobs\"\nexport IMAGE=\"reg/harbor\"\nexport IMAGE_DIGEST=\"sha256:abc\"\n"
		build       = `{"id": "b-1", "substitutions": {"_PROJECT": "tst-project", "_ENV": "tst", "COMMIT_SHA": "deadbeef", "REPO_NAME": "harbor", "_PR_NUMBER": "7"}}`
	)
	tests := []struct {
		name       string
		env        string
		run        *fakeRun
		wantOut    []string
		wantImage  string
		wantLabels map[string]any
		wantErr    string
	}{
		{
			name:    "a torn-down environment does nothing",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			run:     newFakeRun(map[string]map[string]any{}),
			wantOut: []string{tornDown},
		},
		{
			name:       "the job takes the image and the labels and is not run",
			env:        environment,
			run:        newFakeRun(map[string]map[string]any{jobName: jobsDoc()}),
			wantOut:    []string{"=== Updating job [harbor-jobs] in [us-central1] to this image ===", "Job harbor-jobs updated: its next run is on this build's image; the pipeline does not run it."},
			wantImage:  "reg/harbor@sha256:abc",
			wantLabels: map[string]any{"terraform": "true", managedByLabel: managedByValue, commitLabel: "deadbeef", buildIDLabel: "b-1", sourceRepoLabel: "harbor", environmentLabel: "tst", prNumberLabel: "7"},
		},
		{
			name:    "a pipeline without a job process is refused",
			env:     strings.Replace(environment, "export JOBS_JOB=\"us-central1=harbor-jobs\"\n", "export JOBS_JOB=\"\"\n", 1),
			run:     newFakeRun(map[string]map[string]any{}),
			wantErr: "JOBS_JOB names no job: the stack's substitutions carry _JOBS_JOB when the application has a job process (cmd/jobs), which this step is for; render and apply the stack",
		},
		{
			name:    "a job the project lacks is refused",
			env:     environment,
			run:     newFakeRun(map[string]map[string]any{}),
			wantErr: "Cloud Run answered HTTP 404 to GET /v2/" + jobName,
		},
		{
			name:    "a workspace without the digest is refused",
			env:     strings.Replace(environment, "export IMAGE_DIGEST=\"sha256:abc\"\n", "", 1),
			run:     newFakeRun(map[string]map[string]any{}),
			wantErr: "environment.sh names no image digest (IMAGE, IMAGE_DIGEST): the image build writes it",
		},
		{
			name:    "a job that is not region=name is refused",
			env:     strings.Replace(environment, "us-central1=harbor-jobs", "harbor-jobs", 1),
			run:     newFakeRun(map[string]map[string]any{}),
			wantErr: `JOBS_JOB "harbor-jobs" is not region=name`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: build})
			var out strings.Builder
			err := Jobs(t.Context(), &Clients{Run: tt.run.open}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Jobs() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out.String())
				}

				return
			}
			if err != nil {
				t.Fatalf("Jobs() error = %v; output:\n%s", err, out.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if len(tt.run.ran) != 0 {
				t.Errorf("the step ran a job: %v", tt.run.ran)
			}
			if tt.wantImage == "" {
				return
			}
			patched := tt.run.patched[jobName]
			task, _ := field(patched, "template.template").(map[string]any)
			container, err := firstContainer(task)
			if err != nil || container["image"] != tt.wantImage {
				t.Errorf("job image = %v, want %s (patched %v)", container["image"], tt.wantImage, patched != nil)
			}
			if diff := cmp.Diff(tt.wantLabels, patched["labels"]); diff != "" {
				t.Errorf("labels mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
