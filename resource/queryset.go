package resource

import (
	"slices"

	"github.com/cccteam/ccc/accesstypes"
)

type Row interface {
	New() any
	Resource() accesstypes.Resource
}

type row[T Resourcer] struct{}

func NewRow[T Resourcer]() Row {
	return &row[T]{}
}

func (r *row[T]) New() any {
	return new(T)
}

func (r *row[T]) Resource() accesstypes.Resource {
	var res T

	return res.Resource()
}

type QuerySetCarrier[Resource Resourcer] struct {
}

type QuerySet struct {
	keys   *fieldSet
	fields []accesstypes.Field
	row    Row
}

func NewQuerySet(row Row) *QuerySet {
	return &QuerySet{
		keys: newFieldSet(),
		row:  row,
	}
}

func (q *QuerySet) Resource() accesstypes.Resource {
	return q.row.Resource()
}

func (q *QuerySet) Row() any {
	return q.row.New()
}

func (q *QuerySet) AddField(field accesstypes.Field) *QuerySet {
	if !slices.Contains(q.fields, field) {
		q.fields = append(q.fields, field)
	}

	return q
}

func (q *QuerySet) Fields() []accesstypes.Field {
	return q.fields
}

func (q *QuerySet) SetKey(field accesstypes.Field, value any) {
	q.keys.Set(field, value)
}

func (q *QuerySet) Key(field accesstypes.Field) any {
	return q.keys.Get(field)
}

func (q *QuerySet) Len() int {
	return len(q.fields)
}

func (q *QuerySet) KeySet() KeySet {
	return q.keys.KeySet()
}
