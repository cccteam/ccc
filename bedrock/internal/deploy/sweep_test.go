package deploy

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

func TestSweep(t *testing.T) {
	t.Parallel()

	service := func(name, app, pr string) map[string]any {
		labels := map[string]any{applicationLabel: app}
		if pr != "" {
			labels[pullRequestLabel] = pr
		}

		return map[string]any{"name": name, "labels": labels}
	}
	// job is a job document as the API lists it, its name the resource name.
	job := func(name, app, pr, key string) map[string]any {
		labels := map[string]any{applicationLabel: app, pullRequestLabel: pr}
		if key != "" {
			labels[versionLabel] = key
		}

		return map[string]any{"name": "projects/p/locations/us-central1/jobs/" + name, "labels": labels}
	}
	// The pull request's stack names its own templates in its substitutions output; the
	// trigger's substitutions name the environment's, which no copy of a pull request's is
	// under.
	const output = `{"_SERVICES": "us-central1=quill-pr7", "_MIGRATE_JOB": "us-central1=quill-pr7-migrate", "_JOBS_JOB": "us-central1=quill-pr7-jobs", "_HOSTNAME": "quill-pr7.example.dev"}`
	tests := []struct {
		name        string
		services    map[string]map[string]any
		states      map[int]string
		output      string
		wantOut     []string
		wantTofu    []string
		wantDeleted []string
		wantErr     string
		badRequest  bool
	}{
		{
			name:     "no pull-request environment, nothing to do",
			services: map[string]map[string]any{"projects/p/locations/us-central1/services/quill-app": service("quill-app", "quill", "")},
			wantOut:  []string{"No pull-request environment in tst."},
		},
		{
			name: "a closed pull request's stack is destroyed; an open one's and one GitHub cannot answer for stay",
			services: map[string]map[string]any{
				"projects/p/locations/us-central1/services/quill-pr7":  service("quill-pr7", "quill", "7"),
				"projects/p/locations/us-west3/services/quill-pr7":     service("quill-pr7", "quill", "7"),
				"projects/p/locations/us-central1/services/quill-pr12": service("quill-pr12", "quill", "12"),
				"projects/p/locations/us-central1/services/quill-pr9":  service("quill-pr9", "quill", "9"),
				"projects/p/locations/us-central1/services/other-pr3":  service("other-pr3", "other", "3"),
				// The pull request's templates (the stack's, destroyed with it), its builds'
				// copies (deleted here), and an open pull request's copy (stays).
				"projects/p/locations/us-central1/jobs/quill-pr7-migrate":             job("quill-pr7-migrate", "quill", "7", ""),
				"projects/p/locations/us-central1/jobs/quill-pr7-jobs":                job("quill-pr7-jobs", "quill", "7", ""),
				"projects/p/locations/us-central1/jobs/quill-pr7-migrate-pr7-abc1234": job("quill-pr7-migrate-pr7-abc1234", "quill", "7", "pr7-abc1234"),
				"projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-abc1234":    job("quill-pr7-jobs-pr7-abc1234", "quill", "7", "pr7-abc1234"),
				"projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-def5678":    job("quill-pr7-jobs-pr7-def5678", "quill", "7", "pr7-def5678"),
				"projects/p/locations/us-central1/jobs/quill-pr12-jobs-pr12-abc1234":  job("quill-pr12-jobs-pr12-abc1234", "quill", "12", "pr12-abc1234"),
				"projects/p/locations/us-central1/jobs/quill-migrate":                 job("quill-migrate", "quill", "", ""),
			},
			states:  map[int]string{7: "closed", 12: "open"},
			output:  output,
			wantOut: []string{"Pull requests with an environment: 7 9 12", "Pull request 7 is closed: its environment goes.", "Job quill-pr7-migrate-pr7-abc1234 deleted with pull request 7's environment.", "Job quill-pr7-jobs-pr7-abc1234 deleted with pull request 7's environment.", "Job quill-pr7-jobs-pr7-def5678 deleted with pull request 7's environment.", "Notice: GitHub did not answer for pull request 9", "Pull request 12 is open: its environment stays."},
			wantTofu: []string{
				"tofu init -input=false -no-color -backend-config=prefix=3-app/quill/tst/pr7 -backend-config=impersonate_service_account=quill-apply@p.iam.gserviceaccount.com",
				"tofu output -json substitutions",
				"tofu destroy -auto-approve -input=false -no-color -var environment=tst -var pull_request=7",
			},
			wantDeleted: []string{"projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-abc1234", "projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-def5678", "projects/p/locations/us-central1/jobs/quill-pr7-migrate-pr7-abc1234"},
		},
		{
			name: "a closed pull request whose stack has no output is destroyed, its jobs left alone",
			services: map[string]map[string]any{
				"projects/p/locations/us-central1/services/quill-pr7":              service("quill-pr7", "quill", "7"),
				"projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-abc1234": job("quill-pr7-jobs-pr7-abc1234", "quill", "7", "pr7-abc1234"),
			},
			states:  map[int]string{7: "closed"},
			wantOut: []string{"Pull request 7 is closed: its environment goes.", "Notice: pull request 7's stack names no jobs (", "none of its builds' jobs is deleted."},
			wantTofu: []string{
				"tofu init -input=false -no-color -backend-config=prefix=3-app/quill/tst/pr7 -backend-config=impersonate_service_account=quill-apply@p.iam.gserviceaccount.com",
				"tofu output -json substitutions",
				"tofu destroy -auto-approve -input=false -no-color -var environment=tst -var pull_request=7",
			},
		},
		{name: "a build it cannot read is refused", badRequest: true, wantErr: "BUILD_ID is not set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subs := map[string]string{
				appSub: "quill", envSub: tstEnvironment, "_PROJECT": "p", "_SERVICES": "us-central1=quill-app,us-west3=quill-app", applyIdentitySub: "quill-apply@p.iam.gserviceaccount.com",
				"_REPO_CONNECTION_NAME": "conn", "_REPO_NAME": "quill", "_REPO_FULL_NAME": "acme/quill",
			}
			builds := &fakeBuilds{build: buildFor(t, subs), token: "test-token"}
			repo := &githubtest.Repo{PullStates: tt.states}
			_, gh := githubStandIn(t, repo)
			run := &fakeRunner{outputs: map[string]string{"tofu output": tt.output}}
			req := &SweepRequest{BuildID: "b-1", Project: "p", Location: "us-central1"}
			if tt.badRequest {
				req.BuildID = ""
			}
			cloudRun := newFakeRun(tt.services)
			var out strings.Builder
			err := Sweep(t.Context(), &Clients{Builds: builds.open, Run: cloudRun.open, GitHub: gh, Exec: run}, workspaceFiles(t, nil), req, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Sweep() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Sweep() error = %v\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			if diff := cmp.Diff(tt.wantTofu, run.lines(), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("tofu (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantDeleted, cloudRun.deleted, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("deleted (-want +got):\n%s\noutput:\n%s", diff, out.String())
			}
		})
	}
}
