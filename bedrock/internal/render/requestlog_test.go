package render

import (
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// The scope of every rendered exclusion, as Cloud Logging reads it once the stack's
// names are in: the two regional services and the backend of imp-stg's harbor.
const requestLogScope = `((resource.type="cloud_run_revision" AND (resource.labels.service_name="imp-stg-uc1-harbor-app" OR resource.labels.service_name="imp-stg-ue4-harbor-app")) OR (resource.type="http_load_balancer" AND resource.labels.backend_service_name="imp-stg-gbl-harbor-backend"))`

// lodestarSurfaces are the surfaces Lodestar declares: its beacons logged on event with
// traces off, a prefix, and the droids' ingest route logged on event, a route.
var lodestarSurfaces = []derive.Surface{
	{Prefix: "/beacons/", Kind: derive.SurfacePrefix, Log: derive.RequestLogOnEvent, Traces: derive.TracesOff},
	{Prefix: "/droids/sectors/{sectorID}/ingest-droid-reports", Kind: derive.SurfaceRoute, Log: derive.RequestLogOnEvent},
}

// TestRequestLogExclusion evaluates the rendered exclusion of logging.tf the way a plan
// would, with the stack's names in: the complete filter for Lodestar's surfaces (a
// prefix matched by prefix, a route to the end of its path), for a sampled surface, for
// a never surface and for a surface excepting the one declared beneath it, the
// resource's name, no resource for a pull-request stack (its count is 0) and none
// rendered at all for an application whose surfaces exclude nothing.
func TestRequestLogExclusion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// fixture renders harbor's own surfaces; otherwise surfaces replaces them.
		fixture     bool
		surfaces    []derive.Surface
		pullRequest bool
		// want is the complete filter; empty when no exclusion is rendered.
		want string
	}{
		{
			name:     "Lodestar: the beacons and the droids' ingest route, on event",
			surfaces: lodestarSurfaces,
			want:     requestLogScope + ` AND ((httpRequest.requestUrl =~ "^https://[^/]+/beacons/" AND httpRequest.status < 400) OR (httpRequest.requestUrl =~ "^https://[^/]+/droids/sectors/[^/]+/ingest-droid-reports([?]|$)" AND httpRequest.status < 400))`,
		},
		{
			name:     "a sampled surface",
			surfaces: []derive.Surface{{Prefix: "/api/", Kind: derive.SurfacePrefix, Log: derive.RequestLogSampled, Fraction: 0.1}},
			want:     requestLogScope + ` AND ((httpRequest.requestUrl =~ "^https://[^/]+/api/" AND httpRequest.status < 400 AND NOT sample(insertId, 0.1)))`,
		},
		{
			name:     "a never surface, a route",
			surfaces: []derive.Surface{{Prefix: "/healthz", Kind: derive.SurfaceRoute, Log: derive.RequestLogNever}},
			want:     requestLogScope + ` AND ((httpRequest.requestUrl =~ "^https://[^/]+/healthz([?]|$)"))`,
		},
		{
			name:     "a looser child under its parent: the root sampled, the outlet always",
			surfaces: []derive.Surface{{Prefix: "/", Kind: derive.SurfacePrefix, Log: derive.RequestLogSampled, Fraction: 0.1}, {Prefix: "/api/", Kind: derive.SurfacePrefix, Log: derive.RequestLogAlways}},
			want:     requestLogScope + ` AND ((httpRequest.requestUrl =~ "^https://[^/]+/" AND NOT httpRequest.requestUrl =~ "^https://[^/]+/api/" AND httpRequest.status < 400 AND NOT sample(insertId, 0.1)))`,
		},
		{
			name:    "harbor's own: the outlet sampled excepting the stored file's route beneath it, the route never and anchored at the end of its path, the scheduled route always",
			fixture: true,
			want:    requestLogScope + ` AND ((httpRequest.requestUrl =~ "^https://[^/]+/api/" AND NOT httpRequest.requestUrl =~ "^https://[^/]+/api/manifests/[^/]+/file([?]|$)" AND httpRequest.status < 400 AND NOT sample(insertId, 0.1)) OR (httpRequest.requestUrl =~ "^https://[^/]+/api/manifests/[^/]+/file([?]|$)"))`,
		},
		{name: "a pull-request stack renders none", surfaces: lodestarSurfaces, pullRequest: true},
		{name: "surfaces logged always render none", surfaces: []derive.Surface{{Prefix: "/", Kind: derive.SurfacePrefix, Log: derive.RequestLogAlways}, {Prefix: "/api/", Kind: derive.SurfacePrefix, Log: derive.RequestLogAlways, Traces: derive.TracesOff}}},
		{name: "no surface renders none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := deriveFixture(t, "harbor", "placement.json")
			if !tt.fixture {
				m.Surfaces = tt.surfaces
				m.RequestLog = derive.NewRequestLogExclusion(tt.surfaces)
			}
			body := renderedBody(t, m, "logging.tf")
			block := findResource(body, "google_logging_project_exclusion.request_log")
			if tt.want == "" && !tt.pullRequest {
				if block != nil {
					t.Fatalf("logging.tf renders an exclusion for surfaces that exclude nothing")
				}

				return
			}
			if block == nil {
				t.Fatalf("logging.tf renders no exclusion")
			}
			ctx := requestLogContext(t, body, tt.pullRequest)
			count := evalValue(t, block.Body.Attributes["count"].Expr, ctx)
			if tt.pullRequest {
				if n, _ := count.AsBigFloat().Int64(); n != 0 {
					t.Errorf("count = %v in a pull-request stack, want 0", count.GoString())
				}

				return
			}
			if n, _ := count.AsBigFloat().Int64(); n != 1 {
				t.Errorf("count = %v, want 1", count.GoString())
			}
			if got := evalValue(t, block.Body.Attributes["name"].Expr, ctx).AsString(); got != "imp-stg-gbl-harbor-log-excl" {
				t.Errorf("name = %q, want imp-stg-gbl-harbor-log-excl", got)
			}
			if got := evalValue(t, block.Body.Attributes["filter"].Expr, ctx).AsString(); got != tt.want {
				t.Errorf("filter =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

// renderedBody renders the model and parses the named stack file.
func renderedBody(t *testing.T, m *derive.Model, name string) *hclsyntax.Body {
	t.Helper()

	files, err := Render(m)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	for _, f := range files {
		if f.Root || f.Path != name {
			continue
		}
		file, diags := hclparse.NewParser().ParseHCL(f.Content, name)
		if diags.HasErrors() {
			t.Fatalf("%s: %s", name, diags.Error())
		}
		body, ok := file.Body.(*hclsyntax.Body)
		if !ok {
			t.Fatalf("%s: not native syntax", name)
		}

		return body
	}
	t.Fatalf("Render() wrote no %s", name)

	return nil
}

// findResource is the block of the resource named type.name, or nil when the file has
// none.
func findResource(body *hclsyntax.Body, address string) *hclsyntax.Block {
	for _, b := range body.Blocks {
		if b.Type == "resource" && len(b.Labels) == 2 && b.Labels[0]+"."+b.Labels[1] == address {
			return b
		}
	}

	return nil
}

// requestLogContext is what the exclusion's expressions read, with imp-stg's harbor
// names in: the stack's locals, the clause among them as logging.tf declares it, the
// regional services and the backend by name, and join.
func requestLogContext(t *testing.T, body *hclsyntax.Body, pullRequest bool) *hcl.EvalContext {
	t.Helper()

	locals := localsOf(t, body)
	clause, ok := locals["request_log_clause"]
	if !ok {
		t.Fatalf("logging.tf declares no request_log_clause")
	}
	service := func(name string) cty.Value {
		return cty.ObjectVal(map[string]cty.Value{"name": cty.StringVal(name)})
	}

	return &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"local": cty.ObjectVal(map[string]cty.Value{
				"is_pr":              cty.BoolVal(pullRequest),
				"project_id":         cty.StringVal("imp-stg-gbl-core-3c4d"),
				"name":               cty.StringVal("imp-stg"),
				"app":                cty.StringVal("harbor"),
				"request_log_clause": evalValue(t, clause.Expr, &hcl.EvalContext{}),
			}),
			"var": cty.ObjectVal(map[string]cty.Value{"environment": cty.StringVal("stg")}),
			"google_cloud_run_v2_service": cty.ObjectVal(map[string]cty.Value{
				"app": cty.MapVal(map[string]cty.Value{"uc1": service("imp-stg-uc1-harbor-app"), "ue4": service("imp-stg-ue4-harbor-app")}),
			}),
			"google_compute_backend_service": cty.ObjectVal(map[string]cty.Value{
				"app": cty.TupleVal([]cty.Value{service("imp-stg-gbl-harbor-backend")}),
			}),
		},
		Functions: map[string]function.Function{"join": stdlib.JoinFunc},
	}
}

// evalValue evaluates the expression, failing the test on a diagnostic.
func evalValue(t *testing.T, expr hcl.Expression, ctx *hcl.EvalContext) cty.Value {
	t.Helper()

	value, diags := expr.Value(ctx)
	if diags.HasErrors() {
		t.Fatalf("%s", diags.Error())
	}

	return value
}

// TestRequestLogClauseQuoted holds the clause logging.tf declares to what the derivation
// wrote: the HCL literal reads back as the clause, so a quote in a Logging string
// survives the template.
func TestRequestLogClauseQuoted(t *testing.T) {
	t.Parallel()

	m := deriveFixture(t, "harbor", "placement.json")
	m.Surfaces = lodestarSurfaces
	m.RequestLog = derive.NewRequestLogExclusion(lodestarSurfaces)
	body := renderedBody(t, m, "logging.tf")
	clause := evalValue(t, localsOf(t, body)["request_log_clause"].Expr, &hcl.EvalContext{}).AsString()
	if clause != m.RequestLog.Clause {
		t.Errorf("request_log_clause = %q, want %q", clause, m.RequestLog.Clause)
	}
	if !strings.Contains(clause, `"^https://[^/]+/beacons/"`) {
		t.Errorf("request_log_clause = %q lacks the quoted beacons expression", clause)
	}
}
