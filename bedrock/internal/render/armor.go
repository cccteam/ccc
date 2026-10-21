// armor.go prepares the Cloud Armor policy for cloud-armor.tf and the README: the rules
// in priority order, each with its expression and a description naming its source, from
// the placement's policy (derive.Armor), the file routes the generated router lists and
// the sign-in routes it mounts, which the policy throttles by client address.

package render

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/cccteam/ccc/bedrock/internal/derive"
)

// The rules' priorities: the leading rule sets, then the bypasses, then the trailing rule
// sets, then the sign-in throttles, with room between the groups; the default rule's is
// the lowest Cloud Armor has. The throttles come after the rule sets so that a sign-in
// POST is still read by them: a rule that matches decides, and a throttle under its
// threshold allows.
const (
	armorLeadingBase  = 1000
	armorBypassBase   = 2000
	armorTrailingBase = 3000
	armorThrottleBase = 4000
	armorSetStep      = 10
	armorDefaultRule  = 2147483647

	armorAllow    = "allow"
	armorDeny     = "deny(403)"
	armorThrottle = "throttle"
	// armorDenyVerb is a deny as the README's table says it; allow and throttle read as
	// their actions.
	armorDenyVerb = "deny"
	// armorDescriptionLimit is the most characters Google accepts in a security policy
	// rule's description; the README's rule table carries the whole text.
	armorDescriptionLimit = 256
	// armorDescriptionCut ends a description the limit shortened.
	armorDescriptionCut = "..."
)

// The sign-in throttle's threshold, as terraform.tfvars sets it: the POSTs one client
// address may make to a sign-in route in the interval before its further ones answer
// 429, and the interval in seconds. The defaults are chosen for a login page: a person
// types a handful of attempts in a minute and an office behind one address a few dozen
// at its busiest, so neither meets thirty, while a password guessed from one address is
// held to thirty tries a minute; a minute holds a guessing script to the count and keeps
// a person who met it waiting little.
const (
	SignInCountVariable    = "cloud_armor_sign_in_count"
	SignInIntervalVariable = "cloud_armor_sign_in_interval"
	signInCount            = "30"
	signInInterval         = "60"
)

// armorView is the policy as cloud-armor.tf and the README read it.
type armorView struct {
	// Rules are the policy's rules in priority order; the default rule, which allows
	// every other request, is the template's.
	Rules []armorRule
	// Scope is the CEL expression that limits a trailing rule set to the outlets'
	// routes, empty when the site declares no outlet prefix; ScopeProse names the
	// prefixes ("/api and /portal/api").
	Scope      string
	ScopeProse string
	// Default reports a placement whose rule sets are bedrock's defaults.
	Default bool
	// Exclusions are the placement's field exclusions, prepared for the README.
	Exclusions []armorExclusion
	// DefaultPriority is the default rule's priority.
	DefaultPriority int
	// Throttled reports a policy with sign-in throttles: the application has a sign-in
	// route. SignInProse names the routes, each in backticks; SignInCount and
	// SignInInterval are the threshold's defaults, as variables.tf writes them.
	Throttled      bool
	SignInProse    string
	SignInCount    string
	SignInInterval string
}

// armorRule is one rule of the policy.
type armorRule struct {
	Priority int
	// Action is allow, deny(403) or throttle; Verb is the same as the README's table
	// says it (allow, deny, throttle). Deny reports a deny and Throttle a throttle,
	// whose rate limit the template writes; the mode's preview turns either into a log
	// line.
	Action   string
	Verb     string
	Deny     bool
	Throttle bool
	// What says what the rule matches and Source where it comes from; Text is both, the
	// rule's description, and Description the same HCL-quoted.
	What        string
	Source      string
	Text        string
	Description string
	// Expression is the rule's CEL expression, HCL-quoted.
	Expression string
	// Exclusions are the fields the rule's set leaves alone.
	Exclusions []armorExclusion
}

// armorExclusion is one field exclusion as the template and the README write it.
type armorExclusion struct {
	derive.FieldExclusion
	// Operator is the API's spelling, Block the provider's block, Value the field
	// HCL-quoted (empty under any), RuleIDList the rule ids as an HCL list (empty for
	// every rule), FieldProse the field as the README names it.
	Operator   string
	Block      string
	Value      string
	RuleIDList string
	FieldProse string
	RuleProse  string
}

// newArmorView prepares the policy: the leading sets on every path, the bypasses from
// the generated router and the placement, the trailing sets on the outlets' routes, and
// the sign-in throttles on the routes the generated router takes a credential at.
func newArmorView(m *derive.Model) *armorView {
	policy := m.Placement.Armor()
	v := &armorView{Default: m.Placement.CloudArmor == nil || len(m.Placement.CloudArmor.RuleSets) == 0, DefaultPriority: armorDefaultRule, SignInCount: signInCount, SignInInterval: signInInterval}
	v.scope(m.Outlets)
	for i := range policy.FieldExclusions {
		v.Exclusions = append(v.Exclusions, newArmorExclusion(&policy.FieldExclusions[i]))
	}
	source := "placement.json cloudArmor.ruleSets"
	if v.Default {
		source = "bedrock's default rule sets"
	}
	for i, s := range policy.Leading {
		v.add(armorLeadingBase+i*armorSetStep, armorDeny, fmt.Sprintf("%s (%s, sensitivity %d) on every path", s.Detects(), s.Name, s.Sensitivity), source, wafExpression(s, ""), v.exclusionsOf(s.Name))
	}
	priority := armorBypassBase
	for i := range m.FileRoutes {
		r := &m.FileRoutes[i]
		v.add(priority, armorAllow, r.Method+" "+r.Path, r.Declaration()+", "+path.Join(m.RouterDir, derive.ReleaseFileName)+"; "+fileRouteReason(r), routeExpression(r.Method, r.Path), nil)
		priority++
	}
	for i := range policy.Bypasses {
		b := &policy.Bypasses[i]
		v.add(priority, armorAllow, b.Route(), "placement.json cloudArmor.bypasses; "+b.Reason, routeExpression(b.Method, b.Path), nil)
		priority++
	}
	for i, s := range policy.Trailing {
		where := "on every path"
		if v.Scope != "" {
			where = "on " + v.ScopeProse
		}
		v.add(armorTrailingBase+i*armorSetStep, armorDeny, fmt.Sprintf("%s (%s, sensitivity %d) %s", s.Detects(), s.Name, s.Sensitivity, where), source, wafExpression(s, v.Scope), v.exclusionsOf(s.Name))
	}
	v.throttles(m.SignInRoutes)

	return v
}

// throttles adds one throttle per sign-in route, keyed by client address, after the
// rule sets: past the threshold's count of POSTs to the route in its interval, an
// address answers 429 on it until the interval passes.
func (v *armorView) throttles(routes []derive.SignInRoute) {
	v.Throttled = len(routes) > 0
	prose := make([]string, 0, len(routes))
	for i := range routes {
		r := &routes[i]
		v.add(armorThrottleBase+i, armorThrottle, r.Method+" "+r.Path+", throttled by client address", r.Declaration()+", the generated router; past the sign-in count in its interval (terraform.tfvars) an address answers 429", routeExpression(r.Method, r.Path), nil)
		prose = append(prose, "`"+r.Method+" "+r.Path+"`")
	}
	v.SignInProse = joinAnd(prose)
}

// add appends a rule. Its description is the text within Google's limit
// (fitDescription); the README's table and the template's comment carry the whole text.
func (v *armorView) add(priority int, action, what, source, expression string, exclusions []armorExclusion) {
	text := what + "; " + source
	v.Rules = append(v.Rules, armorRule{
		Priority:    priority,
		Action:      action,
		Verb:        verbOf(action),
		Deny:        action == armorDeny,
		Throttle:    action == armorThrottle,
		What:        what,
		Source:      source,
		Text:        text,
		Description: hclQuote(fitDescription(text)),
		Expression:  hclQuote(expression),
		Exclusions:  exclusions,
	})
}

// fitDescription is the rule's text within Google's limit on a rule description: the
// text itself when it fits, else its head cut at the last word boundary that leaves
// room for the ending, with the ending.
func fitDescription(text string) string {
	if len(text) <= armorDescriptionLimit {
		return text
	}
	room := armorDescriptionLimit - len(armorDescriptionCut)
	head := text[:room]
	if i := strings.LastIndex(head, " "); i > 0 {
		head = head[:i]
	}

	return head + armorDescriptionCut
}

// verbOf is the action as the README's table says it: allow, deny or throttle.
func verbOf(action string) string {
	if action == armorDeny {
		return armorDenyVerb
	}

	return action
}

// scope builds the expression that limits a trailing rule set to the outlets' routes,
// from the prefixes the site declares: request.path.startsWith for each prefix, the
// several joined with || in parentheses. Cloud Armor's matcher refuses a capture group in
// a regular expression, so the prefixes are not written as one alternation.
func (v *armorView) scope(outlets []derive.Outlet) {
	var prefixes, prose []string
	for _, o := range outlets {
		if o.Prefix == "" {
			continue
		}
		prefixes = append(prefixes, fmt.Sprintf("request.path.startsWith('/%s/')", o.Prefix))
		prose = append(prose, "/"+o.Prefix)
	}
	switch len(prefixes) {
	case 0:
		return
	case 1:
		v.Scope = prefixes[0]
	default:
		v.Scope = "(" + strings.Join(prefixes, " || ") + ")"
	}
	v.ScopeProse = joinAnd(prose)
}

// exclusionsOf lists the exclusions for the set.
func (v *armorView) exclusionsOf(set string) []armorExclusion {
	var out []armorExclusion
	for i := range v.Exclusions {
		if v.Exclusions[i].RuleSet == set {
			out = append(out, v.Exclusions[i])
		}
	}

	return out
}

// newArmorExclusion prepares one exclusion.
func newArmorExclusion(e *derive.FieldExclusion) armorExclusion {
	out := armorExclusion{FieldExclusion: *e, Operator: e.OperatorName(), Block: e.Block()}
	if e.Value != "" {
		out.Value = hclQuote(e.Value)
	}
	kind := map[string]string{derive.FieldHeader: "header", derive.FieldCookie: "cookie", derive.FieldQueryParam: "query parameter", derive.FieldURI: "path"}[e.Field]
	switch e.OperatorName() {
	case "EQUALS_ANY":
		out.FieldProse = "every " + kind
	case "EQUALS":
		out.FieldProse = fmt.Sprintf("the %s `%s`", kind, e.Value)
	default:
		out.FieldProse = fmt.Sprintf("every %s whose name %s `%s`", kind, map[string]string{"STARTS_WITH": "starts with", "ENDS_WITH": "ends with", "CONTAINS": "contains"}[e.OperatorName()], e.Value)
	}
	out.RuleProse = "every rule of the set"
	if len(e.Rules) > 0 {
		quoted := make([]string, 0, len(e.Rules))
		ticked := make([]string, 0, len(e.Rules))
		for _, id := range e.Rules {
			quoted = append(quoted, strconv.Quote(id))
			ticked = append(ticked, "`"+id+"`")
		}
		out.RuleIDList = "[" + strings.Join(quoted, ", ") + "]"
		out.RuleProse = joinAnd(ticked)
	}

	return out
}

// wafExpression is a rule set's expression: evaluatePreconfiguredWaf at the set's
// sensitivity, behind the scope where there is one, so a request outside the outlets'
// routes short-circuits and the set never reads it.
func wafExpression(s derive.RuleSet, scope string) string {
	expr := fmt.Sprintf("evaluatePreconfiguredWaf('%s', {'sensitivity': %d})", s.Name, s.Sensitivity)
	if scope == "" {
		return expr
	}

	return scope + " && " + expr
}

// routeExpression matches the route: its path as a regular expression
// (derive.PathRegex, the same the request log's exclusion matches a surface with), under
// its method where one is given.
func routeExpression(method, route string) string {
	expr := fmt.Sprintf("request.path.matches('^%s$')", derive.PathRegex(route))
	if method == "" {
		return expr
	}

	return fmt.Sprintf("request.method == '%s' && %s", method, expr)
}

// fileRouteReason says why the route carries no input a rule can judge.
func fileRouteReason(r *derive.FileRoute) string {
	if r.Kind == derive.FileRouteUpload {
		return "its body is a file, not JSON"
	}

	return "its answer is the stored object"
}

// hclQuote writes s as an HCL string literal: backslashes, quotes and newlines escaped,
// and the template sequences ${ and %{ written so HCL reads them as text.
func hclQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "${", "$${", "%{", "%%{").Replace(s) + `"`
}
