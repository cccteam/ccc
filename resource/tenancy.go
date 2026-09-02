package resource

import (
	"encoding"
	"reflect"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

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
