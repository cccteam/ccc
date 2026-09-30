package deploy

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// sweepFixture is a service with its revisions, its jobs and their executions, as the
// API answers them: the template job, the jobs of six builds (v1 to v6, v6 the newest
// revision, v5 serving, v1 with an execution still running), and a job of another
// application's beside them.
func sweepFixture() map[string]map[string]any {
	const (
		service = "projects/tst-project/locations/us-central1/services/harbor-app"
		jobs    = "projects/tst-project/locations/us-central1/jobs/"
	)
	resources := map[string]map[string]any{
		service: {
			keyName: service,
			"trafficStatuses": []any{
				map[string]any{keyType: targetRevision, keyRevision: "harbor-app-00005-v5", keyPercent: float64(100)},
				map[string]any{keyType: targetRevision, keyRevision: "harbor-app-00006-v6", keyPercent: float64(0), keyTag: "next"},
			},
		},
		jobs + "harbor-jobs":   {keyName: jobs + "harbor-jobs", "labels": map[string]any{"terraform": "true"}},
		jobs + "other-jobs-v1": {keyName: jobs + "other-jobs-v1", "labels": map[string]any{versionLabel: "v1"}},
	}
	for i := 1; i <= 6; i++ {
		key := "v" + string(rune('0'+i))
		revision := service + "/revisions/harbor-app-0000" + string(rune('0'+i)) + "-" + key
		resources[revision] = map[string]any{keyName: revision, "createTime": "2026-09-30T20:0" + string(rune('0'+i)) + ":00Z", "labels": map[string]any{versionLabel: key}}
		resources[jobs+"harbor-jobs-"+key] = map[string]any{keyName: jobs + "harbor-jobs-" + key, "labels": map[string]any{versionLabel: key}}
	}
	resources[jobs+"harbor-jobs-v1/executions/harbor-jobs-v1-run"] = map[string]any{keyName: jobs + "harbor-jobs-v1/executions/harbor-jobs-v1-run"}
	resources[jobs+"harbor-jobs-v2/executions/harbor-jobs-v2-done"] = map[string]any{keyName: jobs + "harbor-jobs-v2/executions/harbor-jobs-v2-done", "completionTime": "2026-09-30T20:02:30Z"}
	resources[jobs+"harbor-jobs-stale"] = map[string]any{keyName: jobs + "harbor-jobs-stale", "labels": map[string]any{versionLabel: "stale"}}

	return resources
}

func TestSweepJobs(t *testing.T) {
	t.Parallel()

	const (
		environment = "export SKIP_DEPLOY=\"\"\nexport JOBS_JOB=\"us-central1=harbor-jobs\"\nexport SERVICES=\"us-central1=harbor-app,us-west3=harbor-app\"\nexport VERSION=\"v7\"\n"
		build       = `{"id": "b-1", "substitutions": {"_PROJECT": "tst-project", "_ENV": "tst", "COMMIT_SHA": "deadbeef", "REPO_NAME": "harbor"}}`
		jobs        = "projects/tst-project/locations/us-central1/jobs/"
	)
	tests := []struct {
		name        string
		env         string
		run         *fakeRun
		wantOut     []string
		wantDeleted []string
		wantErr     string
	}{
		{
			name:    "a torn-down environment does nothing",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			run:     newFakeRun(map[string]map[string]any{}),
			wantOut: []string{tornDown},
		},
		{
			name: "the serving, the five newest and the running stay; the rest go; the template and other jobs are left alone",
			env:  environment,
			run:  newFakeRun(sweepFixture()),
			wantOut: []string{
				"Job harbor-jobs-v6 stays: revision harbor-app-00006-v6 runs it.",
				"Job harbor-jobs-v5 stays: revision harbor-app-00005-v5 runs it.",
				"Job harbor-jobs-v2 stays: revision harbor-app-00002-v2 runs it.",
				"Job harbor-jobs-v1 stays: execution harbor-jobs-v1-run is still running.",
				"Job harbor-jobs-stale deleted: no revision runs it.",
				"Swept the jobs of harbor-jobs: 1 deleted.",
			},
			wantDeleted: []string{jobs + "harbor-jobs-stale"},
		},
		{
			name:    "a pipeline without a job process is refused",
			env:     strings.Replace(environment, "export JOBS_JOB=\"us-central1=harbor-jobs\"\n", "export JOBS_JOB=\"\"\n", 1),
			run:     newFakeRun(map[string]map[string]any{}),
			wantErr: "JOBS_JOB names no job",
		},
		{
			name:    "a job process in a region without the service is refused",
			env:     strings.Replace(environment, "us-central1=harbor-app,", "", 1),
			run:     newFakeRun(sweepFixture()),
			wantErr: "SERVICES names no service in us-central1, the job process's region",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: build})
			var out strings.Builder
			err := SweepJobs(t.Context(), &Clients{Run: tt.run.open}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("SweepJobs() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out.String())
				}

				return
			}
			if err != nil {
				t.Fatalf("SweepJobs() error = %v; output:\n%s", err, out.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if diff := cmp.Diff(tt.wantDeleted, tt.run.deleted); diff != "" {
				t.Errorf("deleted mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDeletePullRequestJobs(t *testing.T) {
	t.Parallel()

	const jobs = "projects/tst-project/locations/us-central1/jobs/"
	run := newFakeRun(map[string]map[string]any{
		jobs + "harbor-pr7-jobs":         {keyName: jobs + "harbor-pr7-jobs", "labels": map[string]any{applicationLabel: "harbor", pullRequestLabel: "7"}},
		jobs + "harbor-pr7-jobs-pr7-abc": {keyName: jobs + "harbor-pr7-jobs-pr7-abc", "labels": map[string]any{applicationLabel: "harbor", pullRequestLabel: "7", versionLabel: "pr7-abc"}},
		jobs + "harbor-pr7-jobs-pr7-def": {keyName: jobs + "harbor-pr7-jobs-pr7-def", "labels": map[string]any{applicationLabel: "harbor", pullRequestLabel: "7", versionLabel: "pr7-def"}},
		jobs + "harbor-pr8-jobs-pr8-abc": {keyName: jobs + "harbor-pr8-jobs-pr8-abc", "labels": map[string]any{applicationLabel: "harbor", pullRequestLabel: "8", versionLabel: "pr8-abc"}},
		jobs + "beacon-pr7-jobs-pr7-abc": {keyName: jobs + "beacon-pr7-jobs-pr7-abc", "labels": map[string]any{applicationLabel: "beacon", pullRequestLabel: "7", versionLabel: "pr7-abc"}},
		jobs + "harbor-pr7-migrate":      {keyName: jobs + "harbor-pr7-migrate", "labels": map[string]any{applicationLabel: "harbor", pullRequestLabel: "7", versionLabel: "pr7-def"}},
	})
	var out strings.Builder
	if err := deletePullRequestJobs(t.Context(), run, "tst-project", "us-central1", jobs+"harbor-pr7-jobs-", "harbor", "7", &out); err != nil {
		t.Fatalf("deletePullRequestJobs() error = %v", err)
	}
	want := []string{jobs + "harbor-pr7-jobs-pr7-abc", jobs + "harbor-pr7-jobs-pr7-def"}
	if diff := cmp.Diff(want, run.deleted); diff != "" {
		t.Errorf("deleted mismatch (-want +got):\n%s", diff)
	}
	if !strings.Contains(out.String(), "Job harbor-pr7-jobs-pr7-abc deleted with pull request 7's environment.") {
		t.Errorf("output lacks the deletion:\n%s", out.String())
	}
}

func TestVersionKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, version, want string
	}{
		{name: "a release tag", version: "v0.1.15", want: "v0-1-15"},
		{name: "a pull request's version", version: "pr39@abc1234", want: "pr39-abc1234"},
		{name: "a pre-release", version: "v1.0.0-rc.1+build.7", want: "v1-0-0-rc-1-build-7"},
		{name: "letter case and the ends", version: "-Hotfix_2--", want: "hotfix-2"},
		{name: "nothing to name", version: "@@", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := versionKey(tt.version); got != tt.want {
				t.Errorf("versionKey(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}
