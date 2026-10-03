// Package crisscross does not type-check, on purpose: it hands a key minted for one
// store to the setter of a column typed for another, in the shape of a generated patch
// setter and a file the upload frame streamed. The typed keys make that a compile
// error, and Test_typedKeys_crissCrossDoesNotTypeCheck pins it.
package crisscross

import "github.com/cccteam/ccc/resource"

// Documents and Photos are two named stores.
type (
	Documents struct{ resource.Store }
	Photos    struct{ resource.Store }
)

// DocumentUpdatePatch is the shape of a generated patch whose key column is typed for
// the Documents store.
type DocumentUpdatePatch struct {
	key resource.Key[Documents]
}

// SetStoreKey is the generated setter: it takes the Documents store's key.
func (p *DocumentUpdatePatch) SetStoreKey(v resource.Key[Documents]) *DocumentUpdatePatch {
	p.key = v

	return p
}

// record hands a photo's key to the documents' column.
func record(p *DocumentUpdatePatch, photo resource.FileIn[Photos]) {
	p.SetStoreKey(photo.Key)
}
