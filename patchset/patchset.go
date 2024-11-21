// package patchset provides types to store json patch set mapping to struct fields.
package patchset

import (
	"maps"
	"slices"

	"github.com/cccteam/ccc/accesstypes"
)

type PatchSet struct {
	data   map[accesstypes.Field]any
	pkey   map[accesstypes.Field]any
	fields []accesstypes.Field
}

func NewPatchSet(data map[accesstypes.Field]any) *PatchSet {
	return &PatchSet{
		data: data,
		pkey: make(map[accesstypes.Field]any),
	}
}

func NewEmptyPatchSet() *PatchSet {
	return &PatchSet{
		data: make(map[accesstypes.Field]any),
		pkey: make(map[accesstypes.Field]any),
	}
}

func (p *PatchSet) Set(field accesstypes.Field, value any) {
	p.data[field] = value
}

func (p *PatchSet) Get(field accesstypes.Field) any {
	return p.data[field]
}

func (p *PatchSet) SetKey(field accesstypes.Field, value any) {
	if _, found := p.data[field]; !found {
		p.fields = append(p.fields, field)
	}
	p.pkey[field] = value
}

func (p *PatchSet) Key(field accesstypes.Field) any {
	return p.pkey[field]
}

func (p *PatchSet) StructFields() []accesstypes.Field {
	return slices.Collect(maps.Keys(p.data))
}

func (p *PatchSet) Len() int {
	return len(p.data)
}

func (p *PatchSet) Data() map[accesstypes.Field]any {
	return p.data
}

func (p *PatchSet) PrimaryKey() PrimaryKey {
	pKey := PrimaryKey{}
	for _, field := range p.fields {
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
