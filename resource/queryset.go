package resource

import (
	"context"
	"fmt"
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

type QuerySet[Resource Resourcer] struct {
	keys   *fieldSet
	filter *Filter
	fields []accesstypes.Field
	rMeta  *ResourceMetadata[Resource]
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

func (q *QuerySet[Resource]) SpannerStmt() (spanner.Statement, error) {
	if q.rMeta.dbType != SpannerDBType {
		return spanner.Statement{}, errors.Newf("can only use SpannerStmt() with dbType %s, got %s", SpannerDBType, q.rMeta.dbType)
	}

	if q.filter != nil {
		return q.spannerFilterStmt()
	}

	return q.spannerIndexStmt()
}

// TODO(bswaney): collapse this into the spanner filter stmt so that we can use the general case
func (q *QuerySet[Resource]) spannerIndexStmt() (spanner.Statement, error) {
	columns, err := q.Columns()
	if err != nil {
		return spanner.Statement{}, errors.Wrap(err, "QuerySet.Columns()")
	}

	where, err := q.Where()
	if err != nil {
		return spanner.Statement{}, errors.Wrap(err, "patcher.Where()")
	}

	stmt := spanner.NewStatement(fmt.Sprintf(`
			SELECT
				%s
			FROM %s 
			%s`, columns, q.Resource(), where.Sql,
	))
	maps.Insert(stmt.Params, maps.All(where.Params))

	return stmt, nil
}

func (q *QuerySet[Resource]) spannerFilterStmt() (spanner.Statement, error) {
	columns, err := q.Columns()
	if err != nil {
		return spanner.Statement{}, errors.Wrap(err, "QuerySet.Columns()")
	}

	filter, err := q.filter.SpannerStmt()
	if err != nil {
		return spanner.Statement{}, err
	}

	stmt := spanner.NewStatement(fmt.Sprintf(`
			SELECT
				%s
			FROM %s 
			%s`,
		columns, q.Resource(), filter.Sql))

	stmt.Params = filter.Params

	return stmt, nil
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

func (q *QuerySet[Resource]) SpannerRead(ctx context.Context, txn *spanner.ReadOnlyTransaction, dst any) error {
	stmt, err := q.SpannerStmt()
	if err != nil {
		return errors.Wrap(err, "patcher.Stmt()")
	}

	if err := spxscan.Get(ctx, txn, dst, stmt); err != nil {
		if errors.Is(err, spxscan.ErrNotFound) {
			return httpio.NewNotFoundMessagef("%s (%s) not found", q.Resource(), q.KeySet().String())
		}

		return errors.Wrap(err, "spxscan.Get()")
	}

	return nil
}

func (q *QuerySet[Resource]) SpannerList(ctx context.Context, txn *spanner.ReadOnlyTransaction, dst any) error {
	stmt, err := q.SpannerStmt()
	if err != nil {
		return errors.Wrap(err, "patcher.Stmt()")
	}

	if err := spxscan.Select(ctx, txn, dst, stmt); err != nil {
		return errors.Wrap(err, "spxscan.Get()")
	}

	return nil
}

func (q *QuerySet[Resource]) SetFilterParam(filterSet *Filter) {
	q.filter = filterSet
}
