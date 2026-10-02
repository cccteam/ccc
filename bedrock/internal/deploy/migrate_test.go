package deploy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
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
	// runs are every RunJob's arguments in order; failedRun is the one of them (1-based)
	// whose execution fails, 0 for none.
	runs      [][]string
	failedRun int
	// created and deleted are the resource names CreateJob and Delete took, in order.
	created, deleted []string
	// policies holds the IAM policy of each resource that has one; policySets are the
	// resources SetIamPolicy set, in order.
	policies   map[string]map[string]any
	policySets []string
	// canceled are the executions CancelExecution took, in order.
	canceled []string
}

func newFakeRun(resources map[string]map[string]any) *fakeRun {
	return &fakeRun{resources: resources, patched: map[string]map[string]any{}, patches: map[string][]map[string]any{}, fields: map[string][]string{}, fieldsOf: map[string][][]string{}, ran: map[string][]string{}, policies: map[string]map[string]any{}}
}

func (r *fakeRun) open(context.Context) (Run, error) {
	return r, nil
}

func (r *fakeRun) Get(_ context.Context, name string) (map[string]any, error) {
	doc, ok := r.resources[name]
	if !ok {
		return nil, errors.Wrap(&apiError{status: 404, method: "GET", path: "/v2/" + name, message: "not found"}, "fakeRun.Get()")
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

func (r *fakeRun) Services(_ context.Context, project, region string) ([]map[string]any, error) {
	prefix := "projects/" + project + "/locations/" + region + "/services/"
	var list []map[string]any
	for name, doc := range r.resources {
		if strings.HasPrefix(name, prefix) {
			list = append(list, doc)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return text(list[i], "name") < text(list[j], "name")
	})

	return list, nil
}

func (r *fakeRun) Jobs(_ context.Context, project, region string) ([]map[string]any, error) {
	return r.listed("projects/" + project + "/locations/" + region + "/jobs/"), nil
}

func (r *fakeRun) Revisions(_ context.Context, service string) ([]map[string]any, error) {
	return r.listed(service + "/revisions/"), nil
}

func (r *fakeRun) Executions(_ context.Context, job string) ([]map[string]any, error) {
	return r.listed(job + "/executions/"), nil
}

func (r *fakeRun) CancelExecution(_ context.Context, name string) error {
	doc, ok := r.resources[name]
	if !ok {
		return errors.Wrap(&apiError{status: 404, method: "POST", path: "/v2/" + name + ":cancel", message: "not found"}, "fakeRun.CancelExecution()")
	}
	doc["completionTime"] = "canceled"
	r.canceled = append(r.canceled, name)

	return nil
}

// listed is the resources whose names start with prefix and go no deeper (a job, not
// its executions), by name.
func (r *fakeRun) listed(prefix string) []map[string]any {
	var list []map[string]any
	for name, doc := range r.resources {
		if strings.HasPrefix(name, prefix) && !strings.Contains(strings.TrimPrefix(name, prefix), "/") {
			list = append(list, doc)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return text(list[i], "name") < text(list[j], "name")
	})

	return list
}

func (r *fakeRun) CreateJob(_ context.Context, parent, id string, job map[string]any) (map[string]any, error) {
	name := parent + "/jobs/" + id
	if _, ok := r.resources[name]; ok {
		return nil, &apiError{status: 409, method: "POST", path: "/v2/" + parent + "/jobs", message: "already exists"}
	}
	doc := map[string]any{keyName: name}
	for key, value := range job {
		doc[key] = value
	}
	r.resources[name] = doc
	r.created = append(r.created, name)

	return doc, nil
}

func (r *fakeRun) Delete(_ context.Context, name string) error {
	if _, ok := r.resources[name]; !ok {
		return &apiError{status: 404, method: "DELETE", path: "/v2/" + name, message: "not found"}
	}
	delete(r.resources, name)
	r.deleted = append(r.deleted, name)

	return nil
}

func (r *fakeRun) RunJob(_ context.Context, name string, args []string) (map[string]any, error) {
	r.ran[name] = append([]string{}, args...)
	r.runs = append(r.runs, append([]string{}, args...))
	if r.runErr != nil {
		return nil, r.runErr
	}
	if r.execution != nil {
		return r.execution, nil
	}
	if r.failedRun == len(r.runs) {
		return map[string]any{"name": name + "/executions/" + shortName(name) + "-failed", "failedCount": float64(1)}, nil
	}

	return map[string]any{"name": name + "/executions/" + shortName(name) + "-abc", "succeededCount": float64(1)}, nil
}

// fakeLogs is Logs in tests: lines by execution name, the reads taken (the view and the
// execution), how many reads answer nothing before the lines are there (Cloud Logging's
// lag), and a refusal.
type fakeLogs struct {
	lines map[string][]string
	reads []string
	after int
	err   error
}

func (l *fakeLogs) open(context.Context) (Logs, error) {
	return l, nil
}

func (l *fakeLogs) Lines(_ context.Context, view, execution string) ([]string, error) {
	l.reads = append(l.reads, view+" "+execution)
	if l.err != nil {
		return nil, l.err
	}
	if len(l.reads) <= l.after {
		return nil, nil
	}

	return l.lines[execution], nil
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
		template    = "projects/tst-project/locations/us-central1/jobs/harbor-migrate"
		jobName     = template + "-v1-2-3"
		execution   = "harbor-migrate-v1-2-3-abc"
		view        = "projects/tst-project/locations/global/buckets/imp-tst-gbl-harbor-migrate-logs/views/_AllLogs"
		environment = "export SKIP_DEPLOY=\"\"\nexport RUN_MIGRATIONS=\"true\"\nexport MIGRATE_JOB=\"us-central1=harbor-migrate\"\nexport VERSION=\"v1.2.3\"\n"
	)
	// build is the build file with the seed, the pull request's number and any other
	// substitutions (the migration operation's, the log view).
	build := func(seed, pr string, extra map[string]string) string {
		subs := map[string]string{"_PROJECT": "tst-project", "_ENV": "tst", "COMMIT_SHA": "deadbeef", "REPO_NAME": "harbor", "_SEED": seed, "_PR_NUMBER": pr}
		for name, value := range extra {
			subs[name] = value
		}
		data, err := json.Marshal(Build{ID: "b-1", Substitutions: subs})
		if err != nil {
			t.Fatal(err)
		}

		return string(data)
	}
	made := func() map[string]map[string]any {
		doc := jobDoc()
		doc[keyName] = jobName

		return map[string]map[string]any{jobName: doc}
	}
	// versionLines are what a -version run writes; stopped is the runner's message when a
	// file stops part way.
	versionLines := []string{"schema: version 41, dirty: 2 statements applied", "data: no version"}
	const stopped = "000041_AccessPolicyScope.up.sql stopped at statement 3 of 5 (ALTER TABLE Grants ADD CONSTRAINT ...): row exists; fix the cause and rerun, which continues from statement 3, or force a version"
	tests := []struct {
		name  string
		env   string
		build string
		run   *fakeRun
		// logs is Cloud Logging as the step reads it; nil leaves the clients without it.
		logs *fakeLogs
		// wantOut are lines the output carries, in this order, whether or not the step failed.
		wantOut []string
		// wantRuns are the job's runs, each its arguments, in order; wantDeleted says the
		// build's job was deleted; wantFacts are facts the step left (an empty value: not
		// left); wantReads are the log reads taken, view and execution.
		wantRuns    [][]string
		wantDeleted bool
		wantFacts   map[string]string
		wantReads   []string
		wantErr     string
	}{
		{
			name:    "a torn-down environment does nothing",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			build:   build("true", "7", nil),
			run:     newFakeRun(map[string]map[string]any{}),
			wantOut: []string{tornDown},
		},
		{
			name:    "a version run's environment does nothing after it, and says why",
			env:     "export SKIP_DEPLOY=\"true\"\nexport SKIP_REASON=\"" + versionSkipped + "\"\n",
			build:   build("false", "", nil),
			run:     newFakeRun(map[string]map[string]any{}),
			wantOut: []string{versionSkipped},
		},
		{
			name:    "a build without migrations has no job to run",
			env:     strings.Replace(environment, `RUN_MIGRATIONS="true"`, `RUN_MIGRATIONS="false"`, 1),
			build:   build("true", "7", nil),
			run:     newFakeRun(map[string]map[string]any{}),
			wantOut: []string{"Skipping the migrate job: this build does not run migrations."},
		},
		{
			name:        "a pull request's build seeds its new database, and the job is deleted after",
			env:         environment,
			build:       build("true", "7", nil),
			run:         newFakeRun(made()),
			wantOut:     []string{"=== Running job [harbor-migrate-v1-2-3] once ===", "Seeding: the migrate job applies schema/devseed as data migrations.", "The job's lines are not read here (no log view): Cloud Logging query " + loggingQuery(execution), "Migrate job done: execution harbor-migrate-v1-2-3-abc succeeded.", "Job harbor-migrate-v1-2-3 deleted: its execution's logs stay in Cloud Logging."},
			wantRuns:    [][]string{{seedArg}},
			wantDeleted: true,
		},
		{
			name:        "a release build runs the schema alone",
			env:         environment,
			build:       build("false", "", nil),
			run:         newFakeRun(made()),
			wantOut:     []string{"=== Running job [harbor-migrate-v1-2-3] once ===", "Migrate job done"},
			wantRuns:    [][]string{{}},
			wantDeleted: true,
		},
		{
			name:        "a plain run prints the lines its job wrote, read through the log view",
			env:         environment,
			build:       build("false", "", map[string]string{migrateLogsSub: view}),
			run:         newFakeRun(made()),
			logs:        &fakeLogs{lines: map[string][]string{execution: {"Applied 000041_AccessPolicyScope.up.sql (5 statements)"}}},
			wantOut:     []string{"=== Running job [harbor-migrate-v1-2-3] once ===", "--- harbor-migrate-v1-2-3-abc wrote ---", "Applied 000041_AccessPolicyScope.up.sql (5 statements)", "---", "Migrate job done"},
			wantRuns:    [][]string{{}},
			wantDeleted: true,
			wantReads:   []string{view + " " + execution},
		},
		{
			name:        "the lines are read again until Cloud Logging holds them",
			env:         environment,
			build:       build("false", "", map[string]string{migrateLogsSub: view}),
			run:         newFakeRun(made()),
			logs:        &fakeLogs{lines: map[string][]string{execution: {"Applied 000041_AccessPolicyScope.up.sql (5 statements)"}}, after: 2},
			wantOut:     []string{"--- harbor-migrate-v1-2-3-abc wrote ---", "Applied 000041_AccessPolicyScope.up.sql (5 statements)"},
			wantRuns:    [][]string{{}},
			wantDeleted: true,
			wantReads:   []string{view + " " + execution, view + " " + execution, view + " " + execution},
		},
		{
			name:        "lines that never arrive leave the query, and the run goes on",
			env:         environment,
			build:       build("false", "", map[string]string{migrateLogsSub: view}),
			run:         newFakeRun(made()),
			logs:        &fakeLogs{after: 100},
			wantOut:     []string{"No line of harbor-migrate-v1-2-3-abc reached Cloud Logging in a minute: query " + loggingQuery(execution), "Migrate job done"},
			wantRuns:    [][]string{{}},
			wantDeleted: true,
			wantReads:   repeated(view+" "+execution, logTries),
		},
		{
			name:        "a read Cloud Logging refused leaves the query, and the run goes on",
			env:         environment,
			build:       build("false", "", map[string]string{migrateLogsSub: view}),
			run:         newFakeRun(made()),
			logs:        &fakeLogs{err: errors.New("Cloud Logging answered HTTP 403 to POST /v2/entries:list: denied")},
			wantOut:     []string{"The job's lines could not be read (", "Cloud Logging answered HTTP 403 to POST /v2/entries:list: denied): Cloud Logging query " + loggingQuery(execution), "Migrate job done"},
			wantRuns:    [][]string{{}},
			wantDeleted: true,
		},
		{
			name:        "a version run runs the job with -version once, prints its lines, and leaves the steps after nothing to do",
			env:         environment,
			build:       build("false", "", map[string]string{migrateActionSub: actionVersion, requesterSub: "octocat", migrateLogsSub: view}),
			run:         newFakeRun(made()),
			logs:        &fakeLogs{lines: map[string][]string{execution: versionLines}},
			wantOut:     []string{"=== Running job [harbor-migrate-v1-2-3] once with -version ===", "--- harbor-migrate-v1-2-3-abc wrote ---", "schema: version 41, dirty: 2 statements applied", "data: no version", "---", "Migrate job done: execution harbor-migrate-v1-2-3-abc succeeded.", versionSkipped, "Job harbor-migrate-v1-2-3 deleted"},
			wantRuns:    [][]string{{versionArg}},
			wantDeleted: true,
			wantFacts:   map[string]string{skipDeploy: trueValue, skipReasonFact: versionSkipped, forcedTableFact: ""},
			wantReads:   []string{view + " " + execution},
		},
		{
			name:        "a rerun runs the job as it always does",
			env:         environment,
			build:       build("true", "", map[string]string{migrateActionSub: actionRerun, requesterSub: "octocat"}),
			run:         newFakeRun(made()),
			wantOut:     []string{"Rerun, asked for by octocat: the migrate job runs as it always does, continuing a file that stopped from its failed statement, and the release continues.", "=== Running job [harbor-migrate-v1-2-3] once ===", "Seeding", "Migrate job done"},
			wantRuns:    [][]string{{seedArg}},
			wantDeleted: true,
			wantFacts:   map[string]string{skipDeploy: "", forcedTableFact: ""},
		},
		{
			name:        "a force runs the force, then the migrations with the seed, and leaves the force for the record",
			env:         environment,
			build:       build("true", "", map[string]string{migrateActionSub: actionForce, migrateVersionSub: "40", requesterSub: "octocat", migrateLogsSub: view}),
			run:         newFakeRun(made()),
			logs:        &fakeLogs{lines: map[string][]string{execution: {"schema: version 41, dirty", "forced schema to version 40", "schema: version 40"}}},
			wantOut:     []string{"Force, asked for by octocat: the schema migrations table is set to version 40; the migrations run after it and the release continues.", "=== Running job [harbor-migrate-v1-2-3] once with -force 40 ===", "schema: version 41, dirty", "forced schema to version 40", "schema: version 40", "Migrate job done", "=== Running job [harbor-migrate-v1-2-3] once ===", "Seeding", "Migrate job done"},
			wantRuns:    [][]string{{forceArg, "40"}, {seedArg}},
			wantDeleted: true,
			wantFacts:   map[string]string{forcedTableFact: tableSchema, forcedVersionFact: "40", skipDeploy: ""},
		},
		{
			name:        "a force of the data table runs -force-data",
			env:         environment,
			build:       build("false", "", map[string]string{migrateActionSub: actionForce, migrateTableSub: tableData, migrateVersionSub: "2", requesterSub: "octocat"}),
			run:         newFakeRun(made()),
			wantOut:     []string{"the data migrations table is set to version 2", "=== Running job [harbor-migrate-v1-2-3] once with -force-data 2 ==="},
			wantRuns:    [][]string{{forceDataArg, "2"}, {}},
			wantDeleted: true,
			wantFacts:   map[string]string{forcedTableFact: tableData, forcedVersionFact: "2"},
		},
		{
			name:        "a force to -1 leaves no version",
			env:         environment,
			build:       build("false", "", map[string]string{migrateActionSub: actionForce, migrateVersionSub: "-1", requesterSub: "octocat"}),
			run:         newFakeRun(made()),
			wantOut:     []string{"the schema migrations table is set to no version", "=== Running job [harbor-migrate-v1-2-3] once with -force -1 ==="},
			wantRuns:    [][]string{{forceArg, "-1"}, {}},
			wantDeleted: true,
			wantFacts:   map[string]string{forcedTableFact: tableSchema, forcedVersionFact: "-1"},
		},
		{
			name:  "a force whose second run fails fails the step with the job's message",
			env:   environment,
			build: build("false", "", map[string]string{migrateActionSub: actionForce, migrateVersionSub: "40", requesterSub: "octocat", migrateLogsSub: view}),
			run: func() *fakeRun {
				r := newFakeRun(made())
				r.failedRun = 2

				return r
			}(),
			logs:        &fakeLogs{lines: map[string][]string{execution: {"forced schema to version 40"}, "harbor-migrate-v1-2-3-failed": {stopped}}},
			wantOut:     []string{"=== Running job [harbor-migrate-v1-2-3] once with -force 40 ===", "=== Running job [harbor-migrate-v1-2-3] once ===", "--- harbor-migrate-v1-2-3-failed wrote ---", stopped},
			wantRuns:    [][]string{{forceArg, "40"}, {}},
			wantDeleted: true,
			wantFacts:   map[string]string{forcedTableFact: tableSchema, forcedVersionFact: "40"},
			wantErr:     "the migrate job failed: execution harbor-migrate-v1-2-3-failed has 1 failed task(s); its logs say why",
		},
		{
			name:    "a force without a version is refused before any job runs",
			env:     environment,
			build:   build("false", "", map[string]string{migrateActionSub: actionForce, requesterSub: "octocat"}),
			run:     newFakeRun(made()),
			wantErr: "_MIGRATE_ACTION=force names no version (_MIGRATE_VERSION): a force says which version the database is at, or -1 for no version",
		},
		{
			name:    "a force with a version that is not an integer is refused before any job runs",
			env:     environment,
			build:   build("false", "", map[string]string{migrateActionSub: actionForce, migrateVersionSub: "forty", requesterSub: "octocat"}),
			run:     newFakeRun(made()),
			wantErr: `_MIGRATE_VERSION "forty" is not a version: an integer 0 or above, or -1 for no version`,
		},
		{
			name:    "a force without a requester is refused before any job runs",
			env:     environment,
			build:   build("false", "", map[string]string{migrateActionSub: actionForce, migrateVersionSub: "40"}),
			run:     newFakeRun(made()),
			wantErr: "_MIGRATE_ACTION=force names no requester (_REQUESTER): a force says who asked for it",
		},
		{
			name:    "an unknown action is refused before any job runs",
			env:     environment,
			build:   build("false", "", map[string]string{migrateActionSub: "undo", requesterSub: "octocat"}),
			run:     newFakeRun(made()),
			wantErr: `unknown _MIGRATE_ACTION "undo" (the actions are version, rerun and force)`,
		},
		{
			name:    "an unknown table is refused before any job runs",
			env:     environment,
			build:   build("false", "", map[string]string{migrateActionSub: actionForce, migrateTableSub: "rows", migrateVersionSub: "40", requesterSub: "octocat"}),
			run:     newFakeRun(made()),
			wantErr: `unknown _MIGRATE_TABLE "rows" (the tables are schema and data)`,
		},
		{
			name:    "a version with a version value is refused before any job runs",
			env:     environment,
			build:   build("false", "", map[string]string{migrateActionSub: actionVersion, migrateVersionSub: "40", requesterSub: "octocat"}),
			run:     newFakeRun(made()),
			wantErr: "_MIGRATE_VERSION=40 with _MIGRATE_ACTION=version: a version goes with force",
		},
		{
			name:    "a table without an action is refused before any job runs",
			env:     environment,
			build:   build("false", "", map[string]string{migrateTableSub: tableData}),
			run:     newFakeRun(made()),
			wantErr: "_MIGRATE_TABLE and _MIGRATE_VERSION go with a _MIGRATE_ACTION (version, rerun or force), and the build names none",
		},
		{
			name:  "a failed execution stops the build, the job deleted all the same",
			env:   environment,
			build: build("false", "", nil),
			run: func() *fakeRun {
				r := newFakeRun(made())
				r.execution = map[string]any{"name": jobName + "/executions/harbor-migrate-v1-2-3-xyz", "failedCount": float64(1)}

				return r
			}(),
			wantDeleted: true,
			wantErr:     "the migrate job failed: execution harbor-migrate-v1-2-3-xyz has 1 failed task(s); its logs say why",
		},
		{
			name:  "a run the API refused stops the build, the job deleted all the same",
			env:   environment,
			build: build("false", "", nil),
			run: func() *fakeRun {
				r := newFakeRun(made())
				r.runErr = errors.New("Cloud Run operation x failed: task timed out")

				return r
			}(),
			wantDeleted: true,
			wantErr:     "Cloud Run operation x failed: task timed out",
		},
		{
			name:    "a build whose job was not made is refused",
			env:     environment,
			build:   build("false", "", nil),
			run:     newFakeRun(map[string]map[string]any{template: jobDoc()}),
			wantErr: "this build's migrate job harbor-migrate-v1-2-3 does not exist: deploy jobs makes it right after the image build",
		},
		{
			name:    "a build without a version is refused",
			env:     strings.Replace(environment, "export VERSION=\"v1.2.3\"\n", "", 1),
			build:   build("false", "", nil),
			run:     newFakeRun(made()),
			wantErr: "environment.sh names no version (VERSION): the resolve step writes it",
		},
		{
			name:    "a migrate job that is not region=name is refused",
			env:     strings.Replace(environment, "us-central1=harbor-migrate", "harbor-migrate", 1),
			build:   build("false", "", nil),
			run:     newFakeRun(map[string]map[string]any{}),
			wantErr: `MIGRATE_JOB "harbor-migrate" is not region=name`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: tt.build})
			clients := &Clients{Run: tt.run.open, Sleep: noSleep}
			if tt.logs != nil {
				clients.Logs = tt.logs.open
			}
			var out strings.Builder
			err := Migrate(t.Context(), clients, w, &out)
			var wantDeleted []string
			if tt.wantDeleted {
				wantDeleted = []string{jobName}
			}
			if diff := cmp.Diff(wantDeleted, tt.run.deleted); diff != "" {
				t.Errorf("deleted mismatch (-want +got):\n%s", diff)
			}
			at := 0
			for _, want := range tt.wantOut {
				i := strings.Index(out.String()[at:], want)
				if i < 0 {
					t.Errorf("output lacks %q in its turn:\n%s", want, out.String())

					continue
				}
				at += i + len(want)
			}
			if tt.wantRuns != nil {
				if diff := cmp.Diff(tt.wantRuns, tt.run.runs); diff != "" {
					t.Errorf("runs mismatch (-want +got):\n%s", diff)
				}
			}
			if tt.wantReads != nil {
				if diff := cmp.Diff(tt.wantReads, tt.logs.reads); diff != "" {
					t.Errorf("log reads mismatch (-want +got):\n%s", diff)
				}
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Migrate() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("Migrate() error = %v; output:\n%s", err, out.String())
			}
			if len(tt.run.patched) != 0 {
				t.Errorf("the step changed a job: %v", tt.run.patched)
			}
			if tt.wantFacts == nil {
				return
			}
			env, err := w.Environment()
			if err != nil {
				t.Fatal(err)
			}
			for name, want := range tt.wantFacts {
				if env[name] != want {
					t.Errorf("fact %s = %q, want %q", name, env[name], want)
				}
			}
		})
	}
}

// repeated is s, n times over.
func repeated(s string, n int) []string {
	reads := make([]string, n)
	for i := range reads {
		reads[i] = s
	}

	return reads
}

// TestCloudLogging proves the reader over a stand-in API: the entries are asked for
// through the view, with a filter that names the execution, page after page, and a
// refusal carries the message.
func TestCloudLogging(t *testing.T) {
	t.Parallel()

	const (
		view      = "projects/p/locations/global/buckets/b/views/_AllLogs"
		execution = "harbor-migrate-v1-2-3-abc"
	)
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodPost || r.URL.Path != "/v2/entries:list" {
			w.WriteHeader(http.StatusNotFound)

			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		if names, _ := body["resourceNames"].([]any); len(names) == 1 && names[0] == "projects/p/locations/global/buckets/denied/views/_AllLogs" {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": 403, "message": "Permission denied on the view"}})

			return
		}
		answer := map[string]any{"entries": []any{map[string]any{"textPayload": "schema: version 41, dirty: 2 statements applied\n"}}, "nextPageToken": "page-2"}
		if body["pageToken"] == "page-2" {
			answer = map[string]any{"entries": []any{map[string]any{"jsonPayload": map[string]any{"message": "data: no version"}}, map[string]any{"severity": "INFO"}}}
		}
		_ = json.NewEncoder(w).Encode(answer)
	}))
	t.Cleanup(srv.Close)
	client := &cloudLogging{cloudRun: &cloudRun{http: srv.Client(), base: srv.URL, poll: time.Millisecond, service: "Cloud Logging"}}

	lines, err := client.Lines(t.Context(), view, execution)
	if err != nil {
		t.Fatalf("Lines() error = %v", err)
	}
	if diff := cmp.Diff([]string{"schema: version 41, dirty: 2 statements applied", "data: no version"}, lines); diff != "" {
		t.Errorf("lines mismatch (-want +got):\n%s", diff)
	}
	mu.Lock()
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2 (one per page)", len(bodies))
	}
	first := bodies[0]
	mu.Unlock()
	if names, _ := first["resourceNames"].([]any); len(names) != 1 || names[0] != view {
		t.Errorf("resourceNames = %v, want the view", first["resourceNames"])
	}
	if filter, _ := first["filter"].(string); filter != `resource.type="cloud_run_job" AND labels."run.googleapis.com/execution_name"="harbor-migrate-v1-2-3-abc"` {
		t.Errorf("filter = %q, want one naming the execution", filter)
	}
	if order, _ := first["orderBy"].(string); order != "timestamp asc" {
		t.Errorf("orderBy = %q, want the oldest first", order)
	}
	if _, err := client.Lines(t.Context(), "projects/p/locations/global/buckets/denied/views/_AllLogs", execution); err == nil || !strings.Contains(err.Error(), "Cloud Logging answered HTTP 403 to POST /v2/entries:list: Permission denied on the view") {
		t.Errorf("Lines() refused: error = %v", err)
	}
}

func TestPipelineLabels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		build   Build
		version string
		want    map[string]string
	}{
		{
			name:    "a pull request's build labels its number and its version as a name",
			build:   Build{ID: "b-1", Substitutions: map[string]string{commitSub: "d", repoNameSub: "harbor", envSub: "tst", prNumberSub: "7"}},
			version: "pr7@d",
			want:    map[string]string{managedByLabel: managedByValue, commitLabel: "d", buildIDLabel: "b-1", sourceRepoLabel: "harbor", environmentLabel: "tst", prNumberLabel: "7", versionLabel: "pr7-d"},
		},
		{
			name:    "a release build clears the number",
			build:   Build{ID: "b-2", Substitutions: map[string]string{commitSub: "e", repoNameSub: "harbor", envSub: "stg"}},
			version: "v1.2.3",
			want:    map[string]string{managedByLabel: managedByValue, commitLabel: "e", buildIDLabel: "b-2", sourceRepoLabel: "harbor", environmentLabel: "stg", prNumberLabel: "", versionLabel: "v1-2-3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.want, pipelineLabels(&tt.build, tt.version)); diff != "" {
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

func (r *fakeRun) GetIamPolicy(_ context.Context, name string) (map[string]any, error) {
	if _, ok := r.resources[name]; !ok {
		return nil, errors.Wrap(&apiError{status: 404, method: "GET", path: "/v2/" + name + ":getIamPolicy", message: "not found"}, "fakeRun.GetIamPolicy()")
	}
	if policy, ok := r.policies[name]; ok {
		return policy, nil
	}

	return map[string]any{keyEtag: "etag-of-" + shortName(name)}, nil
}

func (r *fakeRun) SetIamPolicy(_ context.Context, name string, policy map[string]any) (map[string]any, error) {
	if _, ok := r.resources[name]; !ok {
		return nil, errors.Wrap(&apiError{status: 404, method: "POST", path: "/v2/" + name + ":setIamPolicy", message: "not found"}, "fakeRun.SetIamPolicy()")
	}
	set := map[string]any{}
	for key, value := range policy {
		set[key] = value
	}
	set[keyEtag] = "etag-set-on-" + shortName(name)
	r.policies[name] = set
	r.policySets = append(r.policySets, name)

	return set, nil
}
