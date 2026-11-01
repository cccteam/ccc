package resource

import (
	"context"
	"fmt"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/accesstypes/condition"
	"github.com/go-playground/errors/v5"
)

// The search box under the caller's grants (README section 4). A field the box
// searches is read like a filter field, with one difference: a denied field drops out
// of the box silently instead of refusing the request, because the box names what it
// searches, so its absence discloses nothing, where an explicit match names the field
// and is refused with the other filter fields. A conditionally granted field stays in
// the box with its condition conjoined inside its match, so the search index still
// serves it; a positional field renders bare by declaration. A @searchAlso branch is
// decided by the child field's own grant the same way.

// fieldResource names the child field's permission resource, Resource.Tag.
func (b *SearchBranch) fieldResource() accesstypes.Resource {
	return accesstypes.Resource(fmt.Sprintf("%s.%s", b.Resource, b.Tag))
}

// checkSearchFieldsReadable decides the box's fields and branches under the caller's
// grants: denied drops, conditional carries its decision for the rendering, granted
// renders bare. Nothing is refused here.
func (q *QuerySet[Resource]) checkSearchFieldsReadable(ctx context.Context) error {
	if !q.searchBox {
		return nil
	}

	type target struct {
		field  accesstypes.Field
		branch string
		res    accesstypes.Resource
	}
	var targets []target
	var resources []accesstypes.Resource
	for _, field := range q.searchFields {
		if !q.resourceSet.PermissionRequired(field, q.requiredPermission) {
			continue
		}
		res := q.resourceSet.Resource(field)
		targets = append(targets, target{field: field, res: res})
		resources = append(resources, res)
	}
	for i := range q.rMeta.searchBranches {
		branch := &q.rMeta.searchBranches[i]
		if branch.Tag == "" {
			// The child field is exempt from field-level enforcement (a primary
			// key): its readability follows the child resource's grant.
			continue
		}
		res := branch.fieldResource()
		targets = append(targets, target{branch: branch.Name, res: res})
		resources = append(resources, res)
	}
	if len(resources) == 0 {
		return nil
	}

	decisions, err := q.userPermissions.Check(ctx, q.env, q.scope, q.requiredPermission, resources...)
	if err != nil {
		return errors.Wrap(err, "resource.UserPermissions.Check()")
	}
	for _, t := range targets {
		decision := decisions[t.res]
		switch {
		case decision.IsDenied() && t.branch != "":
			if q.droppedBranches == nil {
				q.droppedBranches = make(map[string]struct{})
			}
			q.droppedBranches[t.branch] = struct{}{}
		case decision.IsDenied():
			if q.droppedSearchFields == nil {
				q.droppedSearchFields = make(map[accesstypes.Field]struct{})
			}
			q.droppedSearchFields[t.field] = struct{}{}
		case decision.IsConditional() && t.branch != "":
			if q.branchDecisions == nil {
				q.branchDecisions = make(map[string]accesstypes.Decision)
			}
			q.branchDecisions[t.branch] = decision
		}
	}
	// A conditional field's decision rides the set like a filter field's, so the
	// read plan renders its condition (addQueryOnlyConditions).
	q.carryConditionalDecisions(decisions)

	return nil
}

// activeSearchFields lists the box's fields that survived the read checks, in
// declaration order.
func (q *QuerySet[Resource]) activeSearchFields() []accesstypes.Field {
	if len(q.droppedSearchFields) == 0 {
		return q.searchFields
	}
	fields := make([]accesstypes.Field, 0, len(q.searchFields))
	for _, field := range q.searchFields {
		if _, dropped := q.droppedSearchFields[field]; !dropped {
			fields = append(fields, field)
		}
	}

	return fields
}

// searchFieldGrants renders what the checks decided for the resource's own search
// fields, by column: dropped where denied, the field's lowered condition where
// conditionally granted and unpruned, bare otherwise. The explicit matches' fields
// are covered too: they are filter fields, whose conditions the read plan rendered.
func (q *QuerySet[Resource]) searchFieldGrants(dbType DBType, rendered *renderedReadConditions) (map[string]searchGrant, error) {
	if len(q.droppedSearchFields) == 0 && (rendered == nil || len(rendered.queryConditions) == 0) {
		return nil, nil
	}
	dbFields := q.rMeta.dbFieldMap(dbType)
	grants := make(map[string]searchGrant)
	for field := range q.droppedSearchFields {
		dbField, ok := dbFields[field]
		if !ok {
			return nil, errors.Newf("search field %s not found in db struct", field)
		}
		grants[dbField.ColumnName] = searchGrant{dropped: true}
	}
	if rendered != nil {
		for field, conditionSQL := range rendered.queryConditions {
			dbField, ok := dbFields[field]
			if !ok {
				return nil, errors.Newf("query field %s not found in db struct", field)
			}
			if grant, dropped := grants[dbField.ColumnName]; dropped && grant.dropped {
				continue
			}
			grants[dbField.ColumnName] = searchGrant{condition: conditionSQL}
		}
	}

	return grants, nil
}

// searchBranchGrants renders what the checks decided for the resource's @searchAlso
// branches, by the branch's dotted name: dropped where the child field is denied,
// the child field's condition lowered in the child's own context where it is
// conditionally granted (the branch's FROM is the child table, so unqualified
// attributes resolve to its columns), bare otherwise. The registry is the
// statement's, so the condition's parameters merge with the rest.
func (q *QuerySet[Resource]) searchBranchGrants(dbType DBType, registry *paramRegistry) (map[string]searchGrant, error) {
	if len(q.droppedBranches) == 0 && len(q.branchDecisions) == 0 {
		return nil, nil
	}
	grants := make(map[string]searchGrant, len(q.droppedBranches)+len(q.branchDecisions))
	for name := range q.droppedBranches {
		grants[name] = searchGrant{dropped: true}
	}
	if len(q.branchDecisions) == 0 {
		return grants, nil
	}
	if q.collection == nil {
		return nil, errors.Newf("resource %s carries conditional decisions on its search branches but no generated collection is wired to render them", q.Resource())
	}
	gen, err := loweredSQLGenerator(dbType)
	if err != nil {
		return nil, err
	}
	_, partitioned := q.scope.Domain()
	for i := range q.rMeta.searchBranches {
		branch := &q.rMeta.searchBranches[i]
		decision, ok := q.branchDecisions[branch.Name]
		if !ok {
			continue
		}
		expr, err := conditionalExpr(branch.fieldResource(), decision)
		if err != nil {
			return nil, err
		}
		expr = condition.WithoutPostImage(expr)
		if t, ok := expr.(condition.Truth); ok && t.Value {
			continue
		}
		bindings, _ := q.collection.Bindings(q.collection.Scope(branch.Resource), branch.Resource)
		lctx := &loweringContext{outer: branch.Table, bindings: bindings, collection: q.collection, partitioned: partitioned}
		sql, err := lowerToSQL(expr, lctx, gen, registry)
		if err != nil {
			return nil, errors.Wrapf(err, "lowering the condition on %s for the search branch %s", branch.fieldResource(), branch.Name)
		}
		grants[branch.Name] = searchGrant{condition: sql}
	}

	return grants, nil
}
