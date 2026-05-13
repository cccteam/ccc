package resource

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/cloudspannerecosystem/memefish"
	"github.com/cloudspannerecosystem/memefish/token"
	"github.com/go-playground/errors/v5"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// QuerySet represents a query for a resource, including fields, keys, filters, and permissions.
type QuerySet[Resource Resourcer] struct {
	keys                   *fieldSet
	fields                 []accesstypes.Field
	sortFields             []SortField
	limit                  *uint64
	offset                 *uint64
	pageSize               *uint64
	pageToken              *PageToken
	primaryKeyFields       []accesstypes.Field
	returnAccessibleFields bool
	rMeta                  *Metadata[Resource]
	resourceSet            *Set[Resource]
	userPermissions        UserPermissions
	requiredPermission     accesstypes.Permission
	filterAst              ExpressionNode
	filterParser           func(DBType) (ExpressionNode, error)
}

// NewQuerySet creates a new, empty QuerySet for a given resource metadata.
func NewQuerySet[Resource Resourcer](rMeta *Metadata[Resource]) *QuerySet[Resource] {
	return &QuerySet[Resource]{
		keys:  newFieldSet(),
		rMeta: rMeta,
	}
}

// Resource returns the name of the resource this QuerySet applies to.
func (q *QuerySet[Resource]) Resource() accesstypes.Resource {
	var r Resource

	return r.Resource()
}

func (q *QuerySet[Resource]) query() (withClause, query string, params map[string]any) {
	var r Resource

	switch t := any(r).(type) {
	case virtualQuerier:
		query, params = t.Subquery()

		withClause, query = extractWithClause(query)

		// newlines before final parenthesis is necessary to combat any trailing comments
		query = fmt.Sprintf("(%s\n) AS %s", query, r.Resource())

		for pramName := range params {
			if strings.HasPrefix(pramName, "_") {
				panic(fmt.Sprintf("Subquery params for %s can not start with an _", r.Resource()))
			}
		}

		return withClause, query, params
	default:
		return "", string(r.Resource()), nil
	}
}

// RequiredPermission returns the permission required to execute the query.
func (q *QuerySet[Resource]) RequiredPermission() accesstypes.Permission {
	return q.requiredPermission
}

// ReturnAccessibleFields configures the QuerySet to automatically include all fields
// the user has access to if no specific fields are requested.
func (q *QuerySet[Resource]) ReturnAccessibleFields(b bool) *QuerySet[Resource] {
	q.returnAccessibleFields = b

	return q
}

// EnableUserPermissionEnforcement enables the checking of user permissions for the QuerySet.
func (q *QuerySet[Resource]) EnableUserPermissionEnforcement(rSet *Set[Resource], userPermissions UserPermissions, requiredPermission accesstypes.Permission) *QuerySet[Resource] {
	q.resourceSet = rSet
	q.userPermissions = userPermissions
	q.requiredPermission = requiredPermission

	return q
}

func (q *QuerySet[Resource]) checkPermissions(ctx context.Context, dbType DBType) error {
	if q.resourceSet != nil {
		if ok, missing, err := q.userPermissions.Check(ctx, q.requiredPermission, q.resourceSet.BaseResource()); err != nil {
			return errors.Wrap(err, "enforcer.RequireResource()")
		} else if !ok {
			return httpio.NewForbiddenMessagef("domain (%s), user (%s) does not have (%s) on %s", q.userPermissions.Domain(), q.userPermissions.User(), q.requiredPermission, missing)
		}
	}

	fields := q.Fields()

	if len(fields) == 0 && q.returnAccessibleFields {
		return q.addAccessibleFields(ctx, dbType)
	}

	if q.resourceSet != nil {
		resources := make([]accesstypes.Resource, 0, len(fields)+1)

		for _, fieldName := range fields {
			if q.resourceSet.PermissionRequired(fieldName, q.requiredPermission) {
				resources = append(resources, q.resourceSet.Resource(fieldName))
			}
		}

		if ok, missing, err := q.userPermissions.Check(ctx, q.requiredPermission, resources...); err != nil {
			return errors.Wrap(err, "enforcer.RequireResource()")
		} else if !ok {
			return httpio.NewForbiddenMessagef("domain (%s), user (%s) does not have (%s) on %s", q.userPermissions.Domain(), q.userPermissions.User(), q.requiredPermission, missing)
		}
	}

	return nil
}

func (q *QuerySet[Resource]) addAccessibleFields(ctx context.Context, dbType DBType) error {
	fields := make([]accesstypes.Field, 0, q.rMeta.DBFieldCount(dbType))

	if q.resourceSet != nil {
		for _, field := range q.rMeta.DBFields(dbType) {
			if !q.resourceSet.PermissionRequired(field, q.RequiredPermission()) {
				fields = append(fields, field)
			} else {
				if ok, _, err := q.userPermissions.Check(ctx, q.requiredPermission, q.resourceSet.Resource(field)); err != nil {
					return errors.Wrap(err, "enforcer.RequireResource()")
				} else if ok {
					fields = append(fields, field)
				}
			}
		}
	} else {
		// If we don't have a resourceSet, just return all fields
		fields = q.rMeta.DBFields(dbType)
	}

	for _, field := range fields {
		q.AddField(field)
	}

	return nil
}

// AddField adds a field to be returned by the query.
func (q *QuerySet[Resource]) AddField(field accesstypes.Field) *QuerySet[Resource] {
	if !slices.Contains(q.fields, field) {
		q.fields = append(q.fields, field)
	}

	return q
}

// Fields returns the list of fields to be returned by the query.
func (q *QuerySet[Resource]) Fields() []accesstypes.Field {
	return q.fields
}

// SetKey sets a primary key field and value for the query's WHERE clause.
func (q *QuerySet[Resource]) SetKey(field accesstypes.Field, value any) {
	q.keys.Set(field, value)
}

// Key retrieves the value of a primary key field.
func (q *QuerySet[Resource]) Key(field accesstypes.Field) any {
	return q.keys.Get(field)
}

// Len returns the number of fields to be returned by the query.
func (q *QuerySet[Resource]) Len() int {
	return len(q.fields)
}

// KeySet returns the KeySet containing the primary key(s) for the resource.
func (q *QuerySet[Resource]) KeySet() KeySet {
	return q.keys.KeySet()
}

// Columns returns a comma-separated string of database column names for the selected fields.
func (q *QuerySet[Resource]) buildOrderByClause(dbType DBType) (string, error) {
	orderByParts := make([]string, 0, len(q.sortFields))
	for _, sf := range q.sortFields {
		dbField, ok := q.rMeta.dbFieldMap(dbType)[accesstypes.Field(sf.Field)]
		if !ok {
			return "", errors.Newf("sort field '%s' not found in resource metadata for query", sf.Field)
		}

		var quotedColumnName string
		switch dbType {
		case SpannerDBType:
			quotedColumnName = fmt.Sprintf("`%s`", dbField.ColumnName)
		case PostgresDBType:
			quotedColumnName = fmt.Sprintf(`"%s"`, dbField.ColumnName)
		default:
			return "", errors.Newf("unsupported dbType for sorting: %s", dbType)
		}

		directionSQL := "ASC"
		if sf.Direction == SortDescending {
			directionSQL = "DESC"
		}
		orderByParts = append(orderByParts, fmt.Sprintf("%s %s", quotedColumnName, directionSQL))
	}
	if len(orderByParts) == 0 {
		return "", nil
	}

	return "ORDER BY " + strings.Join(orderByParts, ", "), nil
}

// buildPKOrderByClause builds an ORDER BY clause for primary key fields
// that are not already present in the sort fields, used as tiebreakers for keyset pagination.
func (q *QuerySet[Resource]) buildPKOrderByClause(dbType DBType) (string, error) {
	sortFieldSet := make(map[string]struct{}, len(q.sortFields))
	for _, sf := range q.sortFields {
		sortFieldSet[sf.Field] = struct{}{}
	}

	var orderByParts []string
	for _, pkField := range q.primaryKeyFields {
		if _, exists := sortFieldSet[string(pkField)]; exists {
			continue
		}

		dbField, ok := q.rMeta.dbFieldMap(dbType)[pkField]
		if !ok {
			return "", errors.Newf("primary key field '%s' not found in resource metadata", pkField)
		}

		orderByParts = append(orderByParts, fmt.Sprintf("%s ASC", quoteColumnName(dbField.ColumnName, dbType)))
	}

	if len(orderByParts) == 0 {
		return "", nil
	}

	return "ORDER BY " + strings.Join(orderByParts, ", "), nil
}

// buildCursorWhereClause builds the keyset cursor WHERE clause from the page token.
func (q *QuerySet[Resource]) buildCursorWhereClause(dbType DBType) (string, map[string]any, error) {
	cfNames := cursorFieldNames(q.sortFields, q.primaryKeyFields)

	tokenMap := make(map[string]any, len(q.pageToken.Values))
	for _, tv := range q.pageToken.Values {
		tokenMap[tv.Field] = tv.Value
	}

	fields := make([]cursorField, 0, len(cfNames))
	for i, fieldName := range cfNames {
		dbField, ok := q.rMeta.dbFieldMap(dbType)[accesstypes.Field(fieldName)]
		if !ok {
			return "", nil, errors.Newf("cursor field '%s' not found in resource metadata", fieldName)
		}

		value, ok := tokenMap[fieldName]
		if !ok {
			return "", nil, httpio.NewBadRequestMessagef("page token missing value for cursor field: %s", fieldName)
		}

		fields = append(fields, cursorField{
			ColumnName: dbField.ColumnName,
			ParamName:  fmt.Sprintf("_cursor_%d", i),
			Value:      value,
			Direction:  cursorFieldDirection(fieldName, q.sortFields),
		})
	}

	whereClause, params := buildKeysetWhereClause(fields, dbType)

	return whereClause, params, nil
}

// columns returns the database struct tags for the fields in databaseType that the user has access to view.
func (q *QuerySet[Resource]) columns(dbType DBType) (Columns, error) {
	dbFields := make([]dbFieldMetadata, 0, q.Len())
	for _, field := range q.Fields() {
		dbField, ok := q.rMeta.dbFieldMap(dbType)[field]
		if !ok {
			return "", errors.Newf("field %s not found in db struct", field)
		}

		dbFields = append(dbFields, dbField)
	}
	sort.Slice(dbFields, func(i, j int) bool {
		return dbFields[i].index < dbFields[j].index
	})

	columns := make([]string, 0, len(dbFields))
	for _, dbField := range dbFields {
		columns = append(columns, dbField.ColumnName)
	}

	switch dbType {
	case SpannerDBType:
		return Columns(strings.Join(columns, ", ")), nil
	case PostgresDBType:
		return Columns(fmt.Sprintf(`"%s"`, strings.Join(columns, `", "`))), nil
	default:
		return "", errors.Newf("unsupported dbType: %s", dbType)
	}
}

func (q *QuerySet[Resource]) astWhereClause(dbType DBType, filterAst ExpressionNode) (*Statement, error) {
	switch dbType {
	case SpannerDBType:
		sql, params, err := NewSpannerGenerator().GenerateSQL(filterAst)
		if err != nil {
			return nil, errors.Wrap(err, "SpannerGenerator.GenerateSQL()")
		}

		return &Statement{SQL: "WHERE " + sql, Params: params}, nil
	case PostgresDBType:
		sql, params, err := NewPostgreSQLGenerator().GenerateSQL(filterAst)
		if err != nil {
			return nil, errors.Wrap(err, "PostgreSQLGenerator.GenerateSQL()")
		}

		return &Statement{SQL: "WHERE " + sql, Params: params}, nil
	default:
		return nil, errors.Newf("unsupported dbType: %s", dbType)
	}
}

// where translates the the fields to database struct tags in databaseType when building the where clause
func (q *QuerySet[Resource]) where(dbType DBType, filterAst ExpressionNode) (*Statement, error) {
	if filterAst != nil {
		return q.astWhereClause(dbType, filterAst)
	}

	parts := q.KeySet().Parts()
	if len(parts) == 0 {
		return &Statement{Params: map[string]any{}}, nil
	}

	builder := strings.Builder{}
	params := make(map[string]any, len(parts))
	for _, part := range parts {
		f, ok := q.rMeta.dbFieldMap(dbType)[part.Key]
		if !ok {
			return nil, errors.Newf("field %s not found in struct", part.Key)
		}
		switch dbType {
		case SpannerDBType:
			fmt.Fprintf(&builder, " AND `%s` = @_%s", f.ColumnName, strings.ToLower(f.ColumnName))
		case PostgresDBType:
			fmt.Fprintf(&builder, ` AND "%s" = @_%s`, f.ColumnName, strings.ToLower(f.ColumnName))
		default:
			return nil, errors.Newf("unsupported dbType: %s", dbType)
		}
		params["_"+strings.ToLower(f.ColumnName)] = part.Value
	}

	return &Statement{
		SQL:    "WHERE " + builder.String()[5:],
		Params: params,
	}, nil
}

// stmt builds a Spanner SQL statement from the QuerySet.
func (q *QuerySet[Resource]) stmt(dbType DBType) (*Statement, error) {
	filterAst, err := q.FilterAst(dbType)
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.FilterAst()")
	}

	if moreThan(1, q.KeySet().Len() != 0, filterAst != nil) {
		return nil, httpio.NewBadRequestMessage("cannot use multiple sources for WHERE clause together (e.g. QueryClause and KeySet)")
	}

	columns, err := q.columns(dbType)
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.Columns()")
	}

	where, err := q.where(dbType, filterAst)
	if err != nil {
		return nil, errors.Wrap(err, "patcher.Where()")
	}

	orderByClause, err := q.buildOrderByClause(dbType)
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.buildOrderByClause()")
	}

	// When pagination is active, ensure PK fields are in ORDER BY for deterministic ordering
	if q.pageSize != nil && len(q.primaryKeyFields) > 0 {
		pkOrderBy, err := q.buildPKOrderByClause(dbType)
		if err != nil {
			return nil, errors.Wrap(err, "QuerySet.buildPKOrderByClause()")
		}

		if orderByClause == "" {
			orderByClause = pkOrderBy
		} else if pkOrderBy != "" {
			orderByClause = orderByClause + ", " + strings.TrimPrefix(pkOrderBy, "ORDER BY ")
		}
	}

	// Apply keyset cursor conditions when a page token is present
	if q.pageToken != nil && len(q.pageToken.Values) > 0 {
		cursorWhere, cursorParams, err := q.buildCursorWhereClause(dbType)
		if err != nil {
			return nil, errors.Wrap(err, "QuerySet.buildCursorWhereClause()")
		}

		for k, v := range cursorParams {
			if _, exists := where.Params[k]; exists {
				return nil, errors.Newf("named parameter collision: cursor parameter %q conflicts with existing parameter", k)
			}

			where.Params[k] = v
		}

		if where.SQL == "" {
			where.SQL = "WHERE (" + cursorWhere + ")"
		} else {
			where.SQL = where.SQL + " AND (" + cursorWhere + ")"
		}
	}

	var limitClause string
	if q.pageSize != nil {
		// Fetch one extra row to detect if there's a next page
		limitClause = fmt.Sprintf("LIMIT %d", *q.pageSize+1)
	} else if q.limit != nil {
		limitClause = fmt.Sprintf("LIMIT %d", *q.limit)
	}

	var offsetClause string
	if q.offset != nil {
		offsetClause = fmt.Sprintf("OFFSET %d", *q.offset)
	}

	withClause, query, subqueryParams := q.query()
	for k := range subqueryParams {
		if _, ok := where.Params[k]; ok {
			return nil, errors.Newf("named parameter collision: %s subquery and where clause both contain named parameter %q", q.Resource(), k)
		}

		where.Params[k] = subqueryParams[k]
	}

	sql := fmt.Sprintf(`
			%s
			SELECT
				%s
			FROM %s
			%s
			%s
			%s
			%s`, withClause, columns, query, where.SQL, orderByClause, limitClause, offsetClause,
	)

	resolvedSQL, err := substituteSQLParams(where.SQL, where.Params)
	if err != nil {
		return nil, errors.Wrap(err, "failed to substitute SQL params for resolvedWhereClause")
	}

	return &Statement{resolvedWhereClause: resolvedSQL, SQL: sql, Params: where.Params}, nil
}

// Read executes the query and returns a single result.
func (q *QuerySet[Resource]) Read(ctx context.Context, txn ReadOnlyTransaction) (*Resource, error) {
	r := newReader[Resource](txn)
	if err := q.checkPermissions(ctx, r.DBType()); err != nil {
		return nil, err
	}

	stmt, err := q.stmt(r.DBType())
	if err != nil {
		return nil, errors.Wrap(err, "patcher.Stmt()")
	}

	dst, err := r.Read(ctx, stmt)
	if err != nil {
		return nil, errors.Wrapf(err, "Reader[%s].Read()", q.Resource())
	}

	return dst, nil
}

// List executes the query and returns an iterator for the results.
func (q *QuerySet[Resource]) List(ctx context.Context, txn ReadOnlyTransaction) iter.Seq2[*Resource, error] {
	return func(yield func(*Resource, error) bool) {
		r := newReader[Resource](txn)
		if err := q.checkPermissions(ctx, r.DBType()); err != nil {
			yield(nil, err)

			return
		}

		stmt, err := q.stmt(r.DBType())
		if err != nil {
			yield(nil, errors.Wrap(err, "patcher.Stmt()"))

			return
		}

		for r, err := range r.List(ctx, stmt) {
			if !yield(r, err) {
				return
			}
		}
	}
}

// BatchList executes the query and returns an iterator for the results in batches.
func (q *QuerySet[Resource]) BatchList(ctx context.Context, client Client, size int) iter.Seq[iter.Seq2[*Resource, error]] {
	return ccc.BatchIter2(q.List(ctx, client), size)
}

// ListPage executes the query and returns a page of results with an optional next page token.
// When pagination is active (pageSize is set), it fetches pageSize+1 rows to detect if
// there are more pages. If not paginating, it returns all results with an empty token.
// extractCursorValues extracts cursor field values from a row by Go struct field name.
func (q *QuerySet[Resource]) ListPage(ctx context.Context, txn ReadOnlyTransaction, extractCursorValues func(*Resource, []string) map[string]any) ([]*Resource, string, error) {
	var results []*Resource
	for row, err := range q.List(ctx, txn) {
		if err != nil {
			return nil, "", err
		}

		results = append(results, row)
	}

	if q.pageSize == nil {
		return results, "", nil
	}

	pageSize := *q.pageSize
	if uint64(len(results)) <= pageSize {
		return results, "", nil
	}

	// Trim the extra row used for next-page detection
	results = results[:pageSize]

	// Build next page token from the last row
	lastRow := results[len(results)-1]
	cfNames := cursorFieldNames(q.sortFields, q.primaryKeyFields)
	token := buildPageToken(extractCursorValues(lastRow, cfNames), cfNames)

	nextPageToken, err := EncodePageToken(token)
	if err != nil {
		return nil, "", errors.Wrap(err, "EncodePageToken()")
	}

	return results, nextPageToken, nil
}

// SetWhereClause sets the filter condition for the query using a QueryClause.
func (q *QuerySet[Resource]) SetWhereClause(qc QueryClause) {
	q.filterAst = qc.tree
}

// SetFilterAst sets the filter condition for the query using a raw expression tree.
func (q *QuerySet[Resource]) SetFilterAst(ast ExpressionNode) {
	q.filterAst = ast
}

// FilterAst returns the filter AST for the query.
func (q *QuerySet[Resource]) FilterAst(dbType DBType) (ExpressionNode, error) {
	if q.filterAst == nil && q.filterParser != nil {
		filterAst, err := q.filterParser(dbType)
		if err != nil {
			return nil, errors.Wrap(err, "filterParser()")
		}

		return filterAst, nil
	}

	return q.filterAst, nil
}

// SetFilterParser sets the filter parser.
func (q *QuerySet[Resource]) SetFilterParser(parser func(DBType) (ExpressionNode, error)) {
	q.filterParser = parser
}

// SetSortFields sets the sorting order for the query results.
func (q *QuerySet[Resource]) SetSortFields(sortFields []SortField) {
	q.sortFields = sortFields
}

// SetLimit sets the maximum number of results to return.
func (q *QuerySet[Resource]) SetLimit(limit *uint64) {
	q.limit = limit
}

// SetOffset sets the starting point for returning results.
func (q *QuerySet[Resource]) SetOffset(offset *uint64) {
	q.offset = offset
}

// SetPageSize sets the page size for keyset pagination.
func (q *QuerySet[Resource]) SetPageSize(pageSize *uint64) {
	q.pageSize = pageSize
}

// PageSize returns the page size, or nil if pagination is not active.
func (q *QuerySet[Resource]) PageSize() *uint64 {
	return q.pageSize
}

// SetPageToken sets the page token for keyset pagination.
func (q *QuerySet[Resource]) SetPageToken(pageToken *PageToken) {
	q.pageToken = pageToken
}

// SetPrimaryKeyFields sets the primary key fields used for keyset pagination tiebreaking.
func (q *QuerySet[Resource]) SetPrimaryKeyFields(fields ...accesstypes.Field) {
	q.primaryKeyFields = fields
}

// SortFields returns the sort fields for the query.
func (q *QuerySet[Resource]) SortFields() []SortField {
	return q.sortFields
}

// PrimaryKeyFields returns the primary key fields used for pagination.
func (q *QuerySet[Resource]) PrimaryKeyFields() []accesstypes.Field {
	return q.primaryKeyFields
}

func extractWithClause(query string) (withClause, remainingQuery string) {
	lex := &memefish.Lexer{
		File: &token.File{Buffer: query},
	}

	depth := 0
	lastClosingParenEnd := -1
	startedWith := false

	for {
		if err := lex.NextToken(); err != nil {
			break
		}

		if lex.Token.Kind == token.TokenEOF {
			break
		}

		// First meaningful token must be WITH
		if !startedWith && depth == 0 && lastClosingParenEnd == -1 {
			if strings.EqualFold(lex.Token.Raw, "WITH") {
				startedWith = true
			} else {
				return "", query
			}
		}

		switch lex.Token.Kind {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				lastClosingParenEnd = int(lex.Token.End)
			}
		default:
			if depth == 0 {
				raw := lex.Token.Raw
				if strings.EqualFold(raw, "SELECT") || strings.EqualFold(raw, "UPDATE") || strings.EqualFold(raw, "DELETE") || strings.EqualFold(raw, "INSERT") {
					if startedWith && lastClosingParenEnd != -1 {
						return query[:lastClosingParenEnd], query[lastClosingParenEnd:]
					}
				}
			}
		}
	}

	return "", query
}

// moreThan checks if more than a given count of boolean expressions are true.
func moreThan(cnt int, exp ...bool) bool {
	count := 0
	for _, v := range exp {
		if v {
			count++
		}
	}

	return count > cnt
}

var _ QuerySetComparer = (*QuerySet[nilResource])(nil)

// QuerySetComparer is an interface for comparing two QuerySet-like objects.
type QuerySetComparer interface {
	Resource() accesstypes.Resource
	Fields() []accesstypes.Field
	KeySet() KeySet
}

// QuerySetDiff compares two QuerySetComparer objects for equality. It checks patch type, data, fields, and primary keys.
func QuerySetDiff(opts ...cmp.Option) func(a, b QuerySetComparer) string {
	return func(a, b QuerySetComparer) string {
		if a.Resource() != b.Resource() {
			return fmt.Sprintf("Resource() mismatch (-want +got):\n- %s\n+ %s", a.Resource(), b.Resource())
		}

		if diff := cmp.Diff(a.Fields(), b.Fields(), cmpopts.SortSlices(func(x, y accesstypes.Field) bool { return x < y })); diff != "" {
			return fmt.Sprintf("Fileds mismatch (-want +got):\n%s", diff)
		}

		aKeyData, bKeyData := a.KeySet().KeyMap(), b.KeySet().KeyMap()
		if diff := cmp.Diff(
			slices.Collect(maps.Keys(aKeyData)),
			slices.Collect(maps.Keys(bKeyData)),
			cmpopts.SortSlices(func(x, y accesstypes.Field) bool { return x < y })); diff != "" {
			return fmt.Sprintf("Query Fields mismatch (-want +got):\n%s", diff)
		}

		for k, v := range aKeyData {
			if diff := cmp.Diff(v, bKeyData[k], opts...); diff != "" {
				return fmt.Sprintf("Query Value mismatch for field %s, (-want +got):\n%s", k, diff)
			}
		}

		return ""
	}
}
