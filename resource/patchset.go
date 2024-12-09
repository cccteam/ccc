package resource

import (
	"github.com/cccteam/ccc/accesstypes"
)

type PatchSet struct {
	querySet *QuerySet
	data     *fieldSet
}

func NewPatchSet(row Row) *PatchSet {
	return &PatchSet{
		querySet: NewQuerySet(row),
		data:     newFieldSet(),
	}
}

func (p *PatchSet) Set(field accesstypes.Field, value any) *PatchSet {
	p.data.Set(field, value)

	return p
}

func (p *PatchSet) Get(field accesstypes.Field) any {
	return p.data.Get(field)
}

func (p *PatchSet) SetKey(field accesstypes.Field, value any) {
	p.querySet.SetKey(field, value)
}

func (p *PatchSet) Key(field accesstypes.Field) any {
	return p.querySet.Key(field)
}

func (p *PatchSet) Fields() []accesstypes.Field {
	return p.data.fields
}

func (p *PatchSet) Len() int {
	return len(p.data.data)
}

func (p *PatchSet) Data() map[accesstypes.Field]any {
	return p.data.data
}

func (p *PatchSet) PrimaryKey() KeySet {
	return p.querySet.KeySet()
}

func (p *PatchSet) HasKey() bool {
	return len(p.querySet.Fields()) > 0
}

func (p *PatchSet) QuerySet() *QuerySet {
	return p.querySet
}

func (p *PatchSet) Row() any {
	return p.querySet.Row()
}

func (p *PatchSet) Resource() accesstypes.Resource {
	return p.querySet.Resource()
}
