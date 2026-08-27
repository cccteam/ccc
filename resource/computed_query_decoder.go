package resource

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

// ComputedQueryDecoder decodes an HTTP request for a computed resource and enforces
// user permissions at decode time.
//
// Computed resources execute application-written functions, so — like RPC methods —
// there is no library execution underneath them to carry the permission check: decode
// is the last library-controlled point before application code runs. QueryDecoder's
// deferred enforcement (discharged by QuerySet.Read/List) therefore cannot serve the
// computed path; this decoder checks eagerly instead, and the QuerySet it returns is a
// pure request carrier — checks already passed, fields already materialized.
type ComputedQueryDecoder[Resource Resourcer, Request any] struct {
	inner *QueryDecoder[Resource, Request]
}

// NewComputedQueryDecoder creates a new ComputedQueryDecoder for a given Resource and
// Request type.
func NewComputedQueryDecoder[Resource Resourcer, Request any](resSet *Set[Resource]) (*ComputedQueryDecoder[Resource, Request], error) {
	inner, err := NewQueryDecoder[Resource, Request](resSet)
	if err != nil {
		return nil, errors.Wrap(err, "NewQueryDecoder()")
	}

	return &ComputedQueryDecoder[Resource, Request]{inner: inner}, nil
}

// MustNewComputedQueryDecoder builds a query decoder for a computed resource and
// request pair. It panics on construction errors: they are programming errors (a
// request struct out of sync with its resource), surfaced at application startup where
// generated handlers construct their decoders.
func MustNewComputedQueryDecoder[Resource Resourcer, Request any](permissions ...accesstypes.Permission) *ComputedQueryDecoder[Resource, Request] {
	rSet, err := NewSet[Resource, Request](permissions...)
	if err != nil {
		panic(err)
	}

	decoder, err := NewComputedQueryDecoder[Resource, Request](rSet)
	if err != nil {
		panic(err)
	}

	return decoder
}

// Decode decodes an http.Request into a QuerySet and checks user permissions in the
// given domain partition. The semantics mirror the deferred enforcement table
// resources get at execution time: a missing resource-level grant is Forbidden, an
// explicitly requested field without its grant is Forbidden, and when no fields are
// requested the QuerySet is narrowed to the accessible fields silently.
func (d *ComputedQueryDecoder[Resource, Request]) Decode(request *http.Request, userPermissions UserPermissions, scope accesstypes.Scope) (*QuerySet[Resource], error) {
	qSet, err := d.inner.DecodeWithoutPermissions(request)
	if err != nil {
		return nil, err
	}

	perms := d.inner.resourceSet.Permissions()
	if len(perms) != 1 {
		panic(fmt.Sprintf("expected one non-mutating permission, found: %d, (%s)", len(perms), perms))
	}
	requiredPermission := perms[0]

	ctx := request.Context()
	rSet := d.inner.resourceSet

	if missing, err := userPermissions.Check(ctx, scope, requiredPermission, rSet.BaseResource()); err != nil {
		return nil, errors.Wrap(err, "enforcer.RequireResource()")
	} else if len(missing) > 0 {
		return nil, httpio.NewForbiddenMessagef("scope (%s), user (%s) does not have (%s) on %s", scope, userPermissions.User(), requiredPermission, missing)
	}

	if len(qSet.Fields()) == 0 {
		if err := d.addAccessibleFields(ctx, qSet, userPermissions, scope, requiredPermission); err != nil {
			return nil, err
		}

		return qSet, nil
	}

	resources := make([]accesstypes.Resource, 0, len(qSet.Fields()))
	for _, field := range qSet.Fields() {
		if rSet.PermissionRequired(field, requiredPermission) {
			resources = append(resources, rSet.Resource(field))
		}
	}

	if missing, err := userPermissions.Check(ctx, scope, requiredPermission, resources...); err != nil {
		return nil, errors.Wrap(err, "enforcer.RequireResource()")
	} else if len(missing) > 0 {
		return nil, httpio.NewForbiddenMessagef("scope (%s), user (%s) does not have (%s) on %s", scope, userPermissions.User(), requiredPermission, missing)
	}

	return qSet, nil
}

// addAccessibleFields materializes the default field set onto the QuerySet: every
// requestable field whose grant the user holds, plus the exempt (perm:"-") fields,
// whose readability follows the resource-level grant already checked. A computed
// resource has no database projection, so the candidates come from the request struct
// rather than database metadata.
func (d *ComputedQueryDecoder[Resource, Request]) addAccessibleFields(ctx context.Context, qSet *QuerySet[Resource], userPermissions UserPermissions, scope accesstypes.Scope, requiredPermission accesstypes.Permission) error {
	rSet := d.inner.resourceSet

	type candidate struct {
		field accesstypes.Field
		res   accesstypes.Resource
	}

	requestable := d.inner.requestFieldMapper.Fields()
	candidates := make([]candidate, 0, len(requestable))
	resources := make([]accesstypes.Resource, 0, len(requestable))

	for _, field := range requestable {
		if !rSet.PermissionRequired(field, requiredPermission) {
			candidates = append(candidates, candidate{field: field})
		} else {
			res := rSet.Resource(field)
			candidates = append(candidates, candidate{field: field, res: res})
			resources = append(resources, res)
		}
	}

	denied := make(map[accesstypes.Resource]struct{})
	if len(resources) > 0 {
		missing, err := userPermissions.Check(ctx, scope, requiredPermission, resources...)
		if err != nil {
			return errors.Wrap(err, "enforcer.RequireResource()")
		}

		for _, res := range missing {
			denied[res] = struct{}{}
		}
	}

	for _, c := range candidates {
		if _, deny := denied[c.res]; c.res == "" || !deny {
			qSet.AddField(c.field)
		}
	}

	return nil
}
