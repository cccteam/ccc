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
	const (
		output    = `{"substitutions": {"value": {"_SERVICES": "us-central1=quill-pr7", "_MIGRATE_ENV": "{}", "_JOBS_JOB": "us-central1=quill-pr7-jobs", "_HOSTNAME": "quill-pr7.example.dev"}}}`
		initLine  = "tofu init -input=false -no-color -backend-config=prefix=3-app/quill/tst/pr7"
		stateList = "tofu state list"
		readOut   = "tofu output -json"
		destroy   = "tofu destroy -auto-approve -input=false -no-color -var environment=tst -var pull_request=7"
		// stateObject is the pull request's state object in the placement's bucket (b).
		stateObject = "gs://b/3-app/quill/tst/pr7/default.tfstate"
	)
	tests := []struct {
		name     string
		services map[string]map[string]any
		states   map[int]string
		output   string
		// empty says the pull request's state holds no resources.
		empty bool
		// another says the build runs as another identity than the apply identity.
		another     bool
		wantOut     []string
		wantTofu    []string
		wantDeleted []string
		// wantState says the state object is still in the bucket after the sweep.
		wantState  bool
		wantErr    string
		badRequest bool
	}{
		{
			name:      "no pull-request environment, nothing to do",
			services:  map[string]map[string]any{"projects/p/locations/us-central1/services/quill-app": service("quill-app", "quill", "")},
			wantOut:   []string{"No pull-request environment in tst."},
			wantState: true,
		},
		{
			name: "a closed pull request's stack is destroyed as the build's own identity; an open one's and one GitHub cannot answer for stay",
			services: map[string]map[string]any{
				"projects/p/locations/us-central1/services/quill-pr7":  service("quill-pr7", "quill", "7"),
				"projects/p/locations/us-west3/services/quill-pr7":     service("quill-pr7", "quill", "7"),
				"projects/p/locations/us-central1/services/quill-pr12": service("quill-pr12", "quill", "12"),
				"projects/p/locations/us-central1/services/quill-pr9":  service("quill-pr9", "quill", "9"),
				"projects/p/locations/us-central1/services/other-pr3":  service("other-pr3", "other", "3"),
				// The pull request's template (the stack's, destroyed with it), its builds'
				// copies (deleted here), an open pull request's copy (stays), and the
				// environment's own template (stays).
				"projects/p/locations/us-central1/jobs/quill-pr7-jobs":               job("quill-pr7-jobs", "quill", "7", ""),
				"projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-abc1234":   job("quill-pr7-jobs-pr7-abc1234", "quill", "7", "pr7-abc1234"),
				"projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-def5678":   job("quill-pr7-jobs-pr7-def5678", "quill", "7", "pr7-def5678"),
				"projects/p/locations/us-central1/jobs/quill-pr12-jobs-pr12-abc1234": job("quill-pr12-jobs-pr12-abc1234", "quill", "12", "pr12-abc1234"),
				"projects/p/locations/us-central1/jobs/quill-jobs":                   job("quill-jobs", "quill", "", ""),
			},
			states:      map[int]string{7: "closed", 12: "open"},
			output:      output,
			wantOut:     []string{"Pull requests with an environment: 7 9 12", "Pull request 7 is closed: its environment goes.", "=== Pull request 7: stack at 3-app/quill/tst/pr7 as quill-apply@p.iam.gserviceaccount.com ===", "Job quill-pr7-jobs-pr7-abc1234 deleted with pull request 7's environment.", "Job quill-pr7-jobs-pr7-def5678 deleted with pull request 7's environment.", "Pull request 7's state object " + stateObject + " is deleted: nothing of its stack is kept.", "Notice: GitHub did not answer for pull request 9", "Pull request 12 is open: its environment stays."},
			wantTofu:    []string{initLine, stateList, readOut, destroy},
			wantDeleted: []string{"projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-abc1234", "projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-def5678"},
		},
		{
			name: "a build run as another identity acts as the apply identity",
			services: map[string]map[string]any{
				"projects/p/locations/us-central1/services/quill-pr7": service("quill-pr7", "quill", "7"),
			},
			states:   map[int]string{7: "closed"},
			output:   output,
			another:  true,
			wantTofu: []string{initLine + " -backend-config=impersonate_service_account=quill-apply@p.iam.gserviceaccount.com", stateList, readOut, destroy},
		},
		{
			name: "a closed pull request whose stack has no output is destroyed, its jobs left alone",
			services: map[string]map[string]any{
				"projects/p/locations/us-central1/services/quill-pr7":              service("quill-pr7", "quill", "7"),
				"projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-abc1234": job("quill-pr7-jobs-pr7-abc1234", "quill", "7", "pr7-abc1234"),
			},
			states:   map[int]string{7: "closed"},
			output:   "{}",
			wantOut:  []string{"Pull request 7 is closed: its environment goes.", "Notice: pull request 7's stack names no jobs (the stack's state has no substitutions output: never applied, or applied by a bedrock before the output); none of its builds' jobs is deleted."},
			wantTofu: []string{initLine, stateList, readOut, destroy},
		},
		{
			name: "a closed pull request whose state holds nothing is skipped before any output is read, and its state object goes",
			services: map[string]map[string]any{
				"projects/p/locations/us-central1/services/quill-pr7": service("quill-pr7", "quill", "7"),
			},
			states:   map[int]string{7: "closed"},
			empty:    true,
			wantOut:  []string{"Pull request 7's stack holds nothing (never applied, or destroyed already); skipped.", "Pull request 7's state object " + stateObject + " is deleted"},
			wantTofu: []string{initLine, stateList},
		},
		{
			name: "an open pull request's state object stays",
			services: map[string]map[string]any{
				"projects/p/locations/us-central1/services/quill-pr7": service("quill-pr7", "quill", "7"),
			},
			states:    map[int]string{7: "open"},
			wantOut:   []string{"Pull request 7 is open: its environment stays."},
			wantState: true,
		},
		{name: "a build it cannot read is refused", badRequest: true, wantErr: "BUILD_ID is not set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subs := map[string]string{
				appSub: "quill", envSub: tstEnvironment, "_PROJECT": "p", "_SERVICES": "us-central1=quill-app,us-west3=quill-app", applyIdentitySub: applyIdentity,
				"_REPO_CONNECTION_NAME": "conn", "_REPO_NAME": "quill", sweepRepoSub: "acme/quill",
			}
			identity := applyIdentityID
			if tt.another {
				identity = ""
			}
			builds := &fakeBuilds{build: jobBuild(t, subs, identity, nil), token: "test-token"}
			repo := &githubtest.Repo{PullStates: tt.states}
			_, gh := githubStandIn(t, repo)
			listed := "google_cloud_run_v2_service.app[0]\n"
			if tt.empty {
				listed = ""
			}
			run := &fakeRunner{outputs: map[string]string{"tofu output": tt.output, stateList: listed}}
			req := &BuildRequest{BuildID: "b-1", Project: "p", Location: "us-central1"}
			if tt.badRequest {
				req.BuildID = ""
			}
			cloudRun := newFakeRun(tt.services)
			store := &memoryStore{objects: map[string]string{stateObject: "{}"}}
			w := workspaceFiles(t, map[string]string{stackDir + "/" + placementFile: testPlacement("")})
			var out strings.Builder
			err := Sweep(t.Context(), &Clients{Builds: builds.open, Run: cloudRun.open, GitHub: gh, Exec: run, Storage: store.open}, w, req, &out)
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
			if _, ok := store.objects[stateObject]; ok != tt.wantState {
				t.Errorf("the state object stays = %v, want %v; output:\n%s", ok, tt.wantState, out.String())
			}
		})
	}
}
