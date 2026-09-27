package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

// fakeRun is Run in tests: resources by name, what was patched (the last patch, and
// every patch with its update mask in order) and run. A patch of a template creates a
// revision, as the API does; a patch of the traffic settles the statuses onto it.
type fakeRun struct {
	resources map[string]map[string]any
	patched   map[string]map[string]any
	patches   map[string][]map[string]any
	fields    map[string][]string
	fieldsOf  map[string][][]string
	ran       map[string][]string
	execution map[string]any
	runErr    error
}

func newFakeRun(resources map[string]map[string]any) *fakeRun {
	return &fakeRun{resources: resources, patched: map[string]map[string]any{}, patches: map[string][]map[string]any{}, fields: map[string][]string{}, fieldsOf: map[string][][]string{}, ran: map[string][]string{}}
}

func (r *fakeRun) open(context.Context) (Run, error) {
	return r, nil
}

func (r *fakeRun) Get(_ context.Context, name string) (map[string]any, error) {
	doc, ok := r.resources[name]
	if !ok {
		return nil, errors.Newf("Cloud Run answered HTTP 404 to GET /v2/%s: not found", name)
	}

	return doc, nil
}

func (r *fakeRun) Patch(_ context.Context, name string, resource map[string]any, fields ...string) (map[string]any, error) {
	r.patched[name] = resource
	r.patches[name] = append(r.patches[name], resource)
	r.fields[name] = fields
	r.fieldsOf[name] = append(r.fieldsOf[name], fields)
	doc := r.resources[name]
	if doc == nil {
		doc = map[string]any{}
		r.resources[name] = doc
	}
	if len(fields) == 0 {
		for key, value := range resource {
			doc[key] = value
		}
		if _, ok := resource["template"]; ok && strings.Contains(name, "/services/") {
			doc["latestCreatedRevision"] = name + "/revisions/" + shortName(name) + "-00008-new"
		}

		return doc, nil
	}
	for _, key := range fields {
		doc[key] = resource[key]
	}
	if traffic, ok := resource[keyTraffic].([]any); ok {
		statuses := make([]any, 0, len(traffic))
		for _, entry := range traffic {
			t, _ := entry.(map[string]any)
			status := map[string]any{keyType: t[keyType], keyRevision: t[keyRevision], keyPercent: t[keyPercent]}
			if t[keyType] == targetLatest {
				status[keyRevision] = shortName(text(doc, "latestReadyRevision"))
			}
			statuses = append(statuses, status)
		}
		doc["trafficStatuses"] = statuses
	}

	return doc, nil
}

func (r *fakeRun) RunJob(_ context.Context, name string, args []string) (map[string]any, error) {
	r.ran[name] = append([]string{}, args...)
	if r.runErr != nil {
		return nil, r.runErr
	}
	if r.execution != nil {
		return r.execution, nil
	}

	return map[string]any{"name": name + "/executions/" + shortName(name) + "-abc", "succeededCount": float64(1)}, nil
}

// jobDoc is a migrate job as the API answers it, the parts the step touches.
func jobDoc() map[string]any {
	return map[string]any{
		keyName:  "projects/tst-project/locations/us-central1/jobs/harbor-migrate",
		"labels": map[string]any{"terraform": "true", prNumberLabel: "2"},
		"template": map[string]any{
			"taskCount": float64(1),
			"template": map[string]any{
				"containers": []any{map[string]any{"image": "reg/harbor@sha256:old", "command": []any{"/app/migrate"}}},
				"timeout":    "600s",
			},
		},
	}
}

func TestMigrate(t *testing.T) {
	t.Parallel()

	const (
		jobName     = "projects/tst-project/locations/us-central1/jobs/harbor-migrate"
		environment = "export SKIP_DEPLOY=\"\"\nexport RUN_MIGRATIONS=\"true\"\nexport MIGRATE_JOB=\"us-central1=harbor-migrate\"\nexport IMAGE=\"reg/harbor\"\nexport IMAGE_DIGEST=\"sha256:abc\"\n"
	)
	build := func(seed, pr string) string {
		return fmt.Sprintf(`{"id": "b-1", "substitutions": {"_PROJECT": "tst-project", "_ENV": "tst", "COMMIT_SHA": "deadbeef", "REPO_NAME": "harbor", "_SEED": %q, "_PR_NUMBER": %q}}`, seed, pr)
	}
	tests := []struct {
		name      string
		env       string
		build     string
		run       *fakeRun
		wantOut   []string
		wantImage string
		// wantLabels are the job's labels after the update; wantArgs the run's arguments.
		wantLabels map[string]any
		wantArgs   []string
		wantErr    string
	}{
		{
			name:    "a torn-down environment does nothing",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			build:   build("true", "7"),
			run:     newFakeRun(map[string]map[string]any{}),
			wantOut: []string{tornDown},
		},
		{
			name:    "a build without migrations skips the job",
			env:     strings.Replace(environment, `RUN_MIGRATIONS="true"`, `RUN_MIGRATIONS="false"`, 1),
			build:   build("true", "7"),
			run:     newFakeRun(map[string]map[string]any{}),
			wantOut: []string{"Skipping the migrate job: this build does not run migrations."},
		},
		{
			name:       "a pull request's build seeds its new database",
			env:        environment,
			build:      build("true", "7"),
			run:        newFakeRun(map[string]map[string]any{jobName: jobDoc()}),
			wantOut:    []string{"=== Updating job [harbor-migrate] in [us-central1] to this image ===", "Seeding: the migrate job applies schema/devseed as data migrations.", "Migrate job done: execution harbor-migrate-abc succeeded."},
			wantImage:  "reg/harbor@sha256:abc",
			wantLabels: map[string]any{"terraform": "true", managedByLabel: managedByValue, commitLabel: "deadbeef", buildIDLabel: "b-1", sourceRepoLabel: "harbor", environmentLabel: "tst", prNumberLabel: "7"},
			wantArgs:   []string{seedArg},
		},
		{
			name:       "a release build runs the schema alone and drops a stale pull-request label",
			env:        environment,
			build:      build("false", ""),
			run:        newFakeRun(map[string]map[string]any{jobName: jobDoc()}),
			wantOut:    []string{"=== Running job [harbor-migrate] ==="},
			wantImage:  "reg/harbor@sha256:abc",
			wantLabels: map[string]any{"terraform": "true", managedByLabel: managedByValue, commitLabel: "deadbeef", buildIDLabel: "b-1", sourceRepoLabel: "harbor", environmentLabel: "tst"},
			wantArgs:   []string{},
		},
		{
			name:  "a failed execution stops the build",
			env:   environment,
			build: build("false", ""),
			run: func() *fakeRun {
				r := newFakeRun(map[string]map[string]any{jobName: jobDoc()})
				r.execution = map[string]any{"name": jobName + "/executions/harbor-migrate-xyz", "failedCount": float64(1)}

				return r
			}(),
			wantErr: "the migrate job failed: execution harbor-migrate-xyz has 1 failed task(s); its logs say why",
		},
		{
			name:  "a run the API refused stops the build",
			env:   environment,
			build: build("false", ""),
			run: func() *fakeRun {
				r := newFakeRun(map[string]map[string]any{jobName: jobDoc()})
				r.runErr = errors.New("Cloud Run operation x failed: task timed out")

				return r
			}(),
			wantErr: "Cloud Run operation x failed: task timed out",
		},
		{
			name:    "a job the project lacks is refused",
			env:     environment,
			build:   build("false", ""),
			run:     newFakeRun(map[string]map[string]any{}),
			wantErr: "Cloud Run answered HTTP 404 to GET /v2/" + jobName,
		},
		{
			name:    "a workspace without the digest is refused",
			env:     strings.Replace(environment, "export IMAGE_DIGEST=\"sha256:abc\"\n", "", 1),
			build:   build("false", ""),
			run:     newFakeRun(map[string]map[string]any{}),
			wantErr: "environment.sh names no image digest (IMAGE, IMAGE_DIGEST): the image build writes it",
		},
		{
			name:    "a migrate job that is not region=name is refused",
			env:     strings.Replace(environment, "us-central1=harbor-migrate", "harbor-migrate", 1),
			build:   build("false", ""),
			run:     newFakeRun(map[string]map[string]any{}),
			wantErr: `MIGRATE_JOB "harbor-migrate" is not region=name`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: tt.build})
			var out strings.Builder
			err := Migrate(t.Context(), &Clients{Run: tt.run.open}, w, &out)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Migrate() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out.String())
				}

				return
			}
			if err != nil {
				t.Fatalf("Migrate() error = %v; output:\n%s", err, out.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
			if tt.wantImage == "" {
				return
			}
			patched := tt.run.patched[jobName]
			if got := text(patched, "template.template.containers"); patched == nil || got != "" {
				container, _ := field(patched, "template.template").(map[string]any)
				c, err := firstContainer(container)
				if err != nil || c["image"] != tt.wantImage {
					t.Errorf("job image = %v, want %s (patched %v)", c["image"], tt.wantImage, patched != nil)
				}
			}
			if diff := cmp.Diff(tt.wantLabels, patched["labels"]); diff != "" {
				t.Errorf("labels mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantArgs, tt.run.ran[jobName]); diff != "" {
				t.Errorf("run arguments mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPipelineLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build Build
		want  map[string]string
	}{
		{
			name:  "a pull request's build labels its number",
			build: Build{ID: "b-1", Substitutions: map[string]string{commitSub: "d", repoNameSub: "harbor", envSub: "tst", prNumberSub: "7"}},
			want:  map[string]string{managedByLabel: managedByValue, commitLabel: "d", buildIDLabel: "b-1", sourceRepoLabel: "harbor", environmentLabel: "tst", prNumberLabel: "7"},
		},
		{
			name:  "a release build clears the number",
			build: Build{ID: "b-2", Substitutions: map[string]string{commitSub: "e", repoNameSub: "harbor", envSub: "stg"}},
			want:  map[string]string{managedByLabel: managedByValue, commitLabel: "e", buildIDLabel: "b-2", sourceRepoLabel: "harbor", environmentLabel: "stg", prNumberLabel: ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, pipelineLabels(&tt.build)); diff != "" {
				t.Errorf("pipelineLabels() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestCloudRun proves the client over a stand-in API: a change is an operation polled to
// its end, a run carries its overrides, a refusal carries the message.
func TestCloudRun(t *testing.T) {
	t.Parallel()

	const job = "projects/p/locations/l/jobs/j"
	var mu sync.Mutex
	polls := 0
	var patchPath, runBody string
	answer := func(w http.ResponseWriter, status int, body map[string]any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	operation := func(id string, done bool, response map[string]any) map[string]any {
		op := map[string]any{"name": "projects/p/locations/l/operations/" + id, "done": done}
		if response != nil {
			op["response"] = response
		}

		return op
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/"+job:
			answer(w, http.StatusOK, map[string]any{"name": job, "labels": map[string]any{"a": "1"}})
		case r.Method == http.MethodPatch && r.URL.Path == "/v2/"+job:
			patchPath = r.URL.RequestURI()
			answer(w, http.StatusOK, operation("op-1", false, nil))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/projects/p/locations/l/operations/op-1":
			polls++
			if polls < 2 {
				answer(w, http.StatusOK, operation("op-1", false, nil))
			} else {
				answer(w, http.StatusOK, operation("op-1", true, map[string]any{"name": job, "labels": map[string]any{"a": "2"}}))
			}
		case r.Method == http.MethodPost && r.URL.Path == "/v2/"+job+":run":
			data := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(data)
			runBody = string(data)
			answer(w, http.StatusOK, operation("op-2", true, map[string]any{"name": job + "/executions/j-abc", "succeededCount": 1}))
		case r.Method == http.MethodPost && r.URL.Path == "/v2/projects/p/locations/l/jobs/broken:run":
			op := operation("op-3", true, nil)
			op["error"] = map[string]any{"code": 9, "message": "task failed"}
			answer(w, http.StatusOK, op)
		default:
			answer(w, http.StatusForbidden, map[string]any{"error": map[string]any{"code": 403, "message": "Permission denied on resource"}})
		}
	}))
	t.Cleanup(srv.Close)
	client := &cloudRun{http: srv.Client(), base: srv.URL, poll: time.Millisecond}
	ctx := t.Context()

	doc, err := client.Get(ctx, job)
	if err != nil || text(doc, "name") != job {
		t.Fatalf("Get() = %v, %v", doc, err)
	}
	settled, err := client.Patch(ctx, job, doc, "labels")
	if err != nil || text(settled, "labels.a") != "2" {
		t.Fatalf("Patch() = %v, %v", settled, err)
	}
	if patchPath != "/v2/"+job+"?updateMask=labels" {
		t.Errorf("Patch() sent %s, want the update mask", patchPath)
	}
	if polls != 2 {
		t.Errorf("the operation was polled %d times, want 2", polls)
	}
	execution, err := client.RunJob(ctx, job, []string{seedArg})
	if err != nil || text(execution, "name") != job+"/executions/j-abc" {
		t.Fatalf("RunJob() = %v, %v", execution, err)
	}
	if runBody != `{"overrides":{"containerOverrides":[{"args":["-seed"]}]}}` {
		t.Errorf("RunJob() sent %s, want the arguments override", runBody)
	}
	if _, err := client.RunJob(ctx, "projects/p/locations/l/jobs/broken", nil); err == nil || !strings.Contains(err.Error(), "Cloud Run operation projects/p/locations/l/operations/op-3 failed: task failed") {
		t.Errorf("RunJob() on a failed operation: error = %v", err)
	}
	if _, err := client.Get(ctx, "projects/p/locations/l/jobs/denied"); err == nil || !strings.Contains(err.Error(), "Cloud Run answered HTTP 403 to GET /v2/projects/p/locations/l/jobs/denied: Permission denied on resource") {
		t.Errorf("Get() refused: error = %v", err)
	}
}

func TestRunHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func() (string, error)
		want string
		// wantErr is what a refusal must say.
		wantErr string
	}{
		{name: "a target splits", call: func() (string, error) {
			region, name, err := target("SERVICES", "us-central1=harbor-app")

			return region + "/" + name, err
		}, want: "us-central1/harbor-app"},
		{name: "a target without a region is refused", call: func() (string, error) {
			_, _, err := target("SERVICES", "=harbor-app")

			return "", err
		}, wantErr: `SERVICES "=harbor-app" is not region=name`},
		{name: "a short name is the last element", call: func() (string, error) {
			return shortName("projects/p/locations/l/services/s/revisions/s-00007-abc"), nil
		}, want: "s-00007-abc"},
		{name: "a missing field is empty", call: func() (string, error) {
			return text(map[string]any{"a": map[string]any{"b": "c"}}, "a.x.y"), nil
		}, want: ""},
		{name: "a template without containers is refused", call: func() (string, error) {
			_, err := firstContainer(map[string]any{})

			return "", err
		}, wantErr: "the template has no container"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.call()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil || got != tt.want {
				t.Errorf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}
