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
	fields := make([]accesstypes.Field, 0, p.fieldMapper.Len())
	domain, user := p.domainFromCtx(ctx), p.userFromCtx(ctx)
	for _, field := range p.fieldMapper.Fields() {
		if !p.resourceSet.PermissionRequired(field, p.resourceSet.RequiredPermission()) {
			fields = append(fields, field)
		} else {
			if hasPerm, err := hasPermission(ctx, p.permissionChecker, p.resourceSet, domain, user, p.resourceSet.Resource(field)); err != nil {
				return nil, errors.Wrap(err, "hasPermission()")
			} else if hasPerm {
				fields = append(fields, field)
			}
		}
	}

	if len(fields) == 0 {
		return nil, httpio.NewForbiddenMessagef("user %s does not have %s permission on any fields in %s", user, p.resourceSet.RequiredPermission(), p.resourceSet.BaseResource())
	}

	return fields, nil
}

func hasPermission(
	ctx context.Context, enforcer accesstypes.Enforcer, resourceSet *resourceset.ResourceSet, domain accesstypes.Domain, user accesstypes.User, resource accesstypes.Resource,
) (bool, error) {
	if ok, _, err := enforcer.RequireResources(ctx, user, domain, resourceSet.RequiredPermission(), resource); err != nil {
		return false, errors.Wrap(err, "Enforcer.RequireResources()")
	} else if !ok {
		return false, nil
	}

	return true, nil
}
