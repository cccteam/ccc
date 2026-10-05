// armor.go holds the Cloud Armor policy a placement declares (cloudArmor): the rule sets
// the policy evaluates, in order and each at a sensitivity, the request fields a rule set
// leaves alone, and the paths the policy allows ahead of the rule sets beside the ones
// bedrock derives from the generated router. The application's stack renders the policy
// on its backend services (cloud-armor.tf) and an environment turns it on in its
// terraform.tfvars (cloud_armor), in preview first and then enforce; a placement that
// writes no block takes the reference deployment's five rule sets at sensitivity 1.

package derive

import (
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/go-playground/errors/v5"
)

// CloudArmor is a placement's Cloud Armor policy (cloudArmor). Every field is optional:
// a block left out, or an empty one, is the default rule sets with no exclusion and no
// bypass of the placement's.
type CloudArmor struct {
	// RuleSets are the preconfigured rule sets the policy evaluates, in order, each at a
	// sensitivity; absent, DefaultRuleSets. A set that reads no body (scanner detection)
	// runs ahead of the bypasses, on every path; every other set runs after them, on the
	// outlets' routes alone.
	RuleSets []RuleSet `json:"ruleSets,omitempty"`
	// FieldExclusions are the request fields a rule set does not inspect: a cookie, a
	// header, a query parameter or the path, named exactly or by part, for every rule of
	// the set or the rules named. For a field whose legitimate values trip a rule, a
	// session cookie of random bytes under the cross-site scripting rules, say.
	FieldExclusions []FieldExclusion `json:"fieldExclusions,omitempty"`
	// Bypasses are the paths the policy allows ahead of the rule sets, beside the
	// routes bedrock derives from the generated router (each @upload method's and each
	// stored file's): a route whose body is not the application's JSON, such as a
	// webhook under the Root hook whose sender signs its own body, or a media stream.
	Bypasses []Bypass `json:"bypasses,omitempty"`
}

// RuleSet is one preconfigured rule set the policy evaluates, by the name Cloud Armor
// gives it (KnownRuleSets), at a sensitivity from 1 (the rules least likely to misfire)
// to 4 (every rule).
type RuleSet struct {
	Name        string `json:"name"`
	Sensitivity int    `json:"sensitivity"`
}

// FieldExclusion is one request field a rule set does not inspect.
type FieldExclusion struct {
	// RuleSet is the set the exclusion applies to, one of the policy's.
	RuleSet string `json:"ruleSet"`
	// Field is the kind of field: header, cookie, queryParam or uri.
	Field string `json:"field"`
	// Operator says how Value names the field: equals (the default), startsWith,
	// endsWith, contains, or any, which names every field of the kind and takes no
	// Value.
	Operator string `json:"operator,omitempty"`
	// Value is the field's name, or the part of it Operator reads.
	Value string `json:"value,omitempty"`
	// Rules are the rules of the set the exclusion is for, by Cloud Armor's rule ids
	// (owasp-crs-v030301-id941100-xss); none means every rule of the set.
	Rules []string `json:"rules,omitempty"`
	// Reason says why, for the rendered policy's comment.
	Reason string `json:"reason"`
}

// Bypass is one path the policy allows ahead of the rule sets.
type Bypass struct {
	// Path is the route as the application mounts it, with its parameters in braces
	// (/hooks/registry, /streams/{id}/audio); a last segment of * takes the subtree
	// (/streams/*).
	Path string `json:"path"`
	// Method is the HTTP method the bypass is for; empty, every method.
	Method string `json:"method,omitempty"`
	// Reason says why the route carries no input the rules can judge, for the rule's
	// description.
	Reason string `json:"reason"`
}

// Armor is the policy a stack is rendered with: the placement's, with the defaults where
// it writes none, the rule sets split by where they run.
type Armor struct {
	// Leading are the rule sets that run ahead of the bypasses, on every path: the sets
	// that read no body.
	Leading []RuleSet
	// Trailing are the rule sets that run after the bypasses, on the outlets' routes.
	Trailing        []RuleSet
	FieldExclusions []FieldExclusion
	Bypasses        []Bypass
}

// KnownRuleSet is one preconfigured rule set Cloud Armor offers, as bedrock knows it.
type KnownRuleSet struct {
	// Name is the set's name in evaluatePreconfiguredWaf.
	Name string
	// Detects says what the set detects, for the rendered policy and the README.
	Detects string
	// Leading says the set reads no body, so it runs ahead of the bypasses, on every
	// path: a route that carries a file still benefits from it.
	Leading bool
}

// The preconfigured rule sets' names, as evaluatePreconfiguredWaf takes them.
const (
	RuleSetScannerDetection  = "scannerdetection-v33-stable"
	RuleSetSQLi              = "sqli-v33-stable"
	RuleSetJSONSQLi          = "json-sqli-canary"
	RuleSetXSS               = "xss-v33-stable"
	RuleSetProtocolAttack    = "protocolattack-v33-stable"
	RuleSetLFI               = "lfi-v33-stable"
	RuleSetRFI               = "rfi-v33-stable"
	RuleSetRCE               = "rce-v33-stable"
	RuleSetMethodEnforcement = "methodenforcement-v33-stable"
	RuleSetPHP               = "php-v33-stable"
	RuleSetSessionFixation   = "sessionfixation-v33-stable"
	RuleSetJava              = "java-v33-stable"
	RuleSetNodeJS            = "nodejs-v33-stable"
	RuleSetCVE               = "cve-canary"
)

// knownRuleSets are the preconfigured rule sets Cloud Armor offers today, the defaults
// first in their order. A name outside the table is refused: a set bedrock does not know
// is one its order and scope were never decided for.
var knownRuleSets = []KnownRuleSet{
	{Name: RuleSetScannerDetection, Detects: "scanner detection", Leading: true},
	{Name: RuleSetSQLi, Detects: "SQL injection"},
	{Name: RuleSetJSONSQLi, Detects: "SQL injection in JSON bodies"},
	{Name: RuleSetXSS, Detects: "cross-site scripting"},
	{Name: RuleSetProtocolAttack, Detects: "protocol attacks"},
	{Name: RuleSetLFI, Detects: "local file inclusion"},
	{Name: RuleSetRFI, Detects: "remote file inclusion"},
	{Name: RuleSetRCE, Detects: "remote code execution"},
	{Name: RuleSetMethodEnforcement, Detects: "method enforcement"},
	{Name: RuleSetPHP, Detects: "PHP injection"},
	{Name: RuleSetSessionFixation, Detects: "session fixation"},
	{Name: RuleSetJava, Detects: "Java attacks"},
	{Name: RuleSetNodeJS, Detects: "Node.js attacks"},
	{Name: RuleSetCVE, Detects: "known vulnerabilities"},
}

// The sensitivities a rule set runs at, and how many of the known sets are the defaults.
const (
	MinSensitivity     = 1
	MaxSensitivity     = 4
	DefaultSensitivity = 1
	defaultRuleSets    = 5
)

// The kinds of field an exclusion names, and the operators that name one.
const (
	FieldHeader     = "header"
	FieldCookie     = "cookie"
	FieldQueryParam = "queryParam"
	FieldURI        = "uri"

	OperatorEquals     = "equals"
	OperatorStartsWith = "startsWith"
	OperatorEndsWith   = "endsWith"
	OperatorContains   = "contains"
	OperatorAny        = "any"
)

// exclusionBlocks maps a field's kind to the block the Terraform provider takes it in,
// and exclusionOperators an operator to the API's spelling.
var (
	exclusionBlocks = map[string]string{
		FieldHeader:     "request_header",
		FieldCookie:     "request_cookie",
		FieldQueryParam: "request_query_param",
		FieldURI:        "request_uri",
	}
	exclusionOperators = map[string]string{
		OperatorEquals:     "EQUALS",
		OperatorStartsWith: "STARTS_WITH",
		OperatorEndsWith:   "ENDS_WITH",
		OperatorContains:   "CONTAINS",
		OperatorAny:        "EQUALS_ANY",
	}
	bypassMethods = []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions}
	// bypassPathRE is the shape of a bypass path: segments of letters, digits, dashes,
	// underscores, tildes and dots, a parameter in braces, or a star, under the root.
	bypassPathRE = regexp.MustCompile(`^(/(\{[A-Za-z0-9_]+\}|[A-Za-z0-9_.~-]+|\*))+$`)
	// ruleIDRE is the shape of a Cloud Armor rule id (owasp-crs-v030301-id941100-xss).
	ruleIDRE = regexp.MustCompile(`^[a-z0-9-]+$`)
)

// KnownRuleSets lists the preconfigured rule sets a placement may name, the defaults
// first.
func KnownRuleSets() []KnownRuleSet {
	return slices.Clone(knownRuleSets)
}

// RuleSetNamed is the set of the name, and whether bedrock knows it.
func RuleSetNamed(name string) (KnownRuleSet, bool) {
	for _, k := range knownRuleSets {
		if k.Name == name {
			return k, true
		}
	}

	return KnownRuleSet{}, false
}

// DefaultRuleSets are the rule sets of a placement that writes none: the reference
// deployment's five, scanner detection, SQL injection, SQL injection in JSON bodies,
// cross-site scripting and protocol attacks, at sensitivity 1.
func DefaultRuleSets() []RuleSet {
	sets := make([]RuleSet, 0, defaultRuleSets)
	for _, k := range knownRuleSets[:defaultRuleSets] {
		sets = append(sets, RuleSet{Name: k.Name, Sensitivity: DefaultSensitivity})
	}

	return sets
}

// Detects says what the set detects (KnownRuleSets); the name itself for a set bedrock
// does not know, which Validate refuses.
func (s RuleSet) Detects() string {
	if k, ok := RuleSetNamed(s.Name); ok {
		return k.Detects
	}

	return s.Name
}

// Resolved is the policy with the defaults in place of what the placement leaves out; a
// nil CloudArmor is the defaults.
func (a *CloudArmor) Resolved() Armor {
	var out Armor
	sets := DefaultRuleSets()
	if a != nil {
		if len(a.RuleSets) > 0 {
			sets = slices.Clone(a.RuleSets)
		}
		out.FieldExclusions = slices.Clone(a.FieldExclusions)
		out.Bypasses = slices.Clone(a.Bypasses)
	}
	for _, s := range sets {
		if k, _ := RuleSetNamed(s.Name); k.Leading {
			out.Leading = append(out.Leading, s)
		} else {
			out.Trailing = append(out.Trailing, s)
		}
	}

	return out
}

// RuleSets are the policy's rule sets in the order they run: the leading, then the
// trailing.
func (a Armor) RuleSets() []RuleSet {
	return append(slices.Clone(a.Leading), a.Trailing...)
}

// IsDefault reports the policy of a placement that writes nothing: the default rule
// sets, no exclusion and no bypass.
func (a Armor) IsDefault() bool {
	return slices.Equal(a.RuleSets(), DefaultRuleSets()) && len(a.FieldExclusions) == 0 && len(a.Bypasses) == 0
}

// Clone is a copy that shares nothing with a; nil stays nil.
func (a *CloudArmor) Clone() *CloudArmor {
	if a == nil {
		return nil
	}
	out := &CloudArmor{RuleSets: slices.Clone(a.RuleSets), Bypasses: slices.Clone(a.Bypasses)}
	for _, e := range a.FieldExclusions {
		e.Rules = slices.Clone(e.Rules)
		out.FieldExclusions = append(out.FieldExclusions, e)
	}

	return out
}

// Validate refuses a policy the stack could not render as meant: a rule set bedrock does
// not know, or listed twice, or at a sensitivity outside 1 to 4; an exclusion for a set
// the policy does not evaluate, of a field or an operator the API has no spelling for,
// naming no field or a field under any, or without a reason; and a bypass whose path is
// not a route's shape, whose method is not one, listed twice, or without a reason.
func (a *CloudArmor) Validate() error {
	if a == nil {
		return nil
	}
	if err := validateRuleSets(a.RuleSets); err != nil {
		return err
	}
	sets := a.Resolved().RuleSets()
	for i := range a.FieldExclusions {
		if err := a.FieldExclusions[i].validate(sets); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for i := range a.Bypasses {
		b := &a.Bypasses[i]
		if err := b.validate(); err != nil {
			return err
		}
		if seen[b.key()] {
			return errors.Newf("cloudArmor.bypasses lists %s twice", b.key())
		}
		seen[b.key()] = true
	}

	return nil
}

// validateRuleSets refuses a set bedrock does not know, one listed twice, or a
// sensitivity outside the range.
func validateRuleSets(sets []RuleSet) error {
	seen := map[string]bool{}
	for _, s := range sets {
		if _, ok := RuleSetNamed(s.Name); !ok {
			return errors.Newf("cloudArmor.ruleSets names %q, which is not a rule set bedrock knows: %s", s.Name, strings.Join(knownRuleSetNames(), ", "))
		}
		if seen[s.Name] {
			return errors.Newf("cloudArmor.ruleSets lists %s twice", s.Name)
		}
		seen[s.Name] = true
		if s.Sensitivity < MinSensitivity || s.Sensitivity > MaxSensitivity {
			return errors.Newf("cloudArmor.ruleSets %s has the sensitivity %d; a sensitivity is %d (the rules least likely to misfire) to %d (every rule)", s.Name, s.Sensitivity, MinSensitivity, MaxSensitivity)
		}
	}

	return nil
}

// knownRuleSetNames lists the names of the known sets.
func knownRuleSetNames() []string {
	names := make([]string, 0, len(knownRuleSets))
	for _, k := range knownRuleSets {
		names = append(names, k.Name)
	}

	return names
}

// validate refuses an exclusion the policy could not apply: for a set the policy does
// not evaluate, of a field kind or an operator the API has no spelling for, naming no
// field (or one under any), with a rule id of the wrong shape, or without a reason.
func (e *FieldExclusion) validate(sets []RuleSet) error {
	if !slices.ContainsFunc(sets, func(s RuleSet) bool { return s.Name == e.RuleSet }) {
		return errors.Newf("cloudArmor.fieldExclusions names the rule set %q, which the policy does not evaluate; its rule sets are %s", e.RuleSet, strings.Join(ruleSetNames(sets), ", "))
	}
	if _, ok := exclusionBlocks[e.Field]; !ok {
		return errors.Newf("cloudArmor.fieldExclusions for %s has the field %q; a field is %s, %s, %s or %s", e.RuleSet, e.Field, FieldHeader, FieldCookie, FieldQueryParam, FieldURI)
	}
	if _, ok := exclusionOperators[e.operator()]; !ok {
		return errors.Newf("cloudArmor.fieldExclusions for %s has the operator %q; an operator is %s, %s, %s, %s or %s", e.RuleSet, e.Operator, OperatorEquals, OperatorStartsWith, OperatorEndsWith, OperatorContains, OperatorAny)
	}
	switch {
	case e.operator() == OperatorAny && e.Value != "":
		return errors.Newf("cloudArmor.fieldExclusions for %s names the %s %q under %s, which names every %s and takes no value", e.RuleSet, e.Field, e.Value, OperatorAny, e.Field)
	case e.operator() != OperatorAny && e.Value == "":
		return errors.Newf("cloudArmor.fieldExclusions for %s names no %s; a value is the field's name, or the part of it the operator reads", e.RuleSet, e.Field)
	}
	for _, id := range e.Rules {
		if !ruleIDRE.MatchString(id) {
			return errors.Newf("cloudArmor.fieldExclusions for %s names the rule id %q, which is not one (owasp-crs-v030301-id941100-xss)", e.RuleSet, id)
		}
	}
	if strings.TrimSpace(e.Reason) == "" {
		return errors.Newf("cloudArmor.fieldExclusions for %s on the %s gives no reason", e.RuleSet, e.Field)
	}

	return nil
}

// operator is the exclusion's operator, equals when it writes none.
func (e *FieldExclusion) operator() string {
	if e.Operator == "" {
		return OperatorEquals
	}

	return e.Operator
}

// Block is the block the Terraform provider takes the field in (request_cookie).
func (e *FieldExclusion) Block() string {
	return exclusionBlocks[e.Field]
}

// OperatorName is the operator as the API spells it (EQUALS, EQUALS_ANY).
func (e *FieldExclusion) OperatorName() string {
	return exclusionOperators[e.operator()]
}

// ruleSetNames lists the sets' names.
func ruleSetNames(sets []RuleSet) []string {
	names := make([]string, 0, len(sets))
	for _, s := range sets {
		names = append(names, s.Name)
	}

	return names
}

// validate refuses a bypass whose path is not a route's shape, with a star anywhere but
// last, whose method is not an HTTP method in capitals, or without a reason.
func (b *Bypass) validate() error {
	segments := strings.Split(strings.TrimPrefix(b.Path, "/"), "/")
	switch {
	case !bypassPathRE.MatchString(b.Path):
		return errors.Newf("cloudArmor.bypasses path %q is not a route's shape (/hooks/registry, /streams/{id}/audio, /streams/*)", b.Path)
	case slices.Contains(segments[:len(segments)-1], "*"):
		return errors.Newf("cloudArmor.bypasses path %q has a star before its last segment; a star takes the subtree, so it comes last", b.Path)
	case b.Method != "" && !slices.Contains(bypassMethods, b.Method):
		return errors.Newf("cloudArmor.bypasses %s has the method %q; a method is one of %s, or left out for every method", b.Path, b.Method, strings.Join(bypassMethods, ", "))
	case strings.TrimSpace(b.Reason) == "":
		return errors.Newf("cloudArmor.bypasses %s gives no reason", b.key())
	}

	return nil
}

// key is the bypass as prose names it: the method and the path, or the path alone.
func (b *Bypass) key() string {
	if b.Method == "" {
		return b.Path
	}

	return b.Method + " " + b.Path
}

// Route is the bypass as prose names it: the method and the path, or the path alone.
func (b *Bypass) Route() string {
	return b.key()
}

// Covers reports whether the bypass admits the route: the same path, under the bypass's
// method or any.
func (b *Bypass) Covers(r *FileRoute) bool {
	return b.Path == r.Path && (b.Method == "" || b.Method == r.Method)
}

// armor refuses a placement bypass the generated router already derives: the same route
// would be allowed twice, and the placement's reason would hide the declaration's.
func (m *Model) armor() error {
	policy := m.Placement.Armor()
	for i := range policy.Bypasses {
		b := &policy.Bypasses[i]
		for j := range m.FileRoutes {
			if r := &m.FileRoutes[j]; b.Covers(r) {
				return errors.Newf("placement.json cloudArmor.bypasses names %s, which the generated router already lists as a file route (%s, %s); remove it", b.Route(), r.Declaration(), ReleaseFileName)
			}
		}
	}

	return nil
}
