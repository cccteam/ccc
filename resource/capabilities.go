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
// A new.-referencing condition is structurally unevaluable before the user
// types, so it counts as potentially-true: the field renders editable and
// apply-time enforcement judges what they actually typed. Deliberately
// fail-open — for the hint only.

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
// boolean — its footprint is the row — and Execute a positive list of the RPC
// methods whose declared transitions apply to the row (§09). Create has no
// row to ride (the permission digest answers it), and read-shaped permissions
// are answered by the response itself.
func capabilityPermission(name string) (accesstypes.Permission, error) {
	perm := accesstypes.Permission(name)
	if perm != accesstypes.Update && perm != accesstypes.Delete && perm != accesstypes.Execute {
		return "", httpio.NewBadRequestMessagef("capability evaluation supports %s, %s and %s, not %q", accesstypes.Update, accesstypes.Delete, accesstypes.Execute, name)
	}

	return perm, nil
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
			// The affordance question is per declared transition: the RPC
			// method resources whose transitions target this resource (§09).
			if q.collection == nil {
				continue
			}
			resources = resources[:0]
			for _, tm := range q.collection.TransitionsOnto(q.resourceSet.BaseResource()) {
				resources = append(resources, tm.Method)
			}
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
// returning -1 for a potentially-true new.-referencing payload (fail-open,
// hint only — the field is structurally unevaluable before the user types).
func (g *capabilityGroups) intern(res accesstypes.Resource, decision accesstypes.Decision) (int, error) {
	expr, err := conditionalExpr(res, decision)
	if err != nil {
		return 0, err
	}
	if condition.UsesPostImage(expr) {
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

// internStateMembership resolves a transition's from set to its group index:
// a `state IN (…)` boolean over the pre-image's uniform state binding, shared
// by every transition with the same set (sorted for a canonical key).
func (g *capabilityGroups) internStateMembership(from []string) int {
	values := slices.Clone(from)
	slices.Sort(values)
	literals := make([]condition.Literal, 0, len(values))
	for _, v := range values {
		literals = append(literals, condition.StringLiteral{Value: v})
	}
	expr := condition.In{Left: condition.Ref{Name: StateAttribute}, Literals: literals}

	source := expr.String()
	idx, ok := g.bySource[source]
	if !ok {
		idx = len(g.exprs)
		g.bySource[source] = idx
		g.exprs = append(g.exprs, expr)
	}

	return idx
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
// whose declared transitions target this resource (§09), gated by the user's
// Execute grants — settled without data, Execute conditions are row-free and
// fold at check time — and by the row's pre-image state membership in each
// transition's from set, one boolean per distinct set in the same statement.
func (q *QuerySet[Resource]) plannedExecute(decisions accesstypes.Decisions, groups *capabilityGroups) (plannedCapability, error) {
	planned := plannedCapability{perm: accesstypes.Execute, group: -1}
	if q.collection == nil {
		return planned, nil
	}
	for _, tm := range q.collection.TransitionsOnto(q.resourceSet.BaseResource()) {
		decision := decisions[tm.Method]
		switch {
		case decision.IsGranted():
		case decision.IsConditional():
			return plannedCapability{}, errors.Newf("method %s: an Execute grant reached capability planning as Conditional — Execute conditions are row-free and settle at check time", tm.Method)
		default:
			continue
		}
		planned.fields = append(planned.fields, capabilityField{
			jsonName: string(tm.Method),
			group:    groups.internStateMembership(tm.Transition.From),
		})
	}

	return planned, nil
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
