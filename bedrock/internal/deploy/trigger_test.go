package deploy

import (
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"

	"github.com/cccteam/ccc/bedrock/internal/render"
)

func TestRequireBuild(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		id, project, location string
		wantErr               string
	}{
		{name: "a named build", id: "b-1", project: "p", location: "us-central1"},
		{name: "no id", project: "p", location: "us-central1", wantErr: "BUILD_ID is not set: the step passes the build's id, project and location to the command"},
		{name: "no project", id: "b-1", location: "us-central1", wantErr: "PROJECT_ID is not set"},
		{name: "no location", id: "b-1", project: "p", wantErr: "LOCATION is not set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := (&BuildRequest{BuildID: tt.id, Project: tt.project, Location: tt.location}).check()
			if tt.wantErr == "" && err != nil {
				t.Fatalf("check() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("check() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestRepositoryToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		subs      map[string]string
		mintErr   error
		want      string
		wantAsked string
		wantErr   string
	}{
		{
			name:      "the token is minted from the connection and the repository's link",
			subs:      map[string]string{"_REPO_CONNECTION_NAME": "conn", "_REPO_NAME": "quill"},
			want:      "tok",
			wantAsked: "projects/p/locations/us-central1/connections/conn/repositories/quill",
		},
		{
			name:    "a build without a connection is one no trigger started, and is refused",
			subs:    map[string]string{"_REPO_NAME": "quill"},
			wantErr: `_REPO_CONNECTION_NAME="" _REPO_NAME="quill": every build starts from a trigger`,
		},
		{
			name:    "a build without the repository's link is refused the same way",
			subs:    map[string]string{"_REPO_CONNECTION_NAME": "conn"},
			wantErr: `_REPO_CONNECTION_NAME="conn" _REPO_NAME="": every build starts from a trigger`,
		},
		{
			name:    "a failed mint says which connection",
			subs:    map[string]string{"_REPO_CONNECTION_NAME": "conn", "_REPO_NAME": "quill"},
			mintErr: errors.New("denied"),
			wantErr: "could not mint a GitHub token from connection conn",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builds := &fakeBuilds{token: "tok", mintErr: tt.mintErr}
			got, err := repositoryToken(t.Context(), builds, tt.subs, "p", "us-central1")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("repositoryToken() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("repositoryToken() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("repositoryToken() = %q, want %q", got, tt.want)
			}
			if len(builds.asked) != 1 || builds.asked[0] != tt.wantAsked {
				t.Errorf("minted for %v, want %q", builds.asked, tt.wantAsked)
			}
		})
	}
}

func TestRepositoryFullName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		subs    map[string]string
		want    string
		wantErr string
	}{
		{name: "a build a GitHub event started", subs: map[string]string{repoFullNameSub: "acme/quill"}, want: "acme/quill"},
		{name: "a build without the name is refused", subs: map[string]string{}, wantErr: "REPO_FULL_NAME is not set: a trigger passes it as <organization>/<repository>, and every build starts from a trigger"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := repositoryFullName(tt.subs)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("repositoryFullName() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("repositoryFullName() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("repositoryFullName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStaleSteps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		steps   []string
		trigger string
		want    []string
		wantOut string
	}{
		{
			name:    "the trigger's own steps say nothing",
			steps:   render.StackTriggerSteps,
			trigger: render.StackTrigger,
			want:    render.StackTriggerSteps,
		},
		{
			name:    "a build the API describes without its steps says nothing",
			trigger: render.CheckTrigger,
			want:    render.CheckTriggerSteps,
		},
		{
			name:    "an older bedrock's steps are said, with what this bedrock renders",
			steps:   []string{"FetchBedrock", "CheckStack", "PlanEnvironments"},
			trigger: render.CheckTrigger,
			want:    render.CheckTriggerSteps,
			wantOut: "Notice: this build runs under the check trigger's steps as an older bedrock rendered them (FetchBedrock, CheckStack, PlanEnvironments), where this bedrock renders FetchBedrock, BuildBedrockCommit, CheckStack, PlanEnvironments, RefreshPullRequestStack. The steps are the stack's and take effect with the next release's stack apply in tst; this bedrock runs under either.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			build := &Build{Substitutions: map[string]string{envSub: tstEnvironment}}
			for _, id := range tt.steps {
				build.Steps = append(build.Steps, BuildStep{ID: id})
			}
			var out strings.Builder
			staleSteps(build, tt.trigger, tt.want, &out)
			if out.String() != tt.wantOut {
				t.Errorf("staleSteps() said %q, want %q", out.String(), tt.wantOut)
			}
		})
	}
}

func TestBuildRunsAs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		account string
		email   string
		want    bool
	}{
		{name: "the build's own identity", account: "projects/p/serviceAccounts/quill-apply@p.iam.gserviceaccount.com", email: "quill-apply@p.iam.gserviceaccount.com", want: true},
		{name: "another identity", account: "projects/p/serviceAccounts/quill-deploy@p.iam.gserviceaccount.com", email: "quill-apply@p.iam.gserviceaccount.com"},
		{name: "a build the API describes without its identity", email: "quill-apply@p.iam.gserviceaccount.com"},
		{name: "an identity whose email ends in the other's is not it", account: "projects/p/serviceAccounts/xquill-apply@p.iam.gserviceaccount.com", email: "quill-apply@p.iam.gserviceaccount.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := (&Build{ServiceAccount: tt.account}).runsAs(tt.email); got != tt.want {
				t.Errorf("runsAs() = %t, want %t", got, tt.want)
			}
		})
	}
}
