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
	"github.com/cccteam/spxscan/spxapi"
	"github.com/go-playground/errors/v5"
)

type QuerySet[Resource Resourcer] struct {
	keys                   *fieldSet
	filter                 *Filter
	fields                 []accesstypes.Field
	whereClause            whereClauseExprTree
	returnAccessableFields bool
	rMeta                  *ResourceMetadata[Resource]
	resourceSet            *ResourceSet[Resource]
	userPermissions        UserPermissions
	requiredPermission     accesstypes.Permission
}

func NewQuerySet[Resource Resourcer](rMeta *ResourceMetadata[Resource]) *QuerySet[Resource] {
	return &QuerySet[Resource]{
		keys:  newFieldSet(),
		rMeta: rMeta,
	}
}

func (q *QuerySet[Resource]) Resource() accesstypes.Resource {
	var r Resource

	return r.Resource()
}

func (q *QuerySet[Resource]) RequiredPermission() accesstypes.Permission {
	return q.requiredPermission
}

func (q *QuerySet[Resource]) ReturnAccessableFields(b bool) *QuerySet[Resource] {
	q.returnAccessableFields = b

	return q
}

func (q *QuerySet[Resource]) EnableUserPermissionEnforcement(rSet *ResourceSet[Resource], userPermissions UserPermissions, requiredPermission accesstypes.Permission) *QuerySet[Resource] {
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

func (q *QuerySet[Resource]) AddField(field accesstypes.Field) *QuerySet[Resource] {
	if !slices.Contains(q.fields, field) {
		q.fields = append(q.fields, field)
	}

	return q
}

func (q *QuerySet[Resource]) Fields() []accesstypes.Field {
	return q.fields
}

func (q *QuerySet[Resource]) SetKey(field accesstypes.Field, value any) {
	q.keys.Set(field, value)
}

func (q *QuerySet[Resource]) Key(field accesstypes.Field) any {
	return q.keys.Get(field)
}

func (q *QuerySet[Resource]) Len() int {
	return len(q.fields)
}

func (q *QuerySet[Resource]) KeySet() KeySet {
	return q.keys.KeySet()
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
		columns = append(columns, c.tag)
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

// Where translates the the fields to database struct tags in databaseType when building the where clause
func (q *QuerySet[Resource]) Where() (*Statement, error) {
	if q.whereClause != nil {
		return q.queryWhereClause()
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
		key := c.tag
		switch q.rMeta.dbType {
		case SpannerDBType:
			builder.WriteString(fmt.Sprintf(" AND %s = @%s", key, strings.ToLower(key)))
		case PostgresDBType:
			builder.WriteString(fmt.Sprintf(` AND "%s" = @%s`, key, strings.ToLower(key)))
		default:
			return nil, errors.Newf("unsupported dbType: %s", q.rMeta.dbType)
		}
		params[strings.ToLower(key)] = part.Value
	}

	return &Statement{
		Sql:    "WHERE " + builder.String()[5:],
		Params: params,
	}, nil
}

func (q *QuerySet[Resource]) queryWhereClause() (*Statement, error) {
	tw := newTreeWalker()
	sql := tw.walk(q.whereClause)

	return &Statement{Sql: "WHERE " + sql, Params: tw.params}, nil
}

func (q *QuerySet[Resource]) SpannerStmt() (*StatementWrapper, error) {
	if q.rMeta.dbType != SpannerDBType {
		return nil, errors.Newf("can only use SpannerStmt() with dbType %s, got %s", SpannerDBType, q.rMeta.dbType)
	}

	if moreThan(1, q.whereClause != nil, q.KeySet().Len() != 0, q.filter != nil) {
		panic("cannot use QueryClause, KeySet, or Filter together")
	}

	if q.filter != nil {
		return q.spannerFilterStmt()
	}

	return q.spannerIndexStmt()
}

// TODO(bswaney): collapse this into the spanner filter stmt so that we can use the general case
func (q *QuerySet[Resource]) spannerIndexStmt() (*StatementWrapper, error) {
	columns, err := q.Columns()
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.Columns()")
	}

	where, err := q.Where()
	if err != nil {
		return nil, errors.Wrap(err, "patcher.Where()")
	}

	stmt := spanner.NewStatement(fmt.Sprintf(`
			SELECT
				%s
			FROM %s
			%s`, columns, q.Resource(), where.Sql,
	))
	maps.Insert(stmt.Params, maps.All(where.Params))

	return &StatementWrapper{resolvedWhereClause: substituteSQLParams(where.Sql, where.Params), Statement: stmt}, nil
}

func (q *QuerySet[Resource]) spannerFilterStmt() (*StatementWrapper, error) {
	columns, err := q.Columns()
	if err != nil {
		return nil, errors.Wrap(err, "QuerySet.Columns()")
	}

	filter, err := q.filter.SpannerStmt()
	if err != nil {
		return nil, err
	}

	stmt := spanner.NewStatement(fmt.Sprintf(`
			SELECT
				%s
			FROM %s
			%s`,
		columns, q.Resource(), filter.Sql))

	stmt.Params = filter.Params

	return &StatementWrapper{resolvedWhereClause: substituteSQLParams(filter.Sql, filter.Params), Statement: stmt}, nil
}

func (q *QuerySet[Resource]) PostgresStmt() (Statement, error) {
	if q.rMeta.dbType != PostgresDBType {
		return Statement{}, errors.Newf("can only use PostgresStmt() with dbType %s, got %s", PostgresDBType, q.rMeta.dbType)
	}

	columns, err := q.Columns()
	if err != nil {
		return Statement{}, errors.Wrap(err, "QuerySet.Columns()")
	}

	where, err := q.Where()
	if err != nil {
		return Statement{}, errors.Wrap(err, "patcher.Where()")
	}

	sql := fmt.Sprintf(`
			SELECT
				%s
			FROM %s
			%s`, columns, q.Resource(), where.Sql,
	)

	return Statement{
		Sql:    sql,
		Params: where.Params,
	}, nil
}

func (q *QuerySet[Resource]) SpannerRead(ctx context.Context, db spxapi.Querier) (*Resource, error) {
	if err := q.checkPermissions(ctx); err != nil {
		return nil, err
	}

	stmt, err := q.SpannerStmt()
	if err != nil {
		return nil, errors.Wrap(err, "patcher.Stmt()")
	}

	dst := new(Resource)
	if err := spxscan.Get(ctx, db, dst, stmt.Statement); err != nil {
		if errors.Is(err, spxscan.ErrNotFound) {
			return nil, httpio.NewNotFoundMessagef("%s (%s) not found", q.Resource(), stmt.resolvedWhereClause)
		}

		return nil, errors.Wrap(err, "spxscan.Get()")
	}

	return dst, nil
}

func (q *QuerySet[Resource]) SpannerList(ctx context.Context, db spxapi.Querier) iter.Seq2[*Resource, error] {
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

		for r, err := range spxscan.SelectSeq[Resource](ctx, db, stmt.Statement) {
			if !yield(r, err) {
				return
			}
		}
	}
}

func (q *QuerySet[Resource]) SetFilterParam(filterSet *Filter) {
	q.filter = filterSet
}

func (q *QuerySet[Resource]) SetWhereClause(qc QueryClause) {
	q.whereClause = qc.tree
}

func moreThan(cnt int, exp ...bool) bool {
	count := 0
	for _, v := range exp {
		if v {
			count++
		}
	}

	return count > cnt
}
