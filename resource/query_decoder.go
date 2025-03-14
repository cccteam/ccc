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
	filterKeys        *FilterKeys
	resourceSet       *ResourceSet[Resource, Request]
	permissionChecker accesstypes.Enforcer
	domainFromCtx     DomainFromCtx
	userFromCtx       UserFromCtx
}

func NewQueryDecoder[Resource Resourcer, Request any](resSet *ResourceSet[Resource, Request], permChecker accesstypes.Enforcer, domainFromCtx DomainFromCtx, userFromCtx UserFromCtx) (*QueryDecoder[Resource, Request], error) {
	var req Request
	var res Resource

	mapper, err := NewFieldMapper(req)
	if err != nil {
		return nil, errors.Wrap(err, "NewFieldMapper()")
	}

	filterKeys, err := NewFilterKeys[Request](res)
	if err != nil {
		return nil, err
	}

	return &QueryDecoder[Resource, Request]{
		fieldMapper:       mapper,
		filterKeys:        filterKeys,
		resourceSet:       resSet,
		permissionChecker: permChecker,
		domainFromCtx:     domainFromCtx,
		userFromCtx:       userFromCtx,
	}, nil
}

func (d *QueryDecoder[Resource, Request]) Decode(request *http.Request) (*QuerySet[Resource], error) {
	columns, filterSet, err := d.parseQuery(request.URL.Query())
	if err != nil {
		return nil, err
	}

	fields, err := d.fields(request.Context(), columns)
	if err != nil {
		return nil, err
	}

	qSet := NewQuerySet(d.resourceSet.ResourceMetadata())
	qSet.SetFilterParam(filterSet)
	for _, field := range fields {
		qSet.AddField(field)
	}

	return qSet, nil
}

func (d *QueryDecoder[Resource, Request]) fields(ctx context.Context, columnFields []accesstypes.Field) ([]accesstypes.Field, error) {
	domain, user := d.domainFromCtx(ctx), d.userFromCtx(ctx)

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

func (d *QueryDecoder[Resource, Request]) parseQuery(query url.Values) (columnFields []accesstypes.Field, filterSet *Filter, err error) {
	if cols := query.Get("columns"); cols != "" {
		for _, column := range strings.Split(cols, ",") {
			if field, found := d.fieldMapper.StructFieldName(column); found {
				columnFields = append(columnFields, field)
			} else {
				return nil, nil, httpio.NewBadRequestMessagef("unknown column: %s", column)
			}
		}

		delete(query, "columns")
	}

	filterSet, query, err = d.parseFilterParam(d.filterKeys, query)
	if err != nil {
		return nil, nil, err
	}

	if len(query) > 0 {
		return nil, nil, httpio.NewBadRequestMessagef("unknown query parameters: %v", query)
	}

	return columnFields, filterSet, nil
}

func (d *QueryDecoder[Resource, Request]) parseFilterParam(searchKeys *FilterKeys, queryParams url.Values) (filter *Filter, query url.Values, err error) {
	if searchKeys == nil || len(queryParams) == 0 {
		return nil, queryParams, nil
	}

	filterValues := make(map[FilterKey]string)
	var typ FilterType
	for searchKey := range searchKeys.keys {
		if paramCount := len(queryParams[string(searchKey)]); paramCount == 0 {
			continue
		} else if paramCount > 1 {
			return nil, queryParams, httpio.NewBadRequestMessagef("only one search parameter is allowed, found: %v", queryParams[string(searchKey)])
		}

		switch searchKeys.keys[searchKey] {
		case SubString, Ngram, FullText:
			filterValues[searchKey] = queryParams.Get(searchKey.String())

		case Index:
			field, _ := d.fieldMapper.StructFieldName(searchKey.String())
			cacheEntry, found := d.resourceSet.ResourceMetadata().fieldMap[field]
			if !found {
				return nil, queryParams, httpio.NewBadRequestMessagef("field %s not found in metadata", field)
			}

			columnName := FilterKey(cacheEntry.tag)

			filterValues[columnName] = queryParams.Get(searchKey.String())

		default:
			return nil, queryParams, httpio.NewBadRequestMessagef("search type not implemented: %s", searchKeys.keys[searchKey])
		}

		if typ != "" && typ != searchKeys.keys[searchKey] {
			return nil, queryParams, httpio.NewBadRequestMessagef("only one search type is allowed, found: %s and %s", typ, searchKeys.keys[searchKey])
		}

		typ = searchKeys.keys[searchKey]

		delete(queryParams, string(searchKey))
	}

	if len(filterValues) == 0 {
		return nil, queryParams, nil
	}

	if len(filterValues) > 1 && typ != Index {
		return nil, queryParams, httpio.NewBadRequestMessagef("only one search parameter is allowed for: %s", typ)
	}

	return NewFilter(typ, filterValues), queryParams, nil
}
