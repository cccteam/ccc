package deploy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/cccteam/ccc/bedrock/internal/github/githubtest"
)

// stackPlanJSON is a plan of a stack as tofu show -json gives it: a service updated (its
// template pinning a secret version by short name), a bucket created, a job unchanged (its
// template pinning a version by full name), a secret replaced, and a data read.
const stackPlanJSON = `{"resource_changes": [
 {"address": "google_cloud_run_v2_service.app[\"uc1\"]", "type": "google_cloud_run_v2_service", "change": {"actions": ["update"], "after": {"project": "p-stg", "template": [{"containers": [{"env": [{"name": "APP_COOKIE_KEY", "value_source": [{"secret_key_ref": [{"secret": "quill-cookie-key", "version": "3"}]}]}]}]}]}}},
 {"address": "google_storage_bucket.assets", "type": "google_storage_bucket", "change": {"actions": ["create"], "after": {}}},
 {"address": "google_cloud_run_v2_job.migrate", "type": "google_cloud_run_v2_job", "change": {"actions": ["no-op"], "after": {"project": "p-stg", "template": [{"template": [{"containers": [{"env": [{"name": "APP_DB", "value_source": [{"secret_key_ref": [{"secret": "projects/p-stg/secrets/quill-db", "version": "latest"}]}]}]}]}]}]}}},
 {"address": "google_secret_manager_secret.old", "type": "google_secret_manager_secret", "change": {"actions": ["delete", "create"], "before": {}, "after": {}}},
 {"address": "data.google_project.p", "type": "google_project", "change": {"actions": ["read"]}}
]}`

// The versions the plan above pins, and a stack of pins that exist and are enabled.
const (
	cookieVersion = "projects/p-stg/secrets/quill-cookie-key/versions/3"
	dbVersion     = "projects/p-stg/secrets/quill-db/versions/latest"
)

func enabledPins() map[string]string {
	return map[string]string{cookieVersion: enabledState, dbVersion: enabledState}
}

func TestStackPlan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    string
		want    *StackPlan
		wantErr string
	}{
		{
			name: "the counts and the changes, no-ops and reads left out, a replace counted twice",
			data: stackPlanJSON,
			want: &StackPlan{Add: 2, Change: 1, Destroy: 1, Changes: []StackChange{
				{Address: `google_cloud_run_v2_service.app["uc1"]`, Actions: []string{actionUpdate}},
				{Address: "google_storage_bucket.assets", Actions: []string{actionCreate}},
				{Address: "google_secret_manager_secret.old", Actions: []string{actionDelete, actionCreate}},
			}},
		},
		{name: "a plan with no change", data: `{"resource_changes": [{"address": "a.b", "type": "google_x", "change": {"actions": ["no-op"]}}]}`, want: &StackPlan{}},
		{name: "a plan that is not JSON is refused", data: "{", wantErr: "the plan's JSON"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := stackPlan([]byte(tt.data))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("stackPlan() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("stackPlan() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("stackPlan() mismatch (-want +got):\n%s", diff)
			}
			if got.Summary() != fmt.Sprintf("Plan: %d to add, %d to change, %d to destroy.", tt.want.Add, tt.want.Change, tt.want.Destroy) {
				t.Errorf("Summary() = %q", got.Summary())
			}
		})
	}
}

func TestPlannedMounts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    string
		project string
		want    []string
	}{
		{name: "the versions the planned templates pin, a short name under the resource's project, sorted", data: stackPlanJSON, project: "p-tst", want: []string{cookieVersion, dbVersion}},
		{
			name:    "a short name under the build's project when the resource names none",
			data:    `{"resource_changes": [{"address": "google_cloud_run_v2_service.app", "type": "google_cloud_run_v2_service", "change": {"actions": ["create"], "after": {"template": [{"containers": [{"env": [{"value_source": [{"secret_key_ref": [{"secret": "k", "version": "1"}]}]}]}]}]}}}]}`,
			project: "p-tst",
			want:    []string{"projects/p-tst/secrets/k/versions/1"},
		},
		{name: "a plan without a service or a job pins nothing", data: `{"resource_changes": [{"address": "google_storage_bucket.b", "type": "google_storage_bucket", "change": {"actions": ["create"], "after": {"secret_key_ref": [{"secret": "x", "version": "1"}]}}}]}`, project: "p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := plannedMounts([]byte(tt.data), tt.project)
			if err != nil {
				t.Fatalf("plannedMounts() error = %v", err)
			}
			if diff := cmp.Diff(tt.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("plannedMounts() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// tagSubs are a tag build's substitutions for stg.
func tagSubs() map[string]string {
	return map[string]string{appSub: "quill", envSub: stgEnvironment, projectSub: "p-stg", applyIdentitySub: "quill-apply@p-stg.iam.gserviceaccount.com", commitSub: "c9", repoFullNameSub: "acme/quill"}
}

func TestPlanEnvironmentStack(t *testing.T) {
	t.Parallel()

	const (
		initLine = "tofu init -input=false -no-color -backend-config=prefix=3-app/quill/stg -backend-config=impersonate_service_account=quill-apply@p-stg.iam.gserviceaccount.com"
		planLine = "tofu plan -input=false -no-color -out=WS/stack.plan -var environment=stg"
		showLine = "tofu show -json WS/stack.plan"
	)
	tests := []struct {
		name          string
		subs          map[string]string
		pins          map[string]string
		authoritative bool
		wantOut       []string
		wantTofu      []string
		wantFact      string
		wantErr       string
	}{
		{
			name:     "a tag build plans, tests and appends the summary",
			subs:     tagSubs(),
			pins:     enabledPins(),
			wantOut:  []string{"=== stg's stack at 3-app/quill/stg as quill-apply@p-stg.iam.gserviceaccount.com ===", "Plan: 2 to add, 1 to change, 1 to destroy.", `  update google_cloud_run_v2_service.app["uc1"]`, "  delete+create google_secret_manager_secret.old", "Tests passed: no authoritative IAM resource; 2 pinned secret version(s) exist and are enabled."},
			wantTofu: []string{initLine, planLine, showLine},
			wantFact: "Plan: 2 to add, 1 to change, 1 to destroy.",
		},
		{
			name:     "a disabled version is refused before the apply",
			subs:     tagSubs(),
			pins:     map[string]string{cookieVersion: "DISABLED", dbVersion: enabledState},
			wantTofu: []string{initLine, planLine, showLine},
			wantErr:  "Build REJECTED: a planned revision template pins secret version projects/p-stg/secrets/quill-cookie-key/versions/3, which is DISABLED; a revision would fail to start on it",
		},
		{
			name:     "a version that does not exist is refused",
			subs:     tagSubs(),
			pins:     map[string]string{dbVersion: enabledState},
			wantTofu: []string{initLine, planLine, showLine},
			wantErr:  "pins secret version projects/p-stg/secrets/quill-cookie-key/versions/3, which could not be read",
		},
		{
			name:          "an authoritative IAM resource in the stack is refused",
			subs:          tagSubs(),
			pins:          enabledPins(),
			authoritative: true,
			wantTofu:      []string{initLine, planLine, showLine},
			wantErr:       "Build REJECTED: the stack declares an authoritative IAM resource, which replaces every member of its role on each apply: iam.tf:1 google_project_iam_binding.all",
		},
		{
			name:    "a build without an apply identity is refused",
			subs:    map[string]string{appSub: "quill", envSub: stgEnvironment},
			wantErr: "build.json names no apply identity (_APPLY_IDENTITY)",
		},
		{
			name:    "a pull-request build does nothing here",
			subs:    stackSubs(),
			wantOut: []string{pullRequestBuildNotice},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workspaceFiles(t, map[string]string{EnvironmentFile: "export SKIP_DEPLOY=\"\"\n", BuildFile: buildFor(t, tt.subs)})
			if tt.authoritative {
				dir := filepath.Join(string(w), stackDir)
				if err := os.MkdirAll(dir, 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "iam.tf"), []byte(`resource "google_project_iam_binding" "all" {}`+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			run := &fakeRunner{outputs: map[string]string{"tofu show": stackPlanJSON}}
			secrets := &fakeSecrets{states: tt.pins}
			var out strings.Builder
			err := PlanEnvironmentStack(t.Context(), &Clients{Exec: run, SecretsAs: secrets.openAs}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("PlanEnvironmentStack() error = %v, want %q; output:\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("PlanEnvironmentStack() error = %v; output:\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			lines := strings.ReplaceAll(strings.Join(run.lines(), "\n"), string(w), "WS")
			if diff := cmp.Diff(strings.Join(tt.wantTofu, "\n"), lines); diff != "" {
				t.Errorf("tofu (-want +got):\n%s", diff)
			}
			env, err := w.Environment()
			if err != nil {
				t.Fatal(err)
			}
			if env[stackPlanFact] != tt.wantFact {
				t.Errorf("%s = %q, want %q", stackPlanFact, env[stackPlanFact], tt.wantFact)
			}
			if tt.wantFact != "" {
				if _, err := os.Stat(filepath.Join(string(w), StackPlanJSONFile)); err != nil {
					t.Errorf("the plan's JSON is not in the workspace: %v", err)
				}
			}
		})
	}
}

func TestApplyEnvironmentStack(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		subs     map[string]string
		planJSON string
		wantOut  []string
		wantTofu []string
		wantErr  string
	}{
		{
			name:     "the saved plan is applied as the apply identity",
			subs:     tagSubs(),
			planJSON: stackPlanJSON,
			wantOut:  []string{"Applied stg's stack: 2 added, 1 changed, 1 destroyed."},
			wantTofu: []string{"tofu apply -input=false -no-color WS/stack.plan"},
		},
		{
			name:     "a plan with no change applies nothing",
			subs:     tagSubs(),
			planJSON: `{"resource_changes": []}`,
			wantOut:  []string{"Nothing to apply: stg's stack matches the code."},
		},
		{
			name:    "a build whose plan step did not run is refused",
			subs:    tagSubs(),
			wantErr: "stack-plan.json (deploy stack plan writes it)",
		},
		{
			name:    "a pull-request build does nothing here",
			subs:    stackSubs(),
			wantOut: []string{pullRequestBuildNotice},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := map[string]string{EnvironmentFile: "export SKIP_DEPLOY=\"\"\n", BuildFile: buildFor(t, tt.subs)}
			if tt.planJSON != "" {
				files[StackPlanJSONFile] = tt.planJSON
			}
			w := workspaceFiles(t, files)
			run := &fakeRunner{}
			var out strings.Builder
			err := ApplyEnvironmentStack(t.Context(), &Clients{Exec: run}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("ApplyEnvironmentStack() error = %v, want %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("ApplyEnvironmentStack() error = %v; output:\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			lines := strings.ReplaceAll(strings.Join(run.lines(), "\n"), string(w), "WS")
			if diff := cmp.Diff(strings.Join(tt.wantTofu, "\n"), lines); diff != "" {
				t.Errorf("tofu (-want +got):\n%s", diff)
			}
			env, err := w.Environment()
			if err != nil {
				t.Fatal(err)
			}
			if env[environmentApplied] != "" {
				t.Errorf("the apply appended %s", environmentApplied)
			}
		})
	}
}

// environmentApplied is a fact the apply never writes: the record reads the plan's JSON.
const environmentApplied = "STACK_APPLIED"

func TestPlanEnvironments(t *testing.T) {
	t.Parallel()

	prSubs := func() map[string]string {
		subs := stackSubs()
		subs[environmentsSub] = "tst,stg,prd"
		subs[planIdentitiesSub] = "tst=quill-plan@p-tst.iam.gserviceaccount.com,stg=quill-plan@p-stg.iam.gserviceaccount.com,prd=quill-plan@p-prd.iam.gserviceaccount.com"

		return subs
	}
	initOf := func(env, project string) string {
		return "tofu init -input=false -no-color -backend-config=prefix=3-app/quill/" + env + " -backend-config=impersonate_service_account=quill-plan@" + project + ".iam.gserviceaccount.com"
	}
	planOf := func(env string) string {
		return "tofu plan -input=false -no-color -lock=false -out=WS/environment-" + env + ".plan -var environment=" + env
	}
	showOf := func(env string) string {
		return "tofu show -json WS/environment-" + env + ".plan"
	}
	every := []string{initOf("tst", "p-tst"), planOf("tst"), showOf("tst"), initOf("stg", "p-stg"), planOf("stg"), showOf("stg"), initOf("prd", "p-prd"), planOf("prd"), showOf("prd")}
	tests := []struct {
		name        string
		env         string
		subs        map[string]string
		pins        map[string]string
		deployer    bool
		planErr     error
		wantOut     []string
		wantTofu    []string
		wantComment []string
		wantErr     string
	}{
		{
			name:        "every environment is planned as its plan identity, the summaries said on the pull request",
			subs:        prSubs(),
			pins:        enabledPins(),
			deployer:    true,
			wantOut:     []string{"=== tst's stack at 3-app/quill/tst as quill-plan@p-tst.iam.gserviceaccount.com ===", "stg: Plan: 2 to add, 1 to change, 1 to destroy.", "=== prd's stack at 3-app/quill/prd as quill-plan@p-prd.iam.gserviceaccount.com ==="},
			wantTofu:    every,
			wantComment: []string{"The stack's plan for each environment (build b-1), the same tests passed in each:", "**tst**: Plan: 2 to add, 1 to change, 1 to destroy.\n  - `update google_cloud_run_v2_service.app[\"uc1\"]`", "**prd**: Plan: 2 to add, 1 to change, 1 to destroy."},
		},
		{
			name:     "without a deployer app the summaries stay in the log",
			subs:     prSubs(),
			pins:     enabledPins(),
			wantOut:  []string{"prd: Plan: 2 to add, 1 to change, 1 to destroy."},
			wantTofu: every,
		},
		{
			name: "an environment without a plan identity is refused before any plan",
			subs: func() map[string]string {
				subs := prSubs()
				subs[planIdentitiesSub] = "tst=quill-plan@p-tst.iam.gserviceaccount.com,stg=,prd=quill-plan@p-prd.iam.gserviceaccount.com"

				return subs
			}(),
			wantErr: "build.json names no plan identity for stg (_PLAN_IDENTITIES): apply 2-env for stg with this bedrock",
		},
		{
			name:        "a failing test is said on the pull request with the repository's token",
			env:         "export GITHUB_TOKEN=\"test-token\"\n",
			subs:        prSubs(),
			pins:        map[string]string{cookieVersion: "DESTROYED", dbVersion: enabledState},
			wantTofu:    []string{initOf("tst", "p-tst"), planOf("tst"), showOf("tst")},
			wantComment: []string{"The plan of tst's stack failed a test (build b-1): Build REJECTED: a planned revision template pins secret version projects/p-stg/secrets/quill-cookie-key/versions/3, which is DESTROYED"},
			wantErr:     "which is DESTROYED",
		},
		{
			name:        "a failing plan is said on the pull request",
			env:         "export GITHUB_TOKEN=\"test-token\"\n",
			subs:        prSubs(),
			planErr:     errors.New("tofu failed: fake"),
			wantTofu:    []string{initOf("tst", "p-tst"), planOf("tst")},
			wantComment: []string{"The plan of tst's stack failed; the build log says why (build b-1)."},
			wantErr:     "fake",
		},
		{
			name:    "a teardown plans nothing",
			env:     "export DOWN=\"true\"\n",
			subs:    prSubs(),
			wantOut: []string{"/gcbrun down: nothing to plan for the environments."},
		},
		{
			name:    "a tag build does nothing here",
			subs:    tagSubs(),
			wantOut: []string{tagBuildNotice},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			subs := tt.subs
			secrets := &fakeSecrets{states: tt.pins}
			if tt.deployer {
				subs, secrets = deployerSubs(subs), deployerSecrets(t)
				secrets.states = tt.pins
			}
			w := workspaceFiles(t, map[string]string{EnvironmentFile: "export SKIP_DEPLOY=\"\"\n" + tt.env, BuildFile: buildFor(t, subs)})
			run := &fakeRunner{outputs: map[string]string{"tofu show": stackPlanJSON}, fail: map[string]error{"tofu plan": tt.planErr}}
			repo := &githubtest.Repo{}
			_, gh := githubStandIn(t, repo)
			var out strings.Builder
			err := PlanEnvironments(t.Context(), &Clients{Exec: run, GitHub: gh, Secrets: secrets.open, SecretsAs: secrets.openAs}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("PlanEnvironments() error = %v, want %q; output:\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("PlanEnvironments() error = %v; output:\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			lines := strings.ReplaceAll(strings.Join(run.lines(), "\n"), string(w), "WS")
			if diff := cmp.Diff(strings.Join(tt.wantTofu, "\n"), lines); diff != "" {
				t.Errorf("tofu (-want +got):\n%s", diff)
			}
			comments := repo.Comments[7]
			if len(tt.wantComment) == 0 {
				if len(comments) != 0 {
					t.Errorf("posted %q, want nothing", comments[0].Body)
				}

				return
			}
			if len(comments) != 1 {
				t.Fatalf("posted %d comment(s), want one", len(comments))
			}
			containsAll(t, comments[0].Body, tt.wantComment...)
			if _, err := os.Stat(filepath.Join(string(w), stackDir, ".terraform")); !os.IsNotExist(err) {
				t.Errorf("the backend cache stays after the plans: %v", err)
			}
		})
	}
}
