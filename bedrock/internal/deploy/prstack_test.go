package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/cccteam/ccc/bedrock/internal/github"
	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

// stackSubs are a pull request's substitutions for its stack.
func stackSubs() map[string]string {
	return map[string]string{
		prNumberSub: "7", appSub: "quill", envSub: tstEnvironment, projectSub: "p", applyIdentitySub: "quill-apply@p.iam.gserviceaccount.com",
		repoFullNameSub: "acme/quill", defaultBranchSub: "master", commitSub: "c9", migrationsSub: "schema/migrations",
	}
}

func TestPlanStack(t *testing.T) {
	t.Parallel()

	const initLine = "tofu init -input=false -no-color -backend-config=prefix=3-app/quill/tst/pr7 -backend-config=impersonate_service_account=quill-apply@p.iam.gserviceaccount.com"
	plan := "-out=WS/pr.plan -var environment=tst -var pull_request=7"
	tests := []struct {
		name        string
		env         string
		subs        map[string]string
		state       string
		wantLines   []string
		wantOut     []string
		wantReplace bool
	}{
		{
			name:    "a tag build plans nothing",
			subs:    map[string]string{tagSub: "v1.2.3"},
			wantOut: []string{tagBuildNotice},
		},
		{
			name:      "a pull request's stack",
			subs:      stackSubs(),
			wantLines: []string{initLine, "tofu plan -input=false -no-color " + plan, "tofu show -json WS/pr.plan"},
		},
		{
			name:      "/gcbrun down plans the destroy",
			env:       "export DOWN=\"true\"\n",
			subs:      stackSubs(),
			wantLines: []string{initLine, "tofu plan -input=false -no-color " + plan + " -destroy", "tofu show -json WS/pr.plan"},
			wantOut:   []string{"planning the destroy"},
		},
		{
			name:      "shared-db plans without a database of its own",
			env:       "export SHARED_DB=\"true\"\n",
			subs:      stackSubs(),
			wantLines: []string{initLine, "tofu plan -input=false -no-color " + plan + " -var shared_database=true", "tofu show -json WS/pr.plan"},
		},
		{
			name:        "a recreated database is replaced when it exists",
			env:         "export RELOAD_DB=\"true\"\nexport RELOAD_DB_REASON=\"/gcbrun reload-db\"\n",
			subs:        stackSubs(),
			state:       "google_service_account.app\ngoogle_spanner_database.quill[0]\n",
			wantLines:   []string{initLine, "tofu state list", "tofu plan -input=false -no-color " + plan + " -replace=google_spanner_database.quill[0]", "tofu show -json WS/pr.plan"},
			wantOut:     []string{"The pull request's database is recreated: /gcbrun reload-db"},
			wantReplace: true,
		},
		{
			name:      "a database to recreate that does not exist yet is created",
			env:       "export RELOAD_DB=\"true\"\nexport RELOAD_DB_REASON=\"/gcbrun reload-db\"\n",
			subs:      stackSubs(),
			state:     "google_service_account.app\n",
			wantLines: []string{initLine, "tofu state list", "tofu plan -input=false -no-color " + plan, "tofu show -json WS/pr.plan"},
			wantOut:   []string{"has no database yet to recreate"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workspaceFiles(t, map[string]string{EnvironmentFile: "export SKIP_DEPLOY=\"\"\n" + tt.env, BuildFile: buildFor(t, tt.subs)})
			run := &fakeRunner{outputs: map[string]string{"tofu show": `{"resource_changes": []}`, "tofu state": tt.state}}
			var out strings.Builder
			if err := PlanStack(t.Context(), &Clients{Exec: run}, w, &out); err != nil {
				t.Fatalf("PlanStack() error = %v\n%s", err, out.String())
			}
			var got []string
			for _, line := range run.lines() {
				got = append(got, strings.ReplaceAll(line, string(w), "WS"))
			}
			if diff := cmp.Diff(tt.wantLines, got); diff != "" {
				t.Errorf("commands (-want +got):\n%s", diff)
			}
			for _, c := range run.ran {
				if c.Dir != filepath.Join(string(w), stackDir) || !cmp.Equal(c.Env, []string{"GOOGLE_IMPERSONATE_SERVICE_ACCOUNT=quill-apply@p.iam.gserviceaccount.com"}) {
					t.Errorf("%s ran in %s with %v, want the stack directory as the apply identity", c, c.Dir, c.Env)
				}
			}
			containsAll(t, out.String(), tt.wantOut...)
			if len(tt.wantLines) > 0 {
				if data, err := os.ReadFile(filepath.Join(string(w), PlanJSONFile)); err != nil || string(data) != `{"resource_changes": []}` {
					t.Errorf("%s = %q, %v", PlanJSONFile, data, err)
				}
			}
			env, err := w.Environment()
			if err != nil {
				t.Fatal(err)
			}
			if got := env[replaceDatabaseFact] == trueValue; got != tt.wantReplace {
				t.Errorf("%s = %q, want replace %t", replaceDatabaseFact, env[replaceDatabaseFact], tt.wantReplace)
			}
		})
	}
}

func TestGuardPlan(t *testing.T) {
	t.Parallel()

	own := `{"address": "google_cloud_run_v2_service.site[\"us-central1\"]", "type": "google_cloud_run_v2_service", "change": {"actions": ["create"], "after": {"name": "quill-pr7"}}}`
	ownMember := `{"address": "google_project_iam_member.app", "type": "google_project_iam_member", "change": {"actions": ["create"], "after": {"member": "serviceAccount:quill-pr7-app@p.iam.gserviceaccount.com", "project": "p"}}}`
	sleep := `{"address": "time_sleep.wait", "type": "time_sleep", "change": {"actions": ["create"], "after": {}}}`
	noop := `{"address": "google_spanner_instance.shared", "type": "google_spanner_instance", "change": {"actions": ["no-op"], "after": {"name": "tst-shared"}}}`
	foreign := `{"address": "google_cloud_run_v2_service.site_tst", "type": "google_cloud_run_v2_service", "change": {"actions": ["delete"], "before": {"name": "quill-app"}, "after": null}}`
	// A Firestore ruleset is named by the service (name unknown at plan time); the stack
	// names its source file for the database the rules are released to.
	ownRuleset := `{"address": "google_firebaserules_ruleset.firestore", "type": "google_firebaserules_ruleset", "change": {"actions": ["create"], "after": {"name": null, "project": "p", "source": [{"files": [{"content": "rules_version = '2';", "name": "quill-pr7-fs.rules"}], "language": null}]}}}`
	foreignRuleset := `{"address": "google_firebaserules_ruleset.firestore", "type": "google_firebaserules_ruleset", "change": {"actions": ["delete"], "before": {"name": "projects/p/rulesets/5c2a", "project": "p", "source": [{"files": [{"content": "rules_version = '2';", "name": "imp-tst-gbl-quill-fs.rules"}], "language": "FIREBASE_RULES"}]}, "after": null}}`
	plan := func(changes ...string) string {
		return `{"resource_changes": [` + strings.Join(changes, ",") + `]}`
	}
	tests := []struct {
		name        string
		env         string
		plan        string
		changed     []string
		wantOut     []string
		wantErr     string
		wantComment string
	}{
		{
			name:    "the pull request's own resources, its accounts' memberships and what shapes nothing pass",
			plan:    plan(own, ownMember, sleep, noop),
			wantOut: []string{"Guard passed: 3 planned change(s), all pull request 7's."},
		},
		{
			name:    "a ruleset whose source is named for the pull request's database passes",
			plan:    plan(own, ownRuleset),
			wantOut: []string{"Guard passed: 2 planned change(s), all pull request 7's."},
		},
		{
			name:        "a ruleset named for the environment's database is refused",
			plan:        plan(own, foreignRuleset),
			wantOut:     []string{"google_firebaserules_ruleset.firestore (delete)"},
			wantErr:     "Build REJECTED: the plan touches resources that are not pull request 7's",
			wantComment: "A pull-request stack applies only resources named quill-pr7",
		},
		{
			name:        "a resource that is not the pull request's is refused and posted",
			plan:        plan(own, foreign),
			wantOut:     []string{"google_cloud_run_v2_service.site_tst (delete)"},
			wantErr:     "Build REJECTED: the plan touches resources that are not pull request 7's",
			wantComment: "A pull-request stack applies only resources named quill-pr7",
		},
		{
			name:    "shared-db is refused when the pull request changes the migrations",
			env:     "export SHARED_DB=\"true\"\n",
			plan:    plan(own),
			changed: []string{"schema/migrations/000003_Mine.up.sql", "pkg/app/main.go"},
			wantOut: []string{"  schema/migrations/000003_Mine.up.sql"},
			wantErr: "/gcbrun shared-db with changes under schema/migrations",
		},
		{
			name:    "shared-db passes when the migrations are untouched",
			env:     "export SHARED_DB=\"true\"\n",
			plan:    plan(own),
			changed: []string{"pkg/app/main.go"},
			wantOut: []string{"shared-db: nothing under schema/migrations changed against master.", "Guard passed: 1 planned change(s)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workspaceFiles(t, map[string]string{EnvironmentFile: "export GITHUB_TOKEN=\"test-token\"\n" + tt.env, BuildFile: buildFor(t, stackSubs()), PlanJSONFile: tt.plan})
			repo := &githubtest.Repo{
				Refs:     map[string]github.Object{"refs/heads/master": {Type: "commit", SHA: "c4"}},
				Ancestry: map[string][]string{"c9": {"c9", "c4"}, "c4": {"c4"}},
				Changed:  map[string][]string{"c9": tt.changed},
			}
			_, gh := githubStandIn(t, repo)
			var out strings.Builder
			err := GuardPlan(t.Context(), &Clients{GitHub: gh, Secrets: (&fakeSecrets{}).open}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("GuardPlan() error = %v, want %q\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("GuardPlan() error = %v\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			if tt.wantComment != "" && (len(repo.Comments[7]) != 1 || !strings.Contains(repo.Comments[7][0].Body, tt.wantComment)) {
				t.Errorf("comments = %+v, want one holding %q", repo.Comments[7], tt.wantComment)
			}
		})
	}
}

func TestApplyStack(t *testing.T) {
	t.Parallel()

	const substitutions = `{"_SERVICES": "us-central1=quill-pr7", "_MIGRATE_JOB": "us-central1=quill-pr7-migrate", "_JOBS_JOB": "us-central1=quill-pr7-jobs", "_HOSTNAME": "quill-pr7.example.dev", "_ENV": "tst"}`
	const oneChange = `{"resource_changes": [{"address": "google_cloud_run_v2_service.site[\"us-central1\"]", "type": "google_cloud_run_v2_service", "change": {"actions": ["create"], "after": {"name": "quill-pr7"}}}]}`
	tests := []struct {
		name string
		env  string
		// plan is the plan's JSON; one change when empty.
		plan        string
		output      string
		applyErr    error
		deployer    bool
		wantFacts   map[string]string
		wantDeleted []string
		wantComment string
		wantOut     []string
		wantApplied bool
		wantErr     string
	}{
		{
			name:        "the stack's output names what the steps after deploy",
			output:      substitutions,
			wantFacts:   map[string]string{services: "us-central1=quill-pr7", migrateJobFact: "us-central1=quill-pr7-migrate", jobsJobFact: "us-central1=quill-pr7-jobs", prHostnameFact: "quill-pr7.example.dev"},
			wantApplied: true,
		},
		{
			name:      "a plan with nothing to apply is not applied, and the state names what deploys",
			plan:      `{"resource_changes": [{"address": "google_spanner_instance.shared", "type": "google_spanner_instance", "change": {"actions": ["no-op"], "after": {"name": "tst-shared"}}}]}`,
			output:    substitutions,
			wantFacts: map[string]string{services: "us-central1=quill-pr7", migrateJobFact: "us-central1=quill-pr7-migrate"},
			wantOut:   []string{"Nothing to apply: the pull request's stack matches the code."},
		},
		{
			name:        "after a destroy nothing deploys, the pull request's builds' jobs deleted first",
			env:         "export DOWN=\"true\"\n",
			output:      substitutions,
			wantFacts:   map[string]string{skipDeploy: trueValue},
			wantDeleted: []string{"projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-c9"},
			wantApplied: true,
		},
		{
			name:        "a destroy of a stack with no output deletes no job",
			env:         "export DOWN=\"true\"\n",
			wantFacts:   map[string]string{skipDeploy: trueValue},
			wantApplied: true,
		},
		{
			name:    "an output naming no services is refused",
			output:  `{"_MIGRATE_JOB": "us-central1=quill-pr7-migrate"}`,
			wantErr: "named no services or no migrate job",
		},
		{
			name:     "a failed apply stops the build",
			applyErr: errors.New("tofu failed: exit status 1"),
			wantErr:  "tofu failed",
		},
		{
			name:        "a database recreated without being asked is said on the pull request",
			env:         "export REPLACE_DATABASE=\"true\"\nexport RELOAD_DB=\"true\"\nexport RELOAD_DB_REASON=\"the database applied schema/migrations/000002_A.up.sql (build b-0), which the tree no longer carries\"\n",
			output:      substitutions,
			deployer:    true,
			wantFacts:   map[string]string{services: "us-central1=quill-pr7"},
			wantComment: "The pull request's database is recreated this build: the database applied schema/migrations/000002_A.up.sql",
			wantApplied: true,
		},
		{
			name:        "a recreate the comment asked for is not repeated",
			env:         "export REPLACE_DATABASE=\"true\"\nexport RELOAD_DB=\"true\"\nexport RELOAD_DB_REASON=\"/gcbrun reload-db\"\n",
			output:      substitutions,
			deployer:    true,
			wantFacts:   map[string]string{services: "us-central1=quill-pr7"},
			wantApplied: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subs := stackSubs()
			secrets := &fakeSecrets{}
			if tt.deployer {
				subs, secrets = deployerSubs(subs), deployerSecrets(t)
			}
			plan := tt.plan
			if plan == "" {
				plan = oneChange
			}
			w := workspaceFiles(t, map[string]string{EnvironmentFile: "export SKIP_DEPLOY=\"\"\n" + tt.env, BuildFile: buildFor(t, subs), PlanJSONFile: plan})
			run := &fakeRunner{outputs: map[string]string{"tofu output": tt.output}, fail: map[string]error{"tofu apply": tt.applyErr}}
			repo := &githubtest.Repo{}
			_, gh := githubStandIn(t, repo)
			cloudRun := newFakeRun(map[string]map[string]any{
				"projects/p/locations/us-central1/jobs/quill-pr7-jobs":        {"name": "projects/p/locations/us-central1/jobs/quill-pr7-jobs", "labels": map[string]any{applicationLabel: "quill", pullRequestLabel: "7"}},
				"projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-c9": {"name": "projects/p/locations/us-central1/jobs/quill-pr7-jobs-pr7-c9", "labels": map[string]any{applicationLabel: "quill", pullRequestLabel: "7", versionLabel: "pr7-c9"}},
			})
			var out strings.Builder
			err := ApplyStack(t.Context(), &Clients{Exec: run, GitHub: gh, Secrets: secrets.open, Run: cloudRun.open}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ApplyStack() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("ApplyStack() error = %v\n%s", err, out.String())
			}
			applied := false
			for _, line := range run.lines() {
				if strings.HasPrefix(line, "tofu apply") {
					applied = true
				}
			}
			if applied != tt.wantApplied {
				t.Errorf("applied %t, want %t:\n%s", applied, tt.wantApplied, strings.Join(run.lines(), "\n"))
			}
			containsAll(t, out.String(), tt.wantOut...)
			env, err := w.Environment()
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range tt.wantFacts {
				if env[name] != want {
					t.Errorf("%s = %q, want %q", name, env[name], want)
				}
			}
			if diff := cmp.Diff(tt.wantDeleted, cloudRun.deleted, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("deleted (-want +got):\n%s\noutput:\n%s", diff, out.String())
			}
			comments := repo.Comments[7]
			if tt.wantComment == "" && len(comments) > 0 {
				t.Errorf("posted %q, want nothing", comments[0].Body)
			}
			if tt.wantComment != "" && (len(comments) != 1 || !strings.Contains(comments[0].Body, tt.wantComment)) {
				t.Errorf("comments = %+v, want one holding %q", comments, tt.wantComment)
			}
		})
	}
}
