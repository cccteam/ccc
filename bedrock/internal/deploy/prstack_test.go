package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
	"github.com/cccteam/ccc/bedrock/internal/render"
)

// The apply identity the stack tests' pull request is applied as: its email, as the
// substitutions carry it, and its resource name, as a build names the identity it runs as.
const (
	applyIdentity   = "quill-apply@p.iam.gserviceaccount.com"
	applyIdentityID = "projects/p/serviceAccounts/" + applyIdentity
)

// stackSubs are a pull request's substitutions for its stack.
func stackSubs() map[string]string {
	return map[string]string{
		prNumberSub: "7", appSub: "quill", envSub: tstEnvironment, projectSub: "p", applyIdentitySub: applyIdentity,
		repoFullNameSub: "acme/quill", defaultBranchSub: "master", commitSub: "c9", migrationsSub: "schema/migrations",
	}
}

// jobSubs are stackSubs with what a trigger-held job reads beyond them: the connection
// the repository's token is minted from and the records bucket the outputs go to.
func jobSubs() map[string]string {
	subs := stackSubs()
	subs["_REPO_CONNECTION_NAME"], subs["_REPO_NAME"], subs[recordsBucket] = "conn", "quill", "records"

	return subs
}

// jobBuild is a trigger-held job's build as the Cloud Build API describes it: run as
// the identity named (the apply identity's resource name, or none for a build run as
// another identity), with the steps' ids.
func jobBuild(t *testing.T, subs map[string]string, identity string, steps []string) string {
	t.Helper()

	b := Build{ID: "b-1", Substitutions: subs, ServiceAccount: identity}
	for _, id := range steps {
		b.Steps = append(b.Steps, BuildStep{ID: id})
	}
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// The plan JSON the stack tests use: one change of the pull request's own, and one that
// is not the pull request's beside it.
const (
	ownChange     = `{"address": "google_cloud_run_v2_service.site[\"us-central1\"]", "type": "google_cloud_run_v2_service", "change": {"actions": ["create"], "after": {"name": "quill-pr7"}}}`
	foreignChange = `{"address": "google_cloud_run_v2_service.site_tst", "type": "google_cloud_run_v2_service", "change": {"actions": ["delete"], "before": {"name": "quill-app"}, "after": null}}`
	oneChange     = `{"resource_changes": [` + ownChange + `]}`
)

func TestPullRequestStack(t *testing.T) {
	t.Parallel()

	const (
		initLine      = "tofu init -input=false -no-color -backend-config=prefix=3-app/quill/tst/pr7"
		planLine      = "tofu plan -input=false -no-color -out=WS/pr.plan -var environment=tst -var pull_request=7"
		showLine      = "tofu show -json WS/pr.plan"
		applyLine     = "tofu apply -input=false -no-color WS/pr.plan"
		outputLine    = "tofu output -json"
		stateLine     = "tofu state list"
		object        = "gs://records/quill/stacks/tst/pr7/c9.json"
		written       = "The stack's outputs (%s) are at gs://records/quill/stacks/tst/pr7/c9.json for the preview build."
		substitutions = `{"_SERVICES": "us-central1=quill-pr7", "_MIGRATE_ENV": "{\"APP_SERVICE_NAME\":\"quill-migrate\"}", "_MIGRATE_DATABASES": "[\"projects/p/instances/i/databases/quill-pr7-db\"]", "_MIGRATE_SECRETS": "{}", "_JOBS_JOB": "us-central1=quill-pr7-jobs", "_HOSTNAME": "quill-pr7.example.dev", "_BUILD_ARG_FIREBASE_API_KEY": "AIzaPr7Key"}`
		withDatabase  = "google_service_account.app\ngoogle_spanner_database.quill[0]\n"
		staleReason   = "the database applied schema/migrations/000003_Sites.up.sql (build b-0), which the tree no longer carries as applied: renumbered or changed since, so the database is recreated and the migrations apply afresh"
	)
	// applied is the object an applied stack writes, before what a case adds.
	applied := func(comment int64) *StackOutputs {
		return &StackOutputs{
			App: "quill", Env: tstEnvironment, PullRequest: "7", Commit: "c9", Build: "b-1", Comment: comment, Status: StackApplied,
			Services: "us-central1=quill-pr7", JobsJob: "us-central1=quill-pr7-jobs", Hostname: "quill-pr7.example.dev",
			MigrateEnv: `{"APP_SERVICE_NAME":"quill-migrate"}`, MigrateDatabases: `["projects/p/instances/i/databases/quill-pr7-db"]`,
			BuildArguments: map[string]string{"FIREBASE_API_KEY": "AIzaPr7Key"}, Timestamp: "2026-10-12T10:00:00Z",
		}
	}
	tests := []struct {
		name string
		// subs are the build's substitutions (jobSubs when nil); another says the build
		// runs as another identity than the apply identity; steps are the build's step
		// ids (the trigger's own when nil).
		subs     map[string]string
		another  bool
		steps    []string
		ifExists bool
		comments []string
		// plan is the plan's JSON (one change of the pull request's own when empty);
		// state what tofu state list answers; output the stack's substitutions output.
		plan     string
		state    string
		output   string
		records  map[string]string
		tree     map[string]string
		changed  []string
		applyErr error
		deployer bool
		// badRequest names no build.
		badRequest bool
		// generated declares the cookie key generated in the checkout's code; pins are
		// the secret versions that exist, by name, with their state.
		generated  bool
		pins       map[string]string
		wantTofu   []string
		wantOut    []string
		wantNotOut []string
		// wantObject is the outputs object written (none when nil); wantReason the
		// failure's reason, for a failed one.
		wantObject  *StackOutputs
		wantReason  string
		wantDeleted []string
		wantComment string
		wantErr     string
	}{
		{
			name:     "a /gcbrun comment: the stack is planned, guarded, applied and its outputs written for the preview build, as the build's own identity",
			comments: []string{"/gcbrun"},
			wantTofu: []string{initLine, planLine, showLine, applyLine, outputLine},
			wantOut: []string{
				"COMMENT_BODY=/gcbrun",
				"=== Pull request 7: stack at 3-app/quill/tst/pr7 as quill-apply@p.iam.gserviceaccount.com ===",
				"Guard passed: 1 planned change(s), all pull request 7's.",
				"The pull request's stack names SERVICES=us-central1=quill-pr7 JOBS_JOB=us-central1=quill-pr7-jobs PR_HOSTNAME=quill-pr7.example.dev, and the migrate command's settings (MIGRATE_ENV).",
				"The image build passes the pull request's own build arguments: FIREBASE_API_KEY.",
				strings.Replace(written, "%s", "applied", 1),
			},
			wantNotOut: []string{"AIzaPr7Key", "Notice: this build runs under"},
			wantObject: applied(1),
		},
		{
			name:       "a generated secret is mounted at tst's newest enabled version, read as the apply identity; the pull request's stack adds none",
			comments:   []string{"/gcbrun"},
			generated:  true,
			pins:       map[string]string{"projects/p/secrets/imp-tst-gbl-quill-cookie-key/versions/5": enabledState, "projects/p/secrets/imp-tst-gbl-quill-cookie-key/versions/6": "DISABLED"},
			wantTofu:   []string{initLine, planLine + ` -var generated_secret_versions={APP_COOKIE_KEY="5"}`, showLine, applyLine, outputLine},
			wantOut:    []string{"APP_COOKIE_KEY: version 5 of imp-tst-gbl-quill-cookie-key, its newest enabled, is the pin (a generated secret: its version is derived at each plan)."},
			wantObject: applied(1),
		},
		{
			name:       "a generated secret with no version in tst yet is said and left unmounted",
			comments:   []string{"/gcbrun"},
			generated:  true,
			wantTofu:   []string{initLine, planLine, showLine, applyLine, outputLine},
			wantOut:    []string{"APP_COOKIE_KEY: imp-tst-gbl-quill-cookie-key has no enabled version yet (a generated secret, whose first version tst's next release build adds): the plan mounts nothing for it."},
			wantObject: applied(1),
		},
		{
			name:       "a build run as another identity acts as the apply identity",
			another:    true,
			comments:   []string{"/gcbrun"},
			wantTofu:   []string{initLine + " -backend-config=impersonate_service_account=" + applyIdentity, planLine, showLine, applyLine, outputLine},
			wantObject: applied(1),
		},
		{
			name:     "the latest /gcbrun comment is the instruction, and its id goes to the outputs",
			comments: []string{"/gcbrun shared-db", "looks good", "/gcbrun", "deployed"},
			wantTofu: []string{initLine, planLine, showLine, applyLine, outputLine},
			wantObject: func() *StackOutputs {
				o := applied(3)

				return o
			}(),
		},
		{
			name:       "an older bedrock's steps are said, and the job runs under them",
			steps:      []string{"FetchBedrock", "CheckStack", "ApplyPullRequestStack"},
			comments:   []string{"/gcbrun"},
			wantTofu:   []string{initLine, planLine, showLine, applyLine, outputLine},
			wantOut:    []string{"Notice: this build runs under the pr-stack trigger's steps as an older bedrock rendered them (FetchBedrock, CheckStack, ApplyPullRequestStack), where this bedrock renders FetchBedrock, BuildBedrockCommit, CheckStack, ApplyPullRequestStack. The steps are the stack's and take effect with the next release's stack apply in tst; this bedrock runs under either."},
			wantObject: applied(1),
		},
		{
			name:       "a plan with nothing to apply is not applied, and the state names what deploys",
			comments:   []string{"/gcbrun"},
			plan:       `{"resource_changes": [{"address": "google_spanner_instance.shared", "type": "google_spanner_instance", "change": {"actions": ["no-op"], "after": {"name": "tst-shared"}}}]}`,
			wantTofu:   []string{initLine, planLine, showLine, outputLine},
			wantOut:    []string{"Guard passed: 0 planned change(s), all pull request 7's.", "Nothing to apply: the pull request's stack matches the code."},
			wantObject: applied(1),
		},
		{
			name:        "/gcbrun down destroys the environment, its builds' jobs first, and writes the destroy",
			comments:    []string{"/gcbrun down"},
			output:      substitutions,
			wantTofu:    []string{initLine, planLine + " -destroy", showLine, outputLine, applyLine},
			wantOut:     []string{"=== /gcbrun down: planning the destroy of the pull request's environment ===", "Job quill-pr7-jobs-pr7-c9 deleted with pull request 7's environment.", "=== /gcbrun down: the pull request's environment is gone ===", strings.Replace(written, "%s", "destroyed", 1)},
			wantObject:  &StackOutputs{App: "quill", Env: tstEnvironment, PullRequest: "7", Commit: "c9", Build: "b-1", Comment: 1, Status: StackDestroyed, Timestamp: "2026-10-12T10:00:00Z"},
			wantDeleted: []string{"projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-c9"},
		},
		{
			name:     "shared-db plans without a database of its own, when the migrations are untouched",
			comments: []string{"/gcbrun shared-db"},
			changed:  []string{"pkg/app/main.go"},
			wantTofu: []string{initLine, planLine + " -var shared_database=true", showLine, applyLine, outputLine},
			wantOut:  []string{"=== /gcbrun shared-db: the site runs against tst's database; no database of its own ===", "shared-db: nothing under schema/migrations changed against master."},
			wantObject: func() *StackOutputs {
				o := applied(1)
				o.SharedDatabase = true

				return o
			}(),
		},
		{
			name:       "shared-db is refused when the pull request changes the migrations, and the refusal is written",
			comments:   []string{"/gcbrun shared-db"},
			changed:    []string{"schema/migrations/000003_Mine.up.sql"},
			wantTofu:   []string{initLine, planLine + " -var shared_database=true", showLine},
			wantReason: "Build REJECTED: /gcbrun shared-db with changes under schema/migrations",
			wantErr:    "/gcbrun shared-db with changes under schema/migrations",
		},
		{
			name:     "reload-db replaces the pull request's database when it exists, without a word on the pull request",
			comments: []string{"/gcbrun reload-db"},
			state:    withDatabase,
			deployer: true,
			wantTofu: []string{initLine, stateLine, planLine + " -replace=google_spanner_database.quill[0]", showLine, applyLine, outputLine},
			wantOut:  []string{"=== The pull request's database is recreated: /gcbrun reload-db ==="},
			wantObject: func() *StackOutputs {
				o := applied(1)
				o.DatabaseRecreated = "/gcbrun reload-db"

				return o
			}(),
		},
		{
			name:       "a database to recreate that does not exist yet is created",
			comments:   []string{"/gcbrun reload-db"},
			state:      "google_service_account.app\n",
			wantTofu:   []string{initLine, stateLine, planLine, showLine, applyLine, outputLine},
			wantOut:    []string{"has no database yet to recreate"},
			wantObject: applied(1),
		},
		{
			name:     "a migration the last build applied is no longer in the tree: the database is recreated and the pull request told why",
			comments: []string{"/gcbrun"},
			records:  map[string]string{"gs://records/quill/tst/pr7-c0/b-0.json": recordWith("b-0", "2026-09-27T05:00:00Z", Migration{Dir: "schema/migrations", Name: "000003_Sites.up.sql", Hash: hashOf(sitesContent)})},
			tree:     map[string]string{"schema/migrations/000004_Sites.up.sql": sitesContent},
			state:    withDatabase,
			deployer: true,
			wantTofu: []string{initLine, stateLine, planLine + " -replace=google_spanner_database.quill[0]", showLine, applyLine, outputLine},
			wantOut:  []string{"Reload: " + staleReason},
			wantObject: func() *StackOutputs {
				o := applied(1)
				o.DatabaseRecreated = staleReason

				return o
			}(),
			wantComment: "The pull request's database is recreated this build: " + staleReason + " (build b-1).",
		},
		{
			name:        "the guard refuses a resource that is not the pull request's: nothing is applied, the refusal is posted and written",
			comments:    []string{"/gcbrun"},
			plan:        `{"resource_changes": [` + ownChange + `,` + foreignChange + `]}`,
			wantTofu:    []string{initLine, planLine, showLine},
			wantOut:     []string{"google_cloud_run_v2_service.site_tst (delete)", strings.Replace(written, "%s", "failed", 1)},
			wantNotOut:  []string{"The failure could not be written"},
			wantReason:  "Build REJECTED: the plan touches resources that are not pull request 7's",
			wantComment: "A pull-request stack applies only resources named quill-pr7",
			wantErr:     "Build REJECTED: the plan touches resources that are not pull request 7's",
		},
		{
			name:       "a failed apply is written for the preview build",
			comments:   []string{"/gcbrun"},
			applyErr:   errors.New("tofu failed: exit status 1"),
			wantTofu:   []string{initLine, planLine, showLine, applyLine},
			wantOut:    []string{strings.Replace(written, "%s", "failed", 1)},
			wantReason: "tofu failed: exit status 1",
			wantErr:    "tofu failed",
		},
		{
			name:       "an output naming no services is refused",
			comments:   []string{"/gcbrun"},
			output:     `{"_MIGRATE_ENV": "{}"}`,
			wantTofu:   []string{initLine, planLine, showLine, applyLine, outputLine},
			wantReason: "the stack named no services (its substitutions output has no _SERVICES)",
			wantErr:    "the stack named no services",
		},
		{
			name:       "no /gcbrun comment is refused",
			comments:   []string{"looks good"},
			wantReason: "no /gcbrun comment found on pull request 7",
			wantErr:    "no /gcbrun comment found on pull request 7",
		},
		{
			name:       "--if-exists applies the stack again when the pull request has a preview, and a reload-db in the latest comment is that build's",
			ifExists:   true,
			comments:   []string{"/gcbrun reload-db"},
			state:      withDatabase,
			wantTofu:   []string{initLine, stateLine, planLine, showLine, applyLine, outputLine},
			wantOut:    []string{"The latest comment's reload-db was that build's; this refresh recreates nothing unless the migrations the database applied are gone from the tree.", "Pull request 7 has a preview: its stack is applied again so the preview follows the push."},
			wantNotOut: []string{"Notice: this build runs under"},
			wantObject: applied(1),
		},
		{
			name:     "--if-exists with no preview standing does nothing, and writes nothing",
			ifExists: true,
			comments: []string{"/gcbrun"},
			wantTofu: []string{initLine, stateLine},
			wantOut:  []string{"Pull request 7 has no preview standing (its state holds no resources): nothing to refresh; a /gcbrun comment stands one up."},
		},
		{
			name:     "--if-exists after /gcbrun down leaves the environment alone",
			ifExists: true,
			comments: []string{"/gcbrun down"},
			wantOut:  []string{"The latest /gcbrun comment says down: the pull request's environment is gone, or going; nothing to refresh."},
		},
		{
			name:       "--if-exists with no /gcbrun comment applies the stack plain",
			ifExists:   true,
			comments:   []string{"looks good"},
			state:      withDatabase,
			wantTofu:   []string{initLine, stateLine, planLine, showLine, applyLine, outputLine},
			wantOut:    []string{"No /gcbrun comment on pull request 7: the stack is applied plain."},
			wantObject: applied(0),
		},
		{
			name:     "--if-exists under an older bedrock's check steps is said",
			ifExists: true,
			steps:    []string{"FetchBedrock", "CheckStack", "PlanEnvironments"},
			comments: []string{"/gcbrun"},
			wantTofu: []string{initLine, stateLine},
			wantOut:  []string{"Notice: this build runs under the check trigger's steps as an older bedrock rendered them (FetchBedrock, CheckStack, PlanEnvironments), where this bedrock renders FetchBedrock, BuildBedrockCommit, CheckStack, PlanEnvironments, RefreshPullRequestStack."},
		},
		{
			name:    "a tag build is refused",
			subs:    map[string]string{tagSub: "v1.2.3", envSub: tstEnvironment},
			wantErr: "_PR_NUMBER is not set: this command runs in the stack's pull-request triggers",
		},
		{
			name: "a pull request's build outside the first environment is refused",
			subs: func() map[string]string {
				subs := jobSubs()
				subs[envSub] = stgEnvironment

				return subs
			}(),
			wantErr: `a pull request's stack is applied in tst alone (this build's _ENV is "stg")`,
		},
		{
			name: "a build without the repository's full name is refused",
			subs: func() map[string]string {
				subs := jobSubs()
				delete(subs, repoFullNameSub)

				return subs
			}(),
			wantReason: "REPO_FULL_NAME is not set",
			wantErr:    "REPO_FULL_NAME is not set: a trigger passes it as <organization>/<repository>",
		},
		{
			name:       "a build it cannot read is refused",
			badRequest: true,
			wantErr:    "BUILD_ID is not set: the step passes the build's id, project and location to the command",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subs := tt.subs
			if subs == nil {
				subs = jobSubs()
			}
			secrets := &fakeSecrets{}
			if tt.deployer {
				subs, secrets = deployerSubs(subs), deployerSecrets(t)
			}
			secrets.states = tt.pins
			identity := applyIdentityID
			if tt.another {
				identity = ""
			}
			steps := tt.steps
			if steps == nil {
				steps = render.StackTriggerSteps
				if tt.ifExists {
					steps = render.CheckTriggerSteps
				}
			}
			plan, output := tt.plan, tt.output
			if plan == "" {
				plan = oneChange
			}
			if output == "" {
				output = substitutions
			}
			files := map[string]string{placementPath: testPlacement("")}
			for name, content := range tt.tree {
				files[name] = content
			}
			if tt.generated {
				files[configPath] = generatedConfig(t)
			}
			// The job reads the application in the checkout for its generated secrets.
			w := applicationWorkspace(t, files)
			builds := &fakeBuilds{build: jobBuild(t, subs, identity, steps), token: "test-token"}
			comments := &fakeComments{bodies: tt.comments}
			run := &fakeRunner{outputs: map[string]string{"tofu show": plan, "tofu output": `{"substitutions": {"value": ` + output + `}}`, "tofu state": tt.state}, fail: map[string]error{"tofu apply": tt.applyErr}}
			repo := &githubtest.Repo{
				Refs:     map[string]github.Object{"refs/heads/master": {Type: "commit", SHA: "c4"}},
				Ancestry: map[string][]string{"c9": {"c9", "c4"}, "c4": {"c4"}},
				Changed:  map[string][]string{"c9": tt.changed},
			}
			_, gh := githubStandIn(t, repo)
			store := &memoryStore{objects: map[string]string{}}
			for path, content := range tt.records {
				store.objects[path] = content
			}
			cloudRun := newFakeRun(map[string]map[string]any{
				"projects/p/locations/us-central1/jobs/quill-pr7-jobs":        {"name": "projects/p/locations/us-central1/jobs/quill-pr7-jobs", "labels": map[string]any{applicationLabel: "quill", pullRequestLabel: "7"}},
				"projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-c9": {"name": "projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-c9", "labels": map[string]any{applicationLabel: "quill", pullRequestLabel: "7", versionLabel: "pr7-c9"}},
			})
			clients := &Clients{
				Builds: builds.open, Comments: comments.read, Exec: run, GitHub: gh, Secrets: secrets.open, SecretsAs: secrets.openAs, Storage: store.open, Run: cloudRun.open,
				Now: func() time.Time { return time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC) },
			}
			req := &BuildRequest{BuildID: "b-1", Project: "p", Location: "us-central1"}
			if tt.badRequest {
				req = &BuildRequest{}
			}
			var out strings.Builder
			err := PullRequestStack(t.Context(), clients, w, req, tt.ifExists, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("PullRequestStack() error = %v, want %q\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("PullRequestStack() error = %v\n%s", err, out.String())
			}
			lines := strings.ReplaceAll(strings.Join(run.lines(), "\n"), string(w), "WS")
			if diff := cmp.Diff(strings.Join(tt.wantTofu, "\n"), lines); diff != "" {
				t.Errorf("tofu (-want +got):\n%s", diff)
			}
			for _, c := range run.ran {
				wantEnv := []string(nil)
				if tt.another {
					wantEnv = []string{"GOOGLE_IMPERSONATE_SERVICE_ACCOUNT=" + applyIdentity}
				}
				if c.Dir != filepath.Join(string(w), stackDir) || !cmp.Equal(c.Env, wantEnv, cmpopts.EquateEmpty()) {
					t.Errorf("%s ran in %s with %v, want the stack directory with %v", c, c.Dir, c.Env, wantEnv)
				}
			}
			containsAll(t, out.String(), tt.wantOut...)
			for _, unwanted := range tt.wantNotOut {
				if strings.Contains(out.String(), unwanted) {
					t.Errorf("output carries %q:\n%s", unwanted, out.String())
				}
			}
			data, ok := store.objects[object]
			switch {
			case tt.wantObject == nil && tt.wantReason == "":
				if ok {
					t.Errorf("an object was written:\n%s", data)
				}
			case !ok:
				t.Fatalf("no object at %s; output:\n%s", object, out.String())
			default:
				var got StackOutputs
				if err := json.Unmarshal([]byte(data), &got); err != nil {
					t.Fatalf("the object does not read: %v\n%s", err, data)
				}
				if tt.wantReason != "" {
					if got.Status != StackFailed || !strings.Contains(got.Reason, tt.wantReason) {
						t.Errorf("object = %+v, want a failure holding %q", got, tt.wantReason)
					}
				} else if diff := cmp.Diff(tt.wantObject, &got); diff != "" {
					t.Errorf("object (-want +got):\n%s", diff)
				}
				if !strings.HasSuffix(data, "}\n") {
					t.Errorf("the object does not end in a newline: %q", data[len(data)-3:])
				}
			}
			if diff := cmp.Diff(tt.wantDeleted, cloudRun.deleted, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("deleted (-want +got):\n%s", diff)
			}
			comments7 := repo.Comments[7]
			if tt.wantComment == "" && len(comments7) > 0 {
				t.Errorf("posted %q, want nothing", comments7[0].Body)
			}
			if tt.wantComment != "" && (len(comments7) != 1 || !strings.Contains(comments7[0].Body, tt.wantComment)) {
				t.Errorf("comments = %+v, want one holding %q", comments7, tt.wantComment)
			}
			if _, err := os.Stat(filepath.Join(string(w), stackDir, ".terraform")); !os.IsNotExist(err) {
				t.Errorf("the backend cache stays: %v", err)
			}
		})
	}
}

func TestGuardPlan(t *testing.T) {
	t.Parallel()

	ownMember := `{"address": "google_project_iam_member.app", "type": "google_project_iam_member", "change": {"actions": ["create"], "after": {"member": "serviceAccount:quill-pr7-app@p.iam.gserviceaccount.com", "project": "p"}}}`
	// The deploy identity's Firestore role is a project grant whose condition bounds it to
	// the pull request's own database; the same role bounded to the environment's is not.
	ownConditioned := `{"address": "google_project_iam_member.firestore_deploy", "type": "google_project_iam_member", "change": {"actions": ["create"], "after": {"member": "serviceAccount:quill-deploy@p.iam.gserviceaccount.com", "role": "roles/datastore.user", "condition": [{"title": "quill-pr7-fs only", "expression": "resource.name == \"projects/p/databases/quill-pr7-fs\""}]}}}`
	foreignConditioned := `{"address": "google_project_iam_member.firestore_deploy", "type": "google_project_iam_member", "change": {"actions": ["delete"], "before": {"member": "serviceAccount:quill-deploy@p.iam.gserviceaccount.com", "role": "roles/datastore.user", "condition": [{"title": "quill-fs only", "expression": "resource.name == \"projects/p/databases/quill-fs\""}]}, "after": null}}`
	sleep := `{"address": "time_sleep.wait", "type": "time_sleep", "change": {"actions": ["create"], "after": {}}}`
	noop := `{"address": "google_spanner_instance.shared", "type": "google_spanner_instance", "change": {"actions": ["no-op"], "after": {"name": "tst-shared"}}}`
	// A Firestore ruleset is named by the service (name unknown at plan time); the stack
	// names its source file for the database the rules are released to.
	ownRuleset := `{"address": "google_firebaserules_ruleset.firestore", "type": "google_firebaserules_ruleset", "change": {"actions": ["create"], "after": {"name": null, "project": "p", "source": [{"files": [{"content": "rules_version = '2';", "name": "quill-pr7-fs.rules"}], "language": null}]}}}`
	foreignRuleset := `{"address": "google_firebaserules_ruleset.firestore", "type": "google_firebaserules_ruleset", "change": {"actions": ["delete"], "before": {"name": "projects/p/rulesets/5c2a", "project": "p", "source": [{"files": [{"content": "rules_version = '2';", "name": "imp-tst-gbl-quill-fs.rules"}], "language": "FIREBASE_RULES"}]}, "after": null}}`
	// A bucket's policy is named by its bucket: the pull request's own, or the environment's.
	ownPolicy := `{"address": "google_storage_bucket_iam_policy.files", "type": "google_storage_bucket_iam_policy", "change": {"actions": ["create"], "after": {"bucket": "imp-tst-gbl-quill-pr7-files-123", "policy_data": "{}"}}}`
	foreignPolicy := `{"address": "google_storage_bucket_iam_policy.files", "type": "google_storage_bucket_iam_policy", "change": {"actions": ["update"], "before": {"bucket": "imp-tst-gbl-quill-files-123", "policy_data": "{}"}, "after": {"bucket": "imp-tst-gbl-quill-files-123", "policy_data": "{}"}}}`
	plan := func(changes ...string) string {
		return `{"resource_changes": [` + strings.Join(changes, ",") + `]}`
	}
	tests := []struct {
		name        string
		shared      bool
		plan        string
		changed     []string
		wantOut     []string
		wantErr     string
		wantComment string
	}{
		{
			name:    "the pull request's own resources, its accounts' memberships and what shapes nothing pass",
			plan:    plan(ownChange, ownMember, sleep, noop),
			wantOut: []string{"Guard passed: 3 planned change(s), all pull request 7's."},
		},
		{
			name:    "the deploy identity's grant bounded to the pull request's database passes",
			plan:    plan(ownChange, ownConditioned),
			wantOut: []string{"Guard passed: 2 planned change(s), all pull request 7's."},
		},
		{
			name:        "the deploy identity's grant bounded to the environment's database is refused",
			plan:        plan(ownChange, foreignConditioned),
			wantOut:     []string{"google_project_iam_member.firestore_deploy (delete)"},
			wantErr:     "Build REJECTED: the plan touches resources that are not pull request 7's",
			wantComment: "A pull-request stack applies only resources named quill-pr7",
		},
		{
			name:    "a ruleset whose source is named for the pull request's database passes",
			plan:    plan(ownChange, ownRuleset),
			wantOut: []string{"Guard passed: 2 planned change(s), all pull request 7's."},
		},
		{
			name:        "a ruleset named for the environment's database is refused",
			plan:        plan(ownChange, foreignRuleset),
			wantOut:     []string{"google_firebaserules_ruleset.firestore (delete)"},
			wantErr:     "Build REJECTED: the plan touches resources that are not pull request 7's",
			wantComment: "A pull-request stack applies only resources named quill-pr7",
		},
		{
			name:    "the policy of the pull request's own file store passes: its bucket names it",
			plan:    plan(ownChange, ownPolicy),
			wantOut: []string{"Guard passed: 2 planned change(s), all pull request 7's."},
		},
		{
			name:        "the policy of the environment's file store is refused",
			plan:        plan(ownChange, foreignPolicy),
			wantOut:     []string{"google_storage_bucket_iam_policy.files (update)"},
			wantErr:     "Build REJECTED: the plan touches resources that are not pull request 7's",
			wantComment: "A pull-request stack applies only resources named quill-pr7",
		},
		{
			name:        "a resource that is not the pull request's is refused and posted",
			plan:        plan(ownChange, foreignChange),
			wantOut:     []string{"google_cloud_run_v2_service.site_tst (delete)"},
			wantErr:     "Build REJECTED: the plan touches resources that are not pull request 7's",
			wantComment: "A pull-request stack applies only resources named quill-pr7",
		},
		{
			name:    "shared-db is refused when the pull request changes the migrations",
			shared:  true,
			plan:    plan(ownChange),
			changed: []string{"schema/migrations/000003_Mine.up.sql", "pkg/app/main.go"},
			wantOut: []string{"  schema/migrations/000003_Mine.up.sql"},
			wantErr: "/gcbrun shared-db with changes under schema/migrations",
		},
		{
			name:    "shared-db passes when the migrations are untouched",
			shared:  true,
			plan:    plan(ownChange),
			changed: []string{"pkg/app/main.go"},
			wantOut: []string{"shared-db: nothing under schema/migrations changed against master.", "Guard passed: 1 planned change(s)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workspaceFiles(t, map[string]string{PlanJSONFile: tt.plan})
			repo := &githubtest.Repo{
				Refs:     map[string]github.Object{"refs/heads/master": {Type: "commit", SHA: "c4"}},
				Ancestry: map[string][]string{"c9": {"c9", "c4"}, "c4": {"c4"}},
				Changed:  map[string][]string{"c9": tt.changed},
			}
			_, gh := githubStandIn(t, repo)
			env := map[string]string{githubTokenFact: "test-token", sharedDBFact: flag(tt.shared)}
			var out strings.Builder
			err := guardPlan(t.Context(), &Clients{GitHub: gh, Secrets: (&fakeSecrets{}).open}, w, &Build{ID: "b-1", Substitutions: stackSubs()}, env, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("guardPlan() error = %v, want %q\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("guardPlan() error = %v\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			if tt.wantComment != "" && (len(repo.Comments[7]) != 1 || !strings.Contains(repo.Comments[7][0].Body, tt.wantComment)) {
				t.Errorf("comments = %+v, want one holding %q", repo.Comments[7], tt.wantComment)
			}
		})
	}
}

func TestStackFacts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		output  string
		want    map[string]string
		wantErr string
	}{
		{
			name:   "the stack's output names what the steps after deploy, the migrate command's settings and the build arguments among them",
			output: `{"_SERVICES": "us-central1=quill-pr7", "_MIGRATE_ENV": "{}", "_MIGRATE_DATABASES": "[]", "_MIGRATE_SECRETS": "{}", "_JOBS_JOB": "us-central1=quill-pr7-jobs", "_HOSTNAME": "quill-pr7.example.dev", "_ENV": "tst", "_BUILD_ARG_SITE_HOST": "quill-pr7.example.dev"}`,
			want:   map[string]string{services: "us-central1=quill-pr7", migrateEnvFact: "{}", migrateDatabasesFact: "[]", migrateSecretsFact: "{}", jobsJobFact: "us-central1=quill-pr7-jobs", prHostnameFact: "quill-pr7.example.dev", "_BUILD_ARG_SITE_HOST": "quill-pr7.example.dev"},
		},
		{
			name:   "a stack applied by an older bedrock names no settings for the migrate command, which the migrate step refuses later",
			output: `{"_SERVICES": "us-central1=quill-pr7", "_HOSTNAME": "quill-pr7.example.dev"}`,
			want:   map[string]string{services: "us-central1=quill-pr7", prHostnameFact: "quill-pr7.example.dev"},
		},
		{
			name:    "an output naming no services is refused",
			output:  `{"_MIGRATE_ENV": "{}"}`,
			wantErr: "the stack named no services (its substitutions output has no _SERVICES)",
		},
		{
			name:    "an empty job is refused",
			output:  `{"_SERVICES": "us-central1=quill-pr7", "_JOBS_JOB": ""}`,
			wantErr: "the stack names an empty _JOBS_JOB",
		},
		{
			name:    "an output that does not read is refused",
			output:  `{`,
			wantErr: "the stack's substitutions output",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := stackFacts([]byte(tt.output))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("stackFacts() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("stackFacts() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("facts (-want +got):\n%s", diff)
			}
		})
	}
}
