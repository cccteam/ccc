package render

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

// TestRenderedStackParsesAsHCL parses every rendered .tf file under testdata, so a template whose rendering
// is not valid HCL (a stray escape, an unbalanced quote) fails here and not in a pipeline's plan step.
func TestRenderedStackParsesAsHCL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dir  string
	}{
		{name: "beacon", dir: filepath.Join("testdata", "beacon")},
		{name: "harbor", dir: filepath.Join("testdata", "harbor")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			files := tfFiles(t, tt.dir)
			parser := hclparse.NewParser()
			for _, file := range files {
				if _, diags := parser.ParseHCLFile(file); diags.HasErrors() {
					t.Errorf("%s: %s", file, diags.Error())
				}
			}
		})
	}
}

// tfFiles lists the .tf files under dir, failing the test when there are none.
func tfFiles(t *testing.T, dir string) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".tf") {
			files = append(files, path)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	if len(files) == 0 {
		t.Fatalf("no .tf files under %s", dir)
	}

	return files
}

// TestFirestoreProjectEnv holds every process that constructs the Firestore database's
// level to the database's project, set to the environment project and never left to the
// application, which refuses a database named without its project: firestore_env sets
// GOOGLE_CLOUD_FIRESTORE_PROJECT to local.project_id beside the database's id; the
// service's, the job process's and the migrate command's environments merge it; and
// cloud-run.tf gives the service and the job theirs, cloud-build.tf the migrate command
// its own (_MIGRATE_ENV). The Spanner project is the shared instance's where one is
// shared, so it is never the database's. An application without a database has no
// firestore_env at all.
func TestFirestoreProjectEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dir  string
		// wantEnvs are the environments that merge firestore_env; none means the
		// stack has no firestore_env.
		wantEnvs []string
	}{
		{
			name:     "a Firestore database: the service, the job process and the migrate command construct its level",
			dir:      filepath.Join("testdata", "harbor"),
			wantEnvs: []string{"service_env", "migrate_env", "jobs_env"},
		},
		{name: "no Firestore database", dir: filepath.Join("testdata", "beacon")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			locals := localsOf(t, parseBody(t, filepath.Join(tt.dir, "locals.tf")))
			firestoreEnv, ok := locals["firestore_env"]
			if len(tt.wantEnvs) == 0 {
				if ok {
					t.Errorf("locals.tf declares firestore_env, for an application without a Firestore database")
				}

				return
			}
			if !ok {
				t.Fatalf("locals.tf declares no firestore_env")
			}
			if got := objectValue(t, firestoreEnv.Expr, "GOOGLE_CLOUD_FIRESTORE_PROJECT"); got != "local.project_id" {
				t.Errorf("firestore_env sets GOOGLE_CLOUD_FIRESTORE_PROJECT to %q, want local.project_id", got)
			}
			for _, env := range tt.wantEnvs {
				attr, ok := locals[env]
				if !ok {
					t.Errorf("locals.tf declares no %s", env)

					continue
				}
				if !slices.Contains(localsReferenced(attr.Expr), "firestore_env") {
					t.Errorf("%s does not merge firestore_env", env)
				}
			}
			uses := []struct {
				file, block, env string
			}{
				{file: "cloud-run.tf", block: "google_cloud_run_v2_service.app", env: "service_env"},
				{file: "cloud-run.tf", block: "google_cloud_run_v2_job.jobs", env: "jobs_env"},
				{file: "cloud-build.tf", env: "migrate_env"},
			}
			for _, u := range uses {
				body := parseBody(t, filepath.Join(tt.dir, u.file))
				var node hclsyntax.Node = body
				if u.block != "" {
					node = resourceBody(t, body, u.block)
				}
				if !slices.Contains(localsReferenced(node), u.env) {
					t.Errorf("%s %s does not read local.%s", u.file, u.block, u.env)
				}
			}
		})
	}
}

// parseBody parses one rendered .tf file.
func parseBody(t *testing.T, file string) *hclsyntax.Body {
	t.Helper()

	f, diags := hclparse.NewParser().ParseHCLFile(file)
	if diags.HasErrors() {
		t.Fatalf("%s: %s", file, diags.Error())
	}
	body, ok := f.Body.(*hclsyntax.Body)
	if !ok {
		t.Fatalf("%s: not native syntax", file)
	}

	return body
}

// localsOf collects the attributes of every locals block, by name.
func localsOf(t *testing.T, body *hclsyntax.Body) map[string]*hclsyntax.Attribute {
	t.Helper()

	locals := map[string]*hclsyntax.Attribute{}
	for _, b := range body.Blocks {
		if b.Type != "locals" {
			continue
		}
		for name, attr := range b.Body.Attributes {
			locals[name] = attr
		}
	}

	return locals
}

// resourceBody is the body of the resource named type.name.
func resourceBody(t *testing.T, body *hclsyntax.Body, address string) *hclsyntax.Body {
	t.Helper()

	for _, b := range body.Blocks {
		if b.Type == "resource" && len(b.Labels) == 2 && b.Labels[0]+"."+b.Labels[1] == address {
			return b.Body
		}
	}
	t.Fatalf("no resource %s", address)

	return nil
}

// objectValue is the value an object constructor gives the key, as the traversal it is
// (local.project_id), or empty when the key is absent or its value is no traversal.
func objectValue(t *testing.T, expr hclsyntax.Expression, key string) string {
	t.Helper()

	obj, ok := expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		t.Fatalf("not an object constructor")
	}
	for _, item := range obj.Items {
		if hcl.ExprAsKeyword(item.KeyExpr) != key {
			continue
		}
		traversal, diags := hcl.AbsTraversalForExpr(item.ValueExpr)
		if diags.HasErrors() {
			return ""
		}

		return traversalString(traversal)
	}

	return ""
}

// localsReferenced lists the locals the node refers to, by name, in the order met.
func localsReferenced(node hclsyntax.Node) []string {
	var names []string
	_ = hclsyntax.VisitAll(node, func(n hclsyntax.Node) hcl.Diagnostics {
		expr, ok := n.(*hclsyntax.ScopeTraversalExpr)
		if !ok || expr.Traversal.RootName() != "local" || len(expr.Traversal) < 2 {
			return nil
		}
		if attr, ok := expr.Traversal[1].(hcl.TraverseAttr); ok {
			names = append(names, attr.Name)
		}

		return nil
	})

	return names
}

// traversalString writes a traversal of attributes as it is written: local.project_id.
func traversalString(traversal hcl.Traversal) string {
	parts := []string{traversal.RootName()}
	for _, step := range traversal[1:] {
		if attr, ok := step.(hcl.TraverseAttr); ok {
			parts = append(parts, attr.Name)
		}
	}

	return strings.Join(parts, ".")
}

// TestSpannerMetricsWriters evaluates the rendered stacks' spanner_metrics_writers local, the
// identities granted roles/monitoring.metricWriter on the project that owns the Spanner
// instance, where the Spanner client writes its client-side metrics, in both shapes of an
// environment. On the shared instance, whose project is not the environment's: the site,
// the job process where it opens the database, and the deploy identity, which runs the
// migrate command, except in a pull-request stack. On an environment's own instance: none,
// since service-accounts.tf grants the role on the environment project already.
func TestSpannerMetricsWriters(t *testing.T) {
	t.Parallel()

	const (
		envProject = "imp-stg-gbl-core-3c4d"
		spnProject = "imp-spn-gbl-core-9a0b"
	)
	members := map[string]string{
		"app":    "serviceAccount:imp-stg-gbl-app-app@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com",
		"jobs":   "serviceAccount:imp-stg-gbl-app-jobs@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com",
		"deploy": "serviceAccount:imp-stg-gbl-app-deploy@imp-stg-gbl-core-3c4d.iam.gserviceaccount.com",
	}
	tests := []struct {
		name            string
		dir             string
		instanceProject string
		pullRequest     bool
		want            []string
	}{
		{name: "harbor on the shared instance", dir: "harbor", instanceProject: spnProject, want: []string{"app", "deploy", "jobs"}},
		{name: "harbor's pull-request stack on the shared instance", dir: "harbor", instanceProject: spnProject, pullRequest: true, want: []string{"app", "jobs"}},
		{name: "harbor on its own instance", dir: "harbor", instanceProject: envProject},
		{name: "harbor's pull-request stack on its own instance", dir: "harbor", instanceProject: envProject, pullRequest: true},
		{name: "beacon, whose job process does not open the database, on the shared instance", dir: "beacon", instanceProject: spnProject, want: []string{"app", "deploy"}},
		{name: "beacon on its own instance", dir: "beacon", instanceProject: envProject},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join("testdata", tt.dir, "spanner.tf")
			expr := localExpression(t, path, "spanner_metrics_writers")
			ctx := &hcl.EvalContext{
				Variables: map[string]cty.Value{
					"local": cty.ObjectVal(map[string]cty.Value{
						"project_id":  cty.StringVal(envProject),
						"instance":    cty.ObjectVal(map[string]cty.Value{"project": cty.StringVal(tt.instanceProject)}),
						"is_pr":       cty.BoolVal(tt.pullRequest),
						"app_member":  cty.StringVal(members["app"]),
						"jobs_member": cty.StringVal(members["jobs"]),
						"identities":  cty.ObjectVal(map[string]cty.Value{"deploy_identity_member": cty.StringVal(members["deploy"])}),
					}),
				},
				Functions: map[string]function.Function{"merge": stdlib.MergeFunc},
			}
			value, diags := expr.Value(ctx)
			if diags.HasErrors() {
				t.Fatalf("%s: spanner_metrics_writers: %s", path, diags.Error())
			}
			got := map[string]string{}
			for k, v := range value.AsValueMap() {
				got[k] = v.AsString()
			}
			if keys := slices.Sorted(maps.Keys(got)); !slices.Equal(keys, tt.want) {
				t.Fatalf("spanner_metrics_writers = %v, want %v", keys, tt.want)
			}
			for k, member := range got {
				if member != members[k] {
					t.Errorf("spanner_metrics_writers[%q] = %q, want %q", k, member, members[k])
				}
			}

			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			grant := "resource \"google_project_iam_member\" \"spanner_metrics\" {\n" +
				"  for_each = local.spanner_metrics_writers\n\n" +
				"  project = local.instance.project\n" +
				"  role    = \"roles/monitoring.metricWriter\"\n" +
				"  member  = each.value\n"
			if !strings.Contains(string(content), grant) {
				t.Errorf("%s lacks:\n%s", path, grant)
			}
		})
	}
}

// localExpression parses a rendered .tf file and returns the expression of the named
// attribute of its locals blocks, failing the test when there is none.
func localExpression(t *testing.T, path, name string) hcl.Expression {
	t.Helper()

	file, diags := hclparse.NewParser().ParseHCLFile(path)
	if diags.HasErrors() {
		t.Fatalf("%s: %s", path, diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		t.Fatalf("%s is not native HCL syntax", path)
	}
	for _, block := range body.Blocks {
		if block.Type != "locals" {
			continue
		}
		if attr, found := block.Body.Attributes[name]; found {
			return attr.Expr
		}
	}
	t.Fatalf("%s has no local %s", path, name)

	return nil
}

// TestScheduledJobs evaluates the rendered scheduler.tf and locals.tf of a stack whose code
// declares a scheduled route and of one that declares none, in an environment's stack and
// in a pull-request stack. With a route, an environment's stack creates the invoker
// identity and one Cloud Scheduler job per route, which posts to the route on the
// environment's first hostname with an OIDC token of the invoker minted for the same URL,
// at the schedule and in the zone the code declares, and the service receives the
// invoker's email as APP_SCHEDULER_INVOKER; a pull-request stack creates neither and sets
// no variable. Without a route the stack declares none of it.
func TestScheduledJobs(t *testing.T) {
	t.Parallel()

	const (
		host    = "app-tst.example.dev"
		invoker = "imp-tst-gbl-app-sched@imp-tst-gbl-core-3c4d.iam.gserviceaccount.com"
	)
	tests := []struct {
		name        string
		dir         string
		pullRequest bool
		// wantJobs are the jobs by key, each as jobLine writes it; none when no job is
		// created.
		wantJobs map[string]string
		// wantAccounts is how many invoker identities the stack creates.
		wantAccounts int
		// wantInvoker is the APP_SCHEDULER_INVOKER the service receives, empty for none.
		wantInvoker string
		// declaresNone says the code declares no scheduled route, so the stack carries
		// no scheduler locals or resources at all.
		declaresNone bool
	}{
		{
			name:         "a scheduled route in an environment's stack",
			dir:          "harbor",
			wantJobs:     map[string]string{"send-daily-digest": "0 7 * * 1-5 in America/New_York: POST https://app-tst.example.dev/_scheduled/send-daily-digest as " + invoker + " for https://app-tst.example.dev/_scheduled/send-daily-digest"},
			wantAccounts: 1,
			wantInvoker:  invoker,
		},
		{name: "a scheduled route in a pull-request stack", dir: "harbor", pullRequest: true},
		{name: "no scheduled route", dir: "beacon", declaresNone: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scheduler := parseBody(t, filepath.Join("testdata", tt.dir, "scheduler.tf"))
			locals := localsOf(t, parseBody(t, filepath.Join("testdata", tt.dir, "locals.tf")))
			if tt.declaresNone {
				if len(scheduler.Blocks) != 0 {
					t.Errorf("scheduler.tf declares %d block(s), want none", len(scheduler.Blocks))
				}
				for _, name := range []string{"scheduler_account", "scheduler_email", "scheduler_env"} {
					if _, ok := locals[name]; ok {
						t.Errorf("locals.tf declares %s, for code that declares no scheduled route", name)
					}
				}

				return
			}

			routes, diags := localExpression(t, filepath.Join("testdata", tt.dir, "scheduler.tf"), "scheduled_routes").Value(nil)
			if diags.HasErrors() {
				t.Fatalf("scheduled_routes: %s", diags.Error())
			}
			local := map[string]cty.Value{
				"is_pr":            cty.BoolVal(tt.pullRequest),
				"scheduled_routes": routes,
				"scheduler_email":  cty.StringVal(invoker),
				"hostnames":        cty.ListVal([]cty.Value{cty.StringVal(host)}),
			}
			ctx := &hcl.EvalContext{Variables: map[string]cty.Value{"local": cty.ObjectVal(local)}}

			accounts := evalAttribute(t, ctx, resourceBody(t, scheduler, "google_service_account.scheduler"), "count")
			if got, _ := accounts.AsBigFloat().Int64(); got != int64(tt.wantAccounts) {
				t.Errorf("google_service_account.scheduler count = %d, want %d", got, tt.wantAccounts)
			}

			job := resourceBody(t, scheduler, "google_cloud_scheduler_job.scheduled")
			got := map[string]string{}
			for key, route := range evalAttribute(t, ctx, job, "for_each").AsValueMap() {
				each := &hcl.EvalContext{Variables: map[string]cty.Value{
					"local": cty.ObjectVal(local),
					"each":  cty.ObjectVal(map[string]cty.Value{"key": cty.StringVal(key), "value": route}),
				}}
				got[key] = jobLine(t, each, job)
			}
			if len(got) != len(tt.wantJobs) {
				t.Fatalf("jobs = %v, want %v", got, tt.wantJobs)
			}
			for key, want := range tt.wantJobs {
				if got[key] != want {
					t.Errorf("job %s = %q, want %q", key, got[key], want)
				}
			}

			env, ok := locals["scheduler_env"]
			if !ok {
				t.Fatalf("locals.tf declares no scheduler_env")
			}
			value, diags := env.Expr.Value(ctx)
			if diags.HasErrors() {
				t.Fatalf("scheduler_env: %s", diags.Error())
			}
			invokerSet := ""
			if v, ok := value.AsValueMap()["APP_SCHEDULER_INVOKER"]; ok {
				invokerSet = v.AsString()
			}
			if invokerSet != tt.wantInvoker {
				t.Errorf("scheduler_env sets APP_SCHEDULER_INVOKER to %q, want %q", invokerSet, tt.wantInvoker)
			}
			if !slices.Contains(localsReferenced(locals["service_env"].Expr), "scheduler_env") {
				t.Errorf("service_env does not merge scheduler_env")
			}
		})
	}
}

// evalAttribute evaluates the named attribute of body in ctx.
func evalAttribute(t *testing.T, ctx *hcl.EvalContext, body *hclsyntax.Body, name string) cty.Value {
	t.Helper()

	attr, ok := body.Attributes[name]
	if !ok {
		t.Fatalf("no attribute %s", name)
	}
	value, diags := attr.Expr.Value(ctx)
	if diags.HasErrors() {
		t.Fatalf("%s: %s", name, diags.Error())
	}

	return value
}

// nestedBlock is the body of the first block of the type inside body.
func nestedBlock(t *testing.T, body *hclsyntax.Body, typ string) *hclsyntax.Body {
	t.Helper()

	for _, b := range body.Blocks {
		if b.Type == typ {
			return b.Body
		}
	}
	t.Fatalf("no %s block", typ)

	return nil
}

// jobLine is what a Cloud Scheduler job does, on one line: its schedule and zone, then
// the call it makes, the identity whose token it carries and the token's audience.
func jobLine(t *testing.T, ctx *hcl.EvalContext, job *hclsyntax.Body) string {
	t.Helper()

	target := nestedBlock(t, job, "http_target")
	token := nestedBlock(t, target, "oidc_token")

	return evalAttribute(t, ctx, job, "schedule").AsString() + " in " + evalAttribute(t, ctx, job, "time_zone").AsString() + ": " +
		evalAttribute(t, ctx, target, "http_method").AsString() + " " + evalAttribute(t, ctx, target, "uri").AsString() +
		" as " + evalAttribute(t, ctx, token, "service_account_email").AsString() + " for " + evalAttribute(t, ctx, token, "audience").AsString()
}

// TestDatabaseGenerations evaluates the rendered database locals and the restored
// databases over the generation the stack is told (var.database_generation): the first
// generation keeps the database's name, a later one is the restored database named with
// its number, the restored resource is named after the base with the generation and keeps
// the environment's retention from the placement, and the pull-request stack's database
// has one generation; the for_each and the rollback trigger's count are read as written.
func TestDatabaseGenerations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		generation  int64
		pullRequest bool
		environment string
		wantName    string
	}{
		{name: "the first generation in production", generation: 1, environment: "prd", wantName: "imp-prd-gbl-harbor-db"},
		{name: "the third generation in production", generation: 3, environment: "prd", wantName: "imp-prd-gbl-harbor-db-3"},
		{name: "the second generation in tst", generation: 2, environment: "tst", wantName: "imp-tst-gbl-harbor-db-2"},
		{name: "a pull-request stack", generation: 1, pullRequest: true, environment: "tst", wantName: "harbor-pr7-db"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			locals := localsOf(t, parseBody(t, filepath.Join("testdata", "harbor", "locals.tf")))
			spanner := parseBody(t, filepath.Join("testdata", "harbor", "spanner.tf"))
			local := map[string]cty.Value{
				"is_pr":        cty.BoolVal(tt.pullRequest),
				"own_database": cty.True,
				"pr_name":      cty.StringVal("harbor-pr7"),
				"name":         cty.StringVal("imp-" + tt.environment),
				"app":          cty.StringVal("harbor"),
			}
			vars := map[string]cty.Value{
				"environment":         cty.StringVal(tt.environment),
				"database_generation": cty.NumberIntVal(tt.generation),
			}
			ctx := &hcl.EvalContext{Variables: map[string]cty.Value{"local": cty.ObjectVal(local), "var": cty.ObjectVal(vars)}}
			base, diags := locals["database_base"].Expr.Value(ctx)
			if diags.HasErrors() {
				t.Fatalf("database_base: %s", diags.Error())
			}
			local["database_base"] = base
			retention, diags := locals["spanner_retention"].Expr.Value(nil)
			if diags.HasErrors() {
				t.Fatalf("spanner_retention: %s", diags.Error())
			}
			local["spanner_retention"] = retention
			ctx = &hcl.EvalContext{Variables: map[string]cty.Value{"local": cty.ObjectVal(local), "var": cty.ObjectVal(vars)}}
			name, diags := locals["database_name"].Expr.Value(ctx)
			if diags.HasErrors() {
				t.Fatalf("database_name: %s", diags.Error())
			}
			if name.AsString() != tt.wantName {
				t.Errorf("database_name = %q, want %q", name.AsString(), tt.wantName)
			}

			restored := resourceBody(t, spanner, "google_spanner_database.restored")
			if tt.generation > 1 {
				each := &hcl.EvalContext{Variables: map[string]cty.Value{
					"local": cty.ObjectVal(local), "var": cty.ObjectVal(vars),
					"each": cty.ObjectVal(map[string]cty.Value{"key": cty.StringVal(strconv.FormatInt(tt.generation, 10))}),
				}}
				if got := evalAttribute(t, each, restored, "name").AsString(); got != tt.wantName {
					t.Errorf("restored name = %q, want %q", got, tt.wantName)
				}
				if got := evalAttribute(t, each, restored, "version_retention_period").AsString(); got != "7d" {
					t.Errorf("restored version_retention_period = %q, want 7d", got)
				}
			}
			if got := evalAttribute(t, ctx, resourceBody(t, spanner, "google_spanner_database.harbor"), "version_retention_period").AsString(); got != "7d" {
				t.Errorf("version_retention_period = %q, want 7d", got)
			}
		})
	}
}
