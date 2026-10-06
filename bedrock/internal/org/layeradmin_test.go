package org

import (
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

// binding is one role binding of an entitlement as a test reads it back.
type binding struct {
	Role      string
	Condition string
}

// renderedLocal parses one rendered .tf file of the fixture organization and returns
// the expression of the named local, failing the test when there is none.
func renderedLocal(t *testing.T, path, name string) hcl.Expression {
	t.Helper()

	file, diags := hclsyntax.ParseConfig([]byte(renderedFile(t, path)), path, hcl.InitialPos)
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

// bindingsOf reads a list of { role, condition } objects back.
func bindingsOf(t *testing.T, v cty.Value) []binding {
	t.Helper()

	var got []binding
	for it := v.ElementIterator(); it.Next(); {
		_, b := it.Element()
		got = append(got, binding{Role: b.GetAttr("role").AsString(), Condition: b.GetAttr("condition").AsString()})
	}

	return got
}

// ctyStrings converts a list of Go strings to a cty tuple of strings.
func ctyStrings(items []string) cty.Value {
	values := make([]cty.Value, 0, len(items))
	for _, item := range items {
		values = append(values, cty.StringVal(item))
	}

	return cty.TupleVal(values)
}

// TestLayerAdministratorBindings evaluates 2-env's layer administrator bindings, as the
// rendered team-group.tf computes them, over what 1-org publishes for the environment:
// the layer identity's roles, each without a condition, and storage admin under the
// records bucket's condition, the same grants 1-org makes the identity; with nothing
// published (a 1-org applied before it published them), none, and the entitlement is
// not declared while the check says why. No binding acts as the identity.
func TestLayerAdministratorBindings(t *testing.T) {
	t.Parallel()

	const records = `resource.name.startsWith("projects/_/buckets/imp-stg-gbl-records-")`
	roles := []string{"roles/run.admin", "roles/resourcemanager.projectIamAdmin", "organizations/123456789012/roles/secretContainerAdmin"}
	tests := []struct {
		name      string
		published bool
		want      []binding
	}{
		{
			name:      "the layer identity's roles and the records bucket's storage admin",
			published: true,
			want: []binding{
				{Role: "roles/run.admin"},
				{Role: "roles/resourcemanager.projectIamAdmin"},
				{Role: "organizations/123456789012/roles/secretContainerAdmin"},
				{Role: "roles/storage.admin", Condition: records},
			},
		},
		{
			name: "nothing published yet",
		},
	}
	expr := renderedLocal(t, "2-env/team-group.tf", "layer_administrator_bindings")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			grants := cty.NullVal(cty.DynamicPseudoType)
			if tt.published {
				grants = cty.ObjectVal(map[string]cty.Value{"roles": ctyStrings(roles), "storage_admin_condition": cty.StringVal(records)})
			}
			ctx := &hcl.EvalContext{
				Variables: map[string]cty.Value{"local": cty.ObjectVal(map[string]cty.Value{"layer_grants": grants})},
				Functions: map[string]function.Function{"concat": stdlib.ConcatFunc},
			}
			v, diags := expr.Value(ctx)
			if diags.HasErrors() {
				t.Fatalf("layer_administrator_bindings: %s", diags.Error())
			}
			if got := bindingsOf(t, v); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("layer_administrator_bindings = %+v, want %+v", got, tt.want)
			}
		})
	}

	teamGroup := renderedFile(t, "2-env/team-group.tf")
	for _, want := range []string{
		"  layer_grants = try(local.org.environment_layer_grants[var.environment], null)\n",
		"      layer-administrator = {\n        declared = length(local.layer_administrator_bindings) > 0\n        duration = local.entitlement_durations.layer_administrator\n        bindings = local.layer_administrator_bindings\n      }\n",
		"check \"layer_grants_published\" {\n  assert {\n    condition     = local.layer_grants != null\n",
	} {
		if !strings.Contains(teamGroup, want) {
			t.Errorf("2-env/team-group.tf lacks:\n%s", want)
		}
	}
	for _, absent := range []string{"roles/iam.serviceAccountTokenCreator", "layer_identity_condition", "unique_id", "GOOGLE_IMPERSONATE_SERVICE_ACCOUNT"} {
		if strings.Contains(teamGroup, absent) {
			t.Errorf("2-env/team-group.tf still carries %q", absent)
		}
	}
}

// TestLayerStateEntitlements evaluates 1-org's layer state entitlements, as the rendered
// entitlements.tf computes them, over the state prefixes workflow.tf gives each
// environment layer identity: per environment, the team group from the placement, an
// approval in every environment but the first, and the identity's grants in the boot
// project bounded by conditions on the name: the quota, its own prefix to read and write,
// the bucket itself (the list) and the upstream states to read, and the bucket's policy.
func TestLayerStateEntitlements(t *testing.T) {
	t.Parallel()

	const (
		objects = "projects/_/buckets/imp-state-a1b2/objects"
		bucket  = "projects/_/buckets/imp-state-a1b2"
		policy  = "organizations/123456789012/roles/impGblStateBucketPolicyAdmin"
	)
	upstream := []string{"1-org", "2-shr", "2-spn", "2-net", "2-env"}
	tests := []struct {
		env          string
		wantGroup    string
		wantApproval bool
	}{
		{env: "tst", wantGroup: "team-tst@imp.example"},
		{env: "stg", wantGroup: "team-stg@imp.example", wantApproval: true},
		{env: "prd", wantGroup: "team-prd@imp.example", wantApproval: true},
	}
	environments, diags := renderedLocal(t, "1-org/entitlements.tf", "layer_state_environments").Value(nil)
	if diags.HasErrors() {
		t.Fatalf("layer_state_environments: %s", diags.Error())
	}
	prefixes := map[string]cty.Value{}
	upstreams := map[string]cty.Value{}
	for _, env := range Environments {
		prefixes[env] = cty.StringVal("2-env/" + env)
		upstreams[env] = ctyStrings(upstream)
	}
	ctx := &hcl.EvalContext{
		Variables: map[string]cty.Value{"local": cty.ObjectVal(map[string]cty.Value{
			"layer_state_environments": environments,
			"state_bucket_objects":     cty.StringVal(objects),
			"state_bucket_name":        cty.StringVal(bucket),
			"layer_state_prefixes":     cty.ObjectVal(prefixes),
			"layer_upstream_prefixes":  cty.ObjectVal(upstreams),
			"boot":                     cty.ObjectVal(map[string]cty.Value{"state_bucket_policy_admin_role": cty.StringVal(policy)}),
		})},
		Functions: map[string]function.Function{"join": stdlib.JoinFunc, "concat": stdlib.ConcatFunc},
	}
	entitlements, diags := renderedLocal(t, "1-org/entitlements.tf", "layer_state_entitlements").Value(ctx)
	if diags.HasErrors() {
		t.Fatalf("layer_state_entitlements: %s", diags.Error())
	}
	if n := entitlements.LengthInt(); n != len(tests) {
		t.Fatalf("%d layer state entitlements, want %d", n, len(tests))
	}
	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			t.Parallel()

			e := entitlements.GetAttr(tt.env)
			if got := e.GetAttr("group").AsString(); got != tt.wantGroup {
				t.Errorf("group = %q, want %q", got, tt.wantGroup)
			}
			if got := e.GetAttr("approval").True(); got != tt.wantApproval {
				t.Errorf("approval = %v, want %v", got, tt.wantApproval)
			}
			reads := []string{`resource.name == "` + bucket + `"`}
			for _, p := range upstream {
				reads = append(reads, `resource.name.startsWith("`+objects+`/`+p+`/")`)
			}
			want := []binding{
				{Role: "roles/serviceusage.serviceUsageConsumer"},
				{Role: "roles/storage.objectUser", Condition: `resource.name.startsWith("` + objects + `/2-env/` + tt.env + `/")`},
				{Role: "roles/storage.objectViewer", Condition: strings.Join(reads, " || ")},
				{Role: policy, Condition: `resource.name == "` + bucket + `"`},
			}
			if got := bindingsOf(t, e.GetAttr("bindings")); !reflect.DeepEqual(got, want) {
				t.Errorf("bindings =\n%+v\nwant\n%+v", got, want)
			}
		})
	}
}

// TestLayerAdministratorPlace reads where the two recovery entitlements and what they
// need are declared: 1-org publishes the environment layer identity's grants and declares
// the layer state entitlement on the boot project, after setting the service up there,
// with its plan identity's read of it; 0-bootstrap enables the service on the boot project
// and gives the org identity the right to declare entitlements there alone; and the
// READMEs run the recovery as the person, with TF_DATA_DIR and no impersonation, the
// audit log naming the person.
func TestLayerAdministratorPlace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		path   string
		want   []string
		absent []string
	}{
		{
			name: "1-org publishes the identity's grants from the same role set and condition it grants",
			path: "1-org/outputs.tf",
			want: []string{
				"output \"environment_layer_grants\" {",
				"      roles                   = [for role in var.layer_roles[cfg.role_set] : lookup(local.custom_roles, role, role)]\n      storage_admin_condition = local.records_bucket_conditions[k]\n",
			},
			absent: []string{"layer_service_account_unique_ids"},
		},
		{
			name: "the records bucket's condition is one local, for the grant and the output",
			path: "1-org/service-accounts.tf",
			want: []string{"    expression  = local.records_bucket_conditions[each.key]\n"},
		},
		{
			name: "1-org declares the layer state entitlements on the boot project",
			path: "1-org/entitlements.tf",
			want: []string{
				"resource \"google_project_iam_member\" \"pam_service_agent\" {\n  project = var.boot_project_id\n  role    = \"roles/privilegedaccessmanager.serviceAgent\"\n",
				"resource \"google_project_iam_member\" \"org_plan_entitlements\" {\n  project = var.boot_project_id\n  role    = \"roles/privilegedaccessmanager.viewer\"\n  member  = google_service_account.boot_plan[\"org\"].member\n}\n",
				"  entitlement_id       = \"${var.prefix}-${each.key}-layer-state\"\n  location             = \"global\"\n  parent               = \"projects/${var.boot_project_id}\"\n  max_request_duration = local.layer_state_duration\n",
				"  layer_state_duration = \"14400s\"\n",
				"      resource      = \"//cloudresourcemanager.googleapis.com/projects/${var.boot_project_id}\"\n",
				"  depends_on = [google_project_iam_member.pam_service_agent]\n",
			},
		},
		{
			name: "0-bootstrap enables the service on the boot project and lets the org identity declare entitlements there alone",
			path: "0-bootstrap/bootstrap.tf",
			want: []string{
				"    \"privilegedaccessmanager.googleapis.com\", # the environments' layer state entitlements 1-org declares here\n",
				"resource \"google_project_iam_member\" \"org_tofu_entitlements\" {\n  project = module.boot_project.project_id\n  role    = \"roles/privilegedaccessmanager.admin\"\n  member  = google_service_account.org_tofu.member\n}\n",
			},
		},
		{
			name: "2-env's README recovers as the person under the two entitlements",
			path: "2-env/README.md",
			want: []string{
				"export TF_DATA_DIR=.terraform.tst\ntofu init -backend-config=\"prefix=2-env/tst\"\n",
				"the audit log names the person, not the identity.",
				"| `imp-<env>-layer-administrator` | `layerAdministrator` | The roles this environment's layer identity (`imp-<env>-gbl-tofu`) holds on the environment project:",
				"| `imp-<env>-layer-state` | `layerAdministrator` | Declared by `1-org` on the boot project",
				"IAM evaluates no\n`resource.name` for a service account",
			},
			absent: []string{"GOOGLE_IMPERSONATE_SERVICE_ACCOUNT", "layer_service_account_unique_ids"},
		},
		{
			name:   "0-bootstrap's recovery names the two entitlements",
			path:   "0-bootstrap/README.md",
			want:   []string{"asks for two entitlements for the same short time", "and the audit\nlog names the person"},
			absent: []string{"GOOGLE_IMPERSONATE_SERVICE_ACCOUNT", "serviceAccountTokenCreator"},
		},
		{
			name:   "the root README too",
			path:   "README.md",
			absent: []string{"GOOGLE_IMPERSONATE_SERVICE_ACCOUNT"},
			want:   []string{"layer administrator and layer state entitlements"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			content := renderedFile(t, tt.path)
			for _, w := range tt.want {
				if !strings.Contains(content, w) {
					t.Errorf("%s lacks:\n%s", tt.path, w)
				}
			}
			for _, a := range tt.absent {
				if strings.Contains(content, a) {
					t.Errorf("%s still carries %q", tt.path, a)
				}
			}
		})
	}
}
