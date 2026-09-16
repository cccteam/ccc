// Package arrayfixture holds one struct per reading of a query tag on a list column: a
// table and views carrying allow_filter, index, or uniqueindex on an ARRAY column, which
// the extraction refuses, and the shapes it admits beside them (a list with no query
// tag, a filterable byte slice). It holds the same for the field-scope @enumerate: a
// list field declaring a picker on each struct kind, which the extraction refuses, and
// the byte slice it admits, one value on the wire.
package arrayfixture

import (
	"context"

	"github.com/cccteam/ccc"
	"github.com/cccteam/ccc/resource"
)

// Client stands in for the application's RPC client.
type Client struct{}

// Bays is a named slice type: a list, whatever its name.
type Bays []int64

// Digest is a named byte slice: one value, never a list.
type Digest []byte

// Struct-level annotations only parse from a TypeSpec doc comment, so the structs sit in
// a grouped type block like real project sources do.
type (
	// Hold is a table-backed resource declaring its ARRAY column filterable.
	//
	// @resource
	Hold struct {
		ID   ccc.UUID `spanner:"Id"`
		Bays []int64  `spanner:"Bays" allow_filter:"true"`
	}

	// Manifest is a table-backed resource whose lists carry no query tag and whose byte
	// slices carry one: every shape the extraction admits.
	//
	// @resource
	Manifest struct {
		ID    ccc.UUID `spanner:"Id"`
		Bays  []int64  `spanner:"Bays"`
		Named Bays     `spanner:"Named"`
		Seal  []byte   `spanner:"Seal" allow_filter:"true"`
		Hash  *Digest  `spanner:"Hash" allow_filter:"true"`
	}

	// IndexedView is a view declaring an ARRAY column indexed.
	//
	// @virtual
	IndexedView struct {
		ID   ccc.UUID `spanner:"Id" index:"true"` // @primarykey
		Tags []string `spanner:"Tags" index:"true"`
	}

	// UniqueView is a view declaring an ARRAY column unique-indexed.
	//
	// @virtual
	UniqueView struct {
		ID    ccc.UUID `spanner:"Id" index:"true"` // @primarykey
		Codes []string `spanner:"Codes" uniqueindex:"true"`
	}

	// FilteredView is a view declaring a named list type filterable.
	//
	// @virtual
	FilteredView struct {
		ID    ccc.UUID `spanner:"Id" index:"true"` // @primarykey
		Sizes Bays     `spanner:"Sizes" allow_filter:"true"`
	}

	// PointerView is a view declaring a pointer to a list filterable: the one pointer is
	// read through, and the list behind it is still a list.
	//
	// @virtual
	PointerView struct {
		ID    ccc.UUID `spanner:"Id" index:"true"` // @primarykey
		Sizes *[]int64 `spanner:"Sizes" allow_filter:"true"`
	}

	// Crate is a table-backed resource declaring a picker on its ARRAY column: the
	// column stores many keys, and a picker stores one.
	//
	// @resource
	Crate struct {
		ID     ccc.UUID `spanner:"Id"`
		Labels []string `spanner:"Labels"` // @enumerate(Labels)
	}

	// Parcel is a table-backed resource declaring a picker on a byte slice: one value on
	// the wire, so the declaration is recorded for resolution.
	//
	// @resource
	Parcel struct {
		ID   ccc.UUID `spanner:"Id"`
		Seal []byte   `spanner:"Seal"` // @enumerate(Seals)
	}

	// NamedListView is a view declaring a picker on a named list type.
	//
	// @virtual
	NamedListView struct {
		ID    ccc.UUID `spanner:"Id" index:"true"` // @primarykey
		Sizes Bays     `spanner:"Sizes"`           // @enumerate(Sizes)
	}

	// ArrayView is a view declaring a picker on a Go array, a list of fixed length.
	//
	// @virtual
	ArrayView struct {
		ID    ccc.UUID  `spanner:"Id" index:"true"` // @primarykey
		Slots [2]string `spanner:"Slots"`           // @enumerate(Slots)
	}

	// Ledger is a computed resource declaring a picker on a list of keys.
	//
	// @computed
	Ledger struct {
		ID      ccc.UUID   // @primarykey
		HoldIDs []ccc.UUID // @enumerate(Holds)
	}

	// Stow is a method declaring a picker on a list request field.
	//
	// @rpc
	Stow struct {
		HoldIDs []ccc.UUID // @enumerate(Holds)
	}

	// Stamp is a method declaring a picker on a byte slice request field: one value on
	// the wire, so the declaration resolves.
	//
	// @rpc
	Stamp struct {
		Seal []byte // @enumerate(Manifests)
	}
)

func (*Stow) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}

func (*Stamp) Execute(context.Context, resource.ReadWriteTransaction, *Client) error {
	return nil
}
