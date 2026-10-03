package deploy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestMigratePreflight is the migrate step's pre-flight before the wait for the window:
// the build's job run once with -version and kept, in a run that waits for the window
// and replaces no database; nothing else.
func TestMigratePreflight(t *testing.T) {
	t.Parallel()

	const (
		template    = "projects/tst-project/locations/us-central1/jobs/harbor-migrate"
		jobName     = template + "-v1-2-3"
		environment = "export SKIP_DEPLOY=\"\"\nexport RUN_MIGRATIONS=\"true\"\nexport MIGRATE_JOB=\"us-central1=harbor-migrate\"\nexport VERSION=\"v1.2.3\"\n"
		needed      = environment + "export WINDOW_NEEDED=\"true\"\n"
	)
	build, err := json.Marshal(Build{ID: "b-1", Substitutions: map[string]string{"_PROJECT": "tst-project", "_ENV": "tst", "COMMIT_SHA": "deadbeef", "REPO_NAME": "harbor", "_SEED": "false", "_PR_NUMBER": ""}})
	if err != nil {
		t.Fatal(err)
	}
	made := func() map[string]map[string]any {
		doc := jobDoc()
		doc[keyName] = jobName

		return map[string]map[string]any{jobName: doc}
	}
	tests := []struct {
		name string
		env  string
		run  *fakeRun
		// wantOut are lines the output carries; wantRuns the job's runs, each its
		// arguments; the job is never deleted.
		wantOut  []string
		wantRuns [][]string
		wantErr  string
	}{
		{
			name:    "a run that waits for no window has no pre-flight",
			env:     environment,
			run:     newFakeRun(made()),
			wantOut: []string{"No pre-flight: this run waits for no maintenance window."},
		},
		{
			name:    "a restore run has no pre-flight, its database just replaced",
			env:     needed + "export RESTORE=\"empty\"\n",
			run:     newFakeRun(made()),
			wantOut: []string{"No pre-flight: a restore run replaced the database, which the migrations fill from the start."},
		},
		{
			name:     "a window release runs the job once with -version and keeps it",
			env:      needed,
			run:      newFakeRun(made()),
			wantOut:  []string{"=== Pre-flight: running job [harbor-migrate-v1-2-3] once with -version before the window ===", "Migrate job done: execution harbor-migrate-v1-2-3-abc succeeded.", "Pre-flight passed: the migrate job runs on this image against the environment's database; the job stays for the migrations after the window."},
			wantRuns: [][]string{{versionArg}},
		},
		{
			name: "a job that fails before the window stops the run with nothing changed",
			env:  needed,
			run: func() *fakeRun {
				r := newFakeRun(made())
				r.failedRun = 1

				return r
			}(),
			wantOut:  []string{"Pre-flight failed before the window: nothing changed, and the run stops here."},
			wantRuns: [][]string{{versionArg}},
			wantErr:  "the migrate job failed: execution harbor-migrate-v1-2-3-failed has 1 failed task(s); its logs say why",
		},
		{
			name:    "a build whose job was not made is refused",
			env:     needed,
			run:     newFakeRun(map[string]map[string]any{template: jobDoc()}),
			wantErr: "this build's migrate job harbor-migrate-v1-2-3 does not exist: deploy jobs makes it right after the image build",
		},
		{
			name:    "a torn-down environment does nothing",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			run:     newFakeRun(made()),
			wantOut: []string{tornDown},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: string(build)})
			var out strings.Builder
			err := Migrate(t.Context(), &Clients{Run: tt.run.open, Sleep: noSleep}, w, true, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Migrate() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("Migrate() error = %v; output:\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			if diff := cmp.Diff(tt.wantRuns, tt.run.runs); diff != "" {
				t.Errorf("runs mismatch (-want +got):\n%s", diff)
			}
			if len(tt.run.deleted) != 0 {
				t.Errorf("the pre-flight deleted %v; the job stays for the migrations", tt.run.deleted)
			}
			if len(tt.run.patched) != 0 {
				t.Errorf("the pre-flight changed a job: %v", tt.run.patched)
			}
		})
	}
}
