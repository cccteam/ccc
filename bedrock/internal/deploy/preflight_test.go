package deploy

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

// TestMigratePreflight is the migrate step's pre-flight before the wait for the window:
// the release's migrate command run once with -version on the worker, in a run that waits
// for the window and replaces no database; nothing else, and nothing applied.
func TestMigratePreflight(t *testing.T) {
	t.Parallel()

	needed := migrateEnvironment + "export WINDOW_NEEDED=\"true\"\n"
	build, err := json.Marshal(Build{ID: "b-1", Substitutions: map[string]string{"_PROJECT": "tst-project", "_ENV": "tst", "COMMIT_SHA": "deadbeef", "REPO_NAME": "harbor", "_SEED": "false", "_PR_NUMBER": ""}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		env  string
		// fail fails the command; noProgram leaves the worker without it.
		fail      bool
		noProgram bool
		// wantOut are lines the output carries; wantRuns the command's runs, each its
		// arguments.
		wantOut  []string
		wantRuns [][]string
		wantErr  string
	}{
		{
			name:    "a run that waits for no window has no pre-flight",
			env:     migrateEnvironment,
			wantOut: []string{"No pre-flight: this run waits for no maintenance window."},
		},
		{
			name:    "a restore run has no pre-flight, its database just replaced",
			env:     needed + "export RESTORE=\"empty\"\n",
			wantOut: []string{"No pre-flight: a restore run replaced the database, which the migrations fill from the start."},
		},
		{
			name:     "a window release runs the command once with -version",
			env:      needed,
			wantOut:  []string{"=== Pre-flight: running the migrate command once with -version before the window ===", "Migrate command done in", "Pre-flight passed: the release's migrate command loads its configuration and reaches the environment's database; the migrations run after the window."},
			wantRuns: [][]string{{versionArg}},
		},
		{
			name:     "a command that fails before the window stops the run with nothing changed",
			env:      needed,
			fail:     true,
			wantOut:  []string{"Pre-flight failed before the window: nothing changed, and the run stops here."},
			wantRuns: [][]string{{versionArg}},
			wantErr:  "the migration failed after 0s: the migrate command's lines are above (exit status 1)",
		},
		{
			name:      "a worker without the migrate command is refused",
			env:       needed,
			noProgram: true,
			wantErr:   "no migrate command at",
		},
		{
			name:    "a torn-down environment does nothing",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			wantOut: []string{tornDown},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: string(build)})
			program := migrateProgramFile(t)
			if tt.noProgram {
				program = filepath.Join(t.TempDir(), "absent")
			}
			run := &fakeRunner{}
			if tt.fail {
				run.fail = map[string]error{program + " " + versionArg: errors.New("exit status 1")}
			}
			var out strings.Builder
			err := Migrate(t.Context(), &Clients{Exec: run}, w, program, versionVariable, true, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Migrate() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("Migrate() error = %v; output:\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			if diff := cmp.Diff(tt.wantRuns, commandRuns(run, program)); diff != "" {
				t.Errorf("runs mismatch (-want +got):\n%s", diff)
			}
			env, err := w.Environment()
			if err != nil {
				t.Fatal(err)
			}
			if env[skipReasonFact] != "" || env[forcedTableFact] != "" {
				t.Errorf("the pre-flight left facts: %s=%q %s=%q", skipReasonFact, env[skipReasonFact], forcedTableFact, env[forcedTableFact])
			}
		})
	}
}
