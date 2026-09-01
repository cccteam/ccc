package resource

import (
	"encoding"
	"reflect"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/go-playground/errors/v5"
)

// Structural row tenancy (ABAC design plan §06): a domain-scoped resource's
// rows resolve to their tenant through the Collection's domain binding. This
// file holds the create-path half — stamping the bare-column tenant key from
// the request's domain partition, so the checked domain and the written
// domain are the same value by construction. The generated patch structs
// close the column on the wire (json:"-"), so stamping is the only way the
// value can be written; a join-path binding has no local column to stamp and
// is enforced by the write check instead.

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
