// package queryset provides types to store columns that a given user has access to view
package queryset

import (
	"slices"

	"github.com/cccteam/ccc/accesstypes"
)

type QuerySet struct {
	fields []accesstypes.Field
}

func New() *QuerySet {
	return &QuerySet{}
}

func (p *QuerySet) AddField(field accesstypes.Field) *QuerySet {
	if !slices.Contains(p.fields, field) {
		p.fields = append(p.fields, field)
	}

	return p
}

func (p *QuerySet) Fields() []accesstypes.Field {
	return p.fields
}

func (p *QuerySet) Len() int {
	return len(p.fields)
}
