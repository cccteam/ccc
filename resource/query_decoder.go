package resource

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

type (
	DomainFromCtx func(context.Context) accesstypes.Domain
	UserFromCtx   func(context.Context) accesstypes.User
)

// QueryDecoder is a struct that returns columns that a given user has access to view
type QueryDecoder[Resource Resourcer, Request any] struct {
	fieldMapper       *FieldMapper
	searchKeys        *SearchKeys
	resourceSet       *ResourceSet[Resource, Request]
	permissionChecker accesstypes.Enforcer
	domainFromCtx     DomainFromCtx
	userFromCtx       UserFromCtx
}

func NewQueryDecoder[Res Resourcer, Req any](resSet *ResourceSet[Res, Req], permChecker accesstypes.Enforcer, domainFromCtx DomainFromCtx, userFromCtx UserFromCtx) (*QueryDecoder[Res, Req], error) {
	var req Req
	var res Res

	mapper, err := NewFieldMapper(req)
	if err != nil {
		return nil, errors.Wrap(err, "NewFieldMapper()")
	}

	searchKeys := NewSearchKeys[Req](res)

	return &QueryDecoder[Res, Req]{
		fieldMapper:       mapper,
		searchKeys:        searchKeys,
		resourceSet:       resSet,
		permissionChecker: permChecker,
		domainFromCtx:     domainFromCtx,
		userFromCtx:       userFromCtx,
	}, nil
}

func (d *QueryDecoder[Resource, Request]) Decode(request *http.Request) (*QuerySet[Resource], error) {
	fields, err := d.fields(request.Context(), request.URL.Query())
	if err != nil {
		return nil, err
	}

	qSet := NewQuerySet(d.resourceSet.ResourceMetadata())
	for _, field := range fields {
		qSet.AddField(field)
	}

	set, err := parseSearchParam(d.searchKeys, request.URL.Query())
	if err != nil {
		return nil, err
	}
	if set != nil {
		qSet.SetSearchParam(set)
	}

	return qSet, nil
}

func (d *QueryDecoder[Resource, Request]) fields(ctx context.Context, queryParams url.Values) ([]accesstypes.Field, error) {
	domain, user := d.domainFromCtx(ctx), d.userFromCtx(ctx)

	var columnFields []accesstypes.Field
	if cols := queryParams.Get("columns"); cols != "" {
		for _, column := range strings.Split(cols, ",") {
			if field, found := d.fieldMapper.StructFieldName(column); found {
				columnFields = append(columnFields, field)
			} else {
				return nil, httpio.NewBadRequestMessagef("unknown column: %s", column)
			}
		}
	}

	if ok, _, err := d.permissionChecker.RequireResources(ctx, user, domain, d.resourceSet.Permission(), d.resourceSet.BaseResource()); err != nil {
		return nil, errors.Wrap(err, "accesstypes.Enforcer.RequireResources()")
	} else if !ok {
		return nil, httpio.NewForbiddenMessagef("user %s does not have %s permission on %s", user, d.resourceSet.Permission(), d.resourceSet.BaseResource())
	}

	fields := make([]accesstypes.Field, 0, d.fieldMapper.Len())
	for _, field := range d.fieldMapper.Fields() {
		if len(columnFields) > 0 {
			if !slices.Contains(columnFields, field) {
				continue
			}
		}

		if !d.resourceSet.PermissionRequired(field, d.resourceSet.Permission()) {
			fields = append(fields, field)
		} else {
			if hasPerm, _, err := d.permissionChecker.RequireResources(ctx, user, domain, d.resourceSet.Permission(), d.resourceSet.Resource(field)); err != nil {
				return nil, errors.Wrap(err, "hasPermission()")
			} else if hasPerm {
				fields = append(fields, field)
			}
		}
	}

	if len(fields) == 0 {
		return nil, httpio.NewForbiddenMessagef("user %s does not have %s permission on any fields in %s", user, d.resourceSet.Permission(), d.resourceSet.BaseResource())
	}

	return fields, nil
}

func parseSearchParam(searchKeys *SearchKeys, queryParams url.Values) (searchSet *SearchSet, err error) {
	if searchKeys == nil || len(queryParams) == 0 {
		return nil, nil
	}

	var key SearchKey
	for searchKey := range searchKeys.keys {
		if len(queryParams[string(searchKey)]) == 0 {
			continue
		}

		if key != "" {
			return nil, errors.New("only one search key is allowed")
		}

		if len(queryParams[string(searchKey)]) > 1 {
			return nil, errors.New("only one search parameter is allowed")
		}

		key = SearchKey(searchKey)
	}

	typ := searchKeys.keys[key]
	val := queryParams.Get(string(key))

	return NewSearchSet(typ, key, val), nil
}
