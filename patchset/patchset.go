// package patchset provides types to store json patch set mapping to struct fields.
package patchset

import (
	"maps"
	"slices"

	"github.com/cccteam/ccc/accesstypes"
)

type PatchSet struct {
	data map[accesstypes.Field]any
}

func NewPatchSet(data map[accesstypes.Field]any) *PatchSet {
	return &PatchSet{
		data: data,
	}
}

func (p *PatchSet) Set(field accesstypes.Field, value any) {
	p.data[field] = value
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
