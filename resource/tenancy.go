package resource

import (
	"encoding"
	"reflect"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// mutationTenancy is one mutation's structural-tenancy obligation, resolved
// from the request partition and the resource's domain binding:
//
//   - update and delete locate their target row WITHIN the tenant predicate —
//     the check-SELECT's WHERE gains the tenancy AND, so a cross-tenant key is
//     NotFound, indistinguishable from a row that does not exist;
//   - an insert over a bare-column binding is verified in Go: the decoder
//     stamps the tenant key from the request's domain, so the proposed value
//     must equal the partition (a differing value can only come from
//     hand-built code — a programming error, fail loud);
//   - an insert over a join-path binding proves its proposed foreign key lands
//     in the partition: a correlated EXISTS over the proposed image joins the
//     check-SELECT, and false is NotFound (the referenced parent is not in
//     this partition's world);
//   - an insert-or-update on a partitioned resource fails closed: the target
//     row may exist in another partition, and the design pins semantics for
//     insert, update, and delete only.
type mutationTenancy struct {
	domain  accesstypes.Domain
	binding *DomainBindingData
	insert  bool
}

// locatesRow reports whether the mutation's target row must be located within
// the tenant predicate (update and delete).
func (t *mutationTenancy) locatesRow() bool { return t != nil && !t.insert }

// insertPathTerm reports whether the mutation is an insert over a join-path
// binding, whose proposed image joins the check-SELECT as a boolean term.
func (t *mutationTenancy) insertPathTerm() bool {
	return t != nil && t.insert && len(t.binding.Path) > 0
}

// needsQuery reports whether the mutation's tenancy requires the check-SELECT
// to run even with no conditional groups.
func (t *mutationTenancy) needsQuery() bool {
	return t.locatesRow() || t.insertPathTerm()
}

// insertTenancyTerm renders a partitioned insert's tenancy proof over a
// join-path binding: correlated EXISTS leaving through the proposed anchor's
// foreign-key parameter, terminating at the tenant key compared to the
// request partition.
func insertTenancyTerm(t *mutationTenancy, lctx *loweringContext, gen *sqlGenerator, registry *paramRegistry) (string, error) {
	param, touched := lctx.proposed.param(t.binding.Column, registry)
	if !touched {
		return "", errors.Newf("a partitioned create must set %s — the tenant path's anchor column", t.binding.Column)
	}
	start := namedComparand(param)
	target, wrap, err := lctx.pathTarget(&start, t.binding.Path, registry)
	if err != nil {
		return "", err
	}
	node := wrap(&loweredComparisonNode{left: target, op: "=", right: namedComparand(domainParamName)})

	return gen.generateLowered(node, registry)
}

// hopTable names the join path's first table for the insert NotFound message.
func (t *mutationTenancy) hopTable() string { return t.binding.Path[0].Table }

// mutationTenancy resolves the patch's tenancy obligation. A global request
// has no partition; a patch set without the generated collection owns its own
// tenancy (every generated path wires one); a wired collection whose resource
// lacks the domain binding is stale generated code and fails loud.
func (p *PatchSet[Resource]) mutationTenancy() (*mutationTenancy, error) {
	q := p.querySet
	domain, ok := q.scope.Domain()
	if !ok || q.collection == nil {
		return nil, nil
	}
	bindings, _ := q.collection.Bindings(q.collection.Scope(q.Resource()), q.Resource())
	if bindings.Domain == nil {
		return nil, errors.Newf("resource %s: partitioned mutation over a resource with no domain binding — @domain is mandatory on domain-scoped resources; regenerate", q.Resource())
	}
	if p.patchType == CreateOrUpdatePatchType {
		return nil, errors.Newf("structural row tenancy cannot enforce an insert-or-update mutation on %s — the target row may exist in another partition; use a create or update operation", q.Resource())
	}

	t := &mutationTenancy{domain: domain, binding: bindings.Domain, insert: p.patchType == CreatePatchType}

	if len(t.binding.Path) == 0 && p.patchType != DeletePatchType {
		if err := p.verifyTenantKey(t); err != nil {
			return nil, err
		}
	}

	return t, nil
}

// verifyTenantKey checks a proposed bare-column tenant value against the
// request's partition: an insert must carry it (the decoder stamps it) and it
// must equal the domain; an update not touching the column has nothing to
// verify — the column is decode-closed, so only hand-built code can propose a
// value, and a mismatch is a programming error.
func (p *PatchSet[Resource]) verifyTenantKey(t *mutationTenancy) error {
	q := p.querySet
	fieldMap := q.rMeta.dbFieldMap(SpannerDBType)
	for _, field := range q.rMeta.DBFields(SpannerDBType) {
		meta := fieldMap[field]
		if meta.ColumnName != t.binding.Column {
			continue
		}

		var proposed any
		switch {
		case p.IsSet(field):
			proposed = p.Get(field)
		case p.Key(field) != nil:
			proposed = p.Key(field)
		default:
			if t.insert {
				return errors.Newf("resource %s: a partitioned create must carry its tenant key %s — the decoder stamps it from the request's domain; hand-built patch sets must set it", q.Resource(), field)
			}

			return nil
		}

		want, err := tenantKeyValue(meta.fieldType, t.domain)
		if err != nil {
			return errors.Wrapf(err, "verifying tenant key %s on %s", field, q.Resource())
		}
		if !reflect.DeepEqual(proposed, want) {
			return errors.Newf("resource %s: proposed tenant key %s does not equal the request partition — the tenant key is framework-stamped and never re-tenanted", q.Resource(), field)
		}

		return nil
	}

	return errors.Newf("tenant key column %q is not a field of resource %s", t.binding.Column, q.Resource())
}

// Structural row tenancy (ABAC design plan §06): a domain-scoped resource's
// rows resolve to their tenant through the Collection's domain binding, and
// enforcement consumes it as data — a predicate fragment in every partitioned
// statement's WHERE, and a stamped tenant key on the create path — so the
// checked domain and the filtered/written domain are the same value by
// construction. The generated patch structs close the tenant column on the
// wire (json:"-"), so stamping is the only way the value can be written; a
// join-path binding has no local column to stamp and is enforced by the
// write check instead.

// tenancyPredicate renders the resource rule in its compiled form: the tenant
// predicate every partitioned statement carries, unconditionally — present
// with or without conditional grants, never skippable. A global request has
// no partition and renders nothing. A hand-built QuerySet without the
// generated collection owns its own tenancy (every generated path wires the
// collection); a wired collection whose resource lacks the domain binding is
// stale generated code and fails loud.
func (q *QuerySet[Resource]) tenancyPredicate(dbType DBType, registry *paramRegistry) (string, error) {
	if _, ok := q.scope.Domain(); !ok {
		return "", nil
	}
	if q.collection == nil {
		return "", nil
	}
	bindings, _ := q.collection.Bindings(q.collection.Scope(q.Resource()), q.Resource())
	if bindings.Domain == nil {
		return "", errors.Newf("resource %s: partitioned request over a resource with no domain binding — @domain is mandatory on domain-scoped resources; regenerate", q.Resource())
	}

	gen, err := loweredSQLGenerator(dbType)
	if err != nil {
		return "", err
	}

	outer := string(q.Resource())
	var node ExpressionNode
	if len(bindings.Domain.Path) == 0 {
		node = &loweredComparisonNode{
			left:  columnComparand(outer, bindings.Domain.Column),
			op:    "=",
			right: namedComparand(domainParamName),
		}
	} else {
		lctx := &loweringContext{outer: outer, bindings: bindings, collection: q.collection, partitioned: true}
		anchorColumn := columnComparand(outer, bindings.Domain.Column)
		target, wrap, err := lctx.pathTarget(&anchorColumn, bindings.Domain.Path, registry)
		if err != nil {
			return "", err
		}
		node = wrap(&loweredComparisonNode{left: target, op: "=", right: namedComparand(domainParamName)})
	}

	sql, err := gen.generateLowered(node, registry)
	if err != nil {
		return "", err
	}

	return "(" + sql + ")", nil
}

// stampTenantKey writes the request's domain partition into the resource's
// bare-column tenant key. It is a no-op for global requests, resources
// without a bare-column domain binding, and patch sets without a stamped
// collection (hand-built sets own their tenant values).
func (p *PatchSet[Resource]) stampTenantKey() error {
	q := p.querySet
	if q.collection == nil || q.resourceSet == nil {
		return nil
	}
	domain, ok := q.scope.Domain()
	if !ok {
		return nil
	}
	bindings, ok := q.collection.Bindings(accesstypes.DomainPermissionScope, q.resourceSet.BaseResource())
	if !ok || bindings.Domain == nil || len(bindings.Domain.Path) > 0 {
		return nil
	}

	fieldMap := q.rMeta.dbFieldMap(SpannerDBType)
	for _, field := range q.rMeta.DBFields(SpannerDBType) {
		meta := fieldMap[field]
		if meta.ColumnName != bindings.Domain.Column {
			continue
		}
		value, err := tenantKeyValue(meta.fieldType, domain)
		if err != nil {
			return errors.Wrapf(err, "stamping tenant key %s on %s", field, q.resourceSet.BaseResource())
		}
		p.Set(field, value)

		return nil
	}

	return errors.Newf("tenant key column %q is not a field of resource %s", bindings.Domain.Column, q.resourceSet.BaseResource())
}

// tenantKeyValue converts the request's domain into the tenant column's Go
// type: string-kinded types convert directly, everything else must implement
// encoding.TextUnmarshaler (e.g. ccc.UUID).
func tenantKeyValue(t reflect.Type, domain accesstypes.Domain) (any, error) {
	if t.Kind() == reflect.String {
		v := reflect.New(t).Elem()
		v.SetString(string(domain))

		return v.Interface(), nil
	}

	ptr := reflect.New(t)
	if u, ok := ptr.Interface().(encoding.TextUnmarshaler); ok {
		if err := u.UnmarshalText([]byte(domain)); err != nil {
			return nil, errors.Wrapf(err, "unmarshaling domain %q into %s", domain, t)
		}

		return ptr.Elem().Interface(), nil
	}

	return nil, errors.Newf("type %s is neither string-kinded nor an encoding.TextUnmarshaler", t)
}
