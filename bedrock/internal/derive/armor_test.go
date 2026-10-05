package derive

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestCloudArmorValidate holds the policy a placement may write: the defaults for no
// block, a rule set bedrock does not know or listed twice or at a sensitivity outside the
// range refused, an exclusion for a set the policy does not evaluate or of a shape the
// API has none for refused, and a bypass whose path is not a route's, whose star is not
// last, whose method is not one, listed twice or without a reason refused.
func TestCloudArmorValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		set     *CloudArmor
		wantErr string
	}{
		{name: "no block", set: nil},
		{name: "an empty block", set: &CloudArmor{}},
		{name: "the defaults written out", set: &CloudArmor{RuleSets: DefaultRuleSets()}},
		{name: "every known set at every sensitivity", set: &CloudArmor{RuleSets: []RuleSet{{Name: RuleSetLFI, Sensitivity: 4}, {Name: RuleSetCVE, Sensitivity: 3}, {Name: RuleSetMethodEnforcement, Sensitivity: 2}}}},
		{
			name: "exclusions of every field and operator, and bypasses of every shape",
			set: &CloudArmor{
				FieldExclusions: []FieldExclusion{
					{RuleSet: RuleSetXSS, Field: FieldCookie, Value: "session", Reason: "random bytes"},
					{RuleSet: RuleSetSQLi, Field: FieldHeader, Operator: OperatorStartsWith, Value: "X-Harbor-", Reason: "signed"},
					{RuleSet: RuleSetSQLi, Field: FieldQueryParam, Operator: OperatorAny, Reason: "the API reads none", Rules: []string{"owasp-crs-v030301-id942100-sqli"}},
					{RuleSet: RuleSetProtocolAttack, Field: FieldURI, Operator: OperatorContains, Value: "%2F", Reason: "encoded slashes in keys"},
				},
				Bypasses: []Bypass{
					{Path: "/hooks/registry", Method: http.MethodPost, Reason: "signed by the sender"},
					{Path: "/streams/{id}/audio", Reason: "bytes"},
					{Path: "/streams/*", Reason: "bytes"},
					{Path: "/hooks/registry", Method: http.MethodGet, Reason: "the registry's probe"},
				},
			},
		},
		{name: "a set bedrock does not know", set: &CloudArmor{RuleSets: []RuleSet{{Name: "sqli-v42-stable", Sensitivity: 1}}}, wantErr: `cloudArmor.ruleSets names "sqli-v42-stable", which is not a rule set bedrock knows: scannerdetection-v33-stable, sqli-v33-stable, json-sqli-canary, xss-v33-stable, protocolattack-v33-stable, lfi-v33-stable`},
		{name: "a set listed twice", set: &CloudArmor{RuleSets: []RuleSet{{Name: RuleSetSQLi, Sensitivity: 1}, {Name: RuleSetSQLi, Sensitivity: 2}}}, wantErr: "cloudArmor.ruleSets lists sqli-v33-stable twice"},
		{name: "a sensitivity of zero", set: &CloudArmor{RuleSets: []RuleSet{{Name: RuleSetSQLi}}}, wantErr: "cloudArmor.ruleSets sqli-v33-stable has the sensitivity 0; a sensitivity is 1 (the rules least likely to misfire) to 4 (every rule)"},
		{name: "a sensitivity above four", set: &CloudArmor{RuleSets: []RuleSet{{Name: RuleSetSQLi, Sensitivity: 5}}}, wantErr: "cloudArmor.ruleSets sqli-v33-stable has the sensitivity 5"},
		{name: "an exclusion for a set the policy does not evaluate", set: &CloudArmor{FieldExclusions: []FieldExclusion{{RuleSet: RuleSetLFI, Field: FieldCookie, Value: "s", Reason: "r"}}}, wantErr: `cloudArmor.fieldExclusions names the rule set "lfi-v33-stable", which the policy does not evaluate; its rule sets are scannerdetection-v33-stable, sqli-v33-stable, json-sqli-canary, xss-v33-stable, protocolattack-v33-stable`},
		{name: "an exclusion for a set the placement dropped", set: &CloudArmor{RuleSets: []RuleSet{{Name: RuleSetSQLi, Sensitivity: 1}}, FieldExclusions: []FieldExclusion{{RuleSet: RuleSetXSS, Field: FieldCookie, Value: "s", Reason: "r"}}}, wantErr: `names the rule set "xss-v33-stable", which the policy does not evaluate; its rule sets are sqli-v33-stable`},
		{name: "an exclusion of a field kind the API has none for", set: &CloudArmor{FieldExclusions: []FieldExclusion{{RuleSet: RuleSetXSS, Field: "body", Value: "s", Reason: "r"}}}, wantErr: `cloudArmor.fieldExclusions for xss-v33-stable has the field "body"; a field is header, cookie, queryParam or uri`},
		{name: "an exclusion with an operator the API has none for", set: &CloudArmor{FieldExclusions: []FieldExclusion{{RuleSet: RuleSetXSS, Field: FieldCookie, Operator: "matches", Value: "s", Reason: "r"}}}, wantErr: `cloudArmor.fieldExclusions for xss-v33-stable has the operator "matches"; an operator is equals, startsWith, endsWith, contains or any`},
		{name: "an exclusion naming a field under any", set: &CloudArmor{FieldExclusions: []FieldExclusion{{RuleSet: RuleSetXSS, Field: FieldCookie, Operator: OperatorAny, Value: "s", Reason: "r"}}}, wantErr: `cloudArmor.fieldExclusions for xss-v33-stable names the cookie "s" under any, which names every cookie and takes no value`},
		{name: "an exclusion naming no field", set: &CloudArmor{FieldExclusions: []FieldExclusion{{RuleSet: RuleSetXSS, Field: FieldHeader, Reason: "r"}}}, wantErr: "cloudArmor.fieldExclusions for xss-v33-stable names no header; a value is the field's name, or the part of it the operator reads"},
		{name: "an exclusion with a rule id of the wrong shape", set: &CloudArmor{FieldExclusions: []FieldExclusion{{RuleSet: RuleSetXSS, Field: FieldCookie, Value: "s", Rules: []string{"941100 XSS"}, Reason: "r"}}}, wantErr: `cloudArmor.fieldExclusions for xss-v33-stable names the rule id "941100 XSS", which is not one (owasp-crs-v030301-id941100-xss)`},
		{name: "an exclusion without a reason", set: &CloudArmor{FieldExclusions: []FieldExclusion{{RuleSet: RuleSetXSS, Field: FieldCookie, Value: "s", Reason: " "}}}, wantErr: "cloudArmor.fieldExclusions for xss-v33-stable on the cookie gives no reason"},
		{name: "a bypass whose path is not a route's", set: &CloudArmor{Bypasses: []Bypass{{Path: "hooks/registry", Reason: "r"}}}, wantErr: `cloudArmor.bypasses path "hooks/registry" is not a route's shape (/hooks/registry, /streams/{id}/audio, /streams/*)`},
		{name: "a bypass with a space", set: &CloudArmor{Bypasses: []Bypass{{Path: "/hooks/the registry", Reason: "r"}}}, wantErr: `cloudArmor.bypasses path "/hooks/the registry" is not a route's shape`},
		{name: "a bypass with a star before its last segment", set: &CloudArmor{Bypasses: []Bypass{{Path: "/streams/*/audio", Reason: "r"}}}, wantErr: `cloudArmor.bypasses path "/streams/*/audio" has a star before its last segment; a star takes the subtree, so it comes last`},
		{name: "a bypass with a method that is not one", set: &CloudArmor{Bypasses: []Bypass{{Path: "/hooks/registry", Method: "post", Reason: "r"}}}, wantErr: `cloudArmor.bypasses /hooks/registry has the method "post"; a method is one of GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS, or left out for every method`},
		{name: "a bypass without a reason", set: &CloudArmor{Bypasses: []Bypass{{Path: "/hooks/registry", Method: http.MethodPost}}}, wantErr: "cloudArmor.bypasses POST /hooks/registry gives no reason"},
		{name: "a bypass listed twice", set: &CloudArmor{Bypasses: []Bypass{{Path: "/hooks/registry", Reason: "r"}, {Path: "/hooks/registry", Reason: "again"}}}, wantErr: "cloudArmor.bypasses lists /hooks/registry twice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.set.Validate()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Validate() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

// TestCloudArmorResolved splits the policy by where its sets run: the defaults for no
// block or no list, a leading set wherever the list puts it, the exclusions and the
// bypasses carried over, and IsDefault for what a placement writing nothing gets.
func TestCloudArmorResolved(t *testing.T) {
	t.Parallel()

	defaults := DefaultRuleSets()
	tests := []struct {
		name        string
		set         *CloudArmor
		want        Armor
		wantDefault bool
	}{
		{name: "no block", set: nil, want: Armor{Leading: defaults[:1], Trailing: defaults[1:]}, wantDefault: true},
		{name: "an empty block", set: &CloudArmor{}, want: Armor{Leading: defaults[:1], Trailing: defaults[1:]}, wantDefault: true},
		{name: "the defaults written out", set: &CloudArmor{RuleSets: DefaultRuleSets()}, want: Armor{Leading: defaults[:1], Trailing: defaults[1:]}, wantDefault: true},
		{
			name: "scanner detection written last still leads",
			set:  &CloudArmor{RuleSets: []RuleSet{{Name: RuleSetSQLi, Sensitivity: 2}, {Name: RuleSetScannerDetection, Sensitivity: 1}}},
			want: Armor{Leading: []RuleSet{{Name: RuleSetScannerDetection, Sensitivity: 1}}, Trailing: []RuleSet{{Name: RuleSetSQLi, Sensitivity: 2}}},
		},
		{
			name: "no leading set",
			set:  &CloudArmor{RuleSets: []RuleSet{{Name: RuleSetXSS, Sensitivity: 1}}},
			want: Armor{Trailing: []RuleSet{{Name: RuleSetXSS, Sensitivity: 1}}},
		},
		{
			name: "a bypass alone is not the default",
			set:  &CloudArmor{Bypasses: []Bypass{{Path: "/hooks/*", Reason: "signed"}}},
			want: Armor{Leading: defaults[:1], Trailing: defaults[1:], Bypasses: []Bypass{{Path: "/hooks/*", Reason: "signed"}}},
		},
		{
			name: "an exclusion alone is not the default",
			set:  &CloudArmor{FieldExclusions: []FieldExclusion{{RuleSet: RuleSetXSS, Field: FieldCookie, Value: "s", Reason: "r"}}},
			want: Armor{Leading: defaults[:1], Trailing: defaults[1:], FieldExclusions: []FieldExclusion{{RuleSet: RuleSetXSS, Field: FieldCookie, Value: "s", Reason: "r"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.set.Resolved()
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Resolved() mismatch (-want +got):\n%s", diff)
			}
			if got.IsDefault() != tt.wantDefault {
				t.Errorf("IsDefault() = %v, want %v", got.IsDefault(), tt.wantDefault)
			}
			if diff := cmp.Diff(append(append([]RuleSet{}, tt.want.Leading...), tt.want.Trailing...), got.RuleSets()); diff != "" {
				t.Errorf("RuleSets() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestCloudArmorClone: a copy shares nothing with the original, and nil stays nil.
func TestCloudArmorClone(t *testing.T) {
	t.Parallel()

	var none *CloudArmor
	if none.Clone() != nil {
		t.Error("Clone() of nil is not nil")
	}
	set := &CloudArmor{
		RuleSets:        []RuleSet{{Name: RuleSetSQLi, Sensitivity: 1}},
		FieldExclusions: []FieldExclusion{{RuleSet: RuleSetSQLi, Field: FieldCookie, Value: "s", Rules: []string{"owasp-crs-v030301-id942100-sqli"}, Reason: "r"}},
		Bypasses:        []Bypass{{Path: "/hooks/*", Reason: "signed"}},
	}
	got := set.Clone()
	if diff := cmp.Diff(set, got); diff != "" {
		t.Fatalf("Clone() mismatch (-want +got):\n%s", diff)
	}
	got.RuleSets[0].Sensitivity, got.FieldExclusions[0].Rules[0], got.Bypasses[0].Path = 4, "other", "/other"
	if set.RuleSets[0].Sensitivity != 1 || set.FieldExclusions[0].Rules[0] != "owasp-crs-v030301-id942100-sqli" || set.Bypasses[0].Path != "/hooks/*" {
		t.Error("the copy shares storage with the original")
	}
}

// TestExclusionSpellings maps each field kind to the provider's block and each operator
// to the API's name, equals when the placement writes none.
func TestExclusionSpellings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		e            FieldExclusion
		wantBlock    string
		wantOperator string
	}{
		{name: "a header, equals by default", e: FieldExclusion{Field: FieldHeader}, wantBlock: "request_header", wantOperator: "EQUALS"},
		{name: "a cookie, starts with", e: FieldExclusion{Field: FieldCookie, Operator: OperatorStartsWith}, wantBlock: "request_cookie", wantOperator: "STARTS_WITH"},
		{name: "a query parameter, ends with", e: FieldExclusion{Field: FieldQueryParam, Operator: OperatorEndsWith}, wantBlock: "request_query_param", wantOperator: "ENDS_WITH"},
		{name: "the path, contains", e: FieldExclusion{Field: FieldURI, Operator: OperatorContains}, wantBlock: "request_uri", wantOperator: "CONTAINS"},
		{name: "any", e: FieldExclusion{Field: FieldHeader, Operator: OperatorAny}, wantOperator: "EQUALS_ANY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := tt.e.Block(); tt.wantBlock != "" && got != tt.wantBlock {
				t.Errorf("Block() = %q, want %q", got, tt.wantBlock)
			}
			if got := tt.e.OperatorName(); got != tt.wantOperator {
				t.Errorf("OperatorName() = %q, want %q", got, tt.wantOperator)
			}
		})
	}
}

// TestModelArmor refuses a placement bypass the generated router already derives, under
// the route's method or any, and admits one for another route or another method.
func TestModelArmor(t *testing.T) {
	t.Parallel()

	routes := []FileRoute{
		{Kind: FileRouteUpload, Method: http.MethodPost, Path: "/api/attach-manifest", Source: "AttachManifest"},
		{Kind: FileRouteStored, Method: http.MethodGet, Path: "/api/manifests/{id}/file", Source: "Manifest.Key"},
	}
	tests := []struct {
		name     string
		bypasses []Bypass
		wantErr  string
	}{
		{name: "no bypass"},
		{name: "another route", bypasses: []Bypass{{Path: "/hooks/registry", Method: http.MethodPost, Reason: "signed"}}},
		{name: "the upload's path under another method", bypasses: []Bypass{{Path: "/api/attach-manifest", Method: http.MethodGet, Reason: "a probe"}}},
		{name: "the upload's route", bypasses: []Bypass{{Path: "/api/attach-manifest", Method: http.MethodPost, Reason: "a file"}}, wantErr: "placement.json cloudArmor.bypasses names POST /api/attach-manifest, which the generated router already lists as a file route (the @upload method AttachManifest, zz_gen_release.json); remove it"},
		{name: "the stored file's path under any method", bypasses: []Bypass{{Path: "/api/manifests/{id}/file", Reason: "a file"}}, wantErr: "names /api/manifests/{id}/file, which the generated router already lists as a file route (the @file column Manifest.Key, zz_gen_release.json); remove it"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := &Model{Placement: &Placement{CloudArmor: &CloudArmor{Bypasses: tt.bypasses}}, FileRoutes: routes}
			err := m.armor()
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("armor() error = %v, wantErr %q", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("armor() error = %v", err)
			}
		})
	}
}

// TestKnownRuleSets pins the table's shape: the defaults first, scanner detection the
// one leading set, every name findable and every default at sensitivity 1.
func TestKnownRuleSets(t *testing.T) {
	t.Parallel()

	known := KnownRuleSets()
	if len(known) < defaultRuleSets {
		t.Fatalf("KnownRuleSets() has %d sets, fewer than the %d defaults", len(known), defaultRuleSets)
	}
	for i, s := range DefaultRuleSets() {
		if s.Name != known[i].Name || s.Sensitivity != DefaultSensitivity {
			t.Errorf("DefaultRuleSets()[%d] = %+v, want %s at sensitivity %d", i, s, known[i].Name, DefaultSensitivity)
		}
	}
	for _, k := range known {
		got, ok := RuleSetNamed(k.Name)
		if !ok || got != k {
			t.Errorf("RuleSetNamed(%q) = %+v, %v", k.Name, got, ok)
		}
		if k.Leading != (k.Name == RuleSetScannerDetection) {
			t.Errorf("%s leading = %v; scanner detection alone leads", k.Name, k.Leading)
		}
		if (RuleSet{Name: k.Name}).Detects() != k.Detects {
			t.Errorf("%s Detects() = %q, want %q", k.Name, (RuleSet{Name: k.Name}).Detects(), k.Detects)
		}
	}
	if _, ok := RuleSetNamed("sqli-v42-stable"); ok {
		t.Error("RuleSetNamed() knows a set outside the table")
	}
	if got := (RuleSet{Name: "sqli-v42-stable"}).Detects(); got != "sqli-v42-stable" {
		t.Errorf("Detects() of an unknown set = %q, want the name", got)
	}
}
