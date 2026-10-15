package deploy

import (
	"bytes"
	"strings"
	"testing"
)

// TestFirstApply: the stack trigger's step inits the environment's state prefix as the
// apply identity (impersonated when the build runs as the deploy identity, as itself
// otherwise), plans with the environment alone, applies the saved plan and says what it
// did; a plan with no change applies nothing; a stack a release has run on, which its
// deployment records say, is refused before tofu runs, as is a build whose trigger
// carries less than the step needs.
func TestFirstApply(t *testing.T) {
	t.Parallel()

	const (
		deployBuild = `{"id": "b-1", "serviceAccount": "projects/p-stg/serviceAccounts/quill-deploy@p-stg.iam.gserviceaccount.com", "substitutions": {"_APP": "quill", "_ENV": "tst", "_APPLY_IDENTITY": "quill-apply@p-stg.iam.gserviceaccount.com", "_RECORDS_BUCKET": "records"}}`
		applyBuild  = `{"id": "b-2", "serviceAccount": "projects/p-stg/serviceAccounts/quill-apply@p-stg.iam.gserviceaccount.com", "substitutions": {"_APP": "quill", "_ENV": "tst", "_APPLY_IDENTITY": "quill-apply@p-stg.iam.gserviceaccount.com", "_RECORDS_BUCKET": "records"}}`
		bareBuild   = `{"id": "b-3", "substitutions": {"_APP": "quill", "_ENV": "tst", "_APPLY_IDENTITY": "quill-apply@p-stg.iam.gserviceaccount.com"}}`
		initLine    = "tofu init -input=false -no-color -backend-config=prefix=3-app/quill/tst"
		planLine    = "tofu plan -input=false -no-color -out=WS/stack.plan -var environment=tst"
		showLine    = "tofu show -json WS/stack.plan"
		applyLine   = "tofu apply -input=false -no-color WS/stack.plan"
	)
	tests := []struct {
		name    string
		build   string
		objects map[string]string
		// shown is what tofu show answers for the saved plan.
		shown      string
		wantOut    []string
		wantTofu   string
		wantEnv    string
		wantNoTofu bool
		wantErr    string
	}{
		{
			name:     "the first apply, as the deploy identity impersonating the apply identity",
			build:    deployBuild,
			shown:    stackPlanJSON,
			wantOut:  []string{"=== tst's stack at 3-app/quill/tst as quill-apply@p-stg.iam.gserviceaccount.com ===", "Applied tst's stack for the first time: 2 added, 1 changed, 1 destroyed. A release's build applies it from here on."},
			wantTofu: initLine + " -backend-config=impersonate_service_account=quill-apply@p-stg.iam.gserviceaccount.com\n" + planLine + "\n" + showLine + "\n" + applyLine,
			wantEnv:  "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT=quill-apply@p-stg.iam.gserviceaccount.com",
		},
		{
			name:     "a build running as the apply identity impersonates nothing",
			build:    applyBuild,
			shown:    stackPlanJSON,
			wantOut:  []string{"Applied tst's stack for the first time"},
			wantTofu: initLine + "\n" + planLine + "\n" + showLine + "\n" + applyLine,
		},
		{
			name:     "a plan with no change applies nothing",
			build:    deployBuild,
			shown:    `{"resource_changes": []}`,
			wantOut:  []string{"Nothing to apply: tst's stack matches the code already."},
			wantTofu: initLine + " -backend-config=impersonate_service_account=quill-apply@p-stg.iam.gserviceaccount.com\n" + planLine + "\n" + showLine,
			wantEnv:  "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT=quill-apply@p-stg.iam.gserviceaccount.com",
		},
		{
			name:     "a stack with an id and no release record is applied",
			build:    deployBuild,
			objects:  map[string]string{"gs://records/quill/stacks/tst/stack.json": `{"id": "ab12"}`},
			shown:    stackPlanJSON,
			wantOut:  []string{"Applied tst's stack for the first time"},
			wantTofu: initLine + " -backend-config=impersonate_service_account=quill-apply@p-stg.iam.gserviceaccount.com\n" + planLine + "\n" + showLine + "\n" + applyLine,
			wantEnv:  "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT=quill-apply@p-stg.iam.gserviceaccount.com",
		},
		{
			name:       "a stack a release has run on is refused before tofu runs",
			build:      deployBuild,
			objects:    map[string]string{"gs://records/quill/stacks/tst/stack.json": `{"id": "ab12"}`, "gs://records/quill/tst/ab12/releases/v1.0.0/b-0.json": `{}`},
			wantNoTofu: true,
			wantErr:    "tst's stack has deployment records (1 under gs://records/quill/tst/ab12/releases/): a release has run there, and a release's build applies the stack with the generated secrets' pins this first apply does not carry. To apply the stack again, run the live release again (the operations workflow's run action)",
		},
		{
			name:       "a trigger that carries less than the step needs is refused",
			build:      bareBuild,
			wantNoTofu: true,
			wantErr:    "_RECORDS_BUCKET is not set: the stack trigger carries it",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := Workspace(t.TempDir())
			objects := map[string]string{}
			for k, v := range tt.objects {
				objects[k] = v
			}
			store := &memoryStore{objects: objects}
			builds := &fakeBuilds{build: tt.build}
			run := &fakeRunner{outputs: map[string]string{"tofu show": tt.shown}}
			var out bytes.Buffer
			err := FirstApply(t.Context(), &Clients{Builds: builds.open, Exec: run, Storage: store.open}, w, &BuildRequest{BuildID: "b", Project: "p", Location: "l"}, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("FirstApply() error = %v, wantErr %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("FirstApply() error = %v\n%s", err, out.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			lines := strings.ReplaceAll(strings.Join(run.lines(), "\n"), string(w), "WS")
			if tt.wantNoTofu {
				if lines != "" {
					t.Errorf("tofu ran:\n%s", lines)
				}

				return
			}
			if lines != tt.wantTofu {
				t.Errorf("tofu ran:\n%s\nwant:\n%s", lines, tt.wantTofu)
			}
			if tt.wantEnv != "" && (len(run.ran) == 0 || strings.Join(run.ran[0].Env, " ") != tt.wantEnv) {
				t.Errorf("init ran with env %v, want %q", run.ran[0].Env, tt.wantEnv)
			}
			if tt.wantEnv == "" && len(run.ran) > 0 && len(run.ran[0].Env) > 0 {
				t.Errorf("init ran with env %v, want none", run.ran[0].Env)
			}
		})
	}
}
