package resource

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
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
	requestedFields, filterSet, err := d.parseQuery(request.URL.Query())
	if err != nil {
		return nil, err
	}

	qSet := NewQuerySet(d.resourceSet.ResourceMetadata())
	qSet.SetFilterParam(filterSet)
	qSet.SetRequestedFields(requestedFields)

	return qSet, nil
}

func (d *QueryDecoder[Resource, Request]) parseQuery(query url.Values) (columnFields []accesstypes.Field, filterSet *Filter, err error) {
	if cols := query.Get("columns"); cols != "" {
		// column names received in the query parameters are a comma separated list of json field names (ie: json tags on the request struct)
		// we need to convert these to struct field names
		for column := range strings.SplitSeq(cols, ",") {
			if field, found := d.requestFieldMapper.StructFieldName(column); found {
				columnFields = append(columnFields, field)
			} else {
				return nil, nil, httpio.NewBadRequestMessagef("unknown column: %s", column)
			}
		}

		delete(query, "columns")
	} else {
		columnFields = d.requestFieldMapper.Fields()
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

func (d *QueryDecoder[Resource, Request]) parseFilterParam(searchKeys *FilterKeys, queryParams url.Values) (searchSet *Filter, query url.Values, err error) {
	if searchKeys == nil || len(queryParams) == 0 {
		return nil, queryParams, nil
	}

	filterValues := make(map[FilterKey]string)
	filterKinds := make(map[FilterKey]reflect.Kind)
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
			field, _ := d.requestFieldMapper.StructFieldName(searchKey.String())
			cacheEntry, found := d.resourceSet.ResourceMetadata().fieldMap[field]
			if !found {
				return nil, queryParams, httpio.NewBadRequestMessagef("field %s not found in metadata", field)
			}

			columnName := FilterKey(cacheEntry.tag)

			filterValues[columnName] = queryParams.Get(searchKey.String())
			filterKinds[columnName] = searchKeys.kinds[FilterKey(field)]

		default:
			return nil, queryParams, httpio.NewBadRequestMessagef("search type not implemented: %s", searchKeys.keys[searchKey])
		}

		if typ == "" {
			typ = searchKeys.keys[searchKey]
		} else if typ != searchKeys.keys[searchKey] {
			return nil, queryParams, httpio.NewBadRequestMessagef("only one search type is allowed, found: %s and %s", typ, searchKeys.keys[searchKey])
		}

		delete(queryParams, string(searchKey))
	}

	if len(filterValues) == 0 {
		return nil, queryParams, nil
	}

	if len(filterValues) > 1 && typ != Index {
		return nil, queryParams, httpio.NewBadRequestMessagef("only one search parameter is allowed for: %s", typ)
	}

	return NewFilter(typ, filterValues, filterKinds), queryParams, nil
}
