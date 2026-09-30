package deploy

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// jobsDoc is the job process's template job as the API answers it, the parts the step
// copies and the ones it leaves behind.
func jobsDoc() map[string]any {
	return map[string]any{
		keyName:      "projects/tst-project/locations/us-central1/jobs/harbor-jobs",
		"uid":        "u-1",
		"createTime": "2026-09-30T20:00:00Z",
		"labels":     map[string]any{"terraform": "true", "application": "harbor"},
		"template": map[string]any{
			"taskCount": float64(1),
			"template": map[string]any{
				"serviceAccount": "harbor-jobs@tst-project.iam.gserviceaccount.com",
				"containers":     []any{map[string]any{"image": "placeholder", "command": []any{"/jobs"}, "env": []any{map[string]any{"name": "APP_SERVICE_NAME", "value": "harbor-jobs"}}}},
				"timeout":        "1800s",
			},
		},
	}
}

func TestJobs(t *testing.T) {
	t.Parallel()

	const (
		template    = "projects/tst-project/locations/us-central1/jobs/harbor-jobs"
		jobName     = "projects/tst-project/locations/us-central1/jobs/harbor-jobs-v1-2-3"
		environment = "export SKIP_DEPLOY=\"\"\nexport JOBS_JOB=\"us-central1=harbor-jobs\"\nexport VERSION=\"v1.2.3\"\nexport IMAGE=\"reg/harbor\"\nexport IMAGE_DIGEST=\"sha256:abc\"\n"
		build       = `{"id": "b-1", "substitutions": {"_PROJECT": "tst-project", "_ENV": "tst", "COMMIT_SHA": "deadbeef", "REPO_NAME": "harbor", "_PR_NUMBER": "7"}}`
	)
	wantLabels := map[string]any{"terraform": "true", "application": "harbor", managedByLabel: managedByValue, commitLabel: "deadbeef", buildIDLabel: "b-1", sourceRepoLabel: "harbor", environmentLabel: "tst", prNumberLabel: "7", versionLabel: "v1-2-3"}
	tests := []struct {
		name        string
		env         string
		run         *fakeRun
		wantOut     []string
		wantCreated []string
		wantPatched bool
		wantErr     string
	}{
		{
			name:    "a torn-down environment does nothing",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			run:     newFakeRun(map[string]map[string]any{}),
			wantOut: []string{tornDown},
		},
		{
			name:        "the build's job is created from the template on this image, named after the version, and not run",
			env:         environment,
			run:         newFakeRun(map[string]map[string]any{template: jobsDoc()}),
			wantOut:     []string{"=== Making job [harbor-jobs-v1-2-3] from [harbor-jobs] on this image ===", "Job harbor-jobs-v1-2-3 created: the revision this build deploys starts it through the Cloud Run API; the pipeline does not run it."},
			wantCreated: []string{jobName},
		},
		{
			name:        "a version deployed before updates the job it made then",
			env:         environment,
			run:         newFakeRun(map[string]map[string]any{template: jobsDoc(), jobName: {keyName: jobName, "template": map[string]any{"template": map[string]any{"containers": []any{map[string]any{"image": "reg/harbor@sha256:older"}}}}}}),
			wantOut:     []string{"Job harbor-jobs-v1-2-3 updated: the revision this build deploys starts it"},
			wantPatched: true,
		},
		{
			name:    "a pipeline without a job process is refused",
			env:     strings.Replace(environment, "export JOBS_JOB=\"us-central1=harbor-jobs\"\n", "export JOBS_JOB=\"\"\n", 1),
			run:     newFakeRun(map[string]map[string]any{}),
			wantErr: "JOBS_JOB names no job: the stack's substitutions carry _JOBS_JOB when the application has a job process (cmd/jobs), which this step is for; render and apply the stack",
		},
		{
			name:    "a template the project lacks is refused",
			env:     environment,
			run:     newFakeRun(map[string]map[string]any{}),
			wantErr: "Cloud Run answered HTTP 404 to GET /v2/" + template,
		},
		{
			name:    "a build without a version is refused",
			env:     strings.Replace(environment, "export VERSION=\"v1.2.3\"\n", "", 1),
			run:     newFakeRun(map[string]map[string]any{template: jobsDoc()}),
			wantErr: "environment.sh names no version (VERSION): the resolve step writes it",
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
			if diff := cmp.Diff(tt.wantCreated, tt.run.created); diff != "" {
				t.Errorf("created mismatch (-want +got):\n%s", diff)
			}
			if tt.wantCreated == nil && !tt.wantPatched {
				return
			}
			job := tt.run.resources[jobName]
			if tt.wantPatched {
				job = tt.run.patched[jobName]
			}
			if job == nil {
				t.Fatalf("no job at %s (created %v, patched %v)", jobName, tt.run.created, tt.run.patched[jobName] != nil)
			}
			task, _ := field(job, "template.template").(map[string]any)
			container, err := firstContainer(task)
			if err != nil || container["image"] != "reg/harbor@sha256:abc" {
				t.Errorf("job image = %v, want reg/harbor@sha256:abc", container["image"])
			}
			if diff := cmp.Diff(wantLabels, job["labels"]); diff != "" {
				t.Errorf("labels mismatch (-want +got):\n%s", diff)
			}
			for _, key := range []string{"uid", "createTime"} {
				if _, ok := job[key]; ok {
					t.Errorf("the copy carries the template's %s", key)
				}
			}
			if text(job, "template.template.serviceAccount") != "harbor-jobs@tst-project.iam.gserviceaccount.com" {
				t.Errorf("the copy lost the template's identity: %v", job["template"])
			}
			if template := tt.run.resources[template]; text(template, "template.template.containers.0.image") == "reg/harbor@sha256:abc" {
				t.Error("the template job took the image")
			}
		})
	}
}
