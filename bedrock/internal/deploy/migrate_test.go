package deploy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
)

// fakeRun is Run in tests: resources by name, what was patched (the last patch, and
// every patch with its update mask in order). A patch of a template creates a revision,
// as the API does; a patch of the traffic settles the statuses onto it.
type fakeRun struct {
	resources map[string]map[string]any
	patched   map[string]map[string]any
	patches   map[string][]map[string]any
	fields    map[string][]string
	fieldsOf  map[string][][]string
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
	return &fakeRun{resources: resources, patched: map[string]map[string]any{}, patches: map[string][]map[string]any{}, fields: map[string][]string{}, fieldsOf: map[string][][]string{}, policies: map[string]map[string]any{}}
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

// The migrate step's fixtures: the settings the stack derived, as the stack steps leave
// them (a JSON object), the environment file with them, and the variables the command
// then runs with, sorted, the version variable set to the build's version.
const (
	migrateSettingsJSON = `{"APP_SERVICE_NAME":"harbor-migrate","GOOGLE_CLOUD_SPANNER_DATABASE_NAME":"imp-tst-gbl-harbor-db","GOOGLE_CLOUD_SPANNER_PROJECT":"tst-project"}`
	versionVariable     = "APP_VERSION"
)

var (
	migrateEnvironment = "export SKIP_DEPLOY=\"\"\nexport RUN_MIGRATIONS=\"true\"\nexport VERSION=\"v1.2.3\"\nexport MIGRATE_ENV=" + doubleQuote(migrateSettingsJSON) + "\nexport MIGRATE_DATABASES=" + doubleQuote(migrateDatabasesJSON) + "\n"
	// seededEnvironment is that environment file where resolve decided the build seeds
	// (SEED): every pull request, and a release where the checkout's placement says so.
	seededEnvironment = migrateEnvironment + "export SEED=\"true\"\n"
	// The databases the stack names for the command: its Spanner database and its
	// Firestore one.
	migrateDatabasesJSON = `["projects/tst-project/instances/tst-spanner/databases/imp-tst-gbl-harbor-db","projects/tst-project/databases/imp-tst-gbl-harbor-fs"]`
	migrateVars          = []string{"APP_SERVICE_NAME=harbor-migrate", "APP_VERSION=v1.2.3", "GOOGLE_CLOUD_SPANNER_DATABASE_NAME=imp-tst-gbl-harbor-db", "GOOGLE_CLOUD_SPANNER_PROJECT=tst-project"}
)

// migrateProgramFile is a migrate command the image build left: a file at the path, so
// the step finds it.
func migrateProgramFile(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "migrate")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// commandRuns are the runs of the program the runner recorded: each run's arguments, in
// order.
func commandRuns(run *fakeRunner, program string) [][]string {
	var runs [][]string
	for _, c := range run.ran {
		if c.Name == program {
			runs = append(runs, append([]string{}, c.Args...))
		}
	}

	return runs
}

func TestMigrate(t *testing.T) {
	t.Parallel()

	// build is the build file with the trigger's _SEED, the pull request's number and any
	// other substitutions (the migration operation's). The step reads the seed from the
	// facts (SEED), never from _SEED, which is what the trigger said.
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
	// stopped is the runner's message when a file stops part way, which the command
	// prints into the build log.
	const stopped = "000041_AccessPolicyScope.up.sql stopped at statement 3 of 5 (ALTER TABLE Grants ADD CONSTRAINT ...): row exists; fix the cause and rerun, which continues from statement 3, or force a version"
	tests := []struct {
		name  string
		env   string
		build string
		// fail fails the command's runs by their first argument ("-force"), or every run
		// without one (""); lines are what the command prints on a run.
		fail  map[string]error
		lines string
		// versionVariable names the variable set to the build's version; none is set
		// without one. noProgram leaves the worker without the migrate command.
		versionVariable string
		noProgram       bool
		// grants is the reader of the databases as the deploy identity; none skips the wait.
		grants *fakeGrants
		// wantOut are lines the output carries, in this order, whether or not the step failed.
		wantOut []string
		// wantRuns are the command's runs, each its arguments, in order; wantVars the
		// variables every run got; wantFacts are facts the step left (an empty value: not
		// left).
		wantRuns  [][]string
		wantVars  []string
		wantFacts map[string]string
		wantErr   string
	}{
		{
			name:     "the command runs once the deploy identity reads the databases the stack names",
			env:      migrateEnvironment,
			build:    build("false", "", nil),
			grants:   &fakeGrants{},
			wantOut:  []string{"The deploy identity reads projects/tst-project/instances/tst-spanner/databases/imp-tst-gbl-harbor-db, projects/tst-project/databases/imp-tst-gbl-harbor-fs."},
			wantRuns: [][]string{{}},
		},
		{
			name:   "a grant the apply just made is waited for, and said",
			env:    migrateEnvironment,
			build:  build("false", "", nil),
			grants: &fakeGrants{denied: map[string]int{"projects/tst-project/databases/imp-tst-gbl-harbor-fs": 2}},
			wantOut: []string{
				"Waiting for the deploy identity's grant on projects/tst-project/databases/imp-tst-gbl-harbor-fs to take effect (the stack applied it; IAM makes a grant effective within minutes).",
				"The grants are in effect after 10s: the deploy identity reads projects/tst-project/instances/tst-spanner/databases/imp-tst-gbl-harbor-db, projects/tst-project/databases/imp-tst-gbl-harbor-fs.",
			},
			wantRuns: [][]string{{}},
		},
		{
			name:    "a grant not in effect after the wait stops the run before the command",
			env:     migrateEnvironment,
			build:   build("false", "", nil),
			grants:  &fakeGrants{denied: map[string]int{"projects/tst-project/instances/tst-spanner/databases/imp-tst-gbl-harbor-db": 1000}},
			wantOut: []string{"Waiting for the deploy identity's grant on projects/tst-project/instances/tst-spanner/databases/imp-tst-gbl-harbor-db to take effect"},
			wantErr: "the deploy identity's grant on projects/tst-project/instances/tst-spanner/databases/imp-tst-gbl-harbor-db is not in effect after 3m0s: Spanner answered HTTP 403 to GET /v1/projects/tst-project/instances/tst-spanner/databases/imp-tst-gbl-harbor-db: The caller does not have permission; the stack applied it in this build",
		},
		{
			name:    "a read that fails otherwise stops the run with the API's answer",
			env:     migrateEnvironment,
			build:   build("false", "", nil),
			grants:  &fakeGrants{fail: map[string]error{"projects/tst-project/instances/tst-spanner/databases/imp-tst-gbl-harbor-db": &apiError{service: "Spanner", status: 404, method: "GET", path: "/v1/projects/tst-project/instances/tst-spanner/databases/imp-tst-gbl-harbor-db", message: "Database not found"}}},
			wantErr: "Spanner answered HTTP 404 to GET /v1/projects/tst-project/instances/tst-spanner/databases/imp-tst-gbl-harbor-db: Database not found",
		},
		{
			name:    "an environment naming no databases for the command is refused",
			env:     strings.Replace(migrateEnvironment, "export MIGRATE_DATABASES="+doubleQuote(migrateDatabasesJSON)+"\n", "", 1),
			build:   build("false", "", nil),
			wantErr: "environment.sh names no databases for the migrate command (MIGRATE_DATABASES): the stack steps write them from the stack's substitutions output (_MIGRATE_DATABASES)",
		},
		{
			name:    "a torn-down environment does nothing",
			env:     "export SKIP_DEPLOY=\"true\"\n",
			build:   build("true", "7", nil),
			wantOut: []string{tornDown},
		},
		{
			name:    "a version run's environment does nothing after it, and says why",
			env:     "export SKIP_DEPLOY=\"true\"\nexport SKIP_REASON=\"" + versionSkipped + "\"\n",
			build:   build("false", "", nil),
			wantOut: []string{versionSkipped},
		},
		{
			name:    "a build without migrations runs nothing",
			env:     strings.Replace(migrateEnvironment, `RUN_MIGRATIONS="true"`, `RUN_MIGRATIONS="false"`, 1),
			build:   build("true", "7", nil),
			wantOut: []string{"Skipping the migrations: this build does not run them."},
		},
		{
			name:            "a pull request's build seeds its new database, the command run in the checkout with the stack's variables and the build's version",
			env:             seededEnvironment,
			build:           build("true", "7", nil),
			versionVariable: versionVariable,
			wantOut:         []string{"runs on this worker as the deploy identity with 4 variables from the stack.", "Seeding: the migrate command applies schema/devseed as data migrations.", "=== Running the migrate command with -seed ===", "Migrate command done in"},
			wantRuns:        [][]string{{seedArg}},
			wantVars:        migrateVars,
		},
		{
			name:            "a release build runs the schema alone",
			env:             migrateEnvironment,
			build:           build("false", "", nil),
			versionVariable: versionVariable,
			wantOut:         []string{"=== Running the migrate command ===", "Migrate command done in"},
			wantRuns:        [][]string{{}},
			wantVars:        migrateVars,
		},
		{
			name:     "a release build seeds where the facts say so (the checkout's placement put the environment on its seed list), though the trigger's _SEED says false",
			env:      seededEnvironment,
			build:    build("false", "", nil),
			wantOut:  []string{"Seeding: the migrate command applies schema/devseed as data migrations.", "=== Running the migrate command with -seed ===", "Migrate command done in"},
			wantRuns: [][]string{{seedArg}},
		},
		{
			name:     "a release build runs the schema alone where the facts do not seed (the checkout's placement took the environment off its seed list), though the trigger's _SEED says true",
			env:      migrateEnvironment,
			build:    build("true", "", nil),
			wantOut:  []string{"=== Running the migrate command ===", "Migrate command done in"},
			wantRuns: [][]string{{}},
		},
		{
			name:     "a pipeline that names no version variable sets none",
			env:      migrateEnvironment,
			build:    build("false", "", nil),
			wantRuns: [][]string{{}},
			wantVars: []string{"APP_SERVICE_NAME=harbor-migrate", "GOOGLE_CLOUD_SPANNER_DATABASE_NAME=imp-tst-gbl-harbor-db", "GOOGLE_CLOUD_SPANNER_PROJECT=tst-project"},
		},
		{
			name:            "the command's lines are in the build log as it writes them",
			env:             migrateEnvironment,
			build:           build("false", "", nil),
			versionVariable: versionVariable,
			lines:           "Applied 000041_AccessPolicyScope.up.sql (5 statements)\n",
			wantOut:         []string{"=== Running the migrate command ===", "Applied 000041_AccessPolicyScope.up.sql (5 statements)", "Migrate command done in"},
			wantRuns:        [][]string{{}},
		},
		{
			name:            "a version run runs the command with -version once and leaves the steps after nothing to do",
			env:             migrateEnvironment,
			build:           build("false", "", map[string]string{migrateActionSub: actionVersion, requesterSub: "octocat"}),
			versionVariable: versionVariable,
			lines:           "schema: version 41, dirty: 2 statements applied\ndata: no version\n",
			wantOut:         []string{"=== Running the migrate command with -version ===", "schema: version 41, dirty: 2 statements applied", "data: no version", "Migrate command done in", versionSkipped},
			wantRuns:        [][]string{{versionArg}},
			wantFacts:       map[string]string{skipDeploy: trueValue, skipReasonFact: versionSkipped, forcedTableFact: ""},
		},
		{
			name:      "a rerun runs the command as it always does",
			env:       seededEnvironment,
			build:     build("true", "", map[string]string{migrateActionSub: actionRerun, requesterSub: "octocat"}),
			wantOut:   []string{"Rerun, asked for by octocat: the migrate command runs as it always does, continuing a file that stopped from its failed statement, and the release continues.", "=== Running the migrate command with -seed ===", "Migrate command done in"},
			wantRuns:  [][]string{{seedArg}},
			wantFacts: map[string]string{skipDeploy: "", forcedTableFact: ""},
		},
		{
			name:      "a force runs the force, then the migrations with the seed, and leaves the force for the record",
			env:       seededEnvironment,
			build:     build("true", "", map[string]string{migrateActionSub: actionForce, migrateVersionSub: "40", requesterSub: "octocat"}),
			lines:     "schema: version 41, dirty\nforced schema to version 40\nschema: version 40\n",
			wantOut:   []string{"Force, asked for by octocat: the schema migrations table is set to version 40; the migrations run after it and the release continues.", "=== Running the migrate command with -force 40 ===", "forced schema to version 40", "Migrate command done in", "=== Running the migrate command with -seed ===", "Migrate command done in"},
			wantRuns:  [][]string{{forceArg, "40"}, {seedArg}},
			wantFacts: map[string]string{forcedTableFact: tableSchema, forcedVersionFact: "40", skipDeploy: ""},
		},
		{
			name:      "a force of the data table runs -force-data",
			env:       migrateEnvironment,
			build:     build("false", "", map[string]string{migrateActionSub: actionForce, migrateTableSub: tableData, migrateVersionSub: "2", requesterSub: "octocat"}),
			wantOut:   []string{"the data migrations table is set to version 2", "=== Running the migrate command with -force-data 2 ==="},
			wantRuns:  [][]string{{forceDataArg, "2"}, {}},
			wantFacts: map[string]string{forcedTableFact: tableData, forcedVersionFact: "2"},
		},
		{
			name:      "a force to -1 leaves no version",
			env:       migrateEnvironment,
			build:     build("false", "", map[string]string{migrateActionSub: actionForce, migrateVersionSub: "-1", requesterSub: "octocat"}),
			wantOut:   []string{"the schema migrations table is set to no version", "=== Running the migrate command with -force -1 ==="},
			wantRuns:  [][]string{{forceArg, "-1"}, {}},
			wantFacts: map[string]string{forcedTableFact: tableSchema, forcedVersionFact: "-1"},
		},
		{
			name:      "a force whose second run fails fails the step with the command's message in the log",
			env:       migrateEnvironment,
			build:     build("false", "", map[string]string{migrateActionSub: actionForce, migrateVersionSub: "40", requesterSub: "octocat"}),
			fail:      map[string]error{"": errors.New("exit status 1")},
			lines:     stopped + "\n",
			wantOut:   []string{"=== Running the migrate command with -force 40 ===", "=== Running the migrate command ===", stopped},
			wantRuns:  [][]string{{forceArg, "40"}, {}},
			wantFacts: map[string]string{forcedTableFact: tableSchema, forcedVersionFact: "40"},
			wantErr:   "the migration failed after 0s: the migrate command's lines are above (exit status 1)",
		},
		{
			name:    "a force without a version is refused before anything runs",
			env:     migrateEnvironment,
			build:   build("false", "", map[string]string{migrateActionSub: actionForce, requesterSub: "octocat"}),
			wantErr: "_MIGRATE_ACTION=force names no version (_MIGRATE_VERSION): a force says which version the database is at, or -1 for no version",
		},
		{
			name:    "a force with a version that is not an integer is refused before anything runs",
			env:     migrateEnvironment,
			build:   build("false", "", map[string]string{migrateActionSub: actionForce, migrateVersionSub: "forty", requesterSub: "octocat"}),
			wantErr: `_MIGRATE_VERSION "forty" is not a version: an integer 0 or above, or -1 for no version`,
		},
		{
			name:    "a force without a requester is refused before anything runs",
			env:     migrateEnvironment,
			build:   build("false", "", map[string]string{migrateActionSub: actionForce, migrateVersionSub: "40"}),
			wantErr: "_MIGRATE_ACTION=force names no requester (_REQUESTER): a force says who asked for it",
		},
		{
			name:    "an unknown action is refused before anything runs",
			env:     migrateEnvironment,
			build:   build("false", "", map[string]string{migrateActionSub: "undo", requesterSub: "octocat"}),
			wantErr: `unknown _MIGRATE_ACTION "undo" (the actions are version, rerun and force)`,
		},
		{
			name:    "an unknown table is refused before anything runs",
			env:     migrateEnvironment,
			build:   build("false", "", map[string]string{migrateActionSub: actionForce, migrateTableSub: "rows", migrateVersionSub: "40", requesterSub: "octocat"}),
			wantErr: `unknown _MIGRATE_TABLE "rows" (the tables are schema and data)`,
		},
		{
			name:    "a version with a version value is refused before anything runs",
			env:     migrateEnvironment,
			build:   build("false", "", map[string]string{migrateActionSub: actionVersion, migrateVersionSub: "40", requesterSub: "octocat"}),
			wantErr: "_MIGRATE_VERSION=40 with _MIGRATE_ACTION=version: a version goes with force",
		},
		{
			name:    "a table without an action is refused before anything runs",
			env:     migrateEnvironment,
			build:   build("false", "", map[string]string{migrateTableSub: tableData}),
			wantErr: "_MIGRATE_TABLE and _MIGRATE_VERSION go with a _MIGRATE_ACTION (version, rerun or force), and the build names none",
		},
		{
			name:     "a command that exits with an error stops the build, its lines above",
			env:      migrateEnvironment,
			build:    build("false", "", nil),
			fail:     map[string]error{"": errors.New("exit status 1")},
			lines:    stopped + "\n",
			wantOut:  []string{"=== Running the migrate command ===", stopped},
			wantRuns: [][]string{{}},
			wantErr:  "the migration failed after 0s: the migrate command's lines are above (exit status 1)",
		},
		{
			name:    "a workspace without the stack's settings is refused",
			env:     "export SKIP_DEPLOY=\"\"\nexport RUN_MIGRATIONS=\"true\"\nexport VERSION=\"v1.2.3\"\n",
			build:   build("false", "", nil),
			wantErr: "environment.sh names no settings for the migrate command (MIGRATE_ENV): the stack steps write them from the stack's substitutions output (_MIGRATE_ENV)",
		},
		{
			name:    "settings that are not a JSON object are refused",
			env:     "export SKIP_DEPLOY=\"\"\nexport RUN_MIGRATIONS=\"true\"\nexport VERSION=\"v1.2.3\"\nexport MIGRATE_ENV=\"us-central1=harbor-migrate\"\n",
			build:   build("false", "", nil),
			wantErr: "json.Unmarshal(): MIGRATE_ENV",
		},
		{
			name:      "a worker without the migrate command is refused",
			env:       migrateEnvironment,
			build:     build("false", "", nil),
			noProgram: true,
			wantErr:   "no migrate command at",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := workspaceFiles(t, map[string]string{EnvironmentFile: tt.env, BuildFile: tt.build})
			program := migrateProgramFile(t)
			if tt.noProgram {
				program = filepath.Join(t.TempDir(), "absent")
			}
			// The command prints its lines, then exits as the case says for its first
			// argument.
			var out strings.Builder
			run := &fakeRunner{effect: func(c Command) error {
				out.WriteString(tt.lines)
				first := ""
				if len(c.Args) > 0 {
					first = c.Args[0]
				}

				return tt.fail[first]
			}}
			clients := &Clients{Exec: run, Sleep: func(context.Context, time.Duration) error { return nil }}
			if tt.grants != nil {
				clients.Grants = func(context.Context) (Grants, error) { return tt.grants, nil }
			}
			err := Migrate(t.Context(), clients, w, program, tt.versionVariable, false, &out)
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
				if diff := cmp.Diff(tt.wantRuns, commandRuns(run, program)); diff != "" {
					t.Errorf("runs mismatch (-want +got):\n%s", diff)
				}
			}
			for _, c := range run.ran {
				if c.Dir != string(w) {
					t.Errorf("the command ran in %q, want the checkout %q", c.Dir, w)
				}
				if tt.wantVars != nil {
					if diff := cmp.Diff(tt.wantVars, c.Env); diff != "" {
						t.Errorf("variables mismatch (-want +got):\n%s", diff)
					}
				}
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Migrate() error = %v, wantErr %q; output:\n%s", err, tt.wantErr, out.String())
				}
			} else if err != nil {
				t.Fatalf("Migrate() error = %v; output:\n%s", err, out.String())
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
// its end, a deletion whose operation ends in error is that error, a refusal carries the
// message.
func TestCloudRun(t *testing.T) {
	t.Parallel()

	const job = "projects/p/locations/l/jobs/j"
	var mu sync.Mutex
	polls := 0
	var patchPath string
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
		case r.Method == http.MethodDelete && r.URL.Path == "/v2/projects/p/locations/l/jobs/broken":
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
	if err := client.Delete(ctx, "projects/p/locations/l/jobs/broken"); err == nil || !strings.Contains(err.Error(), "Cloud Run operation projects/p/locations/l/operations/op-3 failed: task failed") {
		t.Errorf("Delete() on a failed operation: error = %v", err)
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

// fakeGrants reads the databases as the deploy identity: a read is refused (403) the
// number of times denied says, fails as fail says, or is allowed.
type fakeGrants struct {
	mu     sync.Mutex
	denied map[string]int
	fail   map[string]error
	reads  []string
}

func (g *fakeGrants) Read(_ context.Context, database string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.reads = append(g.reads, database)
	if err := g.fail[database]; err != nil {
		return err
	}
	if g.denied[database] > 0 {
		g.denied[database]--
		service := "Firestore"
		if strings.Contains(database, "/instances/") {
			service = "Spanner"
		}

		return &apiError{service: service, status: http.StatusForbidden, method: http.MethodGet, path: "/v1/" + database, message: "The caller does not have permission"}
	}

	return nil
}
