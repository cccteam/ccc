package resource

import "github.com/cccteam/ccc/accesstypes"

type fieldSet struct {
	data   map[accesstypes.Field]any
	fields []accesstypes.Field
}

func newFieldSet() *fieldSet {
	return &fieldSet{
		data: make(map[accesstypes.Field]any),
	}
}

func (p *fieldSet) Set(field accesstypes.Field, value any) {
	if _, found := p.data[field]; !found {
		p.fields = append(p.fields, field)
	}
	p.data[field] = value
}

func (p *fieldSet) Get(field accesstypes.Field) any {
	return p.data[field]
}

func (p *fieldSet) KeySet() KeySet {
	var keys KeySet
	for _, field := range p.fields {
		keys = keys.Add(field, p.data[field])
	}

	return keys
}

func (p *fieldSet) IsSet(field accesstypes.Field) bool {
	_, found := p.data[field]

	return found
}
