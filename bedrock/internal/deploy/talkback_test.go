package deploy

import (
	"strings"
	"testing"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

func TestTalkBack(t *testing.T) {
	t.Parallel()

	const env = "export RELEASE=\"pr7-c9\"\nexport PR_HOSTNAME=\"quill-pr7.example.dev\"\nexport PROJECT_ID=\"p\"\nexport LOCATION=\"us-central1\"\n"
	tests := []struct {
		name string
		env  string
		// deployer registers the deployer app; tag makes it a tag build.
		deployer, tag bool
		earlier       int
		wantComment   string
		wantStatuses  []string
		wantOut       string
	}{
		{name: "a tag build says nothing", tag: true, deployer: true, wantOut: "No talk-back"},
		{name: "no deployer app yet, nothing is said", wantOut: "No talk-back"},
		{
			name: "a deployment carrying the environment's URL, and the comment", env: env, deployer: true,
			wantComment:  "Deployed pr7-c9 to https://quill-pr7.example.dev/ with its own database (build b-1).",
			wantStatuses: []string{"success https://quill-pr7.example.dev/"},
		},
		{
			name: "shared-db is said", env: env + "export SHARED_DB=\"true\"\n", deployer: true,
			wantComment: "with tst's database (shared-db)",
		},
		{
			name: "a teardown marks every deployment of the environment inactive", env: env + "export DOWN=\"true\"\n", deployer: true, earlier: 2,
			wantComment:  "The environment quill-pr7 is destroyed (/gcbrun down, build b-1).",
			wantStatuses: []string{"inactive ", "inactive "},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subs := map[string]string{prNumberSub: "7", appSub: "quill", envSub: tstEnvironment, repoFullNameSub: "acme/quill", commitSub: "c9"}
			if tt.tag {
				subs = map[string]string{tagSub: "v1.2.3", repoFullNameSub: "acme/quill"}
			}
			secrets := &fakeSecrets{}
			if tt.deployer {
				subs, secrets = deployerSubs(subs), deployerSecrets(t)
			}
			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: buildFor(t, subs)})
			repo := &githubtest.Repo{}
			for i := range tt.earlier {
				repo.Deployments = append(repo.Deployments, &githubtest.Deployment{ID: int64(i + 1), Request: github.DeploymentRequest{Ref: "c1", Environment: "quill-pr7"}})
			}
			_, gh := githubStandIn(t, repo)
			var out strings.Builder
			if err := TalkBack(t.Context(), &Clients{GitHub: gh, Secrets: secrets.open}, w, &out); err != nil {
				t.Fatalf("TalkBack() error = %v\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut)
			comments := repo.Comments[7]
			if tt.wantComment == "" {
				if len(comments) > 0 {
					t.Errorf("posted %q, want nothing", comments[0].Body)
				}

				return
			}
			if len(comments) != 1 || !strings.Contains(comments[0].Body, tt.wantComment) {
				t.Errorf("comments = %+v, want one holding %q", comments, tt.wantComment)
			}
			var statuses []string
			for _, d := range repo.Deployments {
				for _, s := range d.Statuses {
					statuses = append(statuses, s.State+" "+s.EnvironmentURL)
				}
				if d.ID > int64(tt.earlier) && (d.Request.Environment != "quill-pr7" || !d.Request.TransientEnvironment || d.Request.Ref != "c9") {
					t.Errorf("deployment %+v", d.Request)
				}
			}
			if tt.wantStatuses != nil && strings.Join(statuses, "|") != strings.Join(tt.wantStatuses, "|") {
				t.Errorf("statuses = %q, want %q", statuses, tt.wantStatuses)
			}
		})
	}
}
