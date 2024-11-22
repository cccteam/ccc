// package patchset provides types to store json patch set mapping to struct fields.
package patchset

import (
	"github.com/cccteam/ccc/accesstypes"
)

type PatchSet struct {
	data    map[accesstypes.Field]any
	dFields []accesstypes.Field
	pkey    map[accesstypes.Field]any
	kFields []accesstypes.Field
}

func New() *PatchSet {
	return &PatchSet{
		data: make(map[accesstypes.Field]any),
		pkey: make(map[accesstypes.Field]any),
	}
}

func (p *PatchSet) Set(field accesstypes.Field, value any) {
	if _, found := p.data[field]; !found {
		p.dFields = append(p.dFields, field)
	}
	p.data[field] = value
}

func (p *PatchSet) Get(field accesstypes.Field) any {
	return p.data[field]
}

func (p *PatchSet) SetKey(field accesstypes.Field, value any) {
	if _, found := p.pkey[field]; !found {
		p.kFields = append(p.kFields, field)
	}
	p.pkey[field] = value
}

func (p *PatchSet) Key(field accesstypes.Field) any {
	return p.pkey[field]
}

func (p *PatchSet) StructFields() []accesstypes.Field {
	return p.dFields
}

func (p *PatchSet) Len() int {
	return len(p.data)
}

func (p *PatchSet) PrimaryKey() PrimaryKey {
	pKey := PrimaryKey{}
	for _, field := range p.kFields {
		pKey.keyParts = append(pKey.keyParts, KeyPart{
			Key:   field,
			Value: p.pkey[field],
		})
	}

	return pKey
}

func (p *PatchSet) HasKey() bool {
	return len(p.pkey) > 0
}
