package resource

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"cloud.google.com/go/spanner"
	"github.com/cccteam/ccc/accesstypes"
	"github.com/cccteam/spxscan"
	"github.com/go-playground/errors/v5"
)

type QuerySet[Resource Resourcer] struct {
	keys   *fieldSet
	fields []accesstypes.Field
	rMeta  *ResourceMetadata[Resource]
}

func NewQuerySet[Resource Resourcer](rMeta *ResourceMetadata[Resource]) *QuerySet[Resource] {
	return &QuerySet[Resource]{
		keys:  newFieldSet(),
		rMeta: rMeta,
	}
}

func (p *QuerySet[Resource]) Row() *Resource {
	return new(Resource)
}

func (p *QuerySet[Resource]) Rows() []*Resource {
	return make([]*Resource, 0)
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
	case spannerdbType:
		return Columns(strings.Join(columns, ", ")), nil
	case postgresdbType:
		return Columns(fmt.Sprintf(`"%s"`, strings.Join(columns, `", "`))), nil
	default:
		return "", errors.Newf("unsupported dbType: %s", q.rMeta.dbType)
	}
}

// Where translates the the fields to database struct tags in databaseType when building the where clause
func (q *QuerySet[Resource]) Where() (where Where, params map[string]any, err error) {
	parts := q.KeySet().Parts()
	if len(parts) == 0 {
		return "", nil, nil
	}

	builder := strings.Builder{}
	params = make(map[string]any, len(parts))
	for _, part := range parts {
		c, ok := q.rMeta.fieldMap[part.Key]
		if !ok {
			return "", nil, errors.Newf("field %s not found in struct", part.Key)
		}
		key := c.tag
		switch q.rMeta.dbType {
		case spannerdbType:
			builder.WriteString(fmt.Sprintf(" AND %s = @%s", key, strings.ToLower(key)))
		case postgresdbType:
			builder.WriteString(fmt.Sprintf(` AND "%s" = @%s`, key, strings.ToLower(key)))
		default:
			return "", nil, errors.Newf("unsupported dbType: %s", q.rMeta.dbType)
		}
		params[strings.ToLower(key)] = part.Value
	}

	return Where("WHERE " + builder.String()[5:]), params, nil
}

func (q *QuerySet[Resource]) SpannerStmt() (spanner.Statement, error) {
	if q.rMeta.dbType != spannerdbType {
		return spanner.Statement{}, errors.Newf("can only use SpannerStmt() with dbType %s, got %s", spannerdbType, q.rMeta.dbType)
	}

	columns, err := q.Columns()
	if err != nil {
		return spanner.Statement{}, errors.Wrap(err, "SpannerPatcher.Columns()")
	}

	where, params, err := q.Where()
	if err != nil {
		return spanner.Statement{}, errors.Wrap(err, "patcher.Where()")
	}

	stmt := spanner.NewStatement(fmt.Sprintf(`
			SELECT
				%s
			FROM %s 
			%s`, columns, q.Resource(), where,
	))
	for param, value := range params {
		stmt.Params[param] = value
	}

	return stmt, nil
}

func (q *QuerySet[Resource]) PostgresStmt() (stmt Stmt, params map[string]any, err error) {
	if q.rMeta.dbType != postgresdbType {
		return "", nil, errors.Newf("can only use PostgresStmt() with dbType %s, got %s", postgresdbType, q.rMeta.dbType)
	}

	columns, err := q.Columns()
	if err != nil {
		return "", nil, errors.Wrap(err, "SpannerPatcher.Columns()")
	}

	where, params, err := q.Where()
	if err != nil {
		return "", nil, errors.Wrap(err, "patcher.Where()")
	}

	s := fmt.Sprintf(`
			SELECT
				%s
			FROM %s 
			%s`, columns, q.Resource(), where,
	)

	return Stmt(s), params, nil
}

func (q *QuerySet[Resource]) SpannerRead(ctx context.Context, txn *spanner.ReadOnlyTransaction, dst any) error {
	stmt, err := q.SpannerStmt()
	if err != nil {
		return errors.Wrap(err, "patcher.Stmt()")
	}

	if err := spxscan.Get(ctx, txn, dst, stmt); err != nil {
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
