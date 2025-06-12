package resource

import (
	"context"
	"fmt"
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
	searchKeys         *SearchKeys
	resourceSet        *ResourceSet[Resource]
	parserFields       map[string]FieldInfo
}

func NewQueryDecoder[Resource Resourcer, Request any](resSet *ResourceSet[Resource]) (*QueryDecoder[Resource, Request], error) {
	var req Request
	var res Resource

	mapper, err := NewRequestFieldMapper(req)
	if err != nil {
		return nil, errors.Wrap(err, "NewFieldMapper()")
	}

	parserFields, err := newParserFields(reflect.TypeOf(req), resSet.ResourceMetadata())
	if err != nil {
		return nil, err
	}

	return &QueryDecoder[Resource, Request]{
		requestFieldMapper: mapper,
		searchKeys:         NewSearchKeys[Request](res),
		resourceSet:        resSet,
		parserFields:       parserFields,
	}, nil
}

func (d *QueryDecoder[Resource, Request]) DecodeWithoutPermissions(request *http.Request) (*QuerySet[Resource], error) {
	requestedFields, search, currentParsedAST, err := d.parseQuery(request.URL.Query())
	if err != nil {
		return nil, err
	}

	qSet := NewQuerySet(d.resourceSet.ResourceMetadata())
	qSet.SetFilterAst(currentParsedAST)
	qSet.SetSearchParam(search)
	if len(requestedFields) == 0 {
		qSet.ReturnAccessableFields(true)
	} else {
		for _, field := range requestedFields {
			qSet.AddField(field)
		}
	}

	return qSet, nil
}

func (d *QueryDecoder[Resource, Request]) Decode(request *http.Request, userPermissions UserPermissions) (*QuerySet[Resource], error) {
	qSet, err := d.DecodeWithoutPermissions(request)
	if err != nil {
		return nil, err
	}

	perms := d.resourceSet.Permissions()
	if len(perms) != 1 {
		panic(fmt.Sprintf("expected one non-mutating permission, found: %d, (%s)", len(perms), perms))
	}

	qSet.EnableUserPermissionEnforcement(d.resourceSet, userPermissions, perms[0])

	return qSet, nil
}

func (d *QueryDecoder[Resource, Request]) parseQuery(query url.Values) (columnFields []accesstypes.Field, search *Search, parsedAST ExpressionNode, err error) {
	if cols := query.Get("columns"); cols != "" {
		// column names received in the query parameters are a comma separated list of json field names (ie: json tags on the request struct)
		// we need to convert these to struct field names
		for column := range strings.SplitSeq(cols, ",") {
			if field, found := d.requestFieldMapper.StructFieldName(column); found {
				columnFields = append(columnFields, field)
			} else {
				return nil, nil, nil, httpio.NewBadRequestMessagef("unknown column: %s", column)
			}
		}

		delete(query, "columns")
	}

	if filterStr := query.Get("filter"); filterStr != "" {
		parsedAST, err = d.parseFilterExpression(filterStr)
		if err != nil {
			return nil, nil, nil, err
		}

		delete(query, "filter")
	}

	search, query, err = d.parseFilterParam(query)
	if err != nil {
		return nil, nil, nil, err
	}

	if parsedAST != nil && search != nil {
		return nil, nil, nil, httpio.NewBadRequestMessagef("cannot use 'filter' parameter alongside 'search' parameter")
	}

	if len(query) > 0 {
		return nil, nil, nil, httpio.NewBadRequestMessagef("unknown query parameters: %v", query)
	}

	return columnFields, search, parsedAST, nil
}

// parseFilterExpression parses the filter string and returns an AST.
func (d *QueryDecoder[Resource, Request]) parseFilterExpression(filterStr string) (ExpressionNode, error) {
	parser, err := NewParser(NewLexer(filterStr), d.parserFields)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create filter expression parser")
	}

	ast, err := parser.Parse()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse filter expression")
	}

	return ast, nil
}

func (d *QueryDecoder[Resource, Request]) parseFilterParam(queryParams url.Values) (searchSet *Search, query url.Values, err error) {
	searchValues := make(map[SearchKey]string)
	var typ SearchType
	for searchKey, searchKeyType := range d.searchKeys.keys {
		if paramCount := len(queryParams[string(searchKey)]); paramCount == 0 {
			continue
		} else if paramCount > 1 {
			return nil, queryParams, httpio.NewBadRequestMessagef("only one search parameter is allowed, found: %v", queryParams[string(searchKey)])
		}

		switch searchKeyType {
		case SubString, Ngram, FullText:
			searchValues[searchKey] = queryParams.Get(searchKey.String())
		default:
			return nil, queryParams, httpio.NewBadRequestMessagef("search type not implemented: %s", searchKeyType)
		}

		if typ == "" {
			typ = searchKeyType
		} else if typ != searchKeyType {
			return nil, queryParams, httpio.NewBadRequestMessagef("only one search type is allowed, found: %s and %s", typ, searchKeyType)
		}

		delete(queryParams, string(searchKey))
	}

	if len(searchValues) == 0 {
		return nil, queryParams, nil
	}

	if len(searchValues) > 1 {
		return nil, queryParams, httpio.NewBadRequestMessagef("only one search parameter is allowed for: %s", typ)
	}

	return NewSearch(typ, searchValues), queryParams, nil
}

func newParserFields[Resource Resourcer](reqType reflect.Type, resourceMetadata *ResourceMetadata[Resource]) (map[string]FieldInfo, error) {
	fields := make(map[string]FieldInfo)

	for i := range reqType.NumField() {
		structField := reqType.Field(i)
		tag := structField.Tag.Get("index")
		if tag != "true" {
			continue
		}

		goStructFieldName := structField.Name
		jsonTag := structField.Tag.Get("json")
		jsonFieldName, _, _ := strings.Cut(jsonTag, ",")
		if jsonFieldName == "" || jsonFieldName == "-" {
			return nil, errors.Newf("indexed field %s must have a json tag", goStructFieldName)
		}

		cacheEntry, found := resourceMetadata.fieldMap[accesstypes.Field(goStructFieldName)]
		if !found {
			return nil, errors.Newf("field %s (json: %s) not found in resource metadata", goStructFieldName, jsonFieldName)
		}

		fieldType := structField.Type
		fieldKind := fieldType.Kind()
		if fieldKind == reflect.Pointer {
			fieldType = fieldType.Elem()
			fieldKind = fieldType.Kind()
		}

		fields[jsonFieldName] = FieldInfo{
			Name:      cacheEntry.tag,
			Kind:      fieldKind,
			FieldType: fieldType,
		}
	}

	return fields, nil
}
