package deploy

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

// fakeTasks records the queue verbs it was asked for.
type fakeTasks struct {
	verbs []string
	fail  error
	// state is what State answers; RUNNING when unset.
	state string
}

func (f *fakeTasks) State(_ context.Context, queue string) (string, error) {
	f.verbs = append(f.verbs, "state "+shortName(queue))
	if f.state == "" {
		return "RUNNING", nil
	}

	return f.state, nil
}

func (f *fakeTasks) open(context.Context) (Tasks, error) {
	return f, nil
}

func (f *fakeTasks) Pause(_ context.Context, queue string) (string, error) {
	return f.verb("pause", queue)
}

func (f *fakeTasks) Purge(_ context.Context, queue string) (string, error) {
	return f.verb("purge", queue)
}

func (f *fakeTasks) Resume(_ context.Context, queue string) (string, error) {
	return f.verb("resume", queue)
}

func (f *fakeTasks) verb(verb, queue string) (string, error) {
	if f.fail != nil {
		return "", f.fail
	}
	f.verbs = append(f.verbs, verb+" "+shortName(queue))
	if verb == "resume" {
		return "RUNNING", nil
	}

	return "PAUSED", nil
}

// fakeMetrics answers the active instances per revision: the counts, in order, for every
// read of a revision; a revision it does not list reports no point.
type fakeMetrics struct {
	counts map[string][]int
	reads  int
	fail   error
}

func (f *fakeMetrics) open(context.Context) (Metrics, error) {
	return f, nil
}

func (f *fakeMetrics) ActiveInstances(_ context.Context, _, revision string, _ time.Duration) (count int, known bool, err error) {
	f.reads++
	if f.fail != nil {
		return 0, false, f.fail
	}
	counts, ok := f.counts[revision]
	if !ok || len(counts) == 0 {
		return 0, false, nil
	}
	count = counts[0]
	if len(counts) > 1 {
		f.counts[revision] = counts[1:]
	}

	return count, true, nil
}

// probeAnswers is an HTTP transport answering the probe URL with a status and a marker,
// in order; the last answer repeats.
type probeAnswers struct {
	answers []probeAnswer
	asked   int
}

type probeAnswer struct {
	status int
	marker string
}

func (p *probeAnswers) RoundTrip(req *http.Request) (*http.Response, error) {
	a := p.answers[min(p.asked, len(p.answers)-1)]
	p.asked++
	resp := &http.Response{StatusCode: a.status, Header: http.Header{}, Body: http.NoBody, Request: req}
	if a.marker != "" {
		resp.Header.Set(maintenanceHeader, a.marker)
	}

	return resp, nil
}

// noSleep is the clients' waiting in tests: none.
func noSleep(context.Context, time.Duration) error {
	return nil
}

func TestMaintenanceOn(t *testing.T) {
	t.Parallel()

	const (
		central     = "projects/tst-project/locations/us-central1/services/harbor-app"
		jobTemplate = "projects/tst-project/locations/us-central1/jobs/harbor-jobs"
		queue       = "projects/tst-project/locations/us-central1/queues/harbor-tasks"
		restoring   = "export SKIP_DEPLOY=\"\"\nexport SERVICES=\"us-central1=harbor-app\"\nexport IMAGE=\"reg/harbor\"\nexport IMAGE_DIGEST=\"sha256:abc\"\nexport VERSION=\"v0.2.2\"\nexport RESTORE=\"empty\"\nexport RESTORE_REQUESTER=\"octocat\"\n"
		serving     = "export SKIP_DEPLOY=\"\"\nexport SERVICES=\"us-central1=harbor-app\"\nexport IMAGE=\"reg/harbor\"\nexport IMAGE_DIGEST=\"sha256:abc\"\nexport VERSION=\"v0.2.2\"\nexport RESTORE=\"\"\n"
		live        = `{"app":"harbor","env":"tst","version":"v0.2.1","status":"live","build":"b-0","timestamp":"2026-10-01T10:00:00Z"}`
	)
	subs := map[string]string{"_PROJECT": "tst-project", "_ENV": "tst", "COMMIT_SHA": "deadbeef", "REPO_NAME": "harbor", "_HOSTNAME": "harbor-tst.example.dev", "_APP": "harbor", "_RECORDS_BUCKET": "tst-records", "_JOBS_JOB": "us-central1=harbor-jobs", "_TASKS_QUEUE": queue}
	running := map[string]any{keyName: jobTemplate + "-v0-2-1/executions/harbor-jobs-v0-2-1-abc", "startTime": "2026-10-01T10:00:00Z"}
	ended := map[string]any{keyName: jobTemplate + "-v0-2-1/executions/harbor-jobs-v0-2-1-def", "completionTime": "2026-10-01T10:01:00Z"}
	tests := []struct {
		name    string
		env     string
		subs    map[string]string
		answers []probeAnswer
		// executions are the serving build's job executions; records the live records.
		executions []map[string]any
		records    map[string]string
		counts     map[string][]int
		wantOut    []string
		// wantEnv are the maintenance facts the step leaves; wantVerbs the queue verbs, in
		// order; wantCanceled the executions canceled; wantMaintenanceVar says the new
		// revision carries the variable set to 1 and the traffic moved to it.
		wantEnv            []string
		wantVerbs          []string
		wantCanceled       []string
		wantMaintenanceVar bool
		wantErr            string
	}{
		{
			name:    "a run without a restore keeps the application serving",
			env:     serving,
			subs:    subs,
			wantOut: []string{"No maintenance: this run keeps the application serving"},
		},
		{
			name:       "a restore run starts the maintenance revision, probes it, moves traffic, pauses and purges the queue, cancels the running execution and waits out the old revision",
			env:        restoring,
			subs:       subs,
			answers:    []probeAnswer{{status: http.StatusOK}, {status: http.StatusServiceUnavailable, marker: "1"}},
			executions: []map[string]any{running, ended},
			records:    map[string]string{"gs://tst-records/harbor/tst/v0.2.1/b-0.json": live, "gs://tst-records/harbor/tst/pr5-abc0123/b-8.json": strings.NewReplacer("v0.2.1", "pr5@abc0123", "b-0", "b-8", "10:00:00Z", "11:00:00Z").Replace(live)},
			counts:     map[string][]int{"harbor-app-00007-prev": {2, 0}},
			wantOut: []string{
				"=== Maintenance on: tst's database is replaced (empty) before v0.2.2 deploys",
				"Maintenance revision [harbor-app-00008-new] deployed to [harbor-app] in [us-central1] under the tag [next], with APP_MAINTENANCE=1 and no traffic.",
				"Probe passed: https://harbor-tst-next.example.dev/ answered 503 with X-Maintenance: 1 from the maintenance revision.",
				"Traffic in [us-central1] moved to the maintenance revision",
				"Queue harbor-tasks paused (PAUSED)",
				"Queue harbor-tasks purged",
				"Canceled the running execution harbor-jobs-v0-2-1-abc of v0.2.1's job",
				"2 active instance(s) still finish requests on the old revision(s); waiting.",
				"Requests in flight finished: no active instance on the old revision(s)",
				"Maintenance is on: 1 revision(s) serve the maintenance page",
			},
			wantEnv:            []string{"export MAINTENANCE=\"true\"\n", "export MAINTENANCE_QUEUE=\"" + queue + "\"\n", "export MAINTENANCE_PURGED=\"true\"\n", "export MAINTENANCE_CANCELED=\"1\"\n"},
			wantVerbs:          []string{"pause harbor-tasks", "purge harbor-tasks"},
			wantCanceled:       []string{jobTemplate + "-v0-2-1/executions/harbor-jobs-v0-2-1-abc"},
			wantMaintenanceVar: true,
		},
		{
			name:    "a maintenance revision that ignores the variable stops the run before traffic moves",
			env:     restoring,
			subs:    subs,
			answers: []probeAnswer{{status: http.StatusOK}},
			wantErr: "Build REJECTED: the maintenance revision ignores APP_MAINTENANCE: https://harbor-tst-next.example.dev/ answered 200 with X-Maintenance: \"\" after 3m0s; the application's main does not check maintenance.Requested() before it builds its configuration (impulse check maintenance-switch names the fix). Traffic did not move.",
		},
		{
			name:    "without a queue, a job process or a live record there is nothing to pause or cancel, and no reported instance ends the wait at once",
			env:     restoring,
			subs:    map[string]string{"_PROJECT": "tst-project", "_ENV": "tst", "COMMIT_SHA": "deadbeef", "REPO_NAME": "harbor", "_HOSTNAME": "harbor-tst.example.dev", "_APP": "harbor", "_RECORDS_BUCKET": "tst-records"},
			answers: []probeAnswer{{status: http.StatusServiceUnavailable, marker: "1"}},
			wantOut: []string{"No task queue to pause: the stack names none (_TASKS_QUEUE).", "No job executions to cancel: the application has no job process (_JOBS_JOB).", "Requests in flight finished: no active instance reported on the old revision(s)"},
			wantEnv: []string{"export MAINTENANCE=\"true\"\n", "export MAINTENANCE_CANCELED=\"0\"\n"},
		},
		{
			name:    "a pull-request build never goes into maintenance",
			env:     restoring,
			subs:    map[string]string{"_PROJECT": "tst-project", "_ENV": "tst", "_PR_NUMBER": "7"},
			wantOut: []string{"No maintenance: a pull-request build never goes into maintenance."},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: buildFor(t, tt.subs)})
			resources := map[string]map[string]any{central: serviceDoc(central)}
			if len(tt.executions) > 0 {
				resources[jobTemplate+"-v0-2-1"] = map[string]any{keyName: jobTemplate + "-v0-2-1"}
				for _, e := range tt.executions {
					resources[text(e, keyName)] = e
				}
			}
			run := newFakeRun(resources)
			tasks := &fakeTasks{}
			metrics := &fakeMetrics{counts: tt.counts}
			store := &memoryStore{objects: map[string]string{}}
			for p, c := range tt.records {
				store.objects[p] = c
			}
			probe := &probeAnswers{answers: tt.answers}
			if len(tt.answers) == 0 {
				probe.answers = []probeAnswer{{status: http.StatusBadGateway}}
			}
			clients := &Clients{Run: run.open, Tasks: tasks.open, Metrics: metrics.open, Storage: store.open, HTTP: &http.Client{Transport: probe}, Sleep: noSleep}
			var out strings.Builder
			err := MaintenanceOn(t.Context(), clients, w, false, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("MaintenanceOn() error = %v, want %q", err, tt.wantErr)
				}
				if len(run.patches[central]) > 1 {
					t.Errorf("traffic moved after a failed probe: %d patches", len(run.patches[central]))
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
			if tt.wantMaintenanceVar {
				template, _ := run.patches[central][0][keyTemplate].(map[string]any)
				container, err := firstContainer(template)
				if err != nil {
					t.Fatal(err)
				}
				vars, _ := container["env"].([]any)
				var value string
				for _, v := range vars {
					if m, _ := v.(map[string]any); text(m, keyName) == "APP_MAINTENANCE" {
						value = text(m, "value")
					}
				}
				if value != "1" {
					t.Errorf("APP_MAINTENANCE = %q on the maintenance revision, want 1", value)
				}
				traffic, _ := run.patches[central][len(run.patches[central])-1][keyTraffic].([]any)
				if first, _ := traffic[0].(map[string]any); text(first, keyRevision) != "harbor-app-00008-new" || first[keyPercent] != fullTraffic {
					t.Errorf("traffic after the move = %v", traffic)
				}
			}
			if len(tt.wantEnv) > 0 {
				data, err := os.ReadFile(filepath.Join(string(w), EnvironmentFile))
				if err != nil {
					t.Fatal(err)
				}
				containsAll(t, string(data), tt.wantEnv...)
			}
		})
	}
}

func TestMaintenanceOff(t *testing.T) {
	t.Parallel()

	const queue = "projects/tst-project/locations/us-central1/queues/harbor-tasks"
	tests := []struct {
		name string
		env  string
		// queue is the queue the build's stack names (_TASKS_QUEUE); state what it is in.
		queue string
		state string
		// pullRequest is the build's _PR_NUMBER: set, the build is a pull request's.
		pullRequest string
		wantOut     []string
		wantVerbs   []string
	}{
		{
			name:    "a run that was not in maintenance has nothing to end",
			env:     "export MAINTENANCE=\"\"\nexport VERSION=\"v0.2.2\"\n",
			wantOut: []string{"No maintenance to end: the application served throughout."},
		},
		{
			name:        "a pull-request build leaves the environment's paused queue alone",
			env:         "export MAINTENANCE=\"\"\nexport VERSION=\"pr70@a6c3d70\"\n",
			queue:       queue,
			state:       "PAUSED",
			pullRequest: "70",
			wantOut:     []string{"No maintenance to end: a pull-request build never goes into maintenance, and the environment's queue is left as it is."},
		},
		{
			name:      "a run that was not in maintenance leaves a delivering queue alone",
			env:       "export MAINTENANCE=\"\"\nexport VERSION=\"v0.2.2\"\n",
			queue:     queue,
			wantOut:   []string{"No maintenance to end: the application served throughout."},
			wantVerbs: []string{"state harbor-tasks"},
		},
		{
			name:      "a run that was not in maintenance resumes a queue an earlier run's maintenance left paused",
			env:       "export MAINTENANCE=\"\"\nexport VERSION=\"v0.2.4\"\n",
			queue:     queue,
			state:     "PAUSED",
			wantOut:   []string{"Queue harbor-tasks was left paused by an earlier run's maintenance; resumed (RUNNING): tasks are delivered again, to v0.2.4.", "No maintenance to end: the application served throughout."},
			wantVerbs: []string{"state harbor-tasks", "resume harbor-tasks"},
		},
		{
			name:      "the queue is resumed and maintenance is over",
			env:       "export MAINTENANCE=\"true\"\nexport MAINTENANCE_QUEUE=\"" + queue + "\"\nexport MAINTENANCE_REVISIONS=\"us-central1=harbor-app-00008-maint\"\nexport VERSION=\"v0.2.2\"\n",
			wantOut:   []string{"Queue harbor-tasks resumed (RUNNING): tasks are delivered again, to v0.2.2.", "=== Maintenance off: tst serves v0.2.2; the maintenance revision(s) us-central1=harbor-app-00008-maint take no traffic ==="},
			wantVerbs: []string{"resume harbor-tasks"},
		},
		{
			name:    "without a queue nothing is resumed",
			env:     "export MAINTENANCE=\"true\"\nexport MAINTENANCE_REVISIONS=\"us-central1=harbor-app-00008-maint\"\nexport VERSION=\"v0.2.2\"\n",
			wantOut: []string{"=== Maintenance off: tst serves v0.2.2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			subs := map[string]string{"_PROJECT": "tst-project", "_ENV": "tst"}
			if tt.queue != "" {
				subs["_TASKS_QUEUE"] = tt.queue
			}
			if tt.pullRequest != "" {
				subs["_PR_NUMBER"] = tt.pullRequest
			}
			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: buildFor(t, subs)})
			tasks := &fakeTasks{state: tt.state}
			var out strings.Builder
			if err := MaintenanceOff(t.Context(), &Clients{Tasks: tasks.open}, w, &out); err != nil {
				t.Fatalf("MaintenanceOff() error = %v", err)
			}
			containsAll(t, out.String(), tt.wantOut...)
			if diff := cmp.Diff(tt.wantVerbs, tasks.verbs); diff != "" {
				t.Errorf("queue verbs (-want +got):\n%s", diff)
			}
		})
	}
}
