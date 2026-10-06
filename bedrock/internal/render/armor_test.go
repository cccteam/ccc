package render

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// TestPathRegex turns a route into the expression Cloud Armor matches the path against:
// a parameter matches one segment, a last star the subtree, a dot itself through a
// class, and nothing carries a backslash.
func TestPathRegex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		route string
		want  string
	}{
		{name: "a plain route", route: "/api/attach-manifest", want: "/api/attach-manifest"},
		{name: "a parameter", route: "/api/manifests/{id}/file", want: "/api/manifests/[^/]+/file"},
		{name: "two parameters", route: "/api/domains/{domainId}/photos/{id}/file", want: "/api/domains/[^/]+/photos/[^/]+/file"},
		{name: "a subtree", route: "/streams/*", want: "/streams/.*"},
		{name: "a dot", route: "/hooks/v1.2/registry", want: "/hooks/v1[.]2/registry"},
		{name: "a prefix", route: "portal/api", want: "portal/api"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := pathRegex(tt.route)
			if got != tt.want {
				t.Errorf("pathRegex(%q) = %q, want %q", tt.route, got, tt.want)
			}
			if strings.Contains(got, `\`) {
				t.Errorf("pathRegex(%q) = %q carries a backslash", tt.route, got)
			}
		})
	}
}

// TestBypassExpression matches a route under its method, or under any.
func TestBypassExpression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		want   string
	}{
		{name: "under a method", method: "POST", path: "/api/attach-manifest", want: "request.method == 'POST' && request.path.matches('^/api/attach-manifest$')"},
		{name: "under any", path: "/streams/*", want: "request.path.matches('^/streams/.*$')"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := bypassExpression(tt.method, tt.path); got != tt.want {
				t.Errorf("bypassExpression() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestHclQuote writes a string literal HCL reads back as the text: quotes, backslashes
// and newlines escaped, and the template sequences written as text.
func TestHclQuote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    string
		want string
	}{
		{name: "plain", s: "a route", want: `"a route"`},
		{name: "a quote and a backslash", s: `say "hi" \ there`, want: `"say \"hi\" \\ there"`},
		{name: "an interpolation", s: "${var.x} and %{if}", want: `"$${var.x} and %%{if}"`},
		{name: "a newline", s: "a\nb", want: `"a\nb"`},
		{name: "braces alone are text", s: "/api/{id}", want: `"/api/{id}"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := hclQuote(tt.s); got != tt.want {
				t.Errorf("hclQuote(%q) = %s, want %s", tt.s, got, tt.want)
			}
		})
	}
}

// TestArmorView builds the policy's rules in order from a model: the leading sets on
// every path, the router's file routes and then the placement's bypasses allowed, the
// trailing sets scoped to the outlets' prefixes with their exclusions, each naming its
// source; a site with no outlet prefix scopes nothing; and a placement writing nothing is
// the defaults.
func TestArmorView(t *testing.T) {
	t.Parallel()

	type rule struct {
		Priority   int
		Action     string
		Deny       bool
		Text       string
		Expression string
		Exclusions int
	}
	summarize := func(v *armorView) []rule {
		rules := make([]rule, 0, len(v.Rules))
		for _, r := range v.Rules {
			rules = append(rules, rule{Priority: r.Priority, Action: r.Action, Deny: r.Deny, Text: r.Text, Expression: r.Expression, Exclusions: len(r.Exclusions)})
		}

		return rules
	}
	tests := []struct {
		name        string
		m           *derive.Model
		wantScope   string
		wantProse   string
		wantDefault bool
		wantRules   []rule
		wantExcl    []string
	}{
		{
			name: "nothing written: the defaults on one outlet",
			m: &derive.Model{
				Placement: &derive.Placement{},
				RouterDir: "pkg/router",
				Outlets:   []derive.Outlet{{Name: "default", Prefix: "api"}},
			},
			wantScope:   "request.path.startsWith('/api/')",
			wantProse:   "/api",
			wantDefault: true,
			wantRules: []rule{
				{Priority: 1000, Action: "deny(403)", Deny: true, Text: "scanner detection (scannerdetection-v33-stable, sensitivity 1) on every path; bedrock's default rule sets", Expression: `"evaluatePreconfiguredWaf('scannerdetection-v33-stable', {'sensitivity': 1})"`},
				{Priority: 3000, Action: "deny(403)", Deny: true, Text: "SQL injection (sqli-v33-stable, sensitivity 1) on /api; bedrock's default rule sets", Expression: `"request.path.startsWith('/api/') && evaluatePreconfiguredWaf('sqli-v33-stable', {'sensitivity': 1})"`},
				{Priority: 3010, Action: "deny(403)", Deny: true, Text: "SQL injection in JSON bodies (json-sqli-canary, sensitivity 1) on /api; bedrock's default rule sets", Expression: `"request.path.startsWith('/api/') && evaluatePreconfiguredWaf('json-sqli-canary', {'sensitivity': 1})"`},
				{Priority: 3020, Action: "deny(403)", Deny: true, Text: "cross-site scripting (xss-v33-stable, sensitivity 1) on /api; bedrock's default rule sets", Expression: `"request.path.startsWith('/api/') && evaluatePreconfiguredWaf('xss-v33-stable', {'sensitivity': 1})"`},
				{Priority: 3030, Action: "deny(403)", Deny: true, Text: "protocol attacks (protocolattack-v33-stable, sensitivity 1) on /api; bedrock's default rule sets", Expression: `"request.path.startsWith('/api/') && evaluatePreconfiguredWaf('protocolattack-v33-stable', {'sensitivity': 1})"`},
			},
		},
		{
			name: "the placement's sets, the router's routes, the placement's bypasses and an exclusion, on three outlets",
			m: &derive.Model{
				Placement: &derive.Placement{CloudArmor: &derive.CloudArmor{
					RuleSets: []derive.RuleSet{{Name: derive.RuleSetXSS, Sensitivity: 2}, {Name: derive.RuleSetScannerDetection, Sensitivity: 1}},
					FieldExclusions: []derive.FieldExclusion{
						{RuleSet: derive.RuleSetXSS, Field: derive.FieldCookie, Value: "harbor_session", Reason: "random bytes"},
						{RuleSet: derive.RuleSetXSS, Field: derive.FieldQueryParam, Operator: derive.OperatorAny, Rules: []string{"owasp-crs-v030301-id941100-xss", "owasp-crs-v030301-id941110-xss"}, Reason: "the search box"},
					},
					Bypasses: []derive.Bypass{{Path: "/hooks/registry", Method: http.MethodPost, Reason: "signed by the registry"}, {Path: "/streams/*", Reason: "bytes"}},
				}},
				RouterDir: "pkg/router",
				Outlets:   []derive.Outlet{{Name: "default", Prefix: "api"}, {Name: "droids", Prefix: "droids"}, {Name: "portal", Prefix: "portal/api"}},
				FileRoutes: []derive.FileRoute{
					{Kind: derive.FileRouteUpload, Method: http.MethodPost, Path: "/api/attach-manifest", Source: "AttachManifest"},
					{Kind: derive.FileRouteStored, Method: http.MethodGet, Path: "/portal/api/manifests/{id}/file", Source: "Manifest.Key"},
				},
			},
			wantScope: "(request.path.startsWith('/api/') || request.path.startsWith('/droids/') || request.path.startsWith('/portal/api/'))",
			wantProse: "/api, /droids, and /portal/api",
			wantRules: []rule{
				{Priority: 1000, Action: "deny(403)", Deny: true, Text: "scanner detection (scannerdetection-v33-stable, sensitivity 1) on every path; placement.json cloudArmor.ruleSets", Expression: `"evaluatePreconfiguredWaf('scannerdetection-v33-stable', {'sensitivity': 1})"`},
				{Priority: 2000, Action: "allow", Text: "POST /api/attach-manifest; the @upload method AttachManifest, pkg/router/zz_gen_release.json; its body is a file, not JSON", Expression: `"request.method == 'POST' && request.path.matches('^/api/attach-manifest$')"`},
				{Priority: 2001, Action: "allow", Text: "GET /portal/api/manifests/{id}/file; the @file column Manifest.Key, pkg/router/zz_gen_release.json; its answer is the stored object", Expression: `"request.method == 'GET' && request.path.matches('^/portal/api/manifests/[^/]+/file$')"`},
				{Priority: 2002, Action: "allow", Text: "POST /hooks/registry; placement.json cloudArmor.bypasses; signed by the registry", Expression: `"request.method == 'POST' && request.path.matches('^/hooks/registry$')"`},
				{Priority: 2003, Action: "allow", Text: "/streams/*; placement.json cloudArmor.bypasses; bytes", Expression: `"request.path.matches('^/streams/.*$')"`},
				{Priority: 3000, Action: "deny(403)", Deny: true, Text: "cross-site scripting (xss-v33-stable, sensitivity 2) on /api, /droids, and /portal/api; placement.json cloudArmor.ruleSets", Expression: `"(request.path.startsWith('/api/') || request.path.startsWith('/droids/') || request.path.startsWith('/portal/api/')) && evaluatePreconfiguredWaf('xss-v33-stable', {'sensitivity': 2})"`, Exclusions: 2},
			},
			wantExcl: []string{
				"xss-v33-stable request_cookie EQUALS \"harbor_session\" [] the cookie `harbor_session` under every rule of the set",
				"xss-v33-stable request_query_param EQUALS_ANY  [\"owasp-crs-v030301-id941100-xss\", \"owasp-crs-v030301-id941110-xss\"] every query parameter under `owasp-crs-v030301-id941100-xss` and `owasp-crs-v030301-id941110-xss`",
			},
		},
		{
			name: "no outlet prefix: the trailing sets run on every path",
			m: &derive.Model{
				Placement: &derive.Placement{CloudArmor: &derive.CloudArmor{RuleSets: []derive.RuleSet{{Name: derive.RuleSetSQLi, Sensitivity: 1}}}},
				Outlets:   []derive.Outlet{{Name: "default"}},
			},
			wantRules: []rule{
				{Priority: 3000, Action: "deny(403)", Deny: true, Text: "SQL injection (sqli-v33-stable, sensitivity 1) on every path; placement.json cloudArmor.ruleSets", Expression: `"evaluatePreconfiguredWaf('sqli-v33-stable', {'sensitivity': 1})"`},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			v := newArmorView(tt.m)
			if v.Scope != tt.wantScope || v.ScopeProse != tt.wantProse {
				t.Errorf("Scope = %q, %q; want %q, %q", v.Scope, v.ScopeProse, tt.wantScope, tt.wantProse)
			}
			if v.Default != tt.wantDefault {
				t.Errorf("Default = %v, want %v", v.Default, tt.wantDefault)
			}
			if v.DefaultPriority != 2147483647 {
				t.Errorf("DefaultPriority = %d", v.DefaultPriority)
			}
			if diff := cmp.Diff(tt.wantRules, summarize(v)); diff != "" {
				t.Errorf("Rules mismatch (-want +got):\n%s", diff)
			}
			var excl []string
			for _, e := range v.Exclusions {
				excl = append(excl, strings.Join([]string{e.RuleSet, e.Block, e.Operator, e.Value, "[" + strings.TrimPrefix(strings.TrimSuffix(e.RuleIDList, "]"), "[") + "]", e.FieldProse, "under", e.RuleProse}, " "))
			}
			if diff := cmp.Diff(tt.wantExcl, excl); diff != "" {
				t.Errorf("Exclusions mismatch (-want +got):\n%s", diff)
			}
			for _, r := range v.Rules {
				if r.Description != hclQuote(r.Text) {
					t.Errorf("rule %d: Description = %s, want the text quoted", r.Priority, r.Description)
				}
			}
		})
	}
}
