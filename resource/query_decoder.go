package resource

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/go-playground/errors/v5"
)

type parsedQueryParams struct {
	ColumnFields []accesstypes.Field
	SortFields   []SortField
	FilterParser func(DBType) (ExpressionNode, error)
	Limit        *uint64
	Offset       *uint64
	Capabilities []accesstypes.Permission
}

type filterBody struct {
	Filter string `json:"filter"`
}

// QueryDecoder is a struct that returns columns that a given user has access to view
type QueryDecoder[Resource Resourcer, Request any] struct {
	requestFieldMapper *RequestFieldMapper
	resourceSet        *Set[Resource]
	filterParserFields map[jsonFieldName]FilterFieldInfo
	structDecoder      *StructDecoder[filterBody]

	// collection resolves condition rendering for the QuerySets this decoder
	// builds; nil leaves conditions unrenderable (an error if one ever
	// arrives).
	collection *GeneratedCollection
}

// NewQueryDecoder creates a new QueryDecoder for a given Resource and Request type.
func NewQueryDecoder[Resource Resourcer, Request any](resSet *Set[Resource]) (*QueryDecoder[Resource, Request], error) {
	var req Request

	mapper, err := NewRequestFieldMapper(req)
	if err != nil {
		return nil, errors.Wrap(err, "NewFieldMapper()")
	}

	filterParserFields, err := newFilterParserFields(reflect.TypeOf(req), resSet.ResourceMetadata())
	if err != nil {
		return nil, err
	}

	structDecoder, err := NewStructDecoder[filterBody]()
	if err != nil {
		return nil, errors.Wrap(err, "NewStructDecoder[filterBody]()")
	}

	return &QueryDecoder[Resource, Request]{
		requestFieldMapper: mapper,
		resourceSet:        resSet,
		filterParserFields: filterParserFields,
		structDecoder:      structDecoder,
	}, nil
}

// MustNewQueryDecoder builds a query decoder for a resource and request pair,
// wired to the application's generated collection so conditional grants can
// render. It panics on construction errors: they are programming errors (a
// request struct out of sync with its resource), surfaced at application
// startup where generated handlers construct their decoders.
func MustNewQueryDecoder[Resource Resourcer, Request any](collection *GeneratedCollection, permissions ...accesstypes.Permission) *QueryDecoder[Resource, Request] {
	rSet, err := NewSet[Resource, Request](permissions...)
	if err != nil {
		panic(err)
	}

	decoder, err := NewQueryDecoder[Resource, Request](rSet)
	if err != nil {
		panic(err)
	}
	decoder.collection = collection

	return decoder
}

// DecodeWithoutPermissions decodes an http.Request into a QuerySet without enforcing user permissions.
func (d *QueryDecoder[Resource, Request]) DecodeWithoutPermissions(request *http.Request) (*QuerySet[Resource], error) {
	queryParams := request.URL.Query()

	if filterStr := queryParams.Get(filterParam); filterStr != "" {
		if err := d.checkForPII(filterStr); err != nil {
			return nil, err
		}
	}

	if request.Method == http.MethodPost {
		body, err := d.structDecoder.Decode(request)
		if err != nil {
			return nil, err
		}

		if body.Filter != "" {
			if queryParams.Get(filterParam) != "" {
				return nil, httpio.NewBadRequestMessagef("cannot have 'filter' parameter in both query and body")
			}
			queryParams.Add(filterParam, body.Filter)
		}
	}

	parsedQuery, err := d.parseQuery(queryParams)
	if err != nil {
		return nil, err
	}

	qSet := NewQuerySet(d.resourceSet.ResourceMetadata())
	qSet.env = newRequestEnvironment()
	qSet.requestableFields = d.requestFieldMapper.Fields()
	qSet.collection = d.collection
	qSet.jsonNames = d.requestFieldMapper.JSONNames()
	qSet.SetFilterParser(parsedQuery.FilterParser)
	qSet.SetSortFields(parsedQuery.SortFields)
	qSet.SetLimit(parsedQuery.Limit)
	qSet.SetOffset(parsedQuery.Offset)
	qSet.RequestCapabilities(parsedQuery.Capabilities...)
	if len(parsedQuery.ColumnFields) == 0 {
		qSet.ReturnAccessibleFields(true)
	} else {
		for _, field := range parsedQuery.ColumnFields {
			qSet.AddField(field)
		}
	}

	return qSet, nil
}

// Decode decodes an http.Request into a QuerySet and enables user permission enforcement
// in the given domain partition.
func (d *QueryDecoder[Resource, Request]) Decode(request *http.Request, userPermissions UserPermissions, scope accesstypes.Scope) (*QuerySet[Resource], error) {
	qSet, err := d.DecodeWithoutPermissions(request)
	if err != nil {
		return nil, err
	}

	perms := d.resourceSet.Permissions()
	if len(perms) != 1 {
		panic(fmt.Sprintf("expected one non-mutating permission, found: %d, (%s)", len(perms), perms))
	}

	qSet.EnableUserPermissionEnforcement(d.resourceSet, userPermissions, scope, perms[0])

	return qSet, nil
}

func (d *QueryDecoder[Resource, Request]) parseQuery(query url.Values) (*parsedQueryParams, error) {
	var columnFields []accesstypes.Field
	var sortFields []SortField
	var filterParser func(DBType) (ExpressionNode, error)
	var limit *uint64
	var offset *uint64
	var err error

	if sortParamValue := query.Get(sortParam); sortParamValue != "" {
		sortFields, err = d.parseSortParam(sortParamValue)
		if err != nil {
			return nil, err
		}

		delete(query, sortParam)
	}

	if limitStr := query.Get(limitParam); limitStr != "" {
		limitVal, err := strconv.ParseUint(limitStr, 10, 64)
		if err != nil {
			return nil, httpio.NewBadRequestMessagef("invalid limit value: %s", limitStr)
		}
		limit = &limitVal
		delete(query, limitParam)
	} else {
		defaultLimit := uint64(50)
		limit = &defaultLimit
	}

	if offsetStr := query.Get(offsetParam); offsetStr != "" {
		offsetVal, err := strconv.ParseUint(offsetStr, 10, 64)
		if err != nil {
			return nil, httpio.NewBadRequestMessagef("invalid offset value: %s", offsetStr)
		}
		offset = &offsetVal
		delete(query, offsetParam)
	}

	if cols := query.Get(columnsParam); cols != "" {
		// column names received in the query parameters are a comma separated list of json field names (ie: json tags on the request struct)
		// we need to convert these to struct field names
		for column := range strings.SplitSeq(cols, ",") {
			if field, found := d.requestFieldMapper.StructFieldName(column); found {
				columnFields = append(columnFields, field)
			} else {
				return nil, httpio.NewBadRequestMessagef("unknown column: %s", column)
			}
		}

		delete(query, columnsParam)
	}

	if filterStr := query.Get(filterParam); filterStr != "" {
		filterParser, err = d.filterExpressionParser(filterStr)
		if err != nil {
			return nil, err
		}

		delete(query, filterParam)
	}

	var capabilities []accesstypes.Permission
	if capStr := query.Get(capabilitiesParam); capStr != "" {
		// The capability envelope (README §5): a comma-separated list of the
		// write permissions to evaluate per row.
		for name := range strings.SplitSeq(capStr, ",") {
			perm, err := capabilityPermission(strings.TrimSpace(name))
			if err != nil {
				return nil, err
			}
			if !slices.Contains(capabilities, perm) {
				capabilities = append(capabilities, perm)
			}
		}

		delete(query, capabilitiesParam)
	}

	if len(query) > 0 {
		return nil, httpio.NewBadRequestMessagef("unknown query parameters: %v", query)
	}

	return &parsedQueryParams{
		ColumnFields: columnFields,
		SortFields:   sortFields,
		FilterParser: filterParser,
		Limit:        limit,
		Offset:       offset,
		Capabilities: capabilities,
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

// filterExpressionParser returns a filter parser.
func (d *QueryDecoder[Resource, Request]) filterExpressionParser(filterStr string) (func(DBType) (ExpressionNode, error), error) {
	parser, err := NewFilterParser(NewFilterLexer(filterStr), d.filterParserFields)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create filter expression parser")
	}

	return parser.Parse, nil
}

func (d *QueryDecoder[Resource, Request]) checkForPII(filterStr string) error {
	lexer := NewFilterLexer(filterStr)
	for {
		token, err := lexer.NextToken()
		if err != nil {
			return errors.Wrap(err, "failed to get next token")
		}

		if token.Type == TokenEOF {
			break
		}

		if token.Type == TokenCondition {
			jsonFieldNameStr := strings.SplitN(token.Value, ":", 2)[0]
			if fieldInfo, found := d.filterParserFields[jsonFieldName(jsonFieldNameStr)]; found {
				if fieldInfo.PII {
					return httpio.NewBadRequestMessagef("cannot filter on sensitive field in URL: %s", jsonFieldNameStr)
				}
			}
		}
	}

	return nil
}

func newFilterParserFields[Resource Resourcer](reqType reflect.Type, resourceMetadata *Metadata[Resource]) (map[jsonFieldName]FilterFieldInfo, error) {
	fields := make(map[jsonFieldName]FilterFieldInfo)

	for structField := range reqType.Fields() {
		if structField.Tag.Get(indexTagKey) != trueStr && structField.Tag.Get(allowFilterTagKey) != trueStr {
			continue
		}

		goStructFieldName := structField.Name
		jsonTag := structField.Tag.Get(jsonTagKey)
		jsonFieldNameStr, _, _ := strings.Cut(jsonTag, ",")
		if jsonFieldNameStr == "" || jsonFieldNameStr == "-" {
			return nil, errors.Newf("indexed field %s must have a json tag", goStructFieldName)
		}

		dbColumnNames := make(map[DBType]string)
		for _, dbType := range dbTypes() {
			cacheEntry, found := resourceMetadata.dbFieldMap(dbType)[accesstypes.Field(goStructFieldName)]
			if !found {
				continue
			}

			dbColumnNames[dbType] = cacheEntry.ColumnName
		}

		fieldType := structField.Type
		fieldKind := fieldType.Kind()
		if fieldKind == reflect.Pointer {
			fieldType = fieldType.Elem()
			fieldKind = fieldType.Kind()
		}

		fields[jsonFieldName(jsonFieldNameStr)] = FilterFieldInfo{
			JSONFieldName: jsonFieldNameStr,
			GOFieldName:   goStructFieldName,
			dbColumnNames: dbColumnNames,
			Kind:          fieldKind,
			FieldType:     fieldType,
			Indexed:       structField.Tag.Get(indexTagKey) == trueStr,
			PII:           structField.Tag.Get(piiTagKey) == trueStr,
		}
	}

	return fields, nil
}
