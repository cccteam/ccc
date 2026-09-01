package resource

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/accesstypes/condition"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
	"google.golang.org/api/iterator"
)

// This file renders the write rules of the ABAC design plan (§05, Evaluation)
// — the three stages, applied in order:
//
//	stage 1 (static gate) is the permission check itself: every touched column
//	must be covered by some grant, and a Denied answer is Forbidden with no
//	query run;
//	stage 2 (fold) is the engine's: a column covered by an unconditional grant
//	is settled and never reaches the carried decisions — a mutation with no
//	carried conditions buffers with no check query, byte-identical to RBAC;
//	stage 3 (live check) runs here: inside the transaction, one check-SELECT
//	returns one boolean per covering-set group — OR within a group's payload,
//	AND across groups — and any false (or NULL) group aborts the mutation as
//	Forbidden, naming the failing group's resources. Nothing commits.
//
// Image binding is the pinned contract: the check never reads the target
// row's post-write state. Insert has one image — the proposed values, bound
// as parameters, with no target-row FROM. Update overlays proposed parameters
// on the existing row's columns (new.attr reads the overlay; an unqualified
// attribute reads the pre-image). Delete is a plain read of the existing row,
// and its condition arrives on the base-resource decision — the delete's
// footprint is the whole row, so the base decision is the condition's sole
// carrier, exactly where reads treat it as the gate alone.

// writeCheckGroup is one covering-set group of the mutation's live check.
type writeCheckGroup struct {
	// resources names the group's field scope for the Forbidden message.
	resources []accesstypes.Resource

	// expr is the group's payload: the any-of combination of its covering
	// grants' conditions.
	expr condition.Expr

	// source keys group identity (the payload's verbatim text).
	source string
}

// writeConditionGroups assembles the mutation's live-check groups from the
// carried decisions, or nothing when stage 2 settled every touched column.
func (p *PatchSet[Resource]) writeConditionGroups() ([]writeCheckGroup, error) {
	q := p.querySet
	if q.resourceSet == nil || len(q.conditionalDecisions) == 0 {
		return nil, nil
	}

	if p.patchType == DeletePatchType {
		decision, ok := q.conditionalDecisions[q.resourceSet.BaseResource()]
		if !ok {
			return nil, nil
		}

		expr, err := conditionalExpr(q.resourceSet.BaseResource(), decision)
		if err != nil {
			return nil, err
		}

		return []writeCheckGroup{{
			resources: []accesstypes.Resource{q.resourceSet.BaseResource()},
			expr:      expr,
			source:    expr.String(),
		}}, nil
	}

	bySource := make(map[string]int)
	var groups []writeCheckGroup
	for _, field := range p.Fields() {
		if !q.resourceSet.PermissionRequired(field, q.requiredPermission) {
			continue
		}
		res := q.resourceSet.Resource(field)
		decision, ok := q.conditionalDecisions[res]
		if !ok {
			continue
		}

		expr, err := conditionalExpr(res, decision)
		if err != nil {
			return nil, err
		}

		source := expr.String()
		idx, ok := bySource[source]
		if !ok {
			idx = len(groups)
			bySource[source] = idx
			groups = append(groups, writeCheckGroup{expr: expr, source: source})
		}
		groups[idx].resources = append(groups[idx].resources, res)
	}

	if len(groups) == 0 {
		return nil, nil
	}

	// The upsert has two possible images (the row may or may not exist), and
	// the design pins check semantics for insert, update, and delete only —
	// fail closed rather than guess.
	if p.patchType == CreateOrUpdatePatchType {
		return nil, errors.Newf("conditional grants cannot enforce an insert-or-update mutation on %s; use a create or update operation", q.Resource())
	}

	slices.SortFunc(groups, func(a, b writeCheckGroup) int { return strings.Compare(a.source, b.source) })

	return groups, nil
}

// enforceWriteConditions runs stage 3 for one mutation inside its
// transaction: renders the check-SELECT, evaluates it against the
// transaction's consistent snapshot, and aborts with Forbidden on any group
// that does not hold. No carried conditions — no query.
func (p *PatchSet[Resource]) enforceWriteConditions(ctx context.Context, txn ReadWriteTransaction) error {
	groups, err := p.writeConditionGroups()
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}
	if txn.DBType() != SpannerDBType {
		return errors.Newf("conditional grant enforcement is not implemented for %s", txn.DBType())
	}

	stmt, err := p.writeCheckStatement(txn.DBType(), groups)
	if err != nil {
		return err
	}

	it := txn.SpannerReadOnlyTransaction().Query(ctx, stmt.SpannerStatement())
	defer it.Stop()

	row, err := it.Next()
	if err != nil {
		if errors.Is(err, iterator.Done) {
			// Update and delete locate the target row; no row is NotFound,
			// kept distinguishable from a failing condition's Forbidden.
			return httpio.NewNotFoundMessagef("%s (%s) not found", p.Resource(), p.PrimaryKey().RowID())
		}

		return errors.Wrap(err, "spanner.RowIterator.Next()")
	}

	q := p.querySet
	for i, group := range groups {
		var passed spanner.NullBool
		if err := row.Column(i, &passed); err != nil {
			return errors.Wrap(err, "spanner.Row.Column()")
		}
		if !passed.Valid || !passed.Bool {
			return httpio.NewForbiddenMessagef("scope (%s), user (%s): (%s) on %s is conditionally granted and the condition does not hold for this row",
				q.scope, q.userPermissions.User(), q.requiredPermission, group.resources)
		}
	}

	return nil
}

// writeCheckStatement renders one mutation's check-SELECT: one boolean term
// per group, the proposed values bound as parameters (the post-write overlay
// for updates, the whole image for inserts), and — for update and delete —
// the target row located by primary key. Insert has no target-row FROM.
func (p *PatchSet[Resource]) writeCheckStatement(dbType DBType, groups []writeCheckGroup) (*Statement, error) {
	q := p.querySet
	if q.collection == nil {
		return nil, errors.Newf("resource %s carries conditional decisions but no generated collection is wired to render them", q.Resource())
	}

	gen := newSQLGenerator(Spanner)
	registry := newParamRegistry()

	insert := p.patchType == CreatePatchType
	proposed, err := p.proposedValues(dbType, insert)
	if err != nil {
		return nil, err
	}

	bindings, _ := q.collection.Bindings(q.collection.Scope(q.Resource()), q.Resource())
	_, partitioned := q.scope.Domain()
	outer := string(q.Resource())
	if insert {
		outer = ""
	}
	lctx := &loweringContext{
		outer:       outer,
		bindings:    bindings,
		collection:  q.collection,
		partitioned: partitioned,
		proposed:    proposed,
		insertImage: insert,
	}

	terms := make([]string, 0, len(groups))
	for i, group := range groups {
		sql, err := lowerToSQL(group.expr, lctx, gen, registry)
		if err != nil {
			return nil, err
		}
		terms = append(terms, fmt.Sprintf("(%s) AS g%d", sql, i+1))
	}

	where := &Statement{Params: map[string]any{}}
	var sql string
	if insert {
		sql = "SELECT " + strings.Join(terms, ", ")
	} else {
		var err error
		where, err = q.where(dbType, nil)
		if err != nil {
			return nil, errors.Wrap(err, "QuerySet.where()")
		}

		sql = fmt.Sprintf("SELECT %s FROM %s %s", strings.Join(terms, ", "), q.Resource(), where.SQL)
	}

	if err := p.mergeCheckParams(registry, where.Params); err != nil {
		return nil, err
	}

	return &Statement{SQL: sql, Params: where.Params}, nil
}

// proposedValues assembles the mutation's proposed image — the touched
// columns' values, plus the keys for an insert, where the whole image is
// proposed. Deletes touch nothing. Values bind as parameters lazily, on first
// reference by a condition.
func (p *PatchSet[Resource]) proposedValues(dbType DBType, insert bool) (*proposedOverlay, error) {
	if p.patchType == DeletePatchType {
		return nil, nil
	}

	fieldMap := p.querySet.rMeta.dbFieldMap(dbType)
	values := make(map[string]any, p.Len())

	add := func(field accesstypes.Field, value any) error {
		meta, ok := fieldMap[field]
		if !ok {
			return errors.Newf("field %s not found in db struct", field)
		}
		values[meta.ColumnName] = value

		return nil
	}

	for _, field := range p.Fields() {
		if err := add(field, p.Get(field)); err != nil {
			return nil, err
		}
	}
	if insert {
		for _, part := range p.PrimaryKey().Parts() {
			if err := add(part.Key, part.Value); err != nil {
				return nil, err
			}
		}
	}

	return newProposedOverlay(values), nil
}

// mergeCheckParams folds the lowered fragments' bound values and referenced
// reserved parameters into the statement's parameter map. Proposed-value
// parameters surface in the referenced set under their bound names and are
// already present, so only the reserved vocabulary resolves here.
func (p *PatchSet[Resource]) mergeCheckParams(registry *paramRegistry, params map[string]any) error {
	for _, param := range registry.boundParams() {
		if _, ok := params[param.Name]; ok {
			return errors.Newf("named parameter collision: %s check statement already contains named parameter %q", p.Resource(), param.Name)
		}
		params[param.Name] = param.Value
	}
	for _, name := range registry.referencedNames() {
		if strings.HasPrefix(name, "_") {
			// A proposed-value overlay parameter, bound above.
			continue
		}
		value, err := p.querySet.namedParamValue(name)
		if err != nil {
			return err
		}
		params[name] = value
	}

	return nil
}
