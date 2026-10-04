package render

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
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
