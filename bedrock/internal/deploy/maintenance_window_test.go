package deploy

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestMaintenanceOnWindow is the maintenance step at its window position, after the gate:
// a breaking release takes every maintenance step, an ordinary release inside a window
// takes none, and the second look at the window refuses a window that closed meanwhile.
func TestMaintenanceOnWindow(t *testing.T) {
	t.Parallel()

	const (
		central     = "projects/tst-project/locations/us-central1/services/harbor-app"
		jobTemplate = "projects/tst-project/locations/us-central1/jobs/harbor-jobs"
		queue       = "projects/tst-project/locations/us-central1/queues/harbor-tasks"
		serving     = "export SKIP_DEPLOY=\"\"\nexport SERVICES=\"us-central1=harbor-app\"\nexport IMAGE=\"reg/harbor\"\nexport IMAGE_DIGEST=\"sha256:abc\"\nexport VERSION=\"v0.2.2\"\nexport RESTORE=\"\"\n"
		breaking    = serving + "export WINDOW_NEEDED=\"true\"\nexport WINDOW_BREAKING=\"true\"\nexport WINDOW_REASON=\"the default outlet answers 0.2.2 at the oldest, and tst runs v0.2.1, which it turns away\"\n"
		ordinary    = serving + "export WINDOW_NEEDED=\"true\"\nexport WINDOW_BREAKING=\"\"\nexport WINDOW_REASON=\"every release waits for tst's window (releases: all)\"\n"
		weekly      = `{"tst": {"timeZone": "America/Chicago", "weekly": [{"day": "Sunday", "from": "02:00", "to": "04:00"}]}}`
		live        = `{"app":"harbor","env":"tst","version":"v0.2.1","status":"live","build":"b-0","timestamp":"2026-10-01T10:00:00Z"}`
	)
	subs := map[string]string{"_PROJECT": "tst-project", "_ENV": "tst", "COMMIT_SHA": "deadbeef", "REPO_NAME": "harbor", "_HOSTNAME": "harbor-tst.example.dev", "_APP": "harbor", "_RECORDS_BUCKET": "tst-records", "_JOBS_JOB": "us-central1=harbor-jobs", "_TASKS_QUEUE": queue, "_SERVICES": "us-central1=harbor-app"}
	// running is the serving build's execution, made afresh per case: a cancel marks it ended.
	running := func() map[string]any {
		return map[string]any{keyName: jobTemplate + "-v0-2-1/executions/harbor-jobs-v0-2-1-abc", "startTime": "2026-10-01T10:00:00Z"}
	}
	tests := []struct {
		name          string
		env           string
		placement     string
		inMaintenance bool
		wantOut       []string
		// wantEnv are the maintenance facts the step leaves; wantVerbs the queue verbs in
		// order; wantCanceled the executions canceled; wantPatches how many changes the
		// service took (the maintenance revision, then the traffic move); wantProbed how
		// many times the revision was probed.
		wantEnv      []string
		wantVerbs    []string
		wantCanceled []string
		wantPatches  int
		wantProbed   int
		wantErr      string
	}{
		{
			name:      "a breaking release inside an open window takes every maintenance step, the queue paused and not purged",
			env:       breaking,
			placement: `{"tst": "anytime"}`,
			wantOut: []string{
				"Second look at the window: tst's window is open (anytime); maintenance begins.",
				"=== Maintenance on: v0.2.2 is a breaking release for tst (the default outlet answers 0.2.2 at the oldest, and tst runs v0.2.1, which it turns away), so the application serves its maintenance page while the database migrates ===",
				"Maintenance revision [harbor-app-00008-new] deployed to [harbor-app] in [us-central1] under the tag [next], with APP_MAINTENANCE=1 and no traffic.",
				"Probe passed: https://harbor-tst-next.example.dev/ answered 503 with X-Maintenance: 1 from the maintenance revision.",
				"Traffic in [us-central1] moved to the maintenance revision",
				"Queue harbor-tasks paused (PAUSED)",
				"Canceled the running execution harbor-jobs-v0-2-1-abc of v0.2.1's job: the run interrupts everything the application is doing.",
				"Maintenance is on: 1 revision(s) serve the maintenance page; the database may be replaced or migrated.",
			},
			wantEnv:      []string{"export MAINTENANCE=\"true\"\n", "export MAINTENANCE_QUEUE=\"" + queue + "\"\n", "export MAINTENANCE_PURGED=\"false\"\n", "export MAINTENANCE_CANCELED=\"1\"\n"},
			wantVerbs:    []string{"pause harbor-tasks"},
			wantCanceled: []string{jobTemplate + "-v0-2-1/executions/harbor-jobs-v0-2-1-abc"},
			wantPatches:  2,
			wantProbed:   1,
		},
		{
			name:      "an ordinary release inside a window takes no maintenance step at all",
			env:       ordinary,
			placement: `{"tst": "anytime"}`,
			wantOut:   []string{"No maintenance: v0.2.2 is not a breaking release and deploys the rolling way inside tst's window; no maintenance revision, no probe, no queue pause and no cancel."},
		},
		{
			name:      "a release that needed no window keeps the application serving",
			env:       serving,
			placement: `{"tst": "anytime"}`,
			wantOut:   []string{"No maintenance: this run keeps the application serving."},
		},
		{
			name:      "a run in maintenance already has nothing more to start",
			env:       breaking + "export MAINTENANCE=\"true\"\n",
			placement: `{"tst": "anytime"}`,
			wantOut:   []string{"Maintenance is on already: this run put the application into maintenance before its database was replaced; nothing more to start."},
		},
		{
			name:      "a window that closed before maintenance went on stops the run with nothing changed",
			env:       breaking,
			placement: weekly,
			wantErr:   "Build REJECTED: tst's maintenance window closed before maintenance went on (it next opens Sunday 2026-10-11 02:00 CDT); nothing changed, and the release deploys inside the next window: run it again then.",
		},
		{
			name:          "an environment in maintenance from an earlier run goes on with the window closed",
			env:           breaking,
			placement:     weekly,
			inMaintenance: true,
			wantOut:       []string{"Second look at the window: tst's window is open (in maintenance); maintenance begins.", "Maintenance is on: 1 revision(s) serve the maintenance page"},
			wantEnv:       []string{"export MAINTENANCE=\"true\"\n"},
			wantVerbs:     []string{"pause harbor-tasks"},
			wantCanceled:  []string{jobTemplate + "-v0-2-1/executions/harbor-jobs-v0-2-1-abc"},
			wantPatches:   2,
			wantProbed:    1,
		},
		{
			name:         "the gate's own word that the environment was in maintenance is taken",
			env:          breaking + "export WINDOW_SLOT=\"in maintenance\"\n",
			placement:    weekly,
			wantOut:      []string{"Second look at the window: tst's window is open (in maintenance); maintenance begins."},
			wantVerbs:    []string{"pause harbor-tasks"},
			wantCanceled: []string{jobTemplate + "-v0-2-1/executions/harbor-jobs-v0-2-1-abc"},
			wantPatches:  2,
			wantProbed:   1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: buildFor(t, subs), stackDir + "/" + placementFile: testPlacement(tt.placement)})
			doc := serviceDoc(central)
			if tt.inMaintenance {
				setMaintenanceVariable(t, doc)
			}
			execution := running()
			resources := map[string]map[string]any{central: doc, jobTemplate + "-v0-2-1": {keyName: jobTemplate + "-v0-2-1"}, text(execution, keyName): execution}
			run := newFakeRun(resources)
			tasks := &fakeTasks{}
			metrics := &fakeMetrics{counts: map[string][]int{"harbor-app-00007-prev": {0}}}
			store := &memoryStore{objects: map[string]string{"gs://tst-records/harbor/tst/v0.2.1/b-0.json": live}}
			probe := &probeAnswers{answers: []probeAnswer{{status: http.StatusServiceUnavailable, marker: "1"}}}
			clock := &fakeClock{now: chicago(10, 5, 10, 0)}
			clients := &Clients{Run: run.open, Tasks: tasks.open, Metrics: metrics.open, Storage: store.open, HTTP: &http.Client{Transport: probe}, Sleep: clock.Sleep, Now: clock.Now}
			var out strings.Builder
			err := MaintenanceOn(t.Context(), clients, w, true, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("MaintenanceOn() error = %v, want %q; output:\n%s", err, tt.wantErr, out.String())
				}
				if len(run.patches[central]) != 0 || len(tasks.verbs) != 0 || probe.asked != 0 {
					t.Errorf("the refused step changed something: %d patches, verbs %v, %d probes", len(run.patches[central]), tasks.verbs, probe.asked)
				}

				return
			}
			if err != nil {
				t.Fatalf("MaintenanceOn() error = %v; output:\n%s", err, out.String())
			}
			containsAll(t, out.String(), tt.wantOut...)
			if diff := cmp.Diff(tt.wantVerbs, tasks.verbs); diff != "" {
				t.Errorf("queue verbs (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantCanceled, run.canceled); diff != "" {
				t.Errorf("canceled (-want +got):\n%s", diff)
			}
			if len(run.patches[central]) != tt.wantPatches {
				t.Errorf("the service took %d change(s), want %d", len(run.patches[central]), tt.wantPatches)
			}
			if probe.asked != tt.wantProbed {
				t.Errorf("the revision was probed %d time(s), want %d", probe.asked, tt.wantProbed)
			}
			data, err := os.ReadFile(filepath.Join(string(w), EnvironmentFile))
			if err != nil {
				t.Fatal(err)
			}
			containsAll(t, string(data), tt.wantEnv...)
			if tt.wantPatches == 0 && strings.Contains(string(data), "export MAINTENANCE=\"true\"\n") && !strings.Contains(tt.env, "export MAINTENANCE=\"true\"\n") {
				t.Errorf("the step left MAINTENANCE set without going into maintenance:\n%s", data)
			}
		})
	}
}
