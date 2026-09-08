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
	// FilterFields names the resource fields the filter expression touches, so
	// the read checks can require an unconditional grant on each of them.
	FilterFields []accesstypes.Field
	Page         pageRequest
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

	// requestType is the request struct, consulted for the type behind a sort
	// field: a nested object or a list cannot be ordered by.
	requestType reflect.Type
	// keyFields are the request type's primary-key fields (the perm:"-"
	// markers), appended to every decoded order as the tiebreak.
	keyFields []accesstypes.Field
	// paging is the resource's declared paging contract (WithPaging); the zero
	// value is the generator-wide default.
	paging Paging
	// cursorKey seals and opens the cursors of the pages this decoder builds
	// (WithCursorKey); nil refuses paging past the first page.
	cursorKey *CursorKey
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
		requestType:        reflect.TypeOf(req),
		keyFields:          primaryKeyFields(reflect.TypeOf(req)),
	}, nil
}

// primaryKeyFields returns the request type's primary-key fields, the ones the
// generator marks perm:"-", in declaration order.
func primaryKeyFields(reqType reflect.Type) []accesstypes.Field {
	var keys []accesstypes.Field
	for field := range reqType.Fields() {
		if field.Tag.Get(permTagKey) == permTagExempt && field.Tag.Get(jsonTagKey) != "-" {
			keys = append(keys, accesstypes.Field(field.Name))
		}
	}

	return keys
}

// WithCursorKey installs the key that seals the cursors of the pages this
// decoder builds and opens the ones requests carry. The generated wiring passes
// the application's one key (resource.NewCursorKey over the cookie key) to
// every decoder; without it a list serves first pages only.
func (d *QueryDecoder[Resource, Request]) WithCursorKey(key *CursorKey) *QueryDecoder[Resource, Request] {
	d.cursorKey = key

	return d
}

// WithPaging installs the resource's declared paging contract: the default order
// a sort-less request takes and the page sizes. The generator emits the call from
// the @order and @page annotations. An order field the request type does not
// carry is a programming error and panics at construction, like every other
// generated-code mismatch.
func (d *QueryDecoder[Resource, Request]) WithPaging(paging Paging) *QueryDecoder[Resource, Request] {
	for _, sf := range paging.Order {
		field, ok := d.requestType.FieldByName(sf.Field)
		if !ok || !slices.Contains(d.requestFieldMapper.Fields(), accesstypes.Field(sf.Field)) {
			panic(fmt.Sprintf("resource.QueryDecoder.WithPaging: order field %q is not a field of the request type", sf.Field))
		}
		if !sortableType(field.Type) {
			panic(fmt.Sprintf("resource.QueryDecoder.WithPaging: order field %q has type %s, which cannot be ordered by", sf.Field, field.Type))
		}
	}
	d.paging = paging

	return d
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

	// parseQuery consumes the parameters it recognizes, so the filter's text is
	// kept first: every cursor the page issues is fingerprinted with it.
	filterString := queryParams.Get(filterParam)

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
	qSet.filterFields = parsedQuery.FilterFields
	qSet.keyFields = d.keyFields
	qSet.defaultOrder = d.paging.Order
	qSet.cursorKey = d.cursorKey
	qSet.filterString = filterString
	qSet.SetSortFields(parsedQuery.SortFields)
	qSet.page = &parsedQuery.Page
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

	// The cursor is bound here and not in DecodeWithoutPermissions because its
	// fingerprint covers the scope: a cursor from one tenant's walk is refused
	// in another's.
	if err := qSet.bindCursor(scope); err != nil {
		return nil, err
	}

	return qSet, nil
}

func (d *QueryDecoder[Resource, Request]) parseQuery(query url.Values) (*parsedQueryParams, error) {
	var columnFields []accesstypes.Field
	var sortFields []SortField
	var filterParser func(DBType) (ExpressionNode, error)
	var filterFields []accesstypes.Field
	var err error

	if sortParamValue := query.Get(sortParam); sortParamValue != "" {
		sortFields, err = d.parseSortParam(sortParamValue)
		if err != nil {
			return nil, err
		}

		delete(query, sortParam)
	}

	page, err := d.parsePage(query)
	if err != nil {
		return nil, err
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
		filterFields, err = d.filterFields(filterStr)
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
		FilterFields: filterFields,
		Page:         page,
		Capabilities: capabilities,
	}, nil
}

// parsePage reads the paging parameters against the resource's declared
// contract. A request without limit takes the declared default page (the
// generator-wide DefaultPageSize when none is declared); limit=all is admitted
// only on a resource with no declared maximum; a limit over the maximum is
// refused naming it, never clamped; limit=0 is refused; offset is refused
// naming the cursor as its replacement; count is admitted on a first page only.
func (d *QueryDecoder[Resource, Request]) parsePage(query url.Values) (pageRequest, error) {
	page := pageRequest{size: d.paging.DefaultLimit}
	if page.size == 0 {
		page.size = DefaultPageSize
	}

	if limitStr := query.Get(limitParam); limitStr != "" {
		switch limitStr {
		case allLimit:
			if d.paging.MaxLimit != 0 {
				return pageRequest{}, httpio.NewBadRequestMessagef("limit=all is not permitted: this resource serves at most %d rows per page; follow the Link header", d.paging.MaxLimit)
			}
			page.all = true
		default:
			size, err := strconv.ParseUint(limitStr, 10, 64)
			if err != nil {
				return pageRequest{}, httpio.NewBadRequestMessagef("invalid limit value: %s", limitStr)
			}
			if size == 0 {
				return pageRequest{}, httpio.NewBadRequestMessage("limit must be at least 1; omit it for the default page, or ask for limit=all where the resource permits it")
			}
			if d.paging.MaxLimit != 0 && size > d.paging.MaxLimit {
				return pageRequest{}, httpio.NewBadRequestMessagef("limit %d exceeds this resource's maximum page size of %d", size, d.paging.MaxLimit)
			}
			page.size = size
		}
		delete(query, limitParam)
	}

	if query.Has(offsetParam) {
		return pageRequest{}, httpio.NewBadRequestMessage("offset is not supported: pages are positioned by the cursor the Link header carries")
	}

	if token := query.Get(cursorParam); token != "" {
		if page.all {
			return pageRequest{}, httpio.NewBadRequestMessage("a cursor cannot be combined with limit=all")
		}
		page.token = token
		delete(query, cursorParam)
	}

	if countStr := query.Get(countParam); countStr != "" {
		if countStr != trueStr {
			return pageRequest{}, httpio.NewBadRequestMessagef("invalid count value: %s (only true is accepted)", countStr)
		}
		if page.token != "" {
			return pageRequest{}, httpio.NewBadRequestMessage("count is answered on the first page only; it cannot be combined with a cursor")
		}
		page.count = true
		delete(query, countParam)
	}

	return page, nil
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
			if field, ok := d.requestType.FieldByName(string(goFieldName)); ok && !sortableType(field.Type) {
				return nil, httpio.NewBadRequestMessagef("field %s cannot be sorted by: only text, number, boolean, time, date, decimal, and UUID fields order", jsonFieldName)
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
	for _, fieldInfo := range d.filterConditionFields(filterStr) {
		if fieldInfo.PII {
			return httpio.NewBadRequestMessagef("cannot filter on sensitive field in URL: %s", fieldInfo.JSONFieldName)
		}
	}

	return nil
}

// filterFields names the resource fields a filter expression touches, in first
// appearance order without repeats. The parser has already validated the
// expression, so an unknown field cannot reach here.
func (d *QueryDecoder[Resource, Request]) filterFields(filterStr string) ([]accesstypes.Field, error) {
	var fields []accesstypes.Field
	for _, fieldInfo := range d.filterConditionFields(filterStr) {
		field := accesstypes.Field(fieldInfo.GOFieldName)
		if !slices.Contains(fields, field) {
			fields = append(fields, field)
		}
	}

	return fields, nil
}

// filterConditionFields walks the filter's tokens and returns the filterable
// field behind each condition; conditions on fields the parser would refuse
// are skipped, since the parser's own error is the one the caller sees.
func (d *QueryDecoder[Resource, Request]) filterConditionFields(filterStr string) []FilterFieldInfo {
	var infos []FilterFieldInfo
	lexer := NewFilterLexer(filterStr)
	for {
		token, err := lexer.NextToken()
		if err != nil || token.Type == TokenEOF {
			return infos
		}

		if token.Type == TokenCondition {
			jsonFieldNameStr, _, _ := strings.Cut(token.Value, ":")
			if fieldInfo, found := d.filterParserFields[jsonFieldName(jsonFieldNameStr)]; found {
				infos = append(infos, fieldInfo)
			}
		}
	}
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
