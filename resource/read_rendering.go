package resource

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/accesstypes/condition"
	"github.com/go-playground/errors/v5"
)

// This file renders the read rules of the ABAC design plan (§05, Evaluation)
// into the read statement:
//
//	rule 1 (columns) is decided by the permission checks before any SQL exists;
//	rule 2 (cells) renders as a per-column CASE that yields the column where a
//	covering condition holds and a typed zero filler where it does not, plus one
//	reserved ARRAY<STRING> output column naming the masked cells;
//	rule 3 (rows) renders as a WHERE predicate — TRUE (omitted) when any
//	projected grant-bearing column is unconditionally granted, otherwise the OR
//	of the distinct conditions covering projected columns.
//
// The base-resource decision never renders here: it is the handler gate
// (decided 2026-08-31), and its payload is by construction the union the field
// decisions already deliver. Primary keys sit outside the visibility rules —
// no CASE, no mask term, no row-predicate disjunct — otherwise their
// always-visible cells would defeat row suppression.
//
// Implication pruning (decided 2026-08-31): a column whose condition set
// covers every disjunct of the row predicate skips its CASE and mask term —
// the WHERE has already proven its condition on every surviving row. In the
// common case (one conditional role, the pairing invariant putting one
// condition on every column) every CASE prunes and the statement is a plain
// SELECT with one WHERE condition. With no conditional decisions the whole
// pass is skipped and the statement is byte-identical to RBAC.

// maskedNamesColumnName is the read statement's one reserved output column:
// the JSON names of the row's masked cells. Generation rejects resource
// columns that would collide with it.
const maskedNamesColumnName = "zzMaskedFields"

// readConditionPlan is the policy half of read rendering: which projected
// fields are conditional, their condition disjuncts, and the row predicate.
type readConditionPlan struct {
	// rowPredicate holds the distinct disjuncts of rule 3's row predicate, in
	// first-appearance projection order. Empty means TRUE (some projected
	// grant-bearing column is unconditionally granted) and nothing prunes.
	rowPredicate []condition.Expr

	// fields maps each conditionally granted projected field to its
	// conditions. Fields outside the map are unconditionally granted (plain
	// columns) or permission-exempt primary keys (outside the rules).
	fields map[accesstypes.Field]*fieldConditions
}

// fieldConditions is one conditional field's rendering input.
type fieldConditions struct {
	// disjuncts is the field's covering condition, flattened over OR.
	disjuncts []condition.Expr

	// keys is the canonical form of each disjunct, for the pruning set test.
	keys map[string]struct{}

	// pruned marks a CASE the row predicate makes tautological.
	pruned bool

	// jsonName keys the mask array term; the wire encode tests it via
	// Row.Masked.
	jsonName string
}

// readConditionPlan builds the plan from the carried conditional decisions,
// or returns nil when nothing is conditional and the statement renders
// byte-identical to RBAC.
func (q *QuerySet[Resource]) readConditionPlan() (*readConditionPlan, error) {
	if q.resourceSet == nil || len(q.conditionalDecisions) == 0 {
		return nil, nil
	}

	plan := &readConditionPlan{fields: make(map[accesstypes.Field]*fieldConditions)}
	unionKeys := make(map[string]struct{})
	var union []condition.Expr
	anyUnconditional := false

	for _, field := range q.Fields() {
		if !q.resourceSet.PermissionRequired(field, q.requiredPermission) {
			// The perm:"-" primary-key exemption: outside the visibility rules.
			continue
		}

		decision, ok := q.conditionalDecisions[q.resourceSet.Resource(field)]
		if !ok {
			anyUnconditional = true

			continue
		}

		expr, err := conditionalExpr(q.resourceSet.Resource(field), decision)
		if err != nil {
			return nil, err
		}

		fc := &fieldConditions{
			disjuncts: flattenOr(expr),
			keys:      make(map[string]struct{}),
			jsonName:  q.jsonName(field),
		}
		for _, disjunct := range fc.disjuncts {
			key := disjunct.String()
			fc.keys[key] = struct{}{}
			if _, seen := unionKeys[key]; !seen {
				unionKeys[key] = struct{}{}
				union = append(union, disjunct)
			}
		}
		plan.fields[field] = fc
	}

	if len(plan.fields) == 0 {
		// Only the base resource was conditional: it is the handler gate, and
		// it never renders on the read path.
		return nil, nil
	}

	if anyUnconditional {
		// Rule 3's predicate is TRUE: no row filter, and nothing prunes.
		return plan, nil
	}

	plan.rowPredicate = union
	for _, fc := range plan.fields {
		// keys ⊆ unionKeys always, so covering every disjunct is a size test.
		fc.pruned = len(fc.keys) == len(unionKeys)
	}

	return plan, nil
}

// conditionalExpr extracts the condition covering res from its Conditional
// decision. A Conditional decision without a usable payload is a policy-engine
// invariant breach, never something to render around.
func conditionalExpr(res accesstypes.Resource, decision accesstypes.Decision) (condition.Expr, error) {
	groups := decision.ConditionGroups()
	groupIdx := slices.IndexFunc(groups, func(g accesstypes.ConditionGroup) bool {
		return slices.Contains(g.Resources, res)
	})
	if groupIdx < 0 {
		if len(groups) != 1 {
			return nil, errors.Newf("conditional decision for %s carries no condition group covering it", res)
		}
		groupIdx = 0
	}

	group := groups[groupIdx]
	if group.Condition.IsZero() || group.Condition.Expr() == nil {
		return nil, errors.Newf("conditional decision for %s carries no condition payload", res)
	}

	return group.Condition.Expr(), nil
}

// flattenOr flattens an any-of tree into its disjuncts; OR is associative, so
// finer granularity only sharpens the pruning test, never changes semantics.
func flattenOr(expr condition.Expr) []condition.Expr {
	or, ok := expr.(condition.Or)
	if !ok {
		return []condition.Expr{expr}
	}

	var out []condition.Expr
	for _, operand := range or.Operands {
		out = append(out, flattenOr(operand)...)
	}

	return out
}

// orOf rebuilds one expression from disjuncts.
func orOf(disjuncts []condition.Expr) condition.Expr {
	if len(disjuncts) == 1 {
		return disjuncts[0]
	}

	return condition.Or{Operands: disjuncts}
}

// jsonName resolves a field's wire name. The QueryDecoder stamps the request
// type's JSON names; a hand-built QuerySet falls back to the Go field name.
func (q *QuerySet[Resource]) jsonName(field accesstypes.Field) string {
	if name, ok := q.jsonNames[field]; ok {
		return name
	}

	return string(field)
}

// renderedReadConditions is the SQL half of read rendering, produced against
// one statement's parameter registry.
type renderedReadConditions struct {
	// overrides replaces a projected column's select expression with its CASE.
	overrides map[accesstypes.Field]string

	// maskColumn is the reserved masked-names select item, "" when every CASE
	// pruned.
	maskColumn string

	// rowPredicate is rule 3's predicate, parenthesized, "" when TRUE.
	rowPredicate string
}

// renderReadConditions lowers the plan's conditions and renders the CASE
// expressions, mask terms, and row predicate for one statement. The registry
// is statement-scoped and shared with every other lowered fragment (the
// tenancy predicate), so aliases and placeholders stay unique across them.
func (q *QuerySet[Resource]) renderReadConditions(dbType DBType, plan *readConditionPlan, registry *paramRegistry) (*renderedReadConditions, error) {
	if q.collection == nil {
		return nil, errors.Newf("resource %s carries conditional decisions but no generated collection is wired to render them", q.Resource())
	}

	gen, err := loweredSQLGenerator(dbType)
	if err != nil {
		return nil, err
	}

	lctx := q.loweringCtx()

	rendered := &renderedReadConditions{
		overrides: make(map[accesstypes.Field]string),
	}

	if len(plan.rowPredicate) > 0 {
		sql, err := lowerToSQL(orOf(plan.rowPredicate), lctx, gen, registry)
		if err != nil {
			return nil, err
		}
		if len(plan.rowPredicate) == 1 {
			sql = "(" + sql + ")"
		}
		rendered.rowPredicate = sql
	}

	fieldColumns, err := q.orderedDBFields(dbType)
	if err != nil {
		return nil, err
	}

	var maskTerms []string
	for _, fieldColumn := range fieldColumns {
		fc, ok := plan.fields[fieldColumn.field]
		if !ok || fc.pruned {
			continue
		}

		condSQL, err := lowerToSQL(orOf(fc.disjuncts), lctx, gen, registry)
		if err != nil {
			return nil, err
		}

		filler := registry.bind(reflect.Zero(fieldColumn.meta.fieldType).Interface())
		column := fieldColumn.meta.ColumnName
		if dbType == PostgresDBType {
			column = gen.quoteIdentifier(column)
		}
		rendered.overrides[fieldColumn.field] = fmt.Sprintf("CASE WHEN %s THEN %s ELSE %s END AS %s", condSQL, column, filler, column)
		maskTerms = append(maskTerms, maskTerm(dbType, condSQL, fc.jsonName))
	}

	if len(maskTerms) > 0 {
		rendered.maskColumn = maskColumn(dbType, maskTerms)
	}

	return rendered, nil
}

// loweringCtx builds the read-shaped lowering context: unqualified attributes
// resolve to the outer row's pre-image columns. Shared by the read-condition
// and capability renderers; requires a wired collection.
func (q *QuerySet[Resource]) loweringCtx() *loweringContext {
	bindings, _ := q.collection.Bindings(q.collection.Scope(q.Resource()), q.Resource())
	_, partitioned := q.scope.Domain()

	return &loweringContext{
		outer:       string(q.Resource()),
		bindings:    bindings,
		collection:  q.collection,
		partitioned: partitioned,
	}
}

// lowerToSQL runs one condition through the lowering and the statement's SQL
// emitter.
func lowerToSQL(expr condition.Expr, lctx *loweringContext, gen *sqlGenerator, registry *paramRegistry) (string, error) {
	node, err := lowerCondition(expr, lctx, registry)
	if err != nil {
		return "", err
	}

	sql, err := gen.generateLowered(node, registry)
	if err != nil {
		return "", err
	}

	return sql, nil
}

// maskTerm renders one masked field's contribution to the reserved column:
// empty where the condition holds, the field's JSON name where it masks.
func maskTerm(dbType DBType, condSQL, jsonName string) string {
	name := strings.ReplaceAll(jsonName, "'", "''")
	if dbType == PostgresDBType {
		return fmt.Sprintf("CASE WHEN %s THEN NULL ELSE '%s' END", condSQL, name)
	}

	return fmt.Sprintf("IF(%s, ARRAY<STRING>[], ['%s'])", condSQL, name)
}

// maskColumn combines the mask terms into the reserved select item.
func maskColumn(dbType DBType, terms []string) string {
	if dbType == PostgresDBType {
		return fmt.Sprintf(`ARRAY_REMOVE(ARRAY[%s], NULL) AS %q`, strings.Join(terms, ", "), maskedNamesColumnName)
	}
	if len(terms) == 1 {
		return fmt.Sprintf("%s AS %s", terms[0], maskedNamesColumnName)
	}

	return fmt.Sprintf("ARRAY_CONCAT(%s) AS %s", strings.Join(terms, ", "), maskedNamesColumnName)
}

// loweredSQLGenerator returns the lowered-fragment SQL emitter for dbType.
func loweredSQLGenerator(dbType DBType) (*sqlGenerator, error) {
	switch dbType {
	case SpannerDBType:
		return newSQLGenerator(Spanner), nil
	case PostgresDBType:
		return newSQLGenerator(PostgreSQL), nil
	default:
		return nil, errors.Newf("unsupported dbType: %s", dbType)
	}
}

// mergeRegistryParams folds the statement registry's bound values and
// referenced reserved parameters into the statement's parameter map. The
// lowered @_c… names are disjoint from the filter (@_p…) and keyset
// (@_<column>) namespaces by construction, so a collision can only come from
// a virtual resource's subquery params.
func (q *QuerySet[Resource]) mergeRegistryParams(registry *paramRegistry, params map[string]any) error {
	for _, param := range registry.boundParams() {
		if _, ok := params[param.Name]; ok {
			return errors.Newf("named parameter collision: %s statement already contains named parameter %q", q.Resource(), param.Name)
		}
		params[param.Name] = param.Value
	}
	for _, name := range registry.referencedNames() {
		if _, ok := params[name]; ok {
			return errors.Newf("named parameter collision: %s statement already contains reserved named parameter %q", q.Resource(), name)
		}
		value, err := q.namedParamValue(name)
		if err != nil {
			return err
		}
		params[name] = value
	}

	return nil
}

// namedParamValue supplies a referenced reserved parameter's value: the same
// values the permission check folded with, never re-sampled.
func (q *QuerySet[Resource]) namedParamValue(name string) (any, error) {
	switch name {
	case subjectParamName:
		if q.userPermissions == nil {
			return nil, errors.New("condition references subject but the QuerySet carries no user")
		}

		return string(q.userPermissions.User()), nil
	case nowParamName:
		now, ok := q.env.Now()
		if !ok {
			return nil, errors.New("condition references now but the request environment carries no decision instant")
		}

		return now, nil
	case domainParamName:
		domain, ok := q.scope.Domain()
		if !ok {
			return nil, errors.New("condition rendering referenced the request partition in a global request")
		}

		return string(domain), nil
	default:
		return nil, errors.Newf("condition rendering referenced unknown named parameter %q", name)
	}
}
