package resource

import (
	"context"
	"net/http"
	"net/url"
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
	requestFieldMapper *RequestFieldMapper
	filterKeys         *FilterKeys
	resourceSet        *ResourceSet[Resource, Request]
}

func NewQueryDecoder[Resource Resourcer, Request any](resSet *ResourceSet[Resource, Request]) (*QueryDecoder[Resource, Request], error) {
	var req Request
	var res Resource

	mapper, err := NewRequestFieldMapper(req)
	if err != nil {
		return nil, errors.Wrap(err, "NewFieldMapper()")
	}

	filterKeys, err := NewFilterKeys[Request](res)
	if err != nil {
		return nil, err
	}

	return &QueryDecoder[Resource, Request]{
		requestFieldMapper: mapper,
		filterKeys:         filterKeys,
		resourceSet:        resSet,
	}, nil
}

func (d *QueryDecoder[Resource, Request]) Decode(request *http.Request) (*QuerySet[Resource], error) {
	reqFields, filterSet, err := d.parseQuery(request.URL.Query())
	if err != nil {
		return nil, err
	}

	qSet := NewQuerySet(d.resourceSet.ResourceMetadata())
	qSet.SetFilterParam(filterSet)
	qSet.SetRequestedFields(reqFields)

	return qSet, nil
}

func (d *QueryDecoder[Resource, Request]) parseQuery(query url.Values) (fields []accesstypes.Field, filterSet *FilterSet, err error) {
	if columns := query.Get("columns"); columns != "" {
		// column names received in the query parameters are a comma separated list of json field names (ie: json tags on the request struct)
		// we need to convert these to struct field names
		for jsonColumn := range strings.SplitSeq(columns, ",") {
			if field, found := d.requestFieldMapper.StructFieldName(jsonColumn); found {
				fields = append(fields, field)
			} else {
				return nil, nil, httpio.NewBadRequestMessagef("unknown column: %s", jsonColumn)
			}
		}

		delete(query, "columns")
	} else {
		fields = d.requestFieldMapper.Fields()
	}

	filterSet, query, err = d.parseFilterParam(d.filterKeys, query)
	if err != nil {
		return nil, nil, err
	}

	if len(query) > 0 {
		return nil, nil, httpio.NewBadRequestMessagef("unknown query parameters: %v", query)
	}

	return fields, filterSet, nil
}

func (d *QueryDecoder[Resource, Request]) parseFilterParam(searchKeys *FilterKeys, queryParams url.Values) (searchSet *FilterSet, query url.Values, err error) {
	if searchKeys == nil || len(queryParams) == 0 {
		return nil, queryParams, nil
	}

	var key FilterKey
	var typ FilterType
	var val string
	for searchKey := range searchKeys.keys {
		if len(queryParams[string(searchKey)]) == 0 {
			continue
		}

		if len(queryParams[string(searchKey)]) > 1 {
			return nil, queryParams, httpio.NewBadRequestMessagef("only one search parameter is allowed, found: %v", queryParams[string(searchKey)])
		}

		switch searchKeys.keys[searchKey] {
		case SubString, Ngram, FullText:
			key = searchKey // database column name
		case Index:
			field, _ := d.requestFieldMapper.StructFieldName(string(searchKey))
			cache, found := d.resourceSet.ResourceMetadata().fieldMap[field]
			if !found {
				return nil, queryParams, httpio.NewBadRequestMessagef("field %s not found in metadata", field)
			}
			key = FilterKey(string(cache.tag)) // database column name
		default:
			return nil, queryParams, httpio.NewBadRequestMessagef("search type not implemented: %s", searchKeys.keys[searchKey])
		}

		typ = searchKeys.keys[searchKey]
		val = queryParams.Get(string(searchKey))
		delete(queryParams, string(searchKey))

		break
	}

	if key == "" {
		return nil, queryParams, nil
	}

	return NewFilterSet(typ, key, val), queryParams, nil
}
