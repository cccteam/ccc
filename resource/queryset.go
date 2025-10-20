package resource

import (
	"context"
	"fmt"
	"iter"
	"maps"
	"slices"
	"sort"
	"strings"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/httpio"
	"github.com/cccteam/spxscan"
	"github.com/go-playground/errors/v5"
)

// QuerySet represents a query for a resource, including fields, keys, filters, and permissions.
type QuerySet[Resource Resourcer] struct {
	keys                   *fieldSet
	search                 *Search
	fields                 []accesstypes.Field
	sortFields             []SortField
	limit                  *uint64
	offset                 *uint64
	returnAccessableFields bool
	rMeta                  *Metadata[Resource]
	resourceSet            *Set[Resource]
	userPermissions        UserPermissions
	requiredPermission     accesstypes.Permission
	filterAst              ExpressionNode
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

// RequiredPermission returns the permission required to execute the query.
func (q *QuerySet[Resource]) RequiredPermission() accesstypes.Permission {
	return q.requiredPermission
}

// ReturnAccessableFields configures the QuerySet to automatically include all fields
// the user has access to if no specific fields are requested.
func (q *QuerySet[Resource]) ReturnAccessableFields(b bool) *QuerySet[Resource] {
	q.returnAccessableFields = b

	return q
}

// EnableUserPermissionEnforcement enables the checking of user permissions for the QuerySet.
func (q *QuerySet[Resource]) EnableUserPermissionEnforcement(rSet *Set[Resource], userPermissions UserPermissions, requiredPermission accesstypes.Permission) *QuerySet[Resource] {
	q.resourceSet = rSet
	q.userPermissions = userPermissions
	q.requiredPermission = requiredPermission

	return q
}

func (q *QuerySet[Resource]) checkPermissions(ctx context.Context) error {
	fields := q.Fields()

	if len(fields) == 0 && q.returnAccessableFields {
		return q.addAccessableFields(ctx)
	}

	if q.resourceSet != nil {
		resources := make([]accesstypes.Resource, 0, len(fields)+1)
		resources = append(resources, q.resourceSet.BaseResource())

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

func (q *QuerySet[Resource]) addAccessableFields(ctx context.Context) error {
	fields := make([]accesstypes.Field, 0, q.rMeta.Len())

	if q.resourceSet != nil {
		for _, field := range q.rMeta.Fields() {
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
		fields = q.rMeta.Fields()
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
func (q *QuerySet[Resource]) buildOrderByClause() (string, error) {
	orderByParts := make([]string, 0, len(q.sortFields))
	for _, sf := range q.sortFields {
		cacheEntry, ok := q.rMeta.fieldMap[accesstypes.Field(sf.Field)]
		if !ok {
			return "", errors.Newf("sort field '%s' not found in resource metadata for query", sf.Field)
		}
		dbColumnName := cacheEntry.dbColumnName

		var quotedColumnName string
		switch q.rMeta.dbType {
		case SpannerDBType:
			quotedColumnName = fmt.Sprintf("`%s`", dbColumnName)
		case PostgresDBType:
			quotedColumnName = fmt.Sprintf(`"%s"`, dbColumnName)
		default:
			return "", errors.Newf("unsupported dbType for sorting: %s", q.rMeta.dbType)
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

// Columns returns the database struct tags for the fields in databaseType that the user has access to view.
func (q *QuerySet[Resource]) Columns() (Columns, error) {
	columnEntries := make([]cacheEntry, 0, q.Len())
	for _, field := range q.Fields() {
		c, ok := q.rMeta.fieldMap[field]
		if !ok {
			return "", errors.Newf("field %s not found in struct", field)
		}

		columnEntries = append(columnEntries, c)
	}
	sort.Slice(columnEntries, func(i, j int) bool {
		return columnEntries[i].index < columnEntries[j].index
	})

	columns := make([]string, 0, len(columnEntries))
	for _, c := range columnEntries {
		columns = append(columns, c.dbColumnName)
	}

	switch q.rMeta.dbType {
	case SpannerDBType:
		return Columns(strings.Join(columns, ", ")), nil
	case PostgresDBType:
		return Columns(fmt.Sprintf(`"%s"`, strings.Join(columns, `", "`))), nil
	default:
		return "", errors.Newf("unsupported dbType: %s", q.rMeta.dbType)
	}
}

func (q *QuerySet[Resource]) astWhereClause() (*Statement, error) {
	switch q.rMeta.dbType {
	case SpannerDBType:
		sql, params, err := NewSpannerGenerator().GenerateSQL(q.filterAst)
		if err != nil {
			return nil, errors.Wrap(err, "SpannerGenerator.GenerateSQL()")
		}

		return &Statement{SQL: "WHERE " + sql, SpannerParams: params}, nil
	case PostgresDBType:
		sql, params, err := NewPostgreSQLGenerator().GenerateSQL(q.filterAst)
		if err != nil {
			return nil, errors.Wrap(err, "PostgreSQLGenerator.GenerateSQL()")
		}

		return &Statement{SQL: "WHERE " + sql, PostgreSQLParams: params}, nil
	}

	return nil, errors.Newf("unsupported dbType: %s", q.rMeta.dbType)
}

// where translates the the fields to database struct tags in databaseType when building the where clause
func (q *QuerySet[Resource]) where() (*Statement, error) {
	if q.filterAst != nil {
		return q.astWhereClause()
	}

	parts := q.KeySet().Parts()
	if len(parts) == 0 {
		return &Statement{}, nil
	}

	builder := strings.Builder{}
	params := make(map[string]any, len(parts))
	for _, part := range parts {
		c, ok := q.rMeta.fieldMap[part.Key]
		if !ok {
			return nil, errors.Newf("field %s not found in struct", part.Key)
		}
		key := c.dbColumnName
		switch q.rMeta.dbType {
		case SpannerDBType:
			builder.WriteString(fmt.Sprintf(" AND `%s` = @%s", key, strings.ToLower(key)))
		case PostgresDBType:
			builder.WriteString(fmt.Sprintf(` AND "%s" = @%s`, key, strings.ToLower(key)))
		default:
			return nil, errors.Newf("unsupported dbType: %s", q.rMeta.dbType)
		}
		params[strings.ToLower(key)] = part.Value
	}

	return &Statement{
		SQL:           "WHERE " + builder.String()[5:],
		SpannerParams: params,
	}, nil
}

// SpannerStmt builds a Spanner SQL statement from the QuerySet.
func (q *QuerySet[Resource]) SpannerStmt() (*SpannerStatement, error) {
	if q.rMeta.dbType != SpannerDBType {
		return nil, errors.Newf("can only use SpannerStmt() with dbType %s, got %s", SpannerDBType, q.rMeta.dbType)
	}

	if moreThan(1, q.KeySet().Len() != 0, q.search != nil, q.filterAst != nil) {
		return nil, httpio.NewBadRequestMessage("cannot use multiple sources for WHERE clause together (e.g. QueryClause, KeySet, and Search)")
	}

	if q.search != nil {
		return q.spannerSearchStmt()
	}

	columns, err := q.Columns()
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.Columns()")
	}

	where, err := q.where()
	if err != nil {
		return nil, errors.Wrap(err, "patcher.Where()")
	}

	orderByClause, err := q.buildOrderByClause()
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.buildOrderByClause()")
	}

	var limitClause string
	if q.limit != nil {
		limitClause = fmt.Sprintf("LIMIT %d", *q.limit)
	}

	var offsetClause string
	if q.offset != nil {
		offsetClause = fmt.Sprintf("OFFSET %d", *q.offset)
	}

	stmt := spanner.NewStatement(fmt.Sprintf(`
			SELECT
				%s
			FROM %s
			%s
			%s
			%s
			%s`, columns, q.Resource(), where.SQL, orderByClause, limitClause, offsetClause,
	))
	maps.Insert(stmt.Params, maps.All(where.SpannerParams))

	resolvedSQL, err := substituteSQLParams(where.SQL, where.SpannerParams, Spanner)
	if err != nil {
		return nil, errors.Wrap(err, "failed to substitute SQL params for resolvedWhereClause")
	}

	return &SpannerStatement{resolvedWhereClause: resolvedSQL, Statement: stmt}, nil
}

func (q *QuerySet[Resource]) spannerSearchStmt() (*SpannerStatement, error) {
	columns, err := q.Columns()
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.Columns()")
	}

	search, err := q.search.spannerStmt()
	if err != nil {
		return nil, err
	}

	stmt := spanner.NewStatement(fmt.Sprintf(`
			SELECT
				%s
			FROM %s
			%s`,
		columns, q.Resource(), search.SQL))

	stmt.Params = search.SpannerParams

	resolvedSQL, err := substituteSQLParams(search.SQL, search.SpannerParams, Spanner)
	if err != nil {
		return nil, errors.Wrap(err, "failed to substitute SQL params for resolvedWhereClause")
	}

	return &SpannerStatement{resolvedWhereClause: resolvedSQL, Statement: stmt}, nil
}

// PostgresStmt builds a PostgreSQL SQL statement from the QuerySet.
func (q *QuerySet[Resource]) PostgresStmt() (*PostgresStatement, error) {
	if q.rMeta.dbType != PostgresDBType {
		return nil, errors.Newf("can only use PostgresStmt() with dbType %s, got %s", PostgresDBType, q.rMeta.dbType)
	}

	columns, err := q.Columns()
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.Columns()")
	}

	where, err := q.where()
	if err != nil {
		return nil, errors.Wrap(err, "patcher.Where()")
	}

	orderByClause, err := q.buildOrderByClause()
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.buildOrderByClause()")
	}

	var limitClause string
	if q.limit != nil {
		limitClause = fmt.Sprintf("LIMIT %d", *q.limit)
	}

	var offsetClause string
	if q.offset != nil {
		offsetClause = fmt.Sprintf("OFFSET %d", *q.offset)
	}

	sql := fmt.Sprintf(`
			SELECT
				%s
			FROM %s
			%s
			%s
			%s
			%s`, columns, q.Resource(), where.SQL, orderByClause, limitClause, offsetClause,
	)

	resolvedSQL, err := substituteSQLParams(where.SQL, where.PostgreSQLParams, PostgreSQL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to substitute SQL params for resolvedWhereClause")
	}

	return &PostgresStatement{
		resolvedWhereClause: resolvedSQL,
		SQL:                 sql,
		Params:              where.PostgreSQLParams,
	}, nil
}

// Read executes the query and returns a single result.
func (q *QuerySet[Resource]) Read(ctx context.Context, db ResourceReader) (*Resource, error) {
	if err := q.checkPermissions(ctx); err != nil {
		return nil, err
	}

	switch db.DBType() {
	case SpannerDBType:
		stmt, err := q.SpannerStmt()
		if err != nil {
			return nil, errors.Wrap(err, "patcher.Stmt()")
		}

		dst := new(Resource)
		if err := spxscan.Get(ctx, db.SpannerReadTransaction(), dst, stmt.Statement); err != nil {
			if errors.Is(err, spxscan.ErrNotFound) {
				return nil, httpio.NewNotFoundMessagef("%s (%s) not found", q.Resource(), stmt.resolvedWhereClause)
			}

			return nil, errors.Wrap(err, "spxscan.Get()")
		}

		return dst, nil
	case PostgresDBType:
		panic(fmt.Sprintf("operation not implemented for database type: %s", db.DBType()))
	default:
		panic(fmt.Sprintf("unsupported db type: %s", db.DBType()))
	}
}

// List executes the query and returns an iterator for the results.
func (q *QuerySet[Resource]) List(ctx context.Context, db ResourceReader) iter.Seq2[*Resource, error] {
	switch db.DBType() {
	case SpannerDBType:
		return func(yield func(*Resource, error) bool) {
			if err := q.checkPermissions(ctx); err != nil {
				yield(nil, err)

				return
			}

			stmt, err := q.SpannerStmt()
			if err != nil {
				yield(nil, errors.Wrap(err, "patcher.Stmt()"))

				return
			}

			for r, err := range spxscan.SelectSeq[Resource](ctx, db.SpannerReadTransaction(), stmt.Statement) {
				if !yield(r, err) {
					return
				}
			}
		}
	case PostgresDBType:
		panic(fmt.Sprintf("operation not implemented for database type: %s", db.DBType()))
	default:
		panic(fmt.Sprintf("unsupported db type: %s", db.DBType()))
	}
}

// SetSearchParam sets the search parameters for the query.
func (q *QuerySet[Resource]) SetSearchParam(search *Search) {
	q.search = search
}

// SetWhereClause sets the filter condition for the query using a QueryClause.
func (q *QuerySet[Resource]) SetWhereClause(qc QueryClause) {
	q.filterAst = qc.tree
}

// SetFilterAst sets the filter condition for the query using a raw expression tree.
func (q *QuerySet[Resource]) SetFilterAst(ast ExpressionNode) {
	q.filterAst = ast
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
