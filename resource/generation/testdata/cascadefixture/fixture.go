// Package cascadefixture provides parsed-struct fixtures for the audit pass's cascade
// finding: resources storing files on tables whose rows the database deletes by cascade
// (cascadeFixtureTables in the tests carries the interleave and foreign-key facts), one
// struct per outcome.
package cascadefixture

import "github.com/cccteam/ccc"

// Struct-level annotations only parse from a TypeSpec doc comment, so the structs sit
// in grouped type blocks like real project sources do.
type (
	// Parent is the table the children interleave in; it stores no file.
	//
	// @resource
	Parent struct {
		ID   ccc.UUID `spanner:"Id"`
		Name string   `spanner:"Name"`
	}

	// Interleaved stores a file and is interleaved in Parents ON DELETE CASCADE: one
	// finding naming the parent.
	//
	// @resource
	Interleaved struct {
		ParentID ccc.UUID `spanner:"Id"`
		Sequence int64    `spanner:"Sequence"`
		// @file(photo)
		PhotoKey *string `spanner:"PhotoKey"`
	}

	// Referencing stores a file and carries a foreign key to Parents with ON DELETE
	// CASCADE: one finding naming the column.
	//
	// @resource
	Referencing struct {
		ID       ccc.UUID `spanner:"Id"`
		ParentID ccc.UUID `spanner:"ParentId"`
		// @file
		StoreKey *string `spanner:"StoreKey"`
	}

	// Both stores a file, is interleaved in Parents ON DELETE CASCADE, and carries a
	// cascading foreign key to Parents on another column: two findings, the interleave
	// first, then the column.
	//
	// @resource
	Both struct {
		ParentID ccc.UUID `spanner:"Id"`
		Sequence int64    `spanner:"Sequence"`
		OwnerID  ccc.UUID `spanner:"OwnerId"`
		// @file
		StoreKey *string `spanner:"StoreKey"`
	}

	// Retained stores a file and is interleaved in Parents ON DELETE NO ACTION: silent,
	// since the database refuses the parent's delete while the rows exist.
	//
	// @resource
	Retained struct {
		ParentID ccc.UUID `spanner:"Id"`
		Sequence int64    `spanner:"Sequence"`
		// @file
		StoreKey *string `spanner:"StoreKey"`
	}

	// Plain stores a file and carries a foreign key to Parents with the default delete
	// rule: silent.
	//
	// @resource
	Plain struct {
		ID       ccc.UUID `spanner:"Id"`
		ParentID ccc.UUID `spanner:"ParentId"`
		// @file
		StoreKey *string `spanner:"StoreKey"`
	}

	// Fileless is interleaved in Parents ON DELETE CASCADE and stores no file: silent,
	// since a cascade releases nothing it never held.
	//
	// @resource
	Fileless struct {
		ParentID ccc.UUID `spanner:"Id"`
		Sequence int64    `spanner:"Sequence"`
		Note     *string  `spanner:"Note"`
	}
)
