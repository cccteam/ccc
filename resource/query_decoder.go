package resource

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

type (
	DomainFromCtx func(context.Context) accesstypes.Domain
	UserFromCtx   func(context.Context) accesstypes.User
)

type parsedQueryParams struct {
	ColumnFields []accesstypes.Field
	SortFields   []SortField
	Search       *Search
	ParsedAST    ExpressionNode
	Limit        *uint64
	Offset       *uint64
}

// QueryDecoder is a struct that returns columns that a given user has access to view
type QueryDecoder[Resource Resourcer, Request any] struct {
	requestFieldMapper *RequestFieldMapper
	searchKeys         *SearchKeys
	resourceSet        *ResourceSet[Resource]
	parserFields       map[string]FieldInfo
	structDecoder      *StructDecoder[filterBody]
}

type filterBody struct {
	Filter string `json:"filter"`
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

	structDecoder, err := NewStructDecoder[filterBody]()
	if err != nil {
		return nil, errors.Wrap(err, "NewStructDecoder[filterBody]()")
	}

	return &QueryDecoder[Resource, Request]{
		requestFieldMapper: mapper,
		searchKeys:         NewSearchKeys[Request](res),
		resourceSet:        resSet,
		parserFields:       parserFields,
		structDecoder:      structDecoder,
	}, nil
}

func (d *QueryDecoder[Resource, Request]) DecodeWithoutPermissions(request *http.Request) (*QuerySet[Resource], error) {
	queryParams := request.URL.Query()

	if request.Method == http.MethodPost {
		body, err := d.structDecoder.Decode(request)
		if err != nil && err != io.EOF {
			return nil, err
		}

		if body != nil && body.Filter != "" {
			if queryParams.Get("filter") != "" {
				return nil, httpio.NewBadRequestMessagef("cannot have 'filter' parameter in both query and body")
			}
			queryParams.Add("filter", body.Filter)
		}
	}

	parsedQuery, err := d.parseQuery(queryParams)
	if err != nil {
		return nil, err
	}

	qSet := NewQuerySet(d.resourceSet.ResourceMetadata())
	qSet.SetFilterAst(parsedQuery.ParsedAST)
	qSet.SetSearchParam(parsedQuery.Search)
	qSet.SetSortFields(parsedQuery.SortFields)
	qSet.SetLimit(parsedQuery.Limit)
	qSet.SetOffset(parsedQuery.Offset)
	if len(parsedQuery.ColumnFields) == 0 {
		qSet.ReturnAccessableFields(true)
	} else {
		for _, field := range parsedQuery.ColumnFields {
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

func (d *QueryDecoder[Resource, Request]) parseQuery(query url.Values) (*parsedQueryParams, error) {
	var columnFields []accesstypes.Field
	var sortFields []SortField
	var search *Search
	var parsedAST ExpressionNode
	var limit *uint64
	var offset *uint64
	var err error

	if sortParamValue := query.Get("sort"); sortParamValue != "" {
		sortFields, err = d.parseSortParam(sortParamValue)
		if err != nil {
			return nil, err
		}

		delete(query, "sort")
	}

	if limitStr := query.Get("limit"); limitStr != "" {
		limitVal, err := strconv.ParseUint(limitStr, 10, 64)
		if err != nil {
			return nil, httpio.NewBadRequestMessagef("invalid limit value: %s", limitStr)
		}
		limit = &limitVal
		delete(query, "limit")
	} else {
		defaultLimit := uint64(50)
		limit = &defaultLimit
	}

	if offsetStr := query.Get("offset"); offsetStr != "" {
		offsetVal, err := strconv.ParseUint(offsetStr, 10, 64)
		if err != nil {
			return nil, httpio.NewBadRequestMessagef("invalid offset value: %s", offsetStr)
		}
		offset = &offsetVal
		delete(query, "offset")
	}

	if cols := query.Get("columns"); cols != "" {
		// column names received in the query parameters are a comma separated list of json field names (ie: json tags on the request struct)
		// we need to convert these to struct field names
		for column := range strings.SplitSeq(cols, ",") {
			if field, found := d.requestFieldMapper.StructFieldName(column); found {
				columnFields = append(columnFields, field)
			} else {
				return nil, httpio.NewBadRequestMessagef("unknown column: %s", column)
			}
		}

		delete(query, "columns")
	}

	if filterStr := query.Get("filter"); filterStr != "" {
		parsedAST, err = d.parseFilterExpression(filterStr)
		if err != nil {
			return nil, err
		}

		delete(query, "filter")
	}

	search, query, err = d.parseFilterParam(query)
	if err != nil {
		return nil, err
	}

	if parsedAST != nil && search != nil {
		return nil, httpio.NewBadRequestMessagef("cannot use 'filter' parameter alongside 'search' parameter")
	}

	if search != nil && len(sortFields) > 0 {
		return nil, httpio.NewBadRequestMessage("sorting ('sort=' parameter) cannot be used in conjunction with search parameters")
	}

	if len(query) > 0 {
		return nil, httpio.NewBadRequestMessagef("unknown query parameters: %v", query)
	}

	return &parsedQueryParams{
		ColumnFields: columnFields,
		SortFields:   sortFields,
		Search:       search,
		ParsedAST:    parsedAST,
		Limit:        limit,
		Offset:       offset,
	}, nil
}

func (d *QueryDecoder[Resource, Request]) parseSortParam(sortParamValue string) ([]SortField, error) {
	var sortFields []SortField
	sortParts := strings.Split(sortParamValue, ",")
	if len(sortParts) > 0 {
		sortFields = make([]SortField, 0, len(sortParts))
		for _, part := range sortParts {
			trimmedPart := strings.TrimSpace(part)
			if trimmedPart == "" {
				return nil, httpio.NewBadRequestMessagef("invalid sort field, found empty part in sort parameter: %s", sortParamValue)
			}
			fieldAndDir := strings.SplitN(trimmedPart, ":", 2)
			jsonFieldName := strings.TrimSpace(fieldAndDir[0])

			if jsonFieldName == "" {
				return nil, httpio.NewBadRequestMessagef("sort field name cannot be empty")
			}

			goFieldName, found := d.requestFieldMapper.StructFieldName(jsonFieldName)
			if !found {
				return nil, httpio.NewBadRequestMessagef("unknown sort field: %s", jsonFieldName)
			}
			// Ensure the field exists in the resource metadata
			if _, fieldMetaExists := d.resourceSet.ResourceMetadata().fieldMap[goFieldName]; !fieldMetaExists {
				return nil, httpio.NewBadRequestMessagef("sort field '%s' (resolved to '%s') not found in resource", jsonFieldName, goFieldName)
			}

			direction := SortAscending // Default direction
			if len(fieldAndDir) == 2 {
				dirStr := strings.ToLower(strings.TrimSpace(fieldAndDir[1]))
				switch dirStr {
				case "asc":
					direction = SortAscending
				case "desc":
					direction = SortDescending
				default:
					return nil, httpio.NewBadRequestMessagef("invalid sort direction for field '%s': %s. Must be 'asc' or 'desc'", jsonFieldName, fieldAndDir[1])
				}
			}
			sortFields = append(sortFields, SortField{Field: string(goFieldName), Direction: direction})
		}
	}

	return sortFields, nil
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
		var indexed bool
		structField := reqType.Field(i)
		tag := structField.Tag.Get("index")
		if tag == "true" {
			indexed = true
		} else {
			tag := structField.Tag.Get("allow_filter")
			if tag != "true" {
				continue
			}
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
			Indexed:   indexed,
		}
	}

	return fields, nil
}
