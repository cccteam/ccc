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
	tests := []struct {
		name       string
		services   map[string]map[string]any
		states     map[int]string
		wantOut    []string
		wantTofu   []string
		wantErr    string
		badRequest bool
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
			},
			states:  map[int]string{7: "closed", 12: "open"},
			wantOut: []string{"Pull requests with an environment: 7 9 12", "Pull request 7 is closed: its environment goes.", "Notice: GitHub did not answer for pull request 9", "Pull request 12 is open: its environment stays."},
			wantTofu: []string{
				"tofu init -input=false -no-color -backend-config=prefix=3-app/quill/tst/pr7 -backend-config=impersonate_service_account=quill-apply@p.iam.gserviceaccount.com",
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
			run := &fakeRunner{}
			req := &SweepRequest{BuildID: "b-1", Project: "p", Location: "us-central1"}
			if tt.badRequest {
				req.BuildID = ""
			}
			var out strings.Builder
			err := Sweep(t.Context(), &Clients{Builds: builds.open, Run: newFakeRun(tt.services).open, GitHub: gh, Exec: run}, workspaceFiles(t, nil), req, &out)
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
		})
	}
}
