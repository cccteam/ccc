// package columnset provides types to store columns that a given user has access to view
package columnset

import (
	"context"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/ccc/resourceset"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

type (
	DomainFromCtx func(context.Context) accesstypes.Domain
	UserFromCtx   func(context.Context) accesstypes.User
)

type ColumnSet interface {
	StructFields(ctx context.Context) ([]accesstypes.Field, error)
}

// columnSet is a struct that returns columns that a given user has access to view
type columnSet[T any] struct {
	fieldMapper       *resourceset.FieldMapper
	resourceSet       *resourceset.ResourceSet
	permissionChecker accesstypes.Enforcer
	domainFromCtx     DomainFromCtx
	userFromCtx       UserFromCtx
}

func NewColumnSet[T any](rSet *resourceset.ResourceSet, permissionChecker accesstypes.Enforcer, domainFromCtx DomainFromCtx, userFromCtx UserFromCtx) (ColumnSet, error) {
	target := new(T)

	m, err := resourceset.NewFieldMapper(target)
	if err != nil {
		return nil, errors.Wrap(err, "NewFieldMapper()")
	}

	return &columnSet[T]{
		fieldMapper:       m,
		resourceSet:       rSet,
		permissionChecker: permissionChecker,
		domainFromCtx:     domainFromCtx,
		userFromCtx:       userFromCtx,
	}, nil
}

func (p *columnSet[T]) StructFields(ctx context.Context) ([]accesstypes.Field, error) {
	domain, user := p.domainFromCtx(ctx), p.userFromCtx(ctx)

	if ok, _, err := p.permissionChecker.RequireResources(ctx, user, domain, p.resourceSet.Permission(), p.resourceSet.BaseResource()); err != nil {
		return nil, errors.Wrap(err, "accesstypes.Enforcer.RequireResources()")
	} else if !ok {
		return nil, httpio.NewForbiddenMessagef("user %s does not have %s permission on %s", user, p.resourceSet.Permission(), p.resourceSet.BaseResource())
	}

	fields := make([]accesstypes.Field, 0, p.fieldMapper.Len())
	for _, field := range p.fieldMapper.Fields() {
		if !p.resourceSet.PermissionRequired(field, p.resourceSet.Permission()) {
			fields = append(fields, field)
		} else {
			if hasPerm, _, err := p.permissionChecker.RequireResources(ctx, user, domain, p.resourceSet.Permission(), p.resourceSet.Resource(field)); err != nil {
				return nil, errors.Wrap(err, "hasPermission()")
			} else if hasPerm {
				fields = append(fields, field)
			}
		}
	}

	if len(fields) == 0 {
		return nil, httpio.NewForbiddenMessagef("user %s does not have %s permission on any fields in %s", user, p.resourceSet.Permission(), p.resourceSet.BaseResource())
	}

	return fields, nil
}
