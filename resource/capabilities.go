package resource

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/accesstypes/condition"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

// This file renders the capability envelope of the ABAC design plan (§13):
// per-row write affordances riding the read response, opt-in per request via
// the capabilities query parameter. The answers are advisory hints for the
// UI — which fields to render editable, whether the Delete button is live —
// computed from the same row image and the same decision instant (@now) the
// read already stamped, in the same statement. Enforcement is untouched: the
// write stages (§05) judge the mutation that actually arrives.
//
// Evaluation reuses the write path's decision shape. Per requested
// permission, the engine's decisions settle what they can without data —
// Granted marks a field editable (or the delete live), Denied and absence
// drop it — and only Conditional decisions render SQL: each distinct
// condition payload becomes one boolean in SELECT position against the
// pre-image, deduplicated across fields and permissions exactly like the
// write check's grouping. Pure RBAC therefore folds to zero extra SQL, and a
// capability-free request renders a byte-identical statement.
//
// A new.-referencing term is structurally unevaluable before the user types,
// so it counts as potentially-true — per TERM (condition.WithoutPostImage):
// the fail-open bound assumes every post-image atom passes while the
// evaluable residue still renders, so a state guard beside an old-vs-new
// conjunct keeps narrowing per row. Deliberately fail-open — for the hint
// only; apply-time enforcement judges what they actually typed.

const (
	// CapabilitiesProperty is the reserved per-row JSON property the generated
	// handlers attach capability answers under. Generation rejects resource
	// columns that would collide with it.
	CapabilitiesProperty = "zzCapabilities"

	// capabilityChecksColumnName is the read statement's reserved output
	// column carrying the condition-group booleans, aligned with the plan's
	// group order. Generation rejects resource columns that would collide
	// with it.
	capabilityChecksColumnName = "zzCapabilityChecks"
)

// capabilityPermissions returns the permissions capability evaluation
// supports: Update carries a positive list of editable field names, Delete a
// boolean — its footprint is the row — Execute a positive list of the RPC
// methods whose declared transitions apply to the row (§09), and Create a
// positive list of the workflow member resources the user may create beneath
// the row (§11 — the affordance rides the immediate parent hop; the
// permission digest answers every other create). Read-shaped permissions are
// answered by the response itself.
func capabilityPermission(name string) (accesstypes.Permission, error) {
	perm := accesstypes.Permission(name)
	switch perm {
	case accesstypes.Create, accesstypes.Update, accesstypes.Delete, accesstypes.Execute:
		return perm, nil
	default:
		return "", httpio.NewBadRequestMessagef("capability evaluation supports %s, %s, %s and %s, not %q", accesstypes.Create, accesstypes.Update, accesstypes.Delete, accesstypes.Execute, name)
	}
}

// capabilityPlan is one statement's capability evaluation: how each row's
// reserved property assembles from the scanned group booleans. It is built
// beside the statement and carried on it for the reader.
type capabilityPlan struct {
	// checksColumn names the reserved boolean-array output column, "" when no
	// group needs SQL — the statement is then byte-identical to the
	// capability-free statement and every answer assembles from grants alone.
	checksColumn string

	// groups counts the rendered condition groups; the scanned array must
	// carry exactly this many members.
	groups int

	perms []plannedCapability
}

// plannedCapability is one requested permission's assembly recipe.
type plannedCapability struct {
	perm accesstypes.Permission

	// isDelete selects the boolean shape; otherwise the positive field list.
	isDelete bool

	// fields lists the Update affordance's candidates in projection order.
	fields []capabilityField

	// allowed is Delete's data-free answer; group overrides it when >= 0.
	allowed bool

	// group indexes the checks array for a conditional Delete, -1 when
	// allowed is settled without data.
	group int
}

// capabilityField is one field of an Update affordance: included in the
// positive list when its group boolean holds, or unconditionally when group
// is -1 (an unconditional grant, or a potentially-true new. condition).
type capabilityField struct {
	jsonName string
	group    int
}

// assemble builds one row's capability property from the scanned group
// booleans. A NULL boolean arrives as false — a condition permits only on
// TRUE (§05), and the hint follows the enforcement's reading.
func (p *capabilityPlan) assemble(checks []bool) map[accesstypes.Permission]any {
	holds := func(group int) bool {
		return group < 0 || (group < len(checks) && checks[group])
	}

	out := make(map[accesstypes.Permission]any, len(p.perms))
	for _, planned := range p.perms {
		if planned.isDelete {
			allowed := planned.allowed
			if planned.group >= 0 {
				allowed = holds(planned.group)
			}
			out[planned.perm] = allowed

			continue
		}

		names := make([]string, 0, len(planned.fields))
		for _, field := range planned.fields {
			if holds(field.group) {
				names = append(names, field.jsonName)
			}
		}
		out[planned.perm] = names
	}

	return out
}

// checkCapabilityPermissions runs the requested capability checks — one
// engine call per permission, the same environment and scope as the read —
// and carries the full Decisions for the plan. Capabilities are advisory:
// a Denied answer shapes the hint and never fails the request.
func (q *QuerySet[Resource]) checkCapabilityPermissions(ctx context.Context) error {
	if len(q.capabilities) == 0 || q.resourceSet == nil || q.userPermissions == nil {
		return nil
	}

	for _, perm := range q.capabilities {
		resources := []accesstypes.Resource{q.resourceSet.BaseResource()}
		switch perm {
		case accesstypes.Delete:
			// Delete's footprint is the row: the base resource carries it.
		case accesstypes.Execute:
			// The affordance question is per targeted method: the RPC method
			// resources whose @target row is this resource — a declared
			// transition or the plain located-row form (§09, §12).
			if q.collection == nil {
				continue
			}
			resources = resources[:0]
			for _, tm := range q.collection.MethodsTargeting(q.resourceSet.BaseResource()) {
				resources = append(resources, tm.Method)
			}
			if len(resources) == 0 {
				continue
			}
		case accesstypes.Create:
			// The affordance question is per member resource: the workflow
			// members whose immediate parent hop is this resource (§11).
			if q.collection == nil {
				continue
			}
			resources = q.collection.MembersOf(q.resourceSet.BaseResource())
			if len(resources) == 0 {
				continue
			}
		default:
			// The affordance question is per displayed field: the projected
			// grant-bearing fields, the same set the read rules govern
			// (permission-exempt primary keys are never editable).
			resources = resources[:0]
			for _, field := range q.Fields() {
				if q.resourceSet.PermissionRequired(field, q.requiredPermission) {
					resources = append(resources, q.resourceSet.Resource(field))
				}
			}
			if len(resources) == 0 {
				continue
			}
		}

		decisions, err := q.userPermissions.Check(ctx, q.env, q.scope, perm, resources...)
		if err != nil {
			return errors.Wrap(err, "resource.UserPermissions.Check()")
		}
		if q.capabilityDecisions == nil {
			q.capabilityDecisions = make(map[accesstypes.Permission]accesstypes.Decisions, len(q.capabilities))
		}
		q.capabilityDecisions[perm] = decisions
	}

	return nil
}

// capabilityGroups interns the distinct condition payloads across every
// requested permission — one boolean per distinct payload, exactly the write
// check's grouping.
type capabilityGroups struct {
	bySource map[string]int
	exprs    []condition.Expr
}

// intern resolves one Conditional decision's payload to its group index,
// returning -1 for a payload whose fail-open bound is a constant. Post-image
// terms count potentially-true per TERM (WithoutPostImage): a term depending
// on a value the user has not yet typed assumes TRUE, while the evaluable
// residue still narrows the hint — so a state guard beside an old-vs-new
// conjunct keeps gating per row (§13's fail-open posture, applied per term).
func (g *capabilityGroups) intern(res accesstypes.Resource, decision accesstypes.Decision) (int, error) {
	expr, err := conditionalExpr(res, decision)
	if err != nil {
		return 0, err
	}
	expr = condition.WithoutPostImage(expr)
	if _, ok := expr.(condition.Truth); ok {
		// A fully potentially-true payload (an unknown atom is unknown in
		// both polarities, so the bound never simplifies to FALSE).
		return -1, nil
	}
	source := expr.String()
	idx, ok := g.bySource[source]
	if !ok {
		idx = len(g.exprs)
		g.bySource[source] = idx
		g.exprs = append(g.exprs, expr)
	}

	return idx, nil
}

// internExpr resolves an already-built expression to its group index, deduped
// by canonical source like intern.
func (g *capabilityGroups) internExpr(expr condition.Expr) int {
	source := expr.String()
	idx, ok := g.bySource[source]
	if !ok {
		idx = len(g.exprs)
		g.bySource[source] = idx
		g.exprs = append(g.exprs, expr)
	}

	return idx
}

// stateMembershipExpr builds a transition's from set as a `state IN (…)`
// expression over the pre-image's uniform state binding, sorted for a
// canonical key so every transition with the same set shares one boolean.
func stateMembershipExpr(from []string) condition.Expr {
	values := slices.Clone(from)
	slices.Sort(values)
	literals := make([]condition.Literal, 0, len(values))
	for _, v := range values {
		literals = append(literals, condition.StringLiteral{Value: v})
	}

	return condition.In{Left: condition.Ref{Name: StateAttribute}, Literals: literals}
}

// plannedDelete builds Delete's recipe from the base decision — the delete's
// footprint is the row, so the base decision is the condition's sole carrier
// (§05).
func (q *QuerySet[Resource]) plannedDelete(decisions accesstypes.Decisions, groups *capabilityGroups) (plannedCapability, error) {
	planned := plannedCapability{perm: accesstypes.Delete, isDelete: true, group: -1}
	decision := decisions[q.resourceSet.BaseResource()]
	switch {
	case decision.IsGranted():
		planned.allowed = true
	case decision.IsConditional():
		group, err := groups.intern(q.resourceSet.BaseResource(), decision)
		if err != nil {
			return plannedCapability{}, err
		}
		if group < 0 {
			planned.allowed = true
		} else {
			planned.group = group
		}
	}

	return planned, nil
}

// plannedUpdate builds one write permission's per-field recipe over the
// projected grant-bearing fields — the same set the read rules govern.
func (q *QuerySet[Resource]) plannedUpdate(perm accesstypes.Permission, decisions accesstypes.Decisions, groups *capabilityGroups) (plannedCapability, error) {
	planned := plannedCapability{perm: perm, group: -1}
	for _, field := range q.Fields() {
		if !q.resourceSet.PermissionRequired(field, q.requiredPermission) {
			continue
		}
		decision := decisions[q.resourceSet.Resource(field)]
		group := -1
		switch {
		case decision.IsGranted():
		case decision.IsConditional():
			var err error
			group, err = groups.intern(q.resourceSet.Resource(field), decision)
			if err != nil {
				return plannedCapability{}, err
			}
		default:
			continue
		}
		planned.fields = append(planned.fields, capabilityField{jsonName: q.jsonName(field), group: group})
	}

	return planned, nil
}

// plannedExecute builds the Execute affordance's recipe: the RPC methods
// whose @target row is this resource (§09, §12), gated by the user's Execute
// grants. A transition method's boolean is the row's pre-image state
// membership in its from set; a conditional grant ANDs its condition into the
// same boolean (a plain method's is the condition alone); a plain method with
// an unconditional grant is structural — no boolean, no SQL. Distinct
// expressions share one boolean per statement.
func (q *QuerySet[Resource]) plannedExecute(decisions accesstypes.Decisions, groups *capabilityGroups) (plannedCapability, error) {
	planned := plannedCapability{perm: accesstypes.Execute, group: -1}
	if q.collection == nil {
		return planned, nil
	}
	for _, tm := range q.collection.MethodsTargeting(q.resourceSet.BaseResource()) {
		decision := decisions[tm.Method]
		var exprs []condition.Expr
		if tm.Transition != nil {
			exprs = append(exprs, stateMembershipExpr(tm.Transition.From))
		}
		switch {
		case decision.IsGranted():
		case decision.IsConditional():
			expr, err := conditionalExpr(tm.Method, decision)
			if err != nil {
				return plannedCapability{}, err
			}
			// Deploy validation rejects new. on Execute grants; the bound is
			// posture parity with intern — a post-image term would count
			// potentially-true, never render.
			expr = condition.WithoutPostImage(expr)
			if _, ok := expr.(condition.Truth); !ok {
				exprs = append(exprs, expr)
			}
		default:
			continue
		}

		group := -1
		switch len(exprs) {
		case 1:
			group = groups.internExpr(exprs[0])
		case 2:
			group = groups.internExpr(condition.And{Operands: exprs})
		}
		planned.fields = append(planned.fields, capabilityField{jsonName: string(tm.Method), group: group})
	}

	return planned, nil
}

// plannedCreate builds the create-under-parent affordance's recipe (§11): the
// workflow member resources whose immediate parent hop is this resource,
// gated by the user's Create grants on each member. An unconditional grant is
// structural — the member lists with no SQL; a conditional grant contributes
// its state-evaluable residue, lowered against this row's own uniform state
// binding (the member's synthesized state IS its parent's state, so the
// immediate-parent row answers it), while every other term counts
// potentially-true (§13's fail-open posture, per term).
func (q *QuerySet[Resource]) plannedCreate(decisions accesstypes.Decisions, groups *capabilityGroups) (plannedCapability, error) {
	planned := plannedCapability{perm: accesstypes.Create, group: -1}
	if q.collection == nil {
		return planned, nil
	}
	for _, member := range q.collection.MembersOf(q.resourceSet.BaseResource()) {
		decision := decisions[member]
		group := -1
		switch {
		case decision.IsGranted():
		case decision.IsConditional():
			expr, err := conditionalExpr(member, decision)
			if err != nil {
				return plannedCapability{}, err
			}
			expr = condition.FailOpen(expr, stateEvaluable)
			if _, ok := expr.(condition.Truth); !ok {
				group = groups.internExpr(expr)
			}
		default:
			continue
		}
		planned.fields = append(planned.fields, capabilityField{jsonName: string(member), group: group})
	}

	return planned, nil
}

// stateEvaluable reports whether an atom of a member's Create condition can
// be answered by the parent row: every reference it carries is the uniform
// state binding, read from the pre-image. Anything else — a column the
// created row would carry, a post-image reference, an attribute compared to
// another attribute — is unknown before the user creates, and counts
// potentially-true.
func stateEvaluable(atom condition.Expr) bool {
	switch n := atom.(type) {
	case condition.Comparison:
		if n.Left.PostImage || n.Left.Name != StateAttribute {
			return false
		}
		if _, ok := n.Right.(condition.Ref); ok {
			return false
		}

		return true
	case condition.In:
		return !n.Left.PostImage && n.Left.Name == StateAttribute
	case condition.NullTest:
		return !n.Left.PostImage && n.Left.Name == StateAttribute
	default:
		return true
	}
}

// renderCapabilities builds the statement's capability plan and, when any
// condition group needs data, the reserved boolean-array select item. Both
// are empty when the request asked for no capabilities.
func (q *QuerySet[Resource]) renderCapabilities(dbType DBType, registry *paramRegistry) (*capabilityPlan, string, error) {
	if len(q.capabilities) == 0 || q.resourceSet == nil || q.userPermissions == nil {
		return nil, "", nil
	}

	plan := &capabilityPlan{}
	groups := &capabilityGroups{bySource: make(map[string]int)}

	for _, perm := range q.capabilities {
		var planned plannedCapability
		var err error
		switch perm {
		case accesstypes.Delete:
			planned, err = q.plannedDelete(q.capabilityDecisions[perm], groups)
		case accesstypes.Execute:
			planned, err = q.plannedExecute(q.capabilityDecisions[perm], groups)
		case accesstypes.Create:
			planned, err = q.plannedCreate(q.capabilityDecisions[perm], groups)
		default:
			planned, err = q.plannedUpdate(perm, q.capabilityDecisions[perm], groups)
		}
		if err != nil {
			return nil, "", err
		}
		plan.perms = append(plan.perms, planned)
	}

	groupExprs := groups.exprs
	if len(groupExprs) == 0 {
		return plan, "", nil
	}

	if q.collection == nil {
		return nil, "", errors.Newf("resource %s carries conditional capability decisions but no generated collection is wired to render them", q.Resource())
	}

	gen, err := loweredSQLGenerator(dbType)
	if err != nil {
		return nil, "", err
	}
	lctx := q.loweringCtx()

	terms := make([]string, len(groupExprs))
	for i, expr := range groupExprs {
		sql, err := lowerToSQL(expr, lctx, gen, registry)
		if err != nil {
			return nil, "", err
		}
		terms[i] = "(" + sql + ")"
	}

	plan.checksColumn = capabilityChecksColumnName
	plan.groups = len(groupExprs)

	return plan, capabilityChecksItem(dbType, terms), nil
}

// capabilityChecksItem renders the reserved boolean-array select item.
func capabilityChecksItem(dbType DBType, terms []string) string {
	if dbType == PostgresDBType {
		return fmt.Sprintf(`ARRAY[%s] AS %q`, strings.Join(terms, ", "), capabilityChecksColumnName)
	}

	return fmt.Sprintf("ARRAY<BOOL>[%s] AS %s", strings.Join(terms, ", "), capabilityChecksColumnName)
}
