package resource

import (
	"github.com/cccteam/ccc/accesstypes"
)

type PatchSet struct {
	data    map[accesstypes.Field]any
	dFields []accesstypes.Field
	keys    map[accesstypes.Field]any
	kFields []accesstypes.Field
}

func NewPatchSet() *PatchSet {
	return &PatchSet{
		data: make(map[accesstypes.Field]any),
		keys: make(map[accesstypes.Field]any),
	}
}

func (p *PatchSet) Set(field accesstypes.Field, value any) *PatchSet {
	if _, found := p.data[field]; !found {
		p.dFields = append(p.dFields, field)
	}
	p.data[field] = value

	return p
}

func (p *PatchSet) Get(field accesstypes.Field) any {
	return p.data[field]
}

func (p *PatchSet) SetKey(field accesstypes.Field, value any) {
	if _, found := p.keys[field]; !found {
		p.kFields = append(p.kFields, field)
	}
	p.keys[field] = value
}

func (p *PatchSet) Key(field accesstypes.Field) any {
	return p.keys[field]
}

func (p *PatchSet) Fields() []accesstypes.Field {
	return p.dFields
}

func (p *PatchSet) Len() int {
	return len(p.data)
}

func (p *PatchSet) Data() map[accesstypes.Field]any {
	return p.data
}

func (p *PatchSet) KeySet() KeySet {
	var keys KeySet
	for _, field := range p.kFields {
		keys = keys.Add(field, p.keys[field])
	}

	return keys
}

func (p *PatchSet) HasKey() bool {
	return len(p.keys) > 0
}
