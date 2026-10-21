package render

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// TestRouteExpression matches a route under its method, or under any.
func TestRouteExpression(t *testing.T) {
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

			if got := routeExpression(tt.method, tt.path); got != tt.want {
				t.Errorf("routeExpression() = %q, want %q", got, tt.want)
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
// trailing sets scoped to the outlets' prefixes with their exclusions, the sign-in
// routes throttled by client address after them, each naming its source; a site with no
// outlet prefix scopes nothing; a model with no sign-in route throttles nothing; and a
// placement writing nothing is the defaults.
func TestArmorView(t *testing.T) {
	t.Parallel()

	type rule struct {
		Priority   int
		Action     string
		Verb       string
		Deny       bool
		Throttle   bool
		Text       string
		Expression string
		Exclusions int
	}
	summarize := func(v *armorView) []rule {
		rules := make([]rule, 0, len(v.Rules))
		for _, r := range v.Rules {
			rules = append(rules, rule{Priority: r.Priority, Action: r.Action, Verb: r.Verb, Deny: r.Deny, Throttle: r.Throttle, Text: r.Text, Expression: r.Expression, Exclusions: len(r.Exclusions)})
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
		wantSignIn  string
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
				{Priority: 1000, Action: "deny(403)", Verb: "deny", Deny: true, Text: "scanner detection (scannerdetection-v33-stable, sensitivity 1) on every path; bedrock's default rule sets", Expression: `"evaluatePreconfiguredWaf('scannerdetection-v33-stable', {'sensitivity': 1})"`},
				{Priority: 3000, Action: "deny(403)", Verb: "deny", Deny: true, Text: "SQL injection (sqli-v33-stable, sensitivity 1) on /api; bedrock's default rule sets", Expression: `"request.path.startsWith('/api/') && evaluatePreconfiguredWaf('sqli-v33-stable', {'sensitivity': 1})"`},
				{Priority: 3010, Action: "deny(403)", Verb: "deny", Deny: true, Text: "SQL injection in JSON bodies (json-sqli-canary, sensitivity 1) on /api; bedrock's default rule sets", Expression: `"request.path.startsWith('/api/') && evaluatePreconfiguredWaf('json-sqli-canary', {'sensitivity': 1})"`},
				{Priority: 3020, Action: "deny(403)", Verb: "deny", Deny: true, Text: "cross-site scripting (xss-v33-stable, sensitivity 1) on /api; bedrock's default rule sets", Expression: `"request.path.startsWith('/api/') && evaluatePreconfiguredWaf('xss-v33-stable', {'sensitivity': 1})"`},
				{Priority: 3030, Action: "deny(403)", Verb: "deny", Deny: true, Text: "protocol attacks (protocolattack-v33-stable, sensitivity 1) on /api; bedrock's default rule sets", Expression: `"request.path.startsWith('/api/') && evaluatePreconfiguredWaf('protocolattack-v33-stable', {'sensitivity': 1})"`},
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
				SignInRoutes: []derive.SignInRoute{
					{Outlet: "default", Method: http.MethodPost, Path: "/api/user/password/login", Kind: derive.SignInPassword},
					{Outlet: "default", Method: http.MethodPost, Path: "/api/user/pending/confirm", Kind: derive.SignInConfirm},
					{Outlet: "portal", Method: http.MethodPost, Path: "/portal/api/user/pending/cancel", Kind: derive.SignInCancel},
				},
			},
			wantScope: "(request.path.startsWith('/api/') || request.path.startsWith('/droids/') || request.path.startsWith('/portal/api/'))",
			wantProse: "/api, /droids, and /portal/api",
			wantRules: []rule{
				{Priority: 1000, Action: "deny(403)", Verb: "deny", Deny: true, Text: "scanner detection (scannerdetection-v33-stable, sensitivity 1) on every path; placement.json cloudArmor.ruleSets", Expression: `"evaluatePreconfiguredWaf('scannerdetection-v33-stable', {'sensitivity': 1})"`},
				{Priority: 2000, Action: "allow", Verb: "allow", Text: "POST /api/attach-manifest; the @upload method AttachManifest, pkg/router/zz_gen_release.json; its body is a file, not JSON", Expression: `"request.method == 'POST' && request.path.matches('^/api/attach-manifest$')"`},
				{Priority: 2001, Action: "allow", Verb: "allow", Text: "GET /portal/api/manifests/{id}/file; the @file column Manifest.Key, pkg/router/zz_gen_release.json; its answer is the stored object", Expression: `"request.method == 'GET' && request.path.matches('^/portal/api/manifests/[^/]+/file$')"`},
				{Priority: 2002, Action: "allow", Verb: "allow", Text: "POST /hooks/registry; placement.json cloudArmor.bypasses; signed by the registry", Expression: `"request.method == 'POST' && request.path.matches('^/hooks/registry$')"`},
				{Priority: 2003, Action: "allow", Verb: "allow", Text: "/streams/*; placement.json cloudArmor.bypasses; bytes", Expression: `"request.path.matches('^/streams/.*$')"`},
				{Priority: 3000, Action: "deny(403)", Verb: "deny", Deny: true, Text: "cross-site scripting (xss-v33-stable, sensitivity 2) on /api, /droids, and /portal/api; placement.json cloudArmor.ruleSets", Expression: `"(request.path.startsWith('/api/') || request.path.startsWith('/droids/') || request.path.startsWith('/portal/api/')) && evaluatePreconfiguredWaf('xss-v33-stable', {'sensitivity': 2})"`, Exclusions: 2},
				{Priority: 4000, Action: "throttle", Verb: "throttle", Throttle: true, Text: "POST /api/user/password/login, throttled by client address; the default outlet's password login, the generated router; past the sign-in count in its interval (terraform.tfvars) an address answers 429", Expression: `"request.method == 'POST' && request.path.matches('^/api/user/password/login$')"`},
				{Priority: 4001, Action: "throttle", Verb: "throttle", Throttle: true, Text: "POST /api/user/pending/confirm, throttled by client address; the default outlet's pending sign-in confirmation, the generated router; past the sign-in count in its interval (terraform.tfvars) an address answers 429", Expression: `"request.method == 'POST' && request.path.matches('^/api/user/pending/confirm$')"`},
				{Priority: 4002, Action: "throttle", Verb: "throttle", Throttle: true, Text: "POST /portal/api/user/pending/cancel, throttled by client address; the portal outlet's pending sign-in cancel, the generated router; past the sign-in count in its interval (terraform.tfvars) an address answers 429", Expression: `"request.method == 'POST' && request.path.matches('^/portal/api/user/pending/cancel$')"`},
			},
			wantSignIn: "`POST /api/user/password/login`, `POST /api/user/pending/confirm`, and `POST /portal/api/user/pending/cancel`",
			wantExcl: []string{
				"xss-v33-stable request_cookie EQUALS \"harbor_session\" [] the cookie `harbor_session` under every rule of the set",
				"xss-v33-stable request_query_param EQUALS_ANY  [\"owasp-crs-v030301-id941100-xss\", \"owasp-crs-v030301-id941110-xss\"] every query parameter under `owasp-crs-v030301-id941100-xss` and `owasp-crs-v030301-id941110-xss`",
			},
		},
		{
			name: "a long outlet declaration: the throttle's description is cut at a word boundary within the limit, the text whole",
			m: &derive.Model{
				Placement: &derive.Placement{CloudArmor: &derive.CloudArmor{RuleSets: []derive.RuleSet{{Name: derive.RuleSetSQLi, Sensitivity: 1}}}},
				Outlets:   []derive.Outlet{{Name: "the-partner-portal-for-the-regional-distributors-and-their-field-technicians", Prefix: "partner-portal-for-the-regional-distributors-and-their-field-technicians/api"}},
				SignInRoutes: []derive.SignInRoute{
					{Outlet: "the-partner-portal-for-the-regional-distributors-and-their-field-technicians", Method: http.MethodPost, Path: "/partner-portal-for-the-regional-distributors-and-their-field-technicians/api/user/pending/confirm", Kind: derive.SignInConfirm},
				},
			},
			wantScope: "request.path.startsWith('/partner-portal-for-the-regional-distributors-and-their-field-technicians/api/')",
			wantProse: "/partner-portal-for-the-regional-distributors-and-their-field-technicians/api",
			wantRules: []rule{
				{Priority: 3000, Action: "deny(403)", Verb: "deny", Deny: true, Text: "SQL injection (sqli-v33-stable, sensitivity 1) on /partner-portal-for-the-regional-distributors-and-their-field-technicians/api; placement.json cloudArmor.ruleSets", Expression: `"request.path.startsWith('/partner-portal-for-the-regional-distributors-and-their-field-technicians/api/') && evaluatePreconfiguredWaf('sqli-v33-stable', {'sensitivity': 1})"`},
				{Priority: 4000, Action: "throttle", Verb: "throttle", Throttle: true, Text: "POST /partner-portal-for-the-regional-distributors-and-their-field-technicians/api/user/pending/confirm, throttled by client address; the the-partner-portal-for-the-regional-distributors-and-their-field-technicians outlet's pending sign-in confirmation, the generated router; past the sign-in count in its interval (terraform.tfvars) an address answers 429", Expression: `"request.method == 'POST' && request.path.matches('^/partner-portal-for-the-regional-distributors-and-their-field-technicians/api/user/pending/confirm$')"`},
			},
			wantSignIn: "`POST /partner-portal-for-the-regional-distributors-and-their-field-technicians/api/user/pending/confirm`",
		},
		{
			name: "no outlet prefix: the trailing sets run on every path",
			m: &derive.Model{
				Placement: &derive.Placement{CloudArmor: &derive.CloudArmor{RuleSets: []derive.RuleSet{{Name: derive.RuleSetSQLi, Sensitivity: 1}}}},
				Outlets:   []derive.Outlet{{Name: "default"}},
			},
			wantRules: []rule{
				{Priority: 3000, Action: "deny(403)", Verb: "deny", Deny: true, Text: "SQL injection (sqli-v33-stable, sensitivity 1) on every path; placement.json cloudArmor.ruleSets", Expression: `"evaluatePreconfiguredWaf('sqli-v33-stable', {'sensitivity': 1})"`},
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
			if v.Throttled != (tt.wantSignIn != "") || v.SignInProse != tt.wantSignIn {
				t.Errorf("Throttled = %v, SignInProse = %q; want %v, %q", v.Throttled, v.SignInProse, tt.wantSignIn != "", tt.wantSignIn)
			}
			if v.SignInCount != "30" || v.SignInInterval != "60" {
				t.Errorf("SignInCount, SignInInterval = %q, %q; want the defaults 30, 60", v.SignInCount, v.SignInInterval)
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
				if r.Description != hclQuote(fitDescription(r.Text)) {
					t.Errorf("rule %d: Description = %s, want the text within the limit, quoted", r.Priority, r.Description)
				}
				if n := len(fitDescription(r.Text)); n > armorDescriptionLimit {
					t.Errorf("rule %d: description is %d characters, over Google's limit of %d", r.Priority, n, armorDescriptionLimit)
				}
			}
		})
	}
}

func TestFitDescription(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("word ", 60) + "end"
	tests := []struct {
		name string
		text string
		want string
	}{
		{name: "a text within the limit is itself", text: "POST /api/user/password/login, throttled by client address; the default outlet's password login", want: "POST /api/user/password/login, throttled by client address; the default outlet's password login"},
		{name: "a text of exactly the limit is itself", text: strings.Repeat("x", 256), want: strings.Repeat("x", 256)},
		{name: "a long text is cut at a word boundary with the ending", text: long, want: strings.TrimSuffix(long[:253][:strings.LastIndex(long[:253], " ")], " ") + "..."},
		{name: "a long text without a space is cut hard", text: strings.Repeat("y", 300), want: strings.Repeat("y", 253) + "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := fitDescription(tt.text)
			if got != tt.want {
				t.Errorf("fitDescription() = %q, want %q", got, tt.want)
			}
			if len(got) > armorDescriptionLimit {
				t.Errorf("len = %d, over %d", len(got), armorDescriptionLimit)
			}
		})
	}
}
