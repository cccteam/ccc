// Package arrayfixture holds one struct per reading of a query tag on a list column: a
// table and views carrying allow_filter, index, or uniqueindex on an ARRAY column, which
// the extraction refuses, and the shapes it admits beside them (a list with no query
// tag, a filterable byte slice).
package arrayfixture

import "github.com/cccteam/ccc"

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
)
