package deploy

import (
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// sweepFixture is an application's services in two regions with their revisions, its jobs
// and their executions, as the API answers them: the template jobs; the job process's jobs
// of builds v1 to v8 (v1 with an execution still running, v2 carried by a revision in the
// second region alone, v3 to v6 by revisions in both, v7 by none, v8 made minutes ago) and
// a stale one; migrate jobs a run left behind (v5 with an execution running, v6 old, v7
// made minutes ago); and a job of another application's beside them.
func sweepFixture(now time.Time) map[string]map[string]any {
	const (
		uc1  = "projects/tst-project/locations/us-central1/services/harbor-app"
		uw3  = "projects/tst-project/locations/us-west3/services/harbor-app"
		jobs = "projects/tst-project/locations/us-central1/jobs/"
	)
	old := "2026-01-01T00:00:00Z"
	young := now.Add(-10 * time.Minute).Format(time.RFC3339Nano)
	resources := map[string]map[string]any{
		uc1:                        {keyName: uc1},
		uw3:                        {keyName: uw3},
		jobs + "harbor-jobs":       {keyName: jobs + "harbor-jobs", "labels": map[string]any{"terraform": "true"}, "createTime": old},
		jobs + "harbor-migrate":    {keyName: jobs + "harbor-migrate", "labels": map[string]any{"terraform": "true"}, "createTime": old},
		jobs + "other-jobs-v1":     {keyName: jobs + "other-jobs-v1", "labels": map[string]any{versionLabel: "v1"}, "createTime": old},
		jobs + "harbor-jobs-stale": {keyName: jobs + "harbor-jobs-stale", "labels": map[string]any{versionLabel: "stale"}, "createTime": old},
		jobs + "harbor-jobs-v8":    {keyName: jobs + "harbor-jobs-v8", "labels": map[string]any{versionLabel: "v8"}, "createTime": young},
		jobs + "harbor-migrate-v5": {keyName: jobs + "harbor-migrate-v5", "labels": map[string]any{versionLabel: "v5"}, "createTime": old},
		jobs + "harbor-migrate-v6": {keyName: jobs + "harbor-migrate-v6", "labels": map[string]any{versionLabel: "v6"}, "createTime": old},
		jobs + "harbor-migrate-v7": {keyName: jobs + "harbor-migrate-v7", "labels": map[string]any{versionLabel: "v7"}, "createTime": young},
	}
	for i := 1; i <= 7; i++ {
		key := "v" + string(rune('0'+i))
		resources[jobs+"harbor-jobs-"+key] = map[string]any{keyName: jobs + "harbor-jobs-" + key, "labels": map[string]any{versionLabel: key}, "createTime": old}
		if i >= 3 && i <= 6 {
			revision := uc1 + "/revisions/harbor-app-0000" + string(rune('0'+i)) + "-" + key
			resources[revision] = map[string]any{keyName: revision, "labels": map[string]any{versionLabel: key}}
		}
		if i >= 2 && i <= 6 {
			revision := uw3 + "/revisions/harbor-app-0000" + string(rune('0'+i)) + "-" + key
			resources[revision] = map[string]any{keyName: revision, "labels": map[string]any{versionLabel: key}}
		}
	}
	resources[jobs+"harbor-jobs-v1/executions/harbor-jobs-v1-run"] = map[string]any{keyName: jobs + "harbor-jobs-v1/executions/harbor-jobs-v1-run"}
	resources[jobs+"harbor-jobs-v7/executions/harbor-jobs-v7-done"] = map[string]any{keyName: jobs + "harbor-jobs-v7/executions/harbor-jobs-v7-done", "completionTime": "2026-09-30T20:02:30Z"}
	resources[jobs+"harbor-migrate-v5/executions/harbor-migrate-v5-run"] = map[string]any{keyName: jobs + "harbor-migrate-v5/executions/harbor-migrate-v5-run"}

	return resources
}

func TestSweepJobs(t *testing.T) {
	t.Parallel()

	const (
		environment = "export SKIP_DEPLOY=\"\"\nexport MIGRATE_JOB=\"us-central1=harbor-migrate\"\nexport JOBS_JOB=\"us-central1=harbor-jobs\"\nexport SERVICES=\"us-central1=harbor-app,us-west3=harbor-app\"\nexport VERSION=\"v9\"\n"
		build       = `{"id": "b-1", "substitutions": {"_PROJECT": "tst-project", "_ENV": "tst", "COMMIT_SHA": "deadbeef", "REPO_NAME": "harbor"}}`
		jobs        = "projects/tst-project/locations/us-central1/jobs/"
	)
	now := time.Now()
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
			name: "a job stays while a revision in any region carries its version, an execution runs or it is young; the rest and the migrate leftovers go; templates and other jobs are left alone",
			env:  environment,
			run:  newFakeRun(sweepFixture(now)),
			wantOut: []string{
				"Job harbor-migrate-v5 stays: execution harbor-migrate-v5-run is still running.",
				"Job harbor-migrate-v6 deleted: a migrate job of a run that did not finish.",
				"Job harbor-migrate-v7 stays: made 10m0s ago, its build may still be running.",
				"Job harbor-jobs-stale deleted: no revision carries its version.",
				"Job harbor-jobs-v1 stays: execution harbor-jobs-v1-run is still running.",
				"Job harbor-jobs-v2 stays: revision harbor-app-00002-v2 (us-west3) carries its version.",
				"Job harbor-jobs-v6 stays: revision harbor-app-00006-v6 (us-central1) carries its version.",
				"Job harbor-jobs-v7 deleted: no revision carries its version.",
				"Job harbor-jobs-v8 stays: made 10m0s ago, its build may still be running.",
				"Swept the builds' jobs: 3 deleted.",
			},
			wantDeleted: []string{jobs + "harbor-migrate-v6", jobs + "harbor-jobs-stale", jobs + "harbor-jobs-v7"},
		},
		{
			name:        "an application without a job process sweeps the migrate leftovers alone",
			env:         strings.Replace(environment, "export JOBS_JOB=\"us-central1=harbor-jobs\"\n", "export JOBS_JOB=\"\"\n", 1),
			run:         newFakeRun(sweepFixture(now)),
			wantOut:     []string{"Job harbor-migrate-v6 deleted: a migrate job of a run that did not finish.", "Swept the builds' jobs: 1 deleted."},
			wantDeleted: []string{jobs + "harbor-migrate-v6"},
		},
		{
			name:    "a migrate job that is not region=name is refused",
			env:     strings.Replace(environment, "us-central1=harbor-migrate", "harbor-migrate", 1),
			run:     newFakeRun(map[string]map[string]any{}),
			wantErr: `MIGRATE_JOB "harbor-migrate" is not region=name`,
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
		jobs + "harbor-pr7-jobs":            {keyName: jobs + "harbor-pr7-jobs", "labels": map[string]any{applicationLabel: "harbor", pullRequestLabel: "7"}},
		jobs + "harbor-pr7-jobs-pr7-abc":    {keyName: jobs + "harbor-pr7-jobs-pr7-abc", "labels": map[string]any{applicationLabel: "harbor", pullRequestLabel: "7", versionLabel: "pr7-abc"}},
		jobs + "harbor-pr7-jobs-pr7-def":    {keyName: jobs + "harbor-pr7-jobs-pr7-def", "labels": map[string]any{applicationLabel: "harbor", pullRequestLabel: "7", versionLabel: "pr7-def"}},
		jobs + "harbor-pr8-jobs-pr8-abc":    {keyName: jobs + "harbor-pr8-jobs-pr8-abc", "labels": map[string]any{applicationLabel: "harbor", pullRequestLabel: "8", versionLabel: "pr8-abc"}},
		jobs + "beacon-pr7-jobs-pr7-abc":    {keyName: jobs + "beacon-pr7-jobs-pr7-abc", "labels": map[string]any{applicationLabel: "beacon", pullRequestLabel: "7", versionLabel: "pr7-abc"}},
		jobs + "harbor-pr7-migrate":         {keyName: jobs + "harbor-pr7-migrate", "labels": map[string]any{applicationLabel: "harbor", pullRequestLabel: "7", versionLabel: "pr7-def"}},
		jobs + "harbor-pr7-migrate-pr7-abc": {keyName: jobs + "harbor-pr7-migrate-pr7-abc", "labels": map[string]any{applicationLabel: "harbor", pullRequestLabel: "7", versionLabel: "pr7-abc"}},
	})
	var out strings.Builder
	prefixes := []string{jobs + "harbor-pr7-migrate-", jobs + "harbor-pr7-jobs-"}
	if err := deletePullRequestJobs(t.Context(), run, "tst-project", "us-central1", prefixes, "harbor", "7", &out); err != nil {
		t.Fatalf("deletePullRequestJobs() error = %v", err)
	}
	want := []string{jobs + "harbor-pr7-jobs-pr7-abc", jobs + "harbor-pr7-jobs-pr7-def", jobs + "harbor-pr7-migrate-pr7-abc"}
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
